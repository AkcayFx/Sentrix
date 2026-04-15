package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/provider"
)

// ── Tool names ──────────────────────────────────────────────────────

const (
	ToolNameStore  = "memory_store"
	ToolNameSearch = "memory_search"
)

// ── Arg types for JSON deserialization ──────────────────────────────

type storeArgs struct {
	Content  string `json:"content"`
	Category string `json:"category"`
}

type searchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// ── Tool Definitions (JSON schema for LLM function calling) ────────

// StoreToolDef returns the function-calling schema for memory_store.
func StoreToolDef() provider.ToolDef {
	return provider.ToolDef{
		Name:        ToolNameStore,
		Description: "Save an important fact, finding, or observation to long-term memory for future reference. Use this to remember vulnerabilities found, techniques that worked, or key information about the target.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The information to remember. Be specific and include relevant context.",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "Category for the memory entry.",
					"enum":        []string{"observation", "conclusion", "tool_output", "vulnerability", "technique", "general"},
				},
			},
			"required": []string{"content"},
		},
	}
}

// SearchToolDef returns the function-calling schema for memory_search.
func SearchToolDef() provider.ToolDef {
	return provider.ToolDef{
		Name:        ToolNameSearch,
		Description: "Search long-term memory for previously stored facts, findings, and observations. Use this to recall what was learned in earlier tasks or past assessments.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Natural language query describing what you want to recall from memory.",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results to return (default: 5).",
				},
			},
			"required": []string{"query"},
		},
	}
}

// ── Tool Handlers ──────────────────────────────────────────────────

// MemoryToolHandler wraps MemoryStore operations into agent-callable tool handlers.
type MemoryToolHandler struct {
	store  *MemoryStore
	userID uuid.UUID
	flowID *uuid.UUID
	taskID *uuid.UUID
	subtaskID *uuid.UUID
}

// NewMemoryToolHandler creates a handler scoped to a user and optional flow.
func NewMemoryToolHandler(store *MemoryStore, userID uuid.UUID, flowID, taskID, subtaskID *uuid.UUID) *MemoryToolHandler {
	return &MemoryToolHandler{
		store:     store,
		userID:    userID,
		flowID:    flowID,
		taskID:    taskID,
		subtaskID: subtaskID,
	}
}

// HandleStore is the tool handler for memory_store.
func (h *MemoryToolHandler) HandleStore(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args storeArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("memory_store: invalid arguments: %w", err)
	}

	if args.Content == "" {
		return "Error: content cannot be empty", nil
	}

	category := args.Category
	if category == "" {
		category = CategoryGeneral
	}

	record, err := h.store.Store(ctx, MemoryEntry{
		UserID:   h.userID,
		FlowID:   h.flowID,
		Tier:     TierLongTerm,
		Category: category,
		Content:  args.Content,
	})
	if err != nil {
		log.WithError(err).Error("memory_store: failed to persist")
		h.logOperation(ctx, "store", "", args.Content, 0, map[string]interface{}{
			"category": category,
			"error":    err.Error(),
		})
		return fmt.Sprintf("Failed to store memory: %v", err), nil
	}

	h.logOperation(ctx, "store", "", args.Content, 1, map[string]interface{}{
		"category": category,
		"memory_id": record.ID.String(),
	})

	return fmt.Sprintf(
		"✓ Stored to long-term memory (id: %s, category: %s). This information will be available for future searches.",
		record.ID.String()[:8],
		category,
	), nil
}

// HandleSearch is the tool handler for memory_search.
func (h *MemoryToolHandler) HandleSearch(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args searchArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("memory_search: invalid arguments: %w", err)
	}

	if args.Query == "" {
		return "Error: search query cannot be empty", nil
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}

	results, err := h.store.Search(ctx, h.userID, args.Query, SearchOpts{
		FlowID: h.flowID,
		Limit:  limit,
	})
	if err != nil {
		log.WithError(err).Error("memory_search: query failed")
		h.logOperation(ctx, "search", args.Query, "", 0, map[string]interface{}{
			"error": err.Error(),
			"limit": limit,
		})
		return fmt.Sprintf("Memory search failed: %v", err), nil
	}

	h.logOperation(ctx, "search", args.Query, "", len(results), map[string]interface{}{
		"limit": limit,
	})

	if len(results) == 0 {
		return "No relevant memories found for this query. The memory store has no matching entries.", nil
	}

	// Format results for the agent.
	var out string
	for i, r := range results {
		out += fmt.Sprintf(
			"## Memory %d (score: %.0f%%, category: %s)\n%s\n\n---\n\n",
			i+1, r.Score*100, r.Category, r.Content,
		)
	}

	return fmt.Sprintf("Found %d relevant memories:\n\n%s", len(results), out), nil
}

func (h *MemoryToolHandler) logOperation(
	ctx context.Context,
	action, query, content string,
	resultCount int,
	metadata map[string]interface{},
) {
	if h.store == nil || h.store.db == nil || h.flowID == nil {
		return
	}

	metaJSON := "{}"
	if metadata != nil {
		if raw, err := marshalJSON(metadata); err == nil {
			metaJSON = string(raw)
		}
	}

	record := database.VectorStoreLog{
		FlowID:      *h.flowID,
		TaskID:      h.taskID,
		SubtaskID:   h.subtaskID,
		Action:      action,
		Query:       query,
		Content:     truncate(content, 2000),
		ResultCount: resultCount,
		Metadata:    metaJSON,
	}

	if err := h.store.db.WithContext(ctx).Create(&record).Error; err != nil {
		log.WithError(err).Warn("memory: failed to persist vector store log")
	}
}
