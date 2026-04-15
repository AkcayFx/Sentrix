package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/memory"
	"github.com/yourorg/sentrix/internal/provider"
	"github.com/yourorg/sentrix/internal/tools"
)

func (o *Orchestrator) makeDelegationFunc(
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	subtask *database.Subtask,
	parentTools *tools.ToolRegistry,
	previousResults []string,
	working *memory.WorkingMemory,
) tools.DelegateFunc {
	return func(ctx context.Context, role string, objective string, supportingContext string) (string, error) {
		agentRole := normalizeAgentRole(role, AgentSearcher)
		switch agentRole {
		case AgentAdviser, AgentEnricher:
			return o.runDelegatedTextAgent(ctx, llm, flow, task, subtask, agentRole, objective, supportingContext, previousResults, working)
		case AgentSearcher, AgentResearcher, AgentPentester, AgentCoder, AgentInstaller:
			return o.runDelegatedSpecialist(ctx, llm, flow, task, subtask, parentTools, agentRole, objective, supportingContext, previousResults, working)
		default:
			return "", fmt.Errorf("unsupported delegated role %q", role)
		}
	}
}

func (o *Orchestrator) runDelegatedTextAgent(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	subtask *database.Subtask,
	agentRole AgentType,
	objective string,
	supportingContext string,
	previousResults []string,
	working *memory.WorkingMemory,
) (string, error) {
	promptCtx := delegatedPromptContext(flow, task, subtask, objective, supportingContext, previousResults, working)
	if agentRole == AgentEnricher {
		promptCtx.ExecutionSummary = supportingContext
		promptCtx.LastAgentResponse = supportingContext
	}

	chainType := chainTypeRecoveryAdvice
	userPrompt := "Provide concise recovery guidance."
	if agentRole == AgentEnricher {
		chainType = "delegated_enrichment"
		userPrompt = "Rewrite the supplied material into a clearer, more actionable summary."
	}

	resp, err := runTaskTextAgent(
		ctx,
		o.db,
		llm,
		flow.ID,
		optionalUUID(task.ID),
		optionalUUID(subtask.ID),
		agentRole,
		chainType,
		promptCtx,
		userPrompt,
	)
	if err != nil {
		return "", err
	}

	if err := o.persistDelegatedAction(subtask.ID, agentRole, objective, resp.Content, 0); err != nil {
		return "", err
	}

	return strings.TrimSpace(resp.Content), nil
}

func (o *Orchestrator) runDelegatedSpecialist(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	subtask *database.Subtask,
	parentTools *tools.ToolRegistry,
	agentRole AgentType,
	objective string,
	supportingContext string,
	previousResults []string,
	working *memory.WorkingMemory,
) (string, error) {
	flowID := flow.ID
	childTools := tools.NewToolRegistry(
		o.appCfg,
		o.sandbox,
		o.memStore,
		flow.UserID,
		&flowID,
		optionalUUID(task.ID),
		optionalUUID(subtask.ID),
		o.db,
	)
	if parentTools != nil {
		childTools.SetContainerID(parentTools.ContainerID())
	}

	toolNames := make([]string, 0)
	for _, t := range childTools.Available() {
		toolNames = append(toolNames, t.Name)
	}

	promptCtx := delegatedPromptContext(flow, task, subtask, objective, supportingContext, previousResults, working)
	promptCtx.AvailableTools = toolNames

	agent := &Agent{
		Type:      agentRole,
		FlowID:    flow.ID,
		TaskID:    task.ID,
		SubtaskID: subtask.ID,
		LLM:       llm,
		Tools:     childTools,
		Monitor:   NewExecutionMonitor(o.cfg.SameToolLimit, o.cfg.TotalToolLimit),
		Tracer:    o.tracer,
		DB:        o.db,
	}

	result, err := RunAgent(ctx, agent, promptCtx, o.broadcaster.Publish)
	if err != nil {
		return "", err
	}

	if parentTools != nil {
		parentTools.MergeFindings(childTools.GetFindings())
	}
	if err := o.persistDelegatedAction(subtask.ID, agentRole, objective, result.Content, result.Duration); err != nil {
		return "", err
	}

	return strings.TrimSpace(result.Content), nil
}

func delegatedPromptContext(
	flow *database.Flow,
	task *database.Task,
	subtask *database.Subtask,
	objective string,
	supportingContext string,
	previousResults []string,
	working *memory.WorkingMemory,
) PromptContext {
	subtaskDescription := strings.TrimSpace(objective)
	if strings.TrimSpace(supportingContext) != "" {
		subtaskDescription = strings.TrimSpace(subtaskDescription + "\n\nSupporting context:\n" + supportingContext)
	}
	if subtaskDescription == "" && subtask != nil {
		subtaskDescription = subtask.Description
	}

	promptCtx := PromptContext{
		FlowTitle:          flow.Title,
		FlowDescription:    flow.Description,
		FlowTarget:         flow.Target,
		TaskTitle:          task.Title,
		TaskDescription:    task.Description,
		SubtaskTitle:       subtask.Title,
		SubtaskDescription: subtaskDescription,
		PreviousResults:    previousResults,
	}
	if working != nil {
		promptCtx.WorkingMemory = working.Summary()
	}
	return promptCtx
}

func (o *Orchestrator) persistDelegatedAction(subtaskID uuid.UUID, agentRole AgentType, objective, output string, duration time.Duration) error {
	if o.db == nil || subtaskID == uuid.Nil {
		return nil
	}

	input := fmt.Sprintf(`{"agent_type":"%s","objective":%q}`, agentRole, strings.TrimSpace(objective))
	action := database.Action{
		SubtaskID:  subtaskID,
		ActionType: "delegated_agent_execution",
		Status:     "done",
		Input:      input,
		Output:     &output,
	}
	if duration > 0 {
		ms := int(duration / time.Millisecond)
		action.DurationMs = &ms
	}
	return o.db.Create(&action).Error
}
