package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/provider"
)

// Chain restore and summarization constants.
const (
	chainSummarySizeThreshold = 24 * 1024 // 24 KB of non-system content
	chainSummaryMsgThreshold  = 12        // non-system message count
	chainSummaryKeepRecent    = 6         // keep the latest N non-system messages verbatim
)

// restoreSubtaskChain loads persisted MessageChain rows for a subtask
// execution chain, starting from the latest summary row (if any) plus
// subsequent raw rows. This avoids replaying the full history on resume.
func restoreSubtaskChain(db *gorm.DB, subtaskID uuid.UUID) []provider.Message {
	if db == nil || subtaskID == uuid.Nil {
		return nil
	}

	var rows []database.MessageChain
	if err := db.Where("subtask_id = ? AND chain_type = ?", subtaskID, chainTypeSubtaskExecution).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		log.Warnf("chain: failed to load chain for subtask %s: %v", subtaskID.String()[:8], err)
		return nil
	}

	if len(rows) == 0 {
		return nil
	}

	// Find the latest summary row.
	latestSummaryIdx := -1
	for i := len(rows) - 1; i >= 0; i-- {
		if isSummaryRow(rows[i]) {
			latestSummaryIdx = i
			break
		}
	}

	// Start from the summary if present, otherwise from the beginning.
	startIdx := 0
	if latestSummaryIdx >= 0 {
		startIdx = latestSummaryIdx
	}

	messages := make([]provider.Message, 0, len(rows)-startIdx)
	for _, row := range rows[startIdx:] {
		messages = append(messages, provider.Message{
			Role:    row.Role,
			Content: row.Content,
		})
	}

	return messages
}

// isSummaryRow checks if a MessageChain row is a persisted summary.
func isSummaryRow(row database.MessageChain) bool {
	if row.Metadata == "" || row.Metadata == "{}" {
		return false
	}
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(row.Metadata), &meta); err != nil {
		return false
	}
	return meta["phase"] == "summary"
}

// shouldSummarizeChain returns true when the non-system portion of the
// message chain exceeds size or count thresholds.
func shouldSummarizeChain(messages []provider.Message) bool {
	nonSystem := nonSystemMessages(messages)
	if len(nonSystem) > chainSummaryMsgThreshold {
		return true
	}
	totalBytes := 0
	for _, m := range nonSystem {
		totalBytes += len(m.Content)
	}
	return totalBytes > chainSummarySizeThreshold
}

// summarizeChain compresses the message chain in-place. It preserves the
// system prompt, keeps the latest chainSummaryKeepRecent non-system
// messages verbatim, and compresses older non-system messages into one
// synthetic assistant summary. The summary row is also persisted to the
// MessageChain table.
func summarizeChain(
	ctx context.Context,
	db *gorm.DB,
	llm provider.LLM,
	agent *Agent,
	messages []provider.Message,
) []provider.Message {
	nonSystem := nonSystemMessages(messages)
	if len(nonSystem) <= chainSummaryKeepRecent {
		return messages // nothing to compress
	}

	// Split: older messages to summarize, recent messages to keep.
	// Adjust the cutoff so we never split inside a tool-call block.
	// A safe boundary is one where recentKeep[0] is NOT a tool message
	// and the message before it is NOT an assistant with pending tool_calls.
	cutoff := len(nonSystem) - chainSummaryKeepRecent
	cutoff = findSafeChainCutoff(nonSystem, cutoff)
	if cutoff <= 0 {
		return messages // can't compress safely
	}
	toCompress := nonSystem[:cutoff]
	recentKeep := nonSystem[cutoff:]

	// Build summarization prompt.
	var compressBuf strings.Builder
	for _, m := range toCompress {
		fmt.Fprintf(&compressBuf, "[%s]: %s\n\n", m.Role, m.Content)
	}

	summaryPrompt, err := RenderPrompt(AgentSummarizer, PromptContext{})
	if err != nil {
		log.Warnf("chain: failed to render summarizer prompt: %v", err)
		return messages // keep original on failure
	}

	resp, err := llm.Complete(ctx, []provider.Message{
		{Role: provider.RoleSystem, Content: summaryPrompt},
		{Role: provider.RoleUser, Content: "Summarize the following conversation history:\n\n" + compressBuf.String()},
	}, nil, &provider.CompletionParams{
		Temperature: float64Ptr(0.1),
		MaxTokens:   intPtr(1024),
	})
	if err != nil {
		log.Warnf("chain: summarization LLM call failed: %v", err)
		return messages // keep original on failure
	}

	summaryContent := strings.TrimSpace(resp.Content)
	if summaryContent == "" {
		return messages
	}

	// Persist the summary row.
	persistSummaryRow(ctx, db, agent, summaryContent)

	// Rebuild the chain: system prompt + summary + recent messages.
	rebuilt := make([]provider.Message, 0, 2+len(recentKeep))

	// Preserve system prompt (first message if system).
	if len(messages) > 0 && messages[0].Role == provider.RoleSystem {
		rebuilt = append(rebuilt, messages[0])
	}

	rebuilt = append(rebuilt, provider.Message{
		Role:    provider.RoleAssistant,
		Content: "[Chain Summary]\n" + summaryContent,
	})
	rebuilt = append(rebuilt, recentKeep...)

	return rebuilt
}

// persistSummaryRow writes a summary row to the MessageChain table.
func persistSummaryRow(ctx context.Context, db *gorm.DB, agent *Agent, summary string) {
	if db == nil || agent == nil {
		return
	}

	meta := map[string]interface{}{
		"phase":      "summary",
		"chain_type": chainTypeSubtaskExecution,
		"agent_role": string(agent.Type),
	}

	persistMessageChain(
		ctx,
		db,
		agent.FlowID,
		optionalUUID(agent.TaskID),
		optionalUUID(agent.SubtaskID),
		agent.Type,
		chainTypeSubtaskExecution,
		provider.RoleAssistant,
		"[Chain Summary]\n"+summary,
		0,
		meta,
	)
}

// findSafeChainCutoff walks the cutoff backward until recentKeep[0] is
// not a tool-role message. This prevents splitting in the middle of an
// assistant(tool_calls) → tool(result) sequence, which causes DeepSeek
// (and other strict providers) to reject the request.
func findSafeChainCutoff(nonSystem []provider.Message, cutoff int) int {
	for cutoff > 0 && cutoff < len(nonSystem) {
		if nonSystem[cutoff].Role != provider.RoleTool {
			break
		}
		// This message is a tool result — its assistant is before the cutoff.
		// Move cutoff back to include the whole tool-call block.
		cutoff--
	}
	// Also pull back if the message right before cutoff is an assistant
	// with tool_calls whose results would be split across the boundary.
	if cutoff > 0 && cutoff < len(nonSystem) && len(nonSystem[cutoff-1].ToolCalls) > 0 {
		cutoff--
	}
	return cutoff
}

// nonSystemMessages filters out system messages from the chain.
func nonSystemMessages(messages []provider.Message) []provider.Message {
	out := make([]provider.Message, 0, len(messages))
	for _, m := range messages {
		if m.Role != provider.RoleSystem {
			out = append(out, m)
		}
	}
	return out
}
