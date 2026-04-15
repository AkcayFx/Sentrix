package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/sentrix/internal/memory"
)

// MemoryHandler serves the memory REST endpoints.
type MemoryHandler struct {
	store *memory.MemoryStore
}

// NewMemoryHandler creates a handler backed by the given MemoryStore.
func NewMemoryHandler(store *memory.MemoryStore) *MemoryHandler {
	return &MemoryHandler{store: store}
}

// List returns paginated memories for the authenticated user.
// GET /api/v1/memories?offset=0&limit=20
func (h *MemoryHandler) List(c *gin.Context) {
	userID, ok := extractUserID(c)
	if !ok {
		return
	}

	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	results, total, err := h.store.ListByUser(c.Request.Context(), userID, offset, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list memories"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"memories": results,
		"total":    total,
		"offset":   offset,
		"limit":    limit,
	})
}

// Search performs semantic similarity search on the user's memories.
// POST /api/v1/memories/search  { "query": "...", "limit": 5, "tier": "", "category": "" }
func (h *MemoryHandler) Search(c *gin.Context) {
	userID, ok := extractUserID(c)
	if !ok {
		return
	}

	var body struct {
		Query    string `json:"query" binding:"required"`
		Limit    int    `json:"limit"`
		Tier     string `json:"tier"`
		Category string `json:"category"`
		FlowID   string `json:"flow_id"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query field is required"})
		return
	}

	if !h.store.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "embedding provider not configured"})
		return
	}

	opts := memory.SearchOpts{
		Tier:     memory.MemoryTier(body.Tier),
		Category: body.Category,
		Limit:    body.Limit,
	}

	if body.FlowID != "" {
		if fid, err := uuid.Parse(body.FlowID); err == nil {
			opts.FlowID = &fid
		}
	}

	results, err := h.store.Search(c.Request.Context(), userID, body.Query, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"results": results,
		"count":   len(results),
	})
}

// Delete removes a memory entry by ID.
// DELETE /api/v1/memories/:id
func (h *MemoryHandler) Delete(c *gin.Context) {
	_, ok := extractUserID(c)
	if !ok {
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid memory ID"})
		return
	}

	if err := h.store.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": id.String()})
}

// Stats returns memory system statistics.
// GET /api/v1/memories/stats
func (h *MemoryHandler) Stats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"enabled":    h.store.Enabled(),
	})
}

// extractUserID pulls the authenticated user ID from the gin context.
func extractUserID(c *gin.Context) (uuid.UUID, bool) {
	raw, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return uuid.UUID{}, false
	}
	id, ok := raw.(uuid.UUID)
	if !ok {
		// Try string conversion
		idStr, ok := raw.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid user context"})
			return uuid.UUID{}, false
		}
		parsed, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid user ID"})
			return uuid.UUID{}, false
		}
		return parsed, true
	}
	return id, true
}
