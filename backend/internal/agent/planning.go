package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/provider"
)

type subtaskPlan struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	AgentRole   string `json:"agent_role"`
}

func (o *Orchestrator) generateTaskSubtasks(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	defaultRole AgentType,
	previousResults []string,
	working *memoryPromptContext,
) ([]subtaskPlan, error) {
	promptCtx := PromptContext{
		FlowTitle:        flow.Title,
		FlowDescription:  flow.Description,
		FlowTarget:       flow.Target,
		TaskTitle:        task.Title,
		TaskDescription:  task.Description,
		DefaultAgentRole: string(defaultRole),
		PreviousResults:  previousResults,
	}
	if working != nil {
		promptCtx.WorkingMemory = working.Working
		promptCtx.EpisodicMemory = working.Episodic
	}

	resp, err := runTaskTextAgent(
		ctx,
		o.db,
		llm,
		flow.ID,
		&task.ID,
		nil,
		AgentGenerator,
		chainTypeTaskGeneration,
		promptCtx,
		"Generate the ordered execution backlog for this task as JSON.",
	)
	if err != nil {
		fallback := fallbackSubtaskPlans(task, defaultRole)
		persistAgentLog(ctx, o.db, flow.ID, &task.ID, nil, AgentGenerator, "fallback", "Generator failed, using fallback subtask plan.", map[string]interface{}{
			"error": err.Error(),
			"count": len(fallback),
		})
		return fallback, nil
	}

	plans, parseErr := parseSubtaskPlans(resp.Content)
	if parseErr != nil || len(plans) == 0 {
		fallback := fallbackSubtaskPlans(task, defaultRole)
		persistAgentLog(ctx, o.db, flow.ID, &task.ID, nil, AgentGenerator, "fallback", "Generator returned invalid JSON, using fallback subtask plan.", map[string]interface{}{
			"error": fmt.Sprintf("%v", parseErr),
			"count": len(fallback),
		})
		return fallback, nil
	}

	return normalizeSubtaskPlans(plans, task, defaultRole), nil
}

func (o *Orchestrator) refineTaskSubtasks(
	ctx context.Context,
	llm provider.LLM,
	flow *database.Flow,
	task *database.Task,
	completed []database.Subtask,
	pending []database.Subtask,
	latest *taskOutcome,
) ([]subtaskPlan, bool, error) {
	if len(pending) == 0 {
		return nil, false, nil
	}

	promptCtx := PromptContext{
		FlowTitle:           flow.Title,
		FlowDescription:     flow.Description,
		FlowTarget:          flow.Target,
		TaskTitle:           task.Title,
		TaskDescription:     task.Description,
		CompletedSubtasks:   formatCompletedSubtasks(completed),
		PendingSubtasks:     formatPendingSubtasks(pending),
		LatestSubtaskResult: formatLatestOutcome(latest),
	}

	resp, err := runTaskTextAgent(
		ctx,
		o.db,
		llm,
		flow.ID,
		&task.ID,
		nil,
		AgentRefiner,
		chainTypeTaskRefinement,
		promptCtx,
		"Refine the remaining subtask backlog as JSON. Return only the remaining subtasks.",
	)
	if err != nil {
		log.WithError(err).Warn("orchestrator: refiner failed, keeping existing pending subtasks")
		return nil, false, nil
	}

	plans, parseErr := parseSubtaskPlans(resp.Content)
	if parseErr != nil {
		log.WithError(parseErr).Warn("orchestrator: refiner returned invalid JSON, keeping existing pending subtasks")
		return nil, false, nil
	}

	fallbackRole := AgentResearcher
	if len(pending) > 0 {
		fallbackRole = normalizeAgentRole(pending[0].AgentRole, AgentResearcher)
	}
	return normalizeSubtaskPlans(plans, task, fallbackRole), true, nil
}

func parseSubtaskPlans(raw string) ([]subtaskPlan, error) {
	var plans []subtaskPlan
	if err := json.Unmarshal([]byte(raw), &plans); err == nil {
		return plans, nil
	}

	start := findChar(raw, '[')
	end := findCharReverse(raw, ']')
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(raw[start:end+1]), &plans); err == nil {
			return plans, nil
		}
	}

	return nil, fmt.Errorf("invalid subtask plan JSON")
}

func normalizeSubtaskPlans(plans []subtaskPlan, task *database.Task, fallbackRole AgentType) []subtaskPlan {
	out := make([]subtaskPlan, 0, len(plans))
	for idx, plan := range plans {
		title := strings.TrimSpace(plan.Title)
		if title == "" {
			title = fmt.Sprintf("%s Step %d", task.Title, idx+1)
		}

		description := strings.TrimSpace(plan.Description)
		if description == "" {
			description = strings.TrimSpace(task.Description)
		}
		if description == "" {
			description = "Execute this subtask and produce a clear result with evidence."
		}

		role := string(normalizeAgentRole(plan.AgentRole, fallbackRole))
		out = append(out, subtaskPlan{
			Title:       title,
			Description: description,
			AgentRole:   role,
		})
	}

	if len(out) == 0 {
		return fallbackSubtaskPlans(task, fallbackRole)
	}

	return out
}

func normalizeAgentRole(raw string, fallback AgentType) AgentType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(AgentPrimary):
		return AgentPrimary
	case string(AgentAssistant):
		return AgentAssistant
	case string(AgentResearcher):
		return AgentResearcher
	case string(AgentSearcher):
		return AgentSearcher
	case string(AgentEnricher):
		return AgentEnricher
	case string(AgentInstaller):
		return AgentInstaller
	case string(AgentPentester):
		return AgentPentester
	case string(AgentCoder):
		return AgentCoder
	default:
		return fallback
	}
}

func fallbackSubtaskPlans(task *database.Task, fallbackRole AgentType) []subtaskPlan {
	if task == nil {
		return []subtaskPlan{{
			Title:       "Execute task",
			Description: "Execute the task and produce a clear result.",
			AgentRole:   string(fallbackRole),
		}}
	}

	description := strings.TrimSpace(task.Description)
	if description == "" {
		description = "Execute this task and produce a clear result with evidence."
	}

	return []subtaskPlan{{
		Title:       task.Title,
		Description: description,
		AgentRole:   string(fallbackRole),
	}}
}

func formatCompletedSubtasks(subtasks []database.Subtask) string {
	if len(subtasks) == 0 {
		return ""
	}

	var b strings.Builder
	for idx, subtask := range subtasks {
		fmt.Fprintf(&b, "%d. %s [%s]\n", idx+1, subtask.Title, subtask.AgentRole)
		if subtask.Result != nil && strings.TrimSpace(*subtask.Result) != "" {
			fmt.Fprintf(&b, "Result:\n%s\n\n", truncate(*subtask.Result, 2500))
		}
	}
	return strings.TrimSpace(b.String())
}

func formatPendingSubtasks(subtasks []database.Subtask) string {
	if len(subtasks) == 0 {
		return ""
	}

	var b strings.Builder
	for idx, subtask := range subtasks {
		fmt.Fprintf(&b, "%d. %s [%s]\n%s\n\n", idx+1, subtask.Title, subtask.AgentRole, subtask.Description)
	}
	return strings.TrimSpace(b.String())
}

func formatLatestOutcome(outcome *taskOutcome) string {
	if outcome == nil {
		return ""
	}
	return truncate(strings.TrimSpace(outcome.Content), 3000)
}

type memoryPromptContext struct {
	Working  string
	Episodic string
}
