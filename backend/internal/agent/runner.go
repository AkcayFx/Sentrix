package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/codes"

	"github.com/yourorg/sentrix/internal/observability"
	"github.com/yourorg/sentrix/internal/provider"
)

// RunAgent executes the agent loop:
// 1. Build system prompt from template + context
// 2. Send messages to LLM provider (preferring Stream over Complete)
// 3. If response has tool calls → execute tools → append results → loop
// 4. If response is text → return as result
func RunAgent(ctx context.Context, agent *Agent, promptCtx PromptContext, broadcast func(Event)) (*AgentResult, error) {
	start := time.Now()
	ctx, agentSpan := observability.StartAgentRunSpan(ctx, agent.Tracer, agent.FlowID, agent.SubtaskID, string(agent.Type))
	defer agentSpan.End()

	// Render the system prompt for this agent type.
	systemPrompt, err := RenderPrompt(agent.Type, promptCtx)
	if err != nil {
		agentSpan.RecordError(err)
		agentSpan.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("render prompt: %w", err)
	}

	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: systemPrompt},
		{Role: provider.RoleUser, Content: func() string {
			if promptCtx.SubtaskDescription != "" {
				return promptCtx.SubtaskDescription
			}
			return promptCtx.TaskDescription
		}()},
	}
	userPrompt := promptCtx.TaskDescription
	if promptCtx.SubtaskDescription != "" {
		userPrompt = promptCtx.SubtaskDescription
	}
	recordMessageChain(ctx, agent, provider.RoleUser, userPrompt, 0, map[string]interface{}{
		"agent_role": string(agent.Type),
		"phase":      "prompt",
	})

	tools := agent.Tools.Available()
	totalTokens := 0
	adviserRecoveries := 0
	reflectorRecoveries := 0

	for iteration := 0; ; iteration++ {
		// Check execution limits.
		if err := agent.Monitor.Check(); err != nil {
			if adviserRecoveries < maxAdviserRecoveries {
				advice, adviseErr := runRecoveryAgent(
					ctx,
					agent,
					promptCtx,
					AgentAdviser,
					chainTypeRecoveryAdvice,
					err.Error(),
					"",
					"",
				)
			if adviseErr == nil && strings.TrimSpace(advice) != "" {
				recoveryMessage := "Recovery guidance from adviser:\n" + advice + "\n\nRevise your next step and continue carefully."
				messages = append(messages, provider.Message{Role: provider.RoleUser, Content: recoveryMessage})
				adviserRecoveries++
				agent.Monitor.Reset()
				continue
			}
			}
			log.Warnf("agent[%s] monitor limit reached: %v", agent.Type, err)
			recordAgentLog(ctx, agent, "limit_reached", err.Error(), map[string]interface{}{
				"iteration": iteration,
			})
			break
		}

		// Check context cancellation.
		select {
		case <-ctx.Done():
			return &AgentResult{
				Content:    "Execution cancelled",
				TokensUsed: totalTokens,
				Duration:   time.Since(start),
			}, ctx.Err()
		default:
		}

		// Summarize the chain if it has grown past thresholds.
		if shouldSummarizeChain(messages) {
			messages = summarizeChain(ctx, agent.DB, agent.LLM, agent, messages)
		}

		// Call the LLM — prefer streaming when available.
		resp, err := streamOrComplete(ctx, agent, messages, tools, broadcast, iteration)
		if err != nil {
			agentSpan.RecordError(err)
			agentSpan.SetStatus(codes.Error, err.Error())
			recordAgentLog(ctx, agent, "error", err.Error(), map[string]interface{}{
				"iteration": iteration,
			})
			return nil, fmt.Errorf("llm completion (iteration %d): %w", iteration, err)
		}

		totalTokens += resp.TokensIn + resp.TokensOut
		recordAgentLog(ctx, agent, "response", resp.Content, map[string]interface{}{
			"iteration":     iteration,
			"tool_calls":    len(resp.ToolCalls),
			"tokens_in":     resp.TokensIn,
			"tokens_out":    resp.TokensOut,
			"finish_reason": resp.FinishReason,
		})
		recordMessageChain(ctx, agent, provider.RoleAssistant, resp.Content, resp.TokensOut, map[string]interface{}{
			"agent_role":    string(agent.Type),
			"iteration":     iteration,
			"tool_calls":    len(resp.ToolCalls),
			"finish_reason": resp.FinishReason,
		})

		// Broadcast the assistant's response (compatibility event).
		broadcast(Event{
			FlowID: agent.FlowID.String(),
			Type:   EventActionCompleted,
			Data: map[string]interface{}{
				"agent":     string(agent.Type),
				"subtask":   agent.SubtaskID.String(),
				"iteration": iteration,
				"content":   truncate(resp.Content, 500),
				"has_tools": len(resp.ToolCalls) > 0,
			},
		})

		// If no tool calls, the agent is done reasoning.
		if len(resp.ToolCalls) == 0 {
			totalCalls := agent.Monitor.TotalCalls()
			if reflectorRecoveries < maxReflectorRecoveries && shouldReflectOnResponse(agent, iteration, tools, totalCalls, resp) {
				advice, reflectErr := runRecoveryAgent(
					ctx,
					agent,
					promptCtx,
					AgentReflector,
					chainTypeRecoveryReflection,
					"The agent replied before using any tools. Reflect on whether the response is complete or more evidence is required.",
					resp.Content,
					"",
				)
			if reflectErr == nil && strings.TrimSpace(advice) != "" {
				recoveryMessage := "Reflection guidance:\n" + advice + "\n\nIf more evidence is needed, continue with tools. If not, finalize clearly."
				messages = append(messages, provider.Message{Role: provider.RoleUser, Content: recoveryMessage})
				reflectorRecoveries++
				continue
			}
			}

			recordAgentLog(ctx, agent, "final", resp.Content, map[string]interface{}{
				"iteration":   iteration,
				"tokens_used": totalTokens,
			})
			return &AgentResult{
				Content:       resp.Content,
				TokensUsed:    totalTokens,
				ToolCallsUsed: agent.Monitor.TotalCalls(),
				Duration:      time.Since(start),
			}, nil
		}

		// Append assistant message (with tool calls).
		messages = append(messages, provider.Message{
			Role:      provider.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each tool call. All tool results MUST be appended before
		// any non-tool message (user/assistant) to satisfy strict providers
		// like DeepSeek that require every tool_call to have a matching
		// tool result immediately following the assistant message.
		var lastToolResult string
		for _, tc := range resp.ToolCalls {
			agent.Monitor.RecordToolCall(tc.Name)
			recordAgentLog(ctx, agent, "tool_call", tc.Name, map[string]interface{}{
				"iteration":    iteration,
				"tool":         tc.Name,
				"args":         truncate(tc.Args, 1200),
				"tool_call_id": tc.ID,
			})

			broadcast(Event{
				FlowID: agent.FlowID.String(),
				Type:   EventToolExecuted,
				Data: map[string]interface{}{
					"agent":   string(agent.Type),
					"subtask": agent.SubtaskID.String(),
					"tool":    tc.Name,
					"args":    truncate(tc.Args, 300),
				},
			})

			toolCtx, toolSpan := observability.StartToolCallSpan(ctx, agent.Tracer, agent.FlowID, agent.SubtaskID, string(agent.Type), tc.Name)
			result, execErr := agent.Tools.Execute(toolCtx, tc.Name, tc.Args)
			if execErr != nil {
				toolSpan.RecordError(execErr)
				toolSpan.SetStatus(codes.Error, execErr.Error())
				result = fmt.Sprintf("Error executing tool %s: %v", tc.Name, execErr)
			}
			toolSpan.End()
			recordAgentLog(ctx, agent, "tool_result", result, map[string]interface{}{
				"iteration":    iteration,
				"tool":         tc.Name,
				"tool_call_id": tc.ID,
			})
			recordMessageChain(ctx, agent, provider.RoleTool, result, 0, map[string]interface{}{
				"agent_role":   string(agent.Type),
				"iteration":    iteration,
				"tool":         tc.Name,
				"tool_call_id": tc.ID,
			})

			messages = append(messages, provider.Message{
				Role:       provider.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})
			lastToolResult = result
		}

		// Check adviser AFTER all tool results are appended, so the
		// message chain stays valid for strict providers.
		if shouldAdvise, reason := agent.Monitor.ShouldAdvise(); shouldAdvise && adviserRecoveries < maxAdviserRecoveries {
			advice, adviseErr := runRecoveryAgent(
				ctx,
				agent,
				promptCtx,
				AgentAdviser,
				chainTypeRecoveryAdvice,
				reason,
				resp.Content,
				lastToolResult,
			)
			if adviseErr == nil && strings.TrimSpace(advice) != "" {
				recoveryMessage := "Recovery guidance from adviser:\n" + advice + "\n\nAdjust the next step before continuing."
				messages = append(messages, provider.Message{Role: provider.RoleUser, Content: recoveryMessage})
				adviserRecoveries++
				agent.Monitor.Reset()
				continue
			}
		}
	}

	// If we exited via monitor limit, return the last content.
	return &AgentResult{
		Content:       "Agent execution stopped (tool call limit reached).",
		TokensUsed:    totalTokens,
		ToolCallsUsed: agent.Monitor.TotalCalls(),
		Duration:      time.Since(start),
	}, nil
}

// streamOrComplete tries LLM.Stream first, accumulating chunks into a
// provider.Response with the same shape Complete returns. If Stream fails
// or the channel is nil, it falls back to Complete.
func streamOrComplete(
	ctx context.Context,
	agent *Agent,
	messages []provider.Message,
	tools []provider.ToolDef,
	broadcast func(Event),
	iteration int,
) (*provider.Response, error) {
	params := &provider.CompletionParams{
		Temperature: float64Ptr(0.2),
		MaxTokens:   intPtr(4096),
	}

	streamID := uuid.New().String()

	ch, streamErr := agent.LLM.Stream(ctx, messages, tools, params)
	if streamErr != nil || ch == nil {
		// Fallback to non-streaming path.
		return agent.LLM.Complete(ctx, messages, tools, params)
	}

	var (
		contentBuf  strings.Builder
		accumulated []provider.ToolCall
	)

	for chunk := range ch {
		if chunk.Err != nil {
			// If stream errors partway through, return what we have as an error.
			return nil, chunk.Err
		}

		if chunk.Delta != "" {
			contentBuf.WriteString(chunk.Delta)

			broadcast(Event{
				FlowID: agent.FlowID.String(),
				Type:   EventAgentStreamDelta,
				Data: map[string]interface{}{
					"task_id":    agent.TaskID.String(),
					"subtask_id": agent.SubtaskID.String(),
					"agent_role": string(agent.Type),
					"stream_id":  streamID,
					"delta":      chunk.Delta,
				},
			})
		}

		if len(chunk.ToolCalls) > 0 {
			accumulated = mergeToolCallChunks(accumulated, chunk.ToolCalls)
		}
	}

	finalContent := contentBuf.String()

	// Ensure every accumulated tool call has a non-empty ID. Some providers
	// only send the ID on the first streaming chunk, and if it was missed
	// (or the provider omits it entirely) the downstream API will reject
	// tool-result messages with a blank tool_call_id.
	for i := range accumulated {
		if accumulated[i].ID == "" {
			accumulated[i].ID = "toolcall_" + uuid.New().String()
		}
	}

	// Emit stream-done event.
	broadcast(Event{
		FlowID: agent.FlowID.String(),
		Type:   EventAgentStreamDone,
		Data: map[string]interface{}{
			"task_id":    agent.TaskID.String(),
			"subtask_id": agent.SubtaskID.String(),
			"agent_role": string(agent.Type),
			"stream_id":  streamID,
			"content":    truncate(finalContent, 500),
			"has_tools":  len(accumulated) > 0,
		},
	})

	return &provider.Response{
		Content:      finalContent,
		ToolCalls:    accumulated,
		Model:        agent.LLM.ModelName(),
		FinishReason: "stop",
	}, nil
}

// mergeToolCallChunks merges incremental tool-call chunks into a running
// slice. OpenAI-compatible streaming sends the ID only on the first chunk
// for a given tool call, with subsequent argument-delta chunks carrying
// only the index. We match by index first (always present), then fall
// back to ID matching.
func mergeToolCallChunks(existing []provider.ToolCall, incoming []provider.ToolCall) []provider.ToolCall {
	for _, inc := range incoming {
		merged := false

		// Match by index (primary key for streaming tool-call deltas).
		for i := range existing {
			if existing[i].Index == inc.Index {
				existing[i].Args += inc.Args
				if inc.Name != "" {
					existing[i].Name = inc.Name
				}
				if inc.ID != "" {
					existing[i].ID = inc.ID
				}
				merged = true
				break
			}
		}

		// Fall back to ID matching for non-indexed chunks.
		if !merged && inc.ID != "" {
			for i := range existing {
				if existing[i].ID == inc.ID {
					existing[i].Args += inc.Args
					if inc.Name != "" {
						existing[i].Name = inc.Name
					}
					merged = true
					break
				}
			}
		}

		if !merged {
			existing = append(existing, inc)
		}
	}
	return existing
}

func float64Ptr(v float64) *float64 { return &v }
func intPtr(v int) *int             { return &v }

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
