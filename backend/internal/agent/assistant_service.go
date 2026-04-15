package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/config"
	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/memory"
	"github.com/yourorg/sentrix/internal/observability"
	"github.com/yourorg/sentrix/internal/provider"
	"github.com/yourorg/sentrix/internal/sandbox"
	"github.com/yourorg/sentrix/internal/tools"
)

type AssistantService struct {
	db       *gorm.DB
	registry *provider.Registry
	appCfg   *config.Config
	sandbox  sandbox.Client
	memStore *memory.MemoryStore
	tracer   trace.Tracer
}

func NewAssistantService(
	db *gorm.DB,
	registry *provider.Registry,
	appCfg *config.Config,
	sb sandbox.Client,
	memStore *memory.MemoryStore,
	tracer trace.Tracer,
) *AssistantService {
	return &AssistantService{
		db:       db,
		registry: registry,
		appCfg:   appCfg,
		sandbox:  sb,
		memStore: memStore,
		tracer:   tracer,
	}
}

func (s *AssistantService) GetSession(
	ctx context.Context,
	flow *database.Flow,
) (*database.Assistant, []database.AssistantLog, error) {
	assistant, err := s.ensureSession(ctx, flow)
	if err != nil {
		return nil, nil, err
	}

	logs, err := s.loadLogs(ctx, assistant.ID)
	if err != nil {
		return nil, nil, err
	}

	return assistant, logs, nil
}

func (s *AssistantService) UpdateSession(
	ctx context.Context,
	assistant *database.Assistant,
	useAgents bool,
) (*database.Assistant, error) {
	if assistant == nil {
		return nil, fmt.Errorf("assistant session is required")
	}

	updates := map[string]interface{}{
		"use_agents": useAgents,
	}
	if assistant.Status == "" {
		updates["status"] = "idle"
	}

	if err := s.db.WithContext(ctx).Model(assistant).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("update assistant session: %w", err)
	}

	var refreshed database.Assistant
	if err := s.db.WithContext(ctx).First(&refreshed, "id = ?", assistant.ID).Error; err != nil {
		return nil, fmt.Errorf("reload assistant session: %w", err)
	}

	return &refreshed, nil
}

func (s *AssistantService) SendMessage(
	ctx context.Context,
	flow *database.Flow,
	content string,
	useAgents *bool,
) (*database.Assistant, []database.AssistantLog, error) {
	if flow == nil {
		return nil, nil, fmt.Errorf("flow is required")
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil, fmt.Errorf("assistant message is required")
	}

	assistant, err := s.ensureSession(ctx, flow)
	if err != nil {
		return nil, nil, err
	}

	if useAgents != nil && assistant.UseAgents != *useAgents {
		assistant, err = s.UpdateSession(ctx, assistant, *useAgents)
		if err != nil {
			return nil, nil, err
		}
	}

	if err := s.appendLog(ctx, assistant.ID, provider.RoleUser, string(AgentAssistant), content, map[string]interface{}{
		"flow_id": flow.ID.String(),
	}); err != nil {
		return nil, nil, err
	}
	persistMessageChain(ctx, s.db, flow.ID, nil, nil, AgentAssistant, chainTypeAssistantSession, provider.RoleUser, content, 0, map[string]interface{}{
		"phase": "turn_input",
	})

	if err := s.db.WithContext(ctx).Model(assistant).Updates(map[string]interface{}{"status": "running"}).Error; err != nil {
		return nil, nil, fmt.Errorf("set assistant status: %w", err)
	}
	assistant.Status = "running"

	llm, err := resolveLLMForFlow(ctx, s.db, flow)
	if err != nil {
		_ = s.db.WithContext(ctx).Model(assistant).Updates(map[string]interface{}{"status": "failed"}).Error
		return nil, nil, fmt.Errorf("resolve assistant provider: %w", err)
	}
	llm = observability.WrapLLM(llm, s.tracer)

	resp, runErr := s.runConversation(ctx, llm, flow, assistant)
	if runErr != nil {
		_ = s.db.WithContext(ctx).Model(assistant).Updates(map[string]interface{}{"status": "failed"}).Error
		persistAgentLog(ctx, s.db, flow.ID, nil, nil, AgentAssistant, "error", runErr.Error(), map[string]interface{}{
			"chain_type": chainTypeAssistantSession,
		})
		return nil, nil, runErr
	}

	if err := s.appendLog(ctx, assistant.ID, provider.RoleAssistant, string(AgentAssistant), resp.Content, map[string]interface{}{
		"tokens_in":     resp.TokensIn,
		"tokens_out":    resp.TokensOut,
		"finish_reason": resp.FinishReason,
	}); err != nil {
		return nil, nil, err
	}
	persistMessageChain(ctx, s.db, flow.ID, nil, nil, AgentAssistant, chainTypeAssistantSession, provider.RoleAssistant, resp.Content, resp.TokensOut, map[string]interface{}{
		"finish_reason": resp.FinishReason,
	})
	persistAgentLog(ctx, s.db, flow.ID, nil, nil, AgentAssistant, "final", resp.Content, map[string]interface{}{
		"chain_type":    chainTypeAssistantSession,
		"tokens_in":     resp.TokensIn,
		"tokens_out":    resp.TokensOut,
		"finish_reason": resp.FinishReason,
	})

	if err := s.db.WithContext(ctx).Model(assistant).Updates(map[string]interface{}{"status": "idle"}).Error; err != nil {
		return nil, nil, fmt.Errorf("reset assistant status: %w", err)
	}
	assistant.Status = "idle"

	logs, err := s.loadLogs(ctx, assistant.ID)
	if err != nil {
		return nil, nil, err
	}

	var refreshed database.Assistant
	if err := s.db.WithContext(ctx).First(&refreshed, "id = ?", assistant.ID).Error; err != nil {
		return nil, nil, fmt.Errorf("reload assistant session: %w", err)
	}

	return &refreshed, logs, nil
}

func (s *AssistantService) runConversation(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	assistant *database.Assistant,
) (*provider.Response, error) {
	ctx, span := observability.StartAgentRunSpan(ctx, s.tracer, flow.ID, uuid.Nil, string(AgentAssistant))
	defer span.End()

	toolRegistry := s.newAssistantToolRegistry(flow)
	if assistant.UseAgents {
		toolRegistry.EnableDelegation(s.makeDelegationFunc(llm, flow, toolRegistry))
	}

	promptCtx := PromptContext{
		FlowTitle:       flow.Title,
		FlowDescription: flow.Description,
		FlowTarget:      flow.Target,
		TaskTitle:       "Assistant Session",
		TaskDescription: flow.Description,
		AvailableTools:  toolNames(toolRegistry),
	}

	systemPrompt, err := RenderPrompt(AgentAssistant, promptCtx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("render assistant prompt: %w", err)
	}
	persistMessageChain(ctx, s.db, flow.ID, nil, nil, AgentAssistant, chainTypeAssistantSession, provider.RoleSystem, systemPrompt, 0, map[string]interface{}{
		"phase":      "prompt",
		"use_agents": assistant.UseAgents,
	})

	history, err := s.loadLogs(ctx, assistant.ID)
	if err != nil {
		return nil, fmt.Errorf("load assistant history: %w", err)
	}

	messages := []provider.Message{{Role: provider.RoleSystem, Content: systemPrompt}}
	for _, entry := range history {
		switch entry.Role {
		case provider.RoleUser, provider.RoleAssistant:
			messages = append(messages, provider.Message{
				Role:    entry.Role,
				Content: entry.Content,
			})
		}
	}

	totalTokens := 0
	for iteration := 0; iteration < 8; iteration++ {
		resp, err := llm.Complete(ctx, messages, toolRegistry.Available(), &provider.CompletionParams{
			Temperature: float64Ptr(0.2),
			MaxTokens:   intPtr(4096),
		})
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("assistant completion (iteration %d): %w", iteration, err)
		}

		totalTokens += resp.TokensIn + resp.TokensOut
		if strings.TrimSpace(resp.Content) != "" {
			persistAgentLog(ctx, s.db, flow.ID, nil, nil, AgentAssistant, "response", resp.Content, map[string]interface{}{
				"chain_type":    chainTypeAssistantSession,
				"iteration":     iteration,
				"tool_calls":    len(resp.ToolCalls),
				"tokens_in":     resp.TokensIn,
				"tokens_out":    resp.TokensOut,
				"finish_reason": resp.FinishReason,
			})
		}

		if len(resp.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Content) == "" {
				resp.Content = "No response generated."
			}
			span.SetStatus(codes.Ok, "")
			return resp, nil
		}

		messages = append(messages, provider.Message{
			Role:      provider.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			persistAgentLog(ctx, s.db, flow.ID, nil, nil, AgentAssistant, "tool_call", tc.Name, map[string]interface{}{
				"chain_type":   chainTypeAssistantSession,
				"iteration":    iteration,
				"tool":         tc.Name,
				"args":         truncate(tc.Args, 1200),
				"tool_call_id": tc.ID,
			})

			toolCtx, toolSpan := observability.StartToolCallSpan(ctx, s.tracer, flow.ID, uuid.Nil, string(AgentAssistant), tc.Name)
			result, execErr := toolRegistry.Execute(toolCtx, tc.Name, tc.Args)
			if execErr != nil {
				toolSpan.RecordError(execErr)
				toolSpan.SetStatus(codes.Error, execErr.Error())
				result = fmt.Sprintf("Error executing tool %s: %v", tc.Name, execErr)
			}
			toolSpan.End()

			if err := s.appendLog(ctx, assistant.ID, provider.RoleTool, string(AgentAssistant), result, map[string]interface{}{
				"tool":         tc.Name,
				"args":         truncate(tc.Args, 1200),
				"tool_call_id": tc.ID,
			}); err != nil {
				return nil, err
			}
			persistMessageChain(ctx, s.db, flow.ID, nil, nil, AgentAssistant, chainTypeAssistantSession, provider.RoleTool, result, 0, map[string]interface{}{
				"tool":         tc.Name,
				"tool_call_id": tc.ID,
			})

			messages = append(messages, provider.Message{
				Role:       provider.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	log.WithField("flow_id", flow.ID).Warn("assistant: maximum iteration budget reached")
	return &provider.Response{
		Content:      "Assistant stopped after reaching the current tool-iteration limit. Please refine the request and continue.",
		TokensOut:    totalTokens,
		FinishReason: "length",
	}, nil
}

func (s *AssistantService) makeDelegationFunc(
	llm provider.LLM,
	flow *database.Flow,
	parentTools *tools.ToolRegistry,
) tools.DelegateFunc {
	return func(ctx context.Context, role string, objective string, supportingContext string) (string, error) {
		agentRole := normalizeAgentRole(role, AgentSearcher)
		task := &database.Task{
			ID:          uuid.Nil,
			FlowID:      flow.ID,
			Title:       "Assistant Session",
			Description: flow.Description,
		}
		subtask := &database.Subtask{
			ID:          uuid.Nil,
			TaskID:      uuid.Nil,
			Title:       strings.TrimSpace(objective),
			Description: strings.TrimSpace(objective),
			AgentRole:   string(agentRole),
		}
		if subtask.Title == "" {
			subtask.Title = fmt.Sprintf("%s request", strings.Title(strings.ReplaceAll(string(agentRole), "_", " ")))
		}

		switch agentRole {
		case AgentAdviser, AgentEnricher:
			return s.runDelegatedTextAgent(ctx, llm, flow, task, subtask, agentRole, objective, supportingContext)
		case AgentSearcher, AgentResearcher, AgentPentester, AgentCoder, AgentInstaller:
			return s.runDelegatedSpecialist(ctx, llm, flow, task, subtask, parentTools, agentRole, objective, supportingContext)
		default:
			return "", fmt.Errorf("unsupported delegated role %q", role)
		}
	}
}

func (s *AssistantService) runDelegatedTextAgent(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	subtask *database.Subtask,
	agentRole AgentType,
	objective string,
	supportingContext string,
) (string, error) {
	promptCtx := delegatedPromptContext(flow, task, subtask, objective, supportingContext, nil, nil)
	if agentRole == AgentEnricher {
		promptCtx.ExecutionSummary = supportingContext
		promptCtx.LastAgentResponse = supportingContext
	}

	userPrompt := "Provide concise guidance."
	if agentRole == AgentEnricher {
		userPrompt = "Rewrite the supplied material into a clearer, more actionable summary."
	}

	resp, err := runTaskTextAgent(
		ctx,
		s.db,
		llm,
		flow.ID,
		nil,
		nil,
		agentRole,
		chainTypeAssistantDelegate,
		promptCtx,
		userPrompt,
	)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(resp.Content), nil
}

func (s *AssistantService) runDelegatedSpecialist(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	subtask *database.Subtask,
	parentTools *tools.ToolRegistry,
	agentRole AgentType,
	objective string,
	supportingContext string,
) (string, error) {
	childTools := s.newAssistantToolRegistry(flow)
	if parentTools != nil {
		childTools.SetContainerID(parentTools.ContainerID())
	}

	promptCtx := delegatedPromptContext(flow, task, subtask, objective, supportingContext, nil, nil)
	promptCtx.AvailableTools = toolNames(childTools)

	agent := &Agent{
		Type:      agentRole,
		FlowID:    flow.ID,
		TaskID:    uuid.Nil,
		SubtaskID: uuid.Nil,
		LLM:       llm,
		Tools:     childTools,
		Monitor:   NewExecutionMonitor(s.appCfg.Agent.SameToolLimit, s.appCfg.Agent.TotalToolLimit),
		Tracer:    s.tracer,
		DB:        s.db,
	}

	result, err := RunAgent(ctx, agent, promptCtx, func(Event) {})
	if err != nil {
		return "", err
	}

	if parentTools != nil {
		parentTools.MergeFindings(childTools.GetFindings())
	}

	return strings.TrimSpace(result.Content), nil
}

func (s *AssistantService) ensureSession(ctx context.Context, flow *database.Flow) (*database.Assistant, error) {
	if flow == nil {
		return nil, fmt.Errorf("flow is required")
	}

	tx := s.db.WithContext(ctx)
	var assistant database.Assistant
	err := tx.Where("flow_id = ?", flow.ID).First(&assistant).Error
	switch {
	case err == nil:
	case errors.Is(err, gorm.ErrRecordNotFound):
		assistant = database.Assistant{
			FlowID:    flow.ID,
			UserID:    flow.UserID,
			Title:     strings.TrimSpace(flow.Title),
			Status:    "idle",
			UseAgents: true,
		}
		if assistant.Title == "" {
			assistant.Title = "Flow Assistant"
		}
		if err := tx.Create(&assistant).Error; err != nil {
			return nil, fmt.Errorf("create assistant session: %w", err)
		}
	default:
		return nil, fmt.Errorf("load assistant session: %w", err)
	}

	updates := map[string]interface{}{}
	if assistant.UserID != flow.UserID {
		updates["user_id"] = flow.UserID
	}
	title := strings.TrimSpace(flow.Title)
	if title == "" {
		title = "Flow Assistant"
	}
	if strings.TrimSpace(assistant.Title) != title {
		updates["title"] = title
	}
	if strings.TrimSpace(assistant.Status) == "" {
		updates["status"] = "idle"
	}
	if len(updates) > 0 {
		if err := tx.Model(&assistant).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("refresh assistant session: %w", err)
		}
		if err := tx.First(&assistant, "id = ?", assistant.ID).Error; err != nil {
			return nil, fmt.Errorf("reload assistant session: %w", err)
		}
	}

	return &assistant, nil
}

func (s *AssistantService) loadLogs(ctx context.Context, assistantID uuid.UUID) ([]database.AssistantLog, error) {
	var logs []database.AssistantLog
	if err := s.db.WithContext(ctx).
		Where("assistant_id = ?", assistantID).
		Order("created_at ASC, id ASC").
		Find(&logs).
		Error; err != nil {
		return nil, fmt.Errorf("load assistant logs: %w", err)
	}
	return logs, nil
}

func (s *AssistantService) appendLog(
	ctx context.Context,
	assistantID uuid.UUID,
	role string,
	agentRole string,
	content string,
	metadata map[string]interface{},
) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	record := database.AssistantLog{
		AssistantID: assistantID,
		Role:        role,
		AgentRole:   agentRole,
		Content:     truncate(content, 16000),
		Metadata:    marshalMetadata(metadata),
	}

	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return fmt.Errorf("create assistant log: %w", err)
	}
	return nil
}

func (s *AssistantService) newAssistantToolRegistry(flow *database.Flow) *tools.ToolRegistry {
	flowID := flow.ID
	return tools.NewToolRegistry(
		s.appCfg,
		s.sandbox,
		s.memStore,
		flow.UserID,
		&flowID,
		nil,
		nil,
		s.db,
	)
}

func toolNames(registry *tools.ToolRegistry) []string {
	if registry == nil {
		return nil
	}

	defs := registry.Available()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
}
