package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/agent"
	"github.com/yourorg/sentrix/internal/auth"
	"github.com/yourorg/sentrix/internal/database"
)

type AssistantHandler struct {
	db      *gorm.DB
	service *agent.AssistantService
}

func NewAssistantHandler(db *gorm.DB, service *agent.AssistantService) *AssistantHandler {
	return &AssistantHandler{db: db, service: service}
}

type AssistantDTO struct {
	ID        string `json:"id"`
	FlowID    string `json:"flow_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	UseAgents bool   `json:"use_agents"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type AssistantLogDTO struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	AgentRole string `json:"agent_role"`
	Content   string `json:"content"`
	Metadata  string `json:"metadata"`
	CreatedAt string `json:"created_at"`
}

type AssistantDetailDTO struct {
	Assistant AssistantDTO      `json:"assistant"`
	Logs      []AssistantLogDTO `json:"logs"`
}

type UpdateAssistantRequest struct {
	UseAgents bool `json:"use_agents"`
}

type SendAssistantMessageRequest struct {
	Content   string `json:"content" binding:"required"`
	UseAgents *bool  `json:"use_agents"`
}

func (h *AssistantHandler) Get(c *gin.Context) {
	flow, ok := h.loadFlow(c)
	if !ok {
		return
	}

	assistant, logs, err := h.service.GetSession(c.Request.Context(), flow)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, toAssistantDetailDTO(*assistant, logs))
}

func (h *AssistantHandler) Update(c *gin.Context) {
	flow, ok := h.loadFlow(c)
	if !ok {
		return
	}

	var req UpdateAssistantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	assistant, _, err := h.service.GetSession(c.Request.Context(), flow)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	updated, err := h.service.UpdateSession(c.Request.Context(), assistant, req.UseAgents)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_, logs, err := h.service.GetSession(c.Request.Context(), flow)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, toAssistantDetailDTO(*updated, logs))
}

func (h *AssistantHandler) SendMessage(c *gin.Context) {
	flow, ok := h.loadFlow(c)
	if !ok {
		return
	}

	var req SendAssistantMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "assistant message is required"})
		return
	}

	assistant, logs, err := h.service.SendMessage(c.Request.Context(), flow, content, req.UseAgents)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, toAssistantDetailDTO(*assistant, logs))
}

func (h *AssistantHandler) loadFlow(c *gin.Context) (*database.Flow, bool) {
	userID := auth.GetUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid flow id"})
		return nil, false
	}

	var flow database.Flow
	if err := h.db.Where("id = ? AND user_id = ?", id, userID).First(&flow).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "flow not found"})
		return nil, false
	}

	return &flow, true
}

func toAssistantDetailDTO(assistant database.Assistant, logs []database.AssistantLog) AssistantDetailDTO {
	out := make([]AssistantLogDTO, len(logs))
	for i, entry := range logs {
		out[i] = AssistantLogDTO{
			ID:        entry.ID.String(),
			Role:      entry.Role,
			AgentRole: entry.AgentRole,
			Content:   entry.Content,
			Metadata:  entry.Metadata,
			CreatedAt: entry.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	return AssistantDetailDTO{
		Assistant: AssistantDTO{
			ID:        assistant.ID.String(),
			FlowID:    assistant.FlowID.String(),
			Title:     assistant.Title,
			Status:    assistant.Status,
			UseAgents: assistant.UseAgents,
			CreatedAt: assistant.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt: assistant.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		},
		Logs: out,
	}
}
