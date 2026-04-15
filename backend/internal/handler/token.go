package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/auth"
	"github.com/yourorg/sentrix/internal/database"
)

// APITokenHandler manages bearer API tokens for programmatic access.
type APITokenHandler struct {
	db *gorm.DB
}

func NewAPITokenHandler(db *gorm.DB) *APITokenHandler {
	return &APITokenHandler{db: db}
}

type CreateAPITokenRequest struct {
	Label string `json:"label" binding:"required"`
}

type APITokenDTO struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
	CreatedAt  string  `json:"created_at"`
}

type CreateAPITokenResponse struct {
	Token string      `json:"token"` // Only shown once
	Data  APITokenDTO `json:"data"`
}

func toAPITokenDTO(t database.APIToken) APITokenDTO {
	dto := APITokenDTO{
		ID:        t.ID.String(),
		Label:     t.Label,
		CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if t.LastUsedAt != nil {
		s := t.LastUsedAt.Format("2006-01-02T15:04:05Z")
		dto.LastUsedAt = &s
	}
	if t.ExpiresAt != nil {
		s := t.ExpiresAt.Format("2006-01-02T15:04:05Z")
		dto.ExpiresAt = &s
	}
	return dto
}

// List returns all API tokens for the authenticated user.
func (h *APITokenHandler) List(c *gin.Context) {
	userID := auth.GetUserID(c)
	var tokens []database.APIToken
	if err := h.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&tokens).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch tokens"})
		return
	}

	dtos := make([]APITokenDTO, len(tokens))
	for i, t := range tokens {
		dtos[i] = toAPITokenDTO(t)
	}
	c.JSON(http.StatusOK, dtos)
}

// Create generates a new API token. The raw token value is returned only once.
func (h *APITokenHandler) Create(c *gin.Context) {
	userID := auth.GetUserID(c)
	var req CreateAPITokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	rawToken, tokenHash, err := auth.GenerateAPIToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	tok := database.APIToken{
		UserID:    userID,
		Label:     strings.TrimSpace(req.Label),
		TokenHash: tokenHash,
	}
	if err := h.db.Create(&tok).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store token"})
		return
	}

	c.JSON(http.StatusCreated, CreateAPITokenResponse{
		Token: rawToken,
		Data:  toAPITokenDTO(tok),
	})
}

// Delete revokes an API token by ID.
func (h *APITokenHandler) Delete(c *gin.Context) {
	userID := auth.GetUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token id"})
		return
	}

	result := h.db.Where("id = ? AND user_id = ?", id, userID).Delete(&database.APIToken{})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "token revoked"})
}
