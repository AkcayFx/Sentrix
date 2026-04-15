package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/sentrix/internal/provider"
)

const (
	maxAdviserRecoveries   = 2
	maxReflectorRecoveries = 1
)

func runRecoveryAgent(
	ctx context.Context,
	agent *Agent,
	base PromptContext,
	role AgentType,
	chainType string,
	reason string,
	lastAgentResponse string,
	latestResult string,
) (string, error) {
	if agent == nil {
		return "", fmt.Errorf("agent is required")
	}

	recoveryCtx := base
	recoveryCtx.RecoveryReason = strings.TrimSpace(reason)
	recoveryCtx.LastAgentResponse = truncate(strings.TrimSpace(lastAgentResponse), 3000)
	recoveryCtx.ToolUsageSummary = formatToolUsageSummary(agent.Monitor)
	if strings.TrimSpace(latestResult) != "" {
		recoveryCtx.LatestSubtaskResult = truncate(strings.TrimSpace(latestResult), 3000)
	}

	var userPrompt string
	switch role {
	case AgentAdviser:
		userPrompt = "Provide short recovery guidance for the running agent."
	case AgentReflector:
		userPrompt = "Reflect on whether the running agent should continue with tools or finalize."
	default:
		userPrompt = "Review the current agent execution state."
	}

	resp, err := runTaskTextAgent(
		ctx,
		agent.DB,
		agent.LLM,
		agent.FlowID,
		optionalUUID(agent.TaskID),
		optionalUUID(agent.SubtaskID),
		role,
		chainType,
		recoveryCtx,
		userPrompt,
	)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(resp.Content), nil
}

func formatToolUsageSummary(monitor *ExecutionMonitor) string {
	if monitor == nil {
		return ""
	}

	totalCalls, lastTool, consecutive := monitor.Stats()
	if totalCalls == 0 {
		return "No tools have been executed yet."
	}

	if lastTool == "" {
		return fmt.Sprintf("Total tool calls so far: %d.", totalCalls)
	}

	return fmt.Sprintf(
		"Total tool calls: %d. Latest tool: %s. Consecutive calls to the latest tool: %d.",
		totalCalls,
		lastTool,
		consecutive,
	)
}

func shouldReflectOnResponse(
	agent *Agent,
	iteration int,
	availableTools []provider.ToolDef,
	totalToolCalls int,
	resp *provider.Response,
) bool {
	if agent == nil || resp == nil {
		return false
	}
	if len(resp.ToolCalls) > 0 || len(availableTools) == 0 || totalToolCalls > 0 || iteration > 0 {
		return false
	}

	switch agent.Type {
	case AgentResearcher, AgentPentester, AgentCoder:
		return true
	default:
		return false
	}
}
