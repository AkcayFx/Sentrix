package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
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

// taskPlan is the JSON structure returned by the orchestrator LLM.
type taskPlan struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	AgentRole   string `json:"agent_role"`
}

type taskOutcome struct {
	Content        string
	ReportMarkdown string
	Findings       []tools.ReportFindingArgs
}

type subtaskExecutionResult struct {
	Subtask database.Subtask
	Action  database.Action
	Outcome *taskOutcome
}

// Orchestrator coordinates the multi-agent flow execution.
type Orchestrator struct {
	db          *gorm.DB
	registry    *provider.Registry
	broadcaster *Broadcaster
	appCfg      *config.Config
	cfg         OrchestratorConfig
	sandbox     sandbox.Client
	memStore    *memory.MemoryStore
	tracer      trace.Tracer
}

// OrchestratorConfig holds tuneable settings.
type OrchestratorConfig struct {
	SameToolLimit  int
	TotalToolLimit int
}

// NewOrchestrator creates a new orchestrator instance.
func NewOrchestrator(
	db *gorm.DB,
	reg *provider.Registry,
	broadcast *Broadcaster,
	appCfg *config.Config,
	cfg OrchestratorConfig,
	sb sandbox.Client,
	ms *memory.MemoryStore,
	tracer trace.Tracer,
) *Orchestrator {
	return &Orchestrator{
		db:          db,
		registry:    reg,
		broadcaster: broadcast,
		appCfg:      appCfg,
		cfg:         cfg,
		sandbox:     sb,
		memStore:    ms,
		tracer:      tracer,
	}
}

// ExecuteFlow runs the full orchestration loop for a given flow.
func (o *Orchestrator) ExecuteFlow(ctx context.Context, flowID uuid.UUID) error {
	// Load the flow from DB.
	var flow database.Flow
	if err := o.db.First(&flow, "id = ?", flowID).Error; err != nil {
		return fmt.Errorf("load flow: %w", err)
	}

	ctx, flowSpan := observability.StartFlowSpan(ctx, o.tracer, flow.ID, flow.Title)
	defer flowSpan.End()

	// Update flow status to running.
	o.db.Model(&flow).Update("status", "running")

	o.broadcaster.Publish(Event{
		FlowID: flowID.String(),
		Type:   EventFlowStarted,
		Data: map[string]interface{}{
			"title": flow.Title,
		},
	})

	// Step 1: Use LLM to decompose into tasks.
	llm, err := o.resolveLLMForFlow(ctx, &flow)
	if err != nil {
		o.failFlow(&flow, "no LLM provider available: "+err.Error())
		return err
	}
	llm = observability.WrapLLM(llm, o.tracer)

	tasks, err := o.decomposeIntoTasks(ctx, llm, &flow)
	if err != nil {
		o.failFlow(&flow, "task decomposition failed: "+err.Error())
		return err
	}
	workingMem := memory.NewWorkingMemory()
	goals := make([]string, 0, len(tasks))
	for _, task := range tasks {
		goals = append(goals, task.Title)
	}
	workingMem.SetGoals(goals)

	// Step 2: Create tasks in DB.
	var dbTasks []database.Task
	for i, t := range tasks {
		task := database.Task{
			FlowID:      flowID,
			Title:       t.Title,
			Description: t.Description,
			Status:      "pending",
			SortOrder:   i,
		}
		if err := o.db.Create(&task).Error; err != nil {
			log.Errorf("orchestrator: failed to create task: %v", err)
			continue
		}
		dbTasks = append(dbTasks, task)

		o.broadcaster.Publish(Event{
			FlowID: flowID.String(),
			Type:   EventTaskCreated,
			Data: map[string]interface{}{
				"task_id":    task.ID.String(),
				"title":      task.Title,
				"agent_role": t.AgentRole,
				"sort_order": i,
			},
		})
	}

	// Step 3: Execute each task sequentially.
	var previousResults []string
	var failedTasks []string
	for _, task := range dbTasks {
		select {
		case <-ctx.Done():
			o.stopFlow(&flow)
			return ctx.Err()
		default:
		}

		agentRole := o.getAgentRole(tasks, task.Title)

		result, err := o.executeTask(ctx, llm, &flow, &task, agentRole, previousResults, workingMem)
		if err != nil {
			log.Errorf("orchestrator: task %s failed: %v", task.Title, err)
			o.db.Model(&task).Updates(map[string]interface{}{
				"status": "failed",
				"result": err.Error(),
			})
			workingMem.AddContext("task:"+task.Title, "failed: "+truncate(err.Error(), 800))
			o.recordEpisode(ctx, flow.UserID, flow.ID, task.Title+": "+task.Description, err.Error(), false)
			o.broadcaster.Publish(Event{
				FlowID: flowID.String(),
				Type:   EventTaskCompleted,
				Data: map[string]interface{}{
					"task_id": task.ID.String(),
					"title":   task.Title,
					"status":  "failed",
					"error":   err.Error(),
				},
			})
			failedTasks = append(failedTasks, fmt.Sprintf("%s: %s", task.Title, err.Error()))
			continue
		}

		previousResults = append(previousResults, result.Content)
		o.db.Model(&task).Updates(map[string]interface{}{
			"status": "done",
			"result": result.Content,
		})
		o.rememberTaskOutcome(ctx, &flow, &task, result, workingMem)
		o.recordEpisode(ctx, flow.UserID, flow.ID, task.Title+": "+task.Description, result.Content, true)

		o.broadcaster.Publish(Event{
			FlowID: flowID.String(),
			Type:   EventTaskCompleted,
			Data: map[string]interface{}{
				"task_id": task.ID.String(),
				"title":   task.Title,
				"status":  "done",
			},
		})
	}

	// Step 4: Release sandbox container.
	if o.sandbox != nil {
		if err := o.sandbox.ReleaseContainer(ctx, flowID); err != nil {
			log.Warnf("orchestrator: failed to release sandbox container: %v", err)
		}
	}

	// Step 5: Mark flow as completed or failed based on task outcomes.
	if len(failedTasks) > 0 {
		summary := fmt.Sprintf("%d task(s) failed", len(failedTasks))
		if len(failedTasks) > 0 {
			summary = fmt.Sprintf("%s. First failure: %s", summary, failedTasks[0])
		}
		o.db.Model(&flow).Update("status", "failed")
		o.broadcaster.Publish(Event{
			FlowID: flowID.String(),
			Type:   EventFlowFailed,
			Data: map[string]interface{}{
				"error":      summary,
				"task_count": len(dbTasks),
			},
		})
		return fmt.Errorf("%s", summary)
	}

	o.db.Model(&flow).Update("status", "done")
	o.broadcaster.Publish(Event{
		FlowID: flowID.String(),
		Type:   EventFlowCompleted,
		Data: map[string]interface{}{
			"task_count": len(dbTasks),
		},
	})

	return nil
}

// decomposeIntoTasks uses the orchestrator LLM prompt to plan tasks.
func (o *Orchestrator) decomposeIntoTasks(ctx context.Context, llm provider.LLM, flow *database.Flow) ([]taskPlan, error) {
	promptCtx := PromptContext{
		FlowTitle:       flow.Title,
		FlowDescription: flow.Description,
		FlowTarget:      flow.Target,
	}

	systemPrompt, err := RenderPrompt(AgentOrchestrator, promptCtx)
	if err != nil {
		return nil, fmt.Errorf("render orchestrator prompt: %w", err)
	}

	resp, err := llm.Complete(ctx, []provider.Message{
		{Role: provider.RoleSystem, Content: systemPrompt},
		{Role: provider.RoleUser, Content: "Plan the security assessment tasks for: " + flow.Title + "\n\n" + flow.Description},
	}, nil, &provider.CompletionParams{
		Temperature: float64Ptr(0.3),
		MaxTokens:   intPtr(2048),
	})
	if err != nil {
		return nil, fmt.Errorf("llm completion: %w", err)
	}

	var tasks []taskPlan
	if err := json.Unmarshal([]byte(resp.Content), &tasks); err != nil {
		// Try to extract JSON from the response.
		content := resp.Content
		start := findChar(content, '[')
		end := findCharReverse(content, ']')
		if start >= 0 && end > start {
			if err2 := json.Unmarshal([]byte(content[start:end+1]), &tasks); err2 != nil {
				return nil, fmt.Errorf("parse task plan: %w (raw: %s)", err, truncate(resp.Content, 200))
			}
		} else {
			return nil, fmt.Errorf("parse task plan: %w (raw: %s)", err, truncate(resp.Content, 200))
		}
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("orchestrator produced 0 tasks")
	}

	return tasks, nil
}

// taskExecContext holds per-task state that is created once and reused
// across all subtasks within a single task execution.
type taskExecContext struct {
	registry       *tools.ToolRegistry
	containerID    string
	episodic       *memory.EpisodicMemory
	recallCache    map[string]string // normalized query → formatted episodes
	toolNames      []string          // cached tool definitions list
	taskToolCalls  int               // cumulative tool calls across all subtasks in this task
	taskToolLimit  int               // max total tool calls for the entire task (0 = unlimited)
}

// executeTask runs a task through a generated and refined subtask backlog.
func (o *Orchestrator) executeTask(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	agentRole AgentType,
	previousResults []string,
	working *memory.WorkingMemory,
) (*taskOutcome, error) {
	ctx, taskSpan := observability.StartTaskSpan(ctx, o.tracer, flow.ID, task.ID, task.Title, string(agentRole))
	defer taskSpan.End()

	if err := o.db.Model(task).Update("status", "running").Error; err != nil {
		return nil, fmt.Errorf("mark task running: %w", err)
	}

	taskMemory := o.buildTaskMemoryPromptContext(ctx, flow, task, working)
	plans, err := o.generateTaskSubtasks(ctx, llm, flow, task, agentRole, previousResults, taskMemory)
	if err != nil {
		return nil, fmt.Errorf("generate subtasks: %w", err)
	}

	if err := o.replacePendingSubtasks(task.ID, 0, plans); err != nil {
		return nil, fmt.Errorf("persist generated subtasks: %w", err)
	}

	// Build task-scoped execution context (reused across all subtasks).
	tctx := o.buildTaskExecContext(ctx, flow, task)

	var (
		completedSubtasks []database.Subtask
		completedResults  []string
		allFindings       []tools.ReportFindingArgs
		lastAction        *database.Action
	)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		pending, err := o.loadTaskSubtasksByStatus(task.ID, "pending")
		if err != nil {
			return nil, fmt.Errorf("load pending subtasks: %w", err)
		}
		if len(pending) == 0 {
			break
		}

		current := pending[0]
		subtaskContext := append([]string{}, previousResults...)
		subtaskContext = append(subtaskContext, completedResults...)

		execResult, err := o.executeSubtaskReuse(ctx, llm, flow, task, &current, subtaskContext, working, tctx)
		if err != nil {
			return nil, err
		}

		completedSubtasks = append(completedSubtasks, execResult.Subtask)
		lastAction = &execResult.Action

		// Check task-level tool budget.
		if tctx.taskToolLimit > 0 && tctx.taskToolCalls >= tctx.taskToolLimit {
			log.Warnf("orchestrator: task %s hit task-level tool budget (%d/%d), finishing early",
				task.Title, tctx.taskToolCalls, tctx.taskToolLimit)
			break
		}

		if execResult.Outcome != nil {
			completedResults = append(completedResults, truncate(execResult.Outcome.Content, 1800))
			allFindings = append(allFindings, execResult.Outcome.Findings...)

			if working != nil {
				working.AddContext("subtask:"+execResult.Subtask.Title, truncate(execResult.Outcome.Content, 900))
				for _, finding := range execResult.Outcome.Findings {
					working.AddFinding(fmt.Sprintf("%s: %s", strings.ToUpper(finding.Severity), finding.Title))
				}
			}
		}

		pending, err = o.loadTaskSubtasksByStatus(task.ID, "pending")
		if err != nil {
			return nil, fmt.Errorf("reload pending subtasks: %w", err)
		}
		if len(pending) == 0 {
			continue
		}

		// Checkpoint-gated refinement: only refine when there are at least
		// 2 pending subtasks AND one of the deterministic triggers fires.
		nCompleted := len(completedSubtasks)
		hasFindings := execResult.Outcome != nil && len(execResult.Outcome.Findings) > 0
		shouldRefine := len(pending) >= 2 &&
			(hasFindings || nCompleted == 1 || nCompleted%3 == 0)

		if !shouldRefine {
			continue
		}

		refinedPlans, changed, refineErr := o.refineTaskSubtasks(ctx, llm, flow, task, completedSubtasks, pending, execResult.Outcome)
		if refineErr != nil {
			log.Warnf("orchestrator: failed to refine subtasks for task %s: %v", task.Title, refineErr)
			continue
		}
		if !changed || pendingMatchesPlans(pending, refinedPlans) {
			continue
		}

		if err := o.replacePendingSubtasks(task.ID, len(completedSubtasks), refinedPlans); err != nil {
			return nil, fmt.Errorf("replace pending subtasks: %w", err)
		}
	}

	outcome := &taskOutcome{
		Content:  buildTaskResultContent(task, completedSubtasks),
		Findings: allFindings,
	}
	if strings.TrimSpace(outcome.Content) == "" && len(outcome.Findings) == 0 {
		outcome.Content = "Task completed without a textual summary."
	}

	if lastAction != nil {
		// Build the report deterministically — no LLM reporter call in the
		// hot path.  reportTaskOutcome is still available but no longer
		// invoked during flow execution.
		outcome.ReportMarkdown = strings.TrimSpace(
			buildDeterministicReport(task, completedSubtasks, allFindings),
		)

		reportAction := lastAction
		if outcome.ReportMarkdown != "" {
			action, actionErr := o.createTaskReportAction(lastAction.SubtaskID, outcome.ReportMarkdown)
			if actionErr != nil {
				log.Warnf("orchestrator: failed to persist reporter action for task %s: %v", task.Title, actionErr)
			} else {
				reportAction = action
			}
		}
		o.persistTaskReportArtifact(task, reportAction, outcome)
	}

	return outcome, nil
}

func (o *Orchestrator) executeSubtask(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	subtask *database.Subtask,
	previousResults []string,
	working *memory.WorkingMemory,
) (*subtaskExecutionResult, error) {
	agentRole := normalizeAgentRole(subtask.AgentRole, AgentResearcher)
	if err := o.db.Model(subtask).Update("status", "running").Error; err != nil {
		return nil, fmt.Errorf("mark subtask running: %w", err)
	}

	o.broadcaster.Publish(Event{
		FlowID: flow.ID.String(),
		Type:   EventSubtaskStarted,
		Data: map[string]interface{}{
			"task_id":    task.ID.String(),
			"subtask_id": subtask.ID.String(),
			"agent_role": string(agentRole),
			"title":      subtask.Title,
		},
	})

	flowID := flow.ID
	taskID := task.ID
	subtaskID := subtask.ID
	toolExec := tools.NewToolRegistry(o.appCfg, o.sandbox, o.memStore, flow.UserID, &flowID, &taskID, &subtaskID, o.db)
	if agentRole == AgentPrimary || agentRole == AgentAssistant {
		toolExec.EnableDelegation(o.makeDelegationFunc(llm, flow, task, subtask, toolExec, previousResults, working))
	}

	if o.sandbox != nil {
		containerID, sbErr := o.sandbox.EnsureContainer(ctx, flow.ID)
		if sbErr != nil {
			log.Warnf("orchestrator: sandbox container failed, falling back to host: %v", sbErr)
		} else {
			toolExec.SetContainerID(containerID)
		}
	}

	toolNames := make([]string, 0)
	for _, t := range toolExec.Available() {
		toolNames = append(toolNames, t.Name)
	}

	promptCtx := PromptContext{
		FlowTitle:          flow.Title,
		FlowDescription:    flow.Description,
		FlowTarget:         flow.Target,
		TaskTitle:          task.Title,
		TaskDescription:    task.Description,
		SubtaskTitle:       subtask.Title,
		SubtaskDescription: subtask.Description,
		AvailableTools:     toolNames,
		PreviousResults:    previousResults,
		AgentRole:          string(agentRole),
	}
	if working != nil {
		promptCtx.WorkingMemory = working.Summary()
	}
	if o.memStore != nil && o.memStore.Enabled() {
		episodic := memory.NewEpisodicMemory(o.memStore, flow.UserID, flow.ID)
		recallQuery := strings.TrimSpace(subtask.Title + "\n" + subtask.Description)
		if recallQuery == "" {
			recallQuery = task.Title + "\n" + task.Description
		}
		episodes, recallErr := episodic.RecallSimilar(ctx, recallQuery, 3)
		if recallErr != nil {
			log.Warnf("orchestrator: episodic recall failed: %v", recallErr)
		} else {
			promptCtx.EpisodicMemory = memory.FormatEpisodesForPrompt(episodes)
		}
	}

	agent := &Agent{
		Type:      agentRole,
		FlowID:    flow.ID,
		TaskID:    task.ID,
		SubtaskID: subtask.ID,
		LLM:       llm,
		Tools:     toolExec,
		Monitor:   NewExecutionMonitor(o.cfg.SameToolLimit, o.cfg.TotalToolLimit),
		Tracer:    o.tracer,
		DB:        o.db,
	}

	result, err := RunAgent(ctx, agent, promptCtx, o.broadcaster.Publish)
	if err != nil {
		_ = o.db.Model(subtask).Updates(map[string]interface{}{
			"status": "failed",
			"result": err.Error(),
		}).Error
		return nil, err
	}

	if err := o.db.Model(subtask).Updates(map[string]interface{}{
		"status": "done",
		"result": result.Content,
	}).Error; err != nil {
		return nil, fmt.Errorf("update subtask result: %w", err)
	}

	durationMs := int(result.Duration / time.Millisecond)
	action := database.Action{
		SubtaskID:  subtask.ID,
		ActionType: "agent_execution",
		Status:     "done",
		Input: fmt.Sprintf(
			`{"agent_type":"%s","subtask_title":%q,"tokens_used":%d}`,
			agentRole,
			subtask.Title,
			result.TokensUsed,
		),
		Output:     &result.Content,
		DurationMs: &durationMs,
	}
	if err := o.db.Create(&action).Error; err != nil {
		return nil, fmt.Errorf("create action: %w", err)
	}

	outcome := &taskOutcome{
		Content:  result.Content,
		Findings: toolExec.GetFindings(),
	}
	o.persistFindingArtifacts(task, &action, outcome.Findings)

	o.broadcaster.Publish(Event{
		FlowID: flow.ID.String(),
		Type:   EventSubtaskCompleted,
		Data: map[string]interface{}{
			"task_id":     task.ID.String(),
			"subtask_id":  subtask.ID.String(),
			"agent_role":  string(agentRole),
			"tokens_used": result.TokensUsed,
		},
	})

	completedSubtask := *subtask
	completedSubtask.Status = "done"
	completedSubtask.Result = &result.Content

	return &subtaskExecutionResult{
		Subtask: completedSubtask,
		Action:  action,
		Outcome: outcome,
	}, nil
}

// buildTaskExecContext creates the task-scoped execution context that is
// reused across all subtasks. Registry, sandbox container, episodic memory
// handle, and recall cache are all allocated once here.
func (o *Orchestrator) buildTaskExecContext(ctx context.Context, flow *database.Flow, task *database.Task) *taskExecContext {
	flowID := flow.ID
	taskID := task.ID
	reg := tools.NewToolRegistry(o.appCfg, o.sandbox, o.memStore, flow.UserID, &flowID, &taskID, nil, o.db)

	taskToolLimit := o.appCfg.Agent.TaskToolLimit
	if taskToolLimit <= 0 {
		taskToolLimit = o.appCfg.Agent.TotalToolLimit * 3
	}

	tctx := &taskExecContext{
		registry:      reg,
		recallCache:   make(map[string]string),
		taskToolLimit: taskToolLimit,
	}

	// Resolve sandbox container lazily once per task.
	if o.sandbox != nil {
		containerID, sbErr := o.sandbox.EnsureContainer(ctx, flow.ID)
		if sbErr != nil {
			log.Warnf("orchestrator: sandbox container failed, falling back to host: %v", sbErr)
		} else {
			tctx.containerID = containerID
			reg.SetContainerID(containerID)
		}
	}

	// Cache tool names once.
	for _, t := range reg.Available() {
		tctx.toolNames = append(tctx.toolNames, t.Name)
	}

	// Create episodic memory handle once.
	if o.memStore != nil && o.memStore.Enabled() {
		tctx.episodic = memory.NewEpisodicMemory(o.memStore, flow.UserID, flow.ID)
	}

	return tctx
}

// executeSubtaskReuse runs a single subtask using the shared task-scoped
// execution context instead of creating a fresh registry/sandbox/episodic
// handle each time.
func (o *Orchestrator) executeSubtaskReuse(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	subtask *database.Subtask,
	previousResults []string,
	working *memory.WorkingMemory,
	tctx *taskExecContext,
) (*subtaskExecutionResult, error) {
	agentRole := normalizeAgentRole(subtask.AgentRole, AgentResearcher)
	if err := o.db.Model(subtask).Update("status", "running").Error; err != nil {
		return nil, fmt.Errorf("mark subtask running: %w", err)
	}

	o.broadcaster.Publish(Event{
		FlowID: flow.ID.String(),
		Type:   EventSubtaskStarted,
		Data: map[string]interface{}{
			"task_id":    task.ID.String(),
			"subtask_id": subtask.ID.String(),
			"agent_role": string(agentRole),
			"title":      subtask.Title,
		},
	})

	// Rebind the shared registry to this subtask and clear stale findings.
	tctx.registry.SetExecutionContext(flow.ID, task.ID, subtask.ID)
	tctx.registry.ResetFindings()

	// Enable delegation once on first primary/assistant subtask; the delegate
	// func captures the current subtask via closure but the registry itself
	// is reused.
	if agentRole == AgentPrimary || agentRole == AgentAssistant {
		tctx.registry.EnableDelegation(o.makeDelegationFunc(llm, flow, task, subtask, tctx.registry, previousResults, working))
	}

	promptCtx := PromptContext{
		FlowTitle:          flow.Title,
		FlowDescription:    flow.Description,
		FlowTarget:         flow.Target,
		TaskTitle:          task.Title,
		TaskDescription:    task.Description,
		SubtaskTitle:       subtask.Title,
		SubtaskDescription: subtask.Description,
		AvailableTools:     tctx.toolNames,
		PreviousResults:    previousResults,
		AgentRole:          string(agentRole),
	}
	if working != nil {
		promptCtx.WorkingMemory = working.Summary()
	}

	// Episodic recall with cache-first lookup.
	if tctx.episodic != nil {
		recallQuery := strings.TrimSpace(subtask.Title + "\n" + subtask.Description)
		if recallQuery == "" {
			recallQuery = task.Title + "\n" + task.Description
		}
		if cached, ok := tctx.recallCache[recallQuery]; ok {
			promptCtx.EpisodicMemory = cached
		} else {
			episodes, recallErr := tctx.episodic.RecallSimilar(ctx, recallQuery, 3)
			if recallErr != nil {
				log.Warnf("orchestrator: episodic recall failed: %v", recallErr)
			} else {
				formatted := memory.FormatEpisodesForPrompt(episodes)
				tctx.recallCache[recallQuery] = formatted
				promptCtx.EpisodicMemory = formatted
			}
		}
	}

	agent := &Agent{
		Type:      agentRole,
		FlowID:    flow.ID,
		TaskID:    task.ID,
		SubtaskID: subtask.ID,
		LLM:       llm,
		Tools:     tctx.registry,
		Monitor:   NewExecutionMonitor(o.cfg.SameToolLimit, o.cfg.TotalToolLimit),
		Tracer:    o.tracer,
		DB:        o.db,
	}

	result, err := RunAgent(ctx, agent, promptCtx, o.broadcaster.Publish)
	if err != nil {
		_ = o.db.Model(subtask).Updates(map[string]interface{}{
			"status": "failed",
			"result": err.Error(),
		}).Error
		return nil, err
	}

	// Accumulate tool calls into the task-level budget tracker.
	tctx.taskToolCalls += result.ToolCallsUsed

	if err := o.db.Model(subtask).Updates(map[string]interface{}{
		"status": "done",
		"result": result.Content,
	}).Error; err != nil {
		return nil, fmt.Errorf("update subtask result: %w", err)
	}

	durationMs := int(result.Duration / time.Millisecond)
	action := database.Action{
		SubtaskID:  subtask.ID,
		ActionType: "agent_execution",
		Status:     "done",
		Input: fmt.Sprintf(
			`{"agent_type":"%s","subtask_title":%q,"tokens_used":%d,"tool_calls_used":%d}`,
			agentRole,
			subtask.Title,
			result.TokensUsed,
			result.ToolCallsUsed,
		),
		Output:     &result.Content,
		DurationMs: &durationMs,
	}
	if err := o.db.Create(&action).Error; err != nil {
		return nil, fmt.Errorf("create action: %w", err)
	}

	outcome := &taskOutcome{
		Content:  result.Content,
		Findings: tctx.registry.GetFindings(),
	}
	o.persistFindingArtifacts(task, &action, outcome.Findings)

	o.broadcaster.Publish(Event{
		FlowID: flow.ID.String(),
		Type:   EventSubtaskCompleted,
		Data: map[string]interface{}{
			"task_id":     task.ID.String(),
			"subtask_id":  subtask.ID.String(),
			"agent_role":  string(agentRole),
			"tokens_used": result.TokensUsed,
		},
	})

	completedSubtask := *subtask
	completedSubtask.Status = "done"
	completedSubtask.Result = &result.Content

	return &subtaskExecutionResult{
		Subtask: completedSubtask,
		Action:  action,
		Outcome: outcome,
	}, nil
}

func (o *Orchestrator) buildTaskMemoryPromptContext(
	ctx context.Context,
	flow *database.Flow,
	task *database.Task,
	working *memory.WorkingMemory,
) *memoryPromptContext {
	ctxData := &memoryPromptContext{}
	if working != nil {
		ctxData.Working = working.Summary()
	}

	if o.memStore != nil && o.memStore.Enabled() {
		episodic := memory.NewEpisodicMemory(o.memStore, flow.UserID, flow.ID)
		episodes, err := episodic.RecallSimilar(ctx, task.Title+"\n"+task.Description, 3)
		if err != nil {
			log.Warnf("orchestrator: episodic recall failed: %v", err)
		} else {
			ctxData.Episodic = memory.FormatEpisodesForPrompt(episodes)
		}
	}

	if ctxData.Working == "" && ctxData.Episodic == "" {
		return nil
	}

	return ctxData
}

func (o *Orchestrator) loadTaskSubtasksByStatus(taskID uuid.UUID, statuses ...string) ([]database.Subtask, error) {
	var subtasks []database.Subtask
	query := o.db.Where("task_id = ?", taskID).Order("sort_order ASC, created_at ASC")
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}
	if err := query.Find(&subtasks).Error; err != nil {
		return nil, err
	}
	return subtasks, nil
}

func (o *Orchestrator) replacePendingSubtasks(taskID uuid.UUID, startOrder int, plans []subtaskPlan) error {
	return o.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ? AND status = ?", taskID, "pending").Delete(&database.Subtask{}).Error; err != nil {
			return err
		}

		if len(plans) == 0 {
			return nil
		}

		subtasks := make([]database.Subtask, 0, len(plans))
		for idx, plan := range plans {
			subtasks = append(subtasks, database.Subtask{
				TaskID:      taskID,
				Title:       plan.Title,
				Description: plan.Description,
				AgentRole:   plan.AgentRole,
				SortOrder:   startOrder + idx,
				Status:      "pending",
			})
		}

		return tx.Create(&subtasks).Error
	})
}

func pendingMatchesPlans(existing []database.Subtask, plans []subtaskPlan) bool {
	if len(existing) != len(plans) {
		return false
	}

	for idx := range existing {
		if strings.TrimSpace(existing[idx].Title) != strings.TrimSpace(plans[idx].Title) {
			return false
		}
		if strings.TrimSpace(existing[idx].Description) != strings.TrimSpace(plans[idx].Description) {
			return false
		}
		if strings.TrimSpace(existing[idx].AgentRole) != strings.TrimSpace(plans[idx].AgentRole) {
			return false
		}
	}

	return true
}

func buildTaskResultContent(task *database.Task, completed []database.Subtask) string {
	if len(completed) == 0 {
		return ""
	}

	if len(completed) == 1 && completed[0].Result != nil {
		return strings.TrimSpace(*completed[0].Result)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Task Summary: %s\n\n", task.Title)
	for idx, subtask := range completed {
		fmt.Fprintf(&b, "## %d. %s\n\n", idx+1, subtask.Title)
		if subtask.Description != "" {
			fmt.Fprintf(&b, "Objective: %s\n\n", subtask.Description)
		}
		if subtask.Result != nil && strings.TrimSpace(*subtask.Result) != "" {
			fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(*subtask.Result))
		} else {
			b.WriteString("No textual result captured.\n\n")
		}
	}

	return strings.TrimSpace(b.String())
}

// buildDeterministicReport produces a markdown task report without an LLM
// call, using only the completed subtask results and findings list.
func buildDeterministicReport(task *database.Task, completed []database.Subtask, findings []tools.ReportFindingArgs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Report: %s\n\n", task.Title)

	if task.Description != "" {
		fmt.Fprintf(&b, "**Objective:** %s\n\n", task.Description)
	}

	fmt.Fprintf(&b, "## Execution Summary\n\n")
	for idx, subtask := range completed {
		fmt.Fprintf(&b, "### %d. %s\n\n", idx+1, subtask.Title)
		if subtask.Result != nil && strings.TrimSpace(*subtask.Result) != "" {
			fmt.Fprintf(&b, "%s\n\n", truncate(strings.TrimSpace(*subtask.Result), 3000))
		} else {
			b.WriteString("No textual result captured.\n\n")
		}
	}

	if len(findings) > 0 {
		fmt.Fprintf(&b, "## Findings (%d)\n\n", len(findings))
		for idx, f := range findings {
			fmt.Fprintf(&b, "%d. **[%s] %s**\n", idx+1, strings.ToUpper(f.Severity), f.Title)
			if f.Description != "" {
				fmt.Fprintf(&b, "   %s\n", f.Description)
			}
			if f.Remediation != "" {
				fmt.Fprintf(&b, "   *Remediation:* %s\n", f.Remediation)
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (o *Orchestrator) reportTaskOutcome(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	completed []database.Subtask,
	findings []tools.ReportFindingArgs,
	working *memory.WorkingMemory,
) (string, error) {
	promptCtx := PromptContext{
		FlowTitle:         flow.Title,
		FlowDescription:   flow.Description,
		FlowTarget:        flow.Target,
		TaskTitle:         task.Title,
		TaskDescription:   task.Description,
		CompletedSubtasks: formatCompletedSubtasks(completed),
		FindingsSummary:   formatFindingsForPrompt(findings),
		ExecutionSummary:  truncate(buildTaskResultContent(task, completed), 12000),
	}
	if working != nil {
		promptCtx.WorkingMemory = working.Summary()
	}

	resp, err := runTaskTextAgent(
		ctx,
		o.db,
		llm,
		flow.ID,
		&task.ID,
		nil,
		AgentReporter,
		chainTypeTaskReporting,
		promptCtx,
		"Write the final markdown report for this completed task.",
	)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(resp.Content), nil
}

func (o *Orchestrator) createTaskReportAction(subtaskID uuid.UUID, report string) (*database.Action, error) {
	if o.db == nil || subtaskID == uuid.Nil {
		return nil, fmt.Errorf("task report action requires a valid subtask id")
	}

	input := `{"agent_type":"reporter","source":"task_reporting"}`
	action := &database.Action{
		SubtaskID:  subtaskID,
		ActionType: "task_reporting",
		Status:     "done",
		Input:      input,
		Output:     &report,
	}

	if err := o.db.Create(action).Error; err != nil {
		return nil, err
	}

	return action, nil
}

func (o *Orchestrator) failFlow(flow *database.Flow, reason string) {
	o.releaseFlowSandbox(flow.ID)
	o.db.Model(flow).Update("status", "failed")
	o.broadcaster.Publish(Event{
		FlowID: flow.ID.String(),
		Type:   EventFlowFailed,
		Data: map[string]interface{}{
			"error": reason,
		},
	})
}

func (o *Orchestrator) stopFlow(flow *database.Flow) {
	o.releaseFlowSandbox(flow.ID)
	o.db.Model(flow).Update("status", "stopped")
	o.broadcaster.Publish(Event{
		FlowID: flow.ID.String(),
		Type:   EventFlowStopped,
		Data:   nil,
	})
}

func (o *Orchestrator) releaseFlowSandbox(flowID uuid.UUID) {
	if o.sandbox != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := o.sandbox.ReleaseContainer(ctx, flowID); err != nil {
			log.Warnf("orchestrator: failed to release sandbox for flow %s: %v", flowID.String()[:8], err)
		}
	}
}

func (o *Orchestrator) getAgentRole(plans []taskPlan, title string) AgentType {
	for _, p := range plans {
		if p.Title == title {
			return normalizeAgentRole(p.AgentRole, AgentPrimary)
		}
	}
	return AgentPrimary // default fallback
}

func (o *Orchestrator) rememberTaskOutcome(
	ctx context.Context,
	flow *database.Flow,
	task *database.Task,
	outcome *taskOutcome,
	working *memory.WorkingMemory,
) {
	if outcome == nil {
		return
	}

	if working != nil {
		working.AddContext("task:"+task.Title, truncate(outcome.Content, 1200))
		for _, finding := range outcome.Findings {
			working.AddFinding(fmt.Sprintf("%s: %s", strings.ToUpper(finding.Severity), finding.Title))
		}
	}

	if o.memStore == nil {
		return
	}

	for _, finding := range outcome.Findings {
		content := strings.TrimSpace(finding.Title + "\n\n" + finding.Description)
		if finding.Evidence != "" {
			content += "\n\nEvidence:\n" + finding.Evidence
		}
		if finding.Remediation != "" {
			content += "\n\nRemediation:\n" + finding.Remediation
		}

		if _, err := o.memStore.Store(ctx, memory.MemoryEntry{
			UserID:   flow.UserID,
			FlowID:   &flow.ID,
			Tier:     memory.TierLongTerm,
			Category: memory.CategoryVulnerability,
			Content:  truncate(content, 4000),
			Metadata: map[string]interface{}{
				"severity":   finding.Severity,
				"title":      finding.Title,
				"task_id":    task.ID.String(),
				"task_title": task.Title,
				"source":     "report_finding",
			},
		}); err != nil {
			log.Warnf("orchestrator: failed to store finding memory: %v", err)
		}
	}

	summary := strings.TrimSpace(buildTaskSummary(task, outcome))
	if summary == "" {
		return
	}

	if _, err := o.memStore.Store(ctx, memory.MemoryEntry{
		UserID:   flow.UserID,
		FlowID:   &flow.ID,
		Tier:     memory.TierLongTerm,
		Category: memory.CategoryConclusion,
		Content:  summary,
		Metadata: map[string]interface{}{
			"task_id":       task.ID.String(),
			"task_title":    task.Title,
			"finding_count": len(outcome.Findings),
			"source":        "task_summary",
		},
	}); err != nil {
		log.Warnf("orchestrator: failed to store task summary memory: %v", err)
	}
}

func (o *Orchestrator) persistFindingArtifacts(
	task *database.Task,
	action *database.Action,
	findings []tools.ReportFindingArgs,
) {
	if o.db == nil || task == nil || action == nil || len(findings) == 0 {
		return
	}

	for _, finding := range findings {
		o.createArtifact(action.ID, "finding", truncate(buildFindingArtifactContent(finding), 12000), map[string]interface{}{
			"severity":    finding.Severity,
			"title":       finding.Title,
			"description": finding.Description,
			"evidence":    finding.Evidence,
			"remediation": finding.Remediation,
			"message":     finding.Message,
			"task_id":     task.ID.String(),
			"task_title":  task.Title,
			"source":      "report_finding",
		})

		o.broadcaster.Publish(Event{
			FlowID: task.FlowID.String(),
			Type:   EventFindingCreated,
			Data: map[string]string{
				"severity":    finding.Severity,
				"title":       finding.Title,
				"description": finding.Description,
				"evidence":    finding.Evidence,
				"task_title":  task.Title,
			},
		})
	}
}

func (o *Orchestrator) persistTaskReportArtifact(
	task *database.Task,
	action *database.Action,
	outcome *taskOutcome,
) {
	if o.db == nil || task == nil || action == nil || outcome == nil {
		return
	}

	report := strings.TrimSpace(outcome.ReportMarkdown)
	source := "reporter"
	if report == "" {
		report = strings.TrimSpace(buildTaskReportMarkdown(task, outcome))
		source = "task_summary"
	}
	if report == "" {
		return
	}

	o.createArtifact(action.ID, "task_report_markdown", truncate(report, 12000), map[string]interface{}{
		"task_id":       task.ID.String(),
		"task_title":    task.Title,
		"finding_count": len(outcome.Findings),
		"source":        source,
	})
}

func (o *Orchestrator) createArtifact(actionID uuid.UUID, kind string, content string, metadata map[string]interface{}) {
	if o.db == nil || actionID == uuid.Nil || strings.TrimSpace(kind) == "" {
		return
	}

	rawMeta, err := json.Marshal(metadata)
	if err != nil {
		rawMeta = []byte(`{}`)
	}

	body := strings.TrimSpace(content)
	record := database.Artifact{
		ActionID: actionID,
		Kind:     kind,
		Metadata: string(rawMeta),
	}
	if body != "" {
		record.Content = &body
	}

	if err := o.db.Create(&record).Error; err != nil {
		log.Warnf("orchestrator: failed to persist artifact %s: %v", kind, err)
	}
}

func (o *Orchestrator) recordEpisode(
	ctx context.Context,
	userID uuid.UUID,
	flowID uuid.UUID,
	action string,
	outcome string,
	success bool,
) {
	if o.memStore == nil {
		return
	}

	episodic := memory.NewEpisodicMemory(o.memStore, userID, flowID)
	if err := episodic.RecordEpisode(ctx, action, outcome, success); err != nil {
		log.Warnf("orchestrator: failed to record episode: %v", err)
	}
}

func buildTaskSummary(task *database.Task, outcome *taskOutcome) string {
	if outcome == nil {
		return ""
	}

	result := strings.TrimSpace(outcome.Content)
	if result == "" && len(outcome.Findings) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n", task.Title)
	if task.Description != "" {
		fmt.Fprintf(&b, "Objective: %s\n\n", task.Description)
	}
	if result != "" {
		fmt.Fprintf(&b, "Outcome:\n%s\n", truncate(result, 1800))
	}
	if len(outcome.Findings) > 0 {
		b.WriteString("\nValidated findings:\n")
		for _, finding := range outcome.Findings {
			fmt.Fprintf(&b, "- [%s] %s\n", strings.ToUpper(finding.Severity), finding.Title)
		}
	}

	return strings.TrimSpace(b.String())
}

func buildTaskReportMarkdown(task *database.Task, outcome *taskOutcome) string {
	if task == nil || outcome == nil {
		return ""
	}

	if strings.TrimSpace(outcome.ReportMarkdown) != "" {
		return strings.TrimSpace(outcome.ReportMarkdown)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Task Report: %s\n\n", task.Title)
	if task.Description != "" {
		fmt.Fprintf(&b, "## Objective\n%s\n\n", task.Description)
	}

	result := strings.TrimSpace(outcome.Content)
	if result != "" {
		fmt.Fprintf(&b, "## Outcome\n%s\n\n", result)
	}

	if len(outcome.Findings) > 0 {
		b.WriteString("## Findings\n")
		for _, finding := range outcome.Findings {
			fmt.Fprintf(&b, "\n### [%s] %s\n\n", strings.ToUpper(finding.Severity), finding.Title)
			if finding.Description != "" {
				fmt.Fprintf(&b, "%s\n\n", finding.Description)
			}
			if finding.Evidence != "" {
				fmt.Fprintf(&b, "Evidence:\n%s\n\n", finding.Evidence)
			}
			if finding.Remediation != "" {
				fmt.Fprintf(&b, "Remediation:\n%s\n\n", finding.Remediation)
			}
		}
	}

	return strings.TrimSpace(b.String())
}

func formatFindingsForPrompt(findings []tools.ReportFindingArgs) string {
	if len(findings) == 0 {
		return ""
	}

	var b strings.Builder
	for idx, finding := range findings {
		fmt.Fprintf(&b, "%d. [%s] %s\n", idx+1, strings.ToUpper(finding.Severity), finding.Title)
		if finding.Description != "" {
			fmt.Fprintf(&b, "Description: %s\n", finding.Description)
		}
		if finding.Evidence != "" {
			fmt.Fprintf(&b, "Evidence: %s\n", truncate(finding.Evidence, 1200))
		}
		if finding.Remediation != "" {
			fmt.Fprintf(&b, "Remediation: %s\n", finding.Remediation)
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func buildFindingArtifactContent(finding tools.ReportFindingArgs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# [%s] %s\n\n", strings.ToUpper(finding.Severity), finding.Title)
	if finding.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", finding.Description)
	}
	if finding.Evidence != "" {
		fmt.Fprintf(&b, "## Evidence\n%s\n\n", finding.Evidence)
	}
	if finding.Remediation != "" {
		fmt.Fprintf(&b, "## Remediation\n%s\n\n", finding.Remediation)
	}
	if finding.Message != "" {
		fmt.Fprintf(&b, "## Analyst Note\n%s\n", finding.Message)
	}

	return strings.TrimSpace(b.String())
}

func findChar(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func findCharReverse(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
