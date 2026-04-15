package agent

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/provider"
)

const (
	chainTypeSubtaskExecution   = "subtask_execution"
	chainTypeTaskGeneration     = "task_generation"
	chainTypeTaskRefinement     = "task_refinement"
	chainTypeTaskReporting      = "task_reporting"
	chainTypeRecoveryAdvice     = "recovery_advice"
	chainTypeRecoveryReflection = "recovery_reflection"
	chainTypeAssistantSession   = "assistant_session"
	chainTypeAssistantDelegate  = "assistant_delegation"
)

func runTaskTextAgent(
	ctx context.Context,
	db *gorm.DB,
	llm provider.LLM,
	flowID uuid.UUID,
	taskID *uuid.UUID,
	subtaskID *uuid.UUID,
	agentRole AgentType,
	chainType string,
	promptCtx PromptContext,
	userPrompt string,
) (*provider.Response, error) {
	systemPrompt, err := RenderPrompt(agentRole, promptCtx)
	if err != nil {
		return nil, fmt.Errorf("render %s prompt: %w", agentRole, err)
	}

	persistMessageChain(ctx, db, flowID, taskID, subtaskID, agentRole, chainType, provider.RoleSystem, systemPrompt, 0, map[string]interface{}{
		"phase": "prompt",
	})
	persistMessageChain(ctx, db, flowID, taskID, subtaskID, agentRole, chainType, provider.RoleUser, userPrompt, 0, map[string]interface{}{
		"phase": "prompt",
	})

	resp, err := llm.Complete(ctx, []provider.Message{
		{Role: provider.RoleSystem, Content: systemPrompt},
		{Role: provider.RoleUser, Content: userPrompt},
	}, nil, &provider.CompletionParams{
		Temperature: float64Ptr(0.2),
		MaxTokens:   intPtr(4096),
	})
	if err != nil {
		persistAgentLog(ctx, db, flowID, taskID, subtaskID, agentRole, "error", err.Error(), map[string]interface{}{
			"chain_type": chainType,
		})
		return nil, fmt.Errorf("llm completion for %s: %w", agentRole, err)
	}

	persistAgentLog(ctx, db, flowID, taskID, subtaskID, agentRole, "response", resp.Content, map[string]interface{}{
		"chain_type":    chainType,
		"tokens_in":     resp.TokensIn,
		"tokens_out":    resp.TokensOut,
		"finish_reason": resp.FinishReason,
	})
	persistMessageChain(ctx, db, flowID, taskID, subtaskID, agentRole, chainType, provider.RoleAssistant, resp.Content, resp.TokensOut, map[string]interface{}{
		"chain_type":    chainType,
		"finish_reason": resp.FinishReason,
	})
	persistAgentLog(ctx, db, flowID, taskID, subtaskID, agentRole, "final", resp.Content, map[string]interface{}{
		"chain_type": chainType,
	})

	return resp, nil
}
