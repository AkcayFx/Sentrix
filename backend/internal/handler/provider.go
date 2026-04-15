package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/auth"
	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/provider"
)

// ProviderHandler manages LLM provider configurations.
type ProviderHandler struct {
	db       *gorm.DB
	registry *provider.Registry
}

func NewProviderHandler(db *gorm.DB, reg *provider.Registry) *ProviderHandler {
	return &ProviderHandler{db: db, registry: reg}
}

type CreateProviderRequest struct {
	ProviderType string  `json:"provider_type" binding:"required"`
	ModelName    string  `json:"model_name"`
	APIKey       string  `json:"api_key"`
	BaseURL      *string `json:"base_url"`
	IsDefault    bool    `json:"is_default"`
	Config       string  `json:"config"`
}

type UpdateProviderRequest struct {
	ModelName *string `json:"model_name"`
	APIKey    *string `json:"api_key"`
	BaseURL   *string `json:"base_url"`
	IsDefault *bool   `json:"is_default"`
	Config    *string `json:"config"`
}

type ProviderDTO struct {
	ID           string  `json:"id"`
	ProviderType string  `json:"provider_type"`
	ModelName    string  `json:"model_name"`
	BaseURL      *string `json:"base_url"`
	IsDefault    bool    `json:"is_default"`
	HasAPIKey    bool    `json:"has_api_key"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type TestProviderRequest struct {
	ProviderType string  `json:"provider_type" binding:"required"`
	ProviderID   string  `json:"provider_id,omitempty"`
	APIKey       string  `json:"api_key"`
	BaseURL      *string `json:"base_url"`
	ModelName    string  `json:"model_name"`
}

func toProviderDTO(p database.ProviderConfig) ProviderDTO {
	return ProviderDTO{
		ID:           p.ID.String(),
		ProviderType: p.ProviderType,
		ModelName:    p.ModelName,
		BaseURL:      p.BaseURL,
		IsDefault:    p.IsDefault,
		HasAPIKey:    p.APIKeyEncrypted != nil && *p.APIKeyEncrypted != "",
		CreatedAt:    p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:    p.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// List returns all provider configs for the authenticated user.
func (h *ProviderHandler) List(c *gin.Context) {
	userID := auth.GetUserID(c)
	var providers []database.ProviderConfig
	if err := h.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&providers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch providers"})
		return
	}

	dtos := make([]ProviderDTO, len(providers))
	for i, p := range providers {
		dtos[i] = toProviderDTO(p)
	}
	c.JSON(http.StatusOK, dtos)
}

// Create stores a new provider configuration.
func (h *ProviderHandler) Create(c *gin.Context) {
	userID := auth.GetUserID(c)
	var req CreateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	cfg := strings.TrimSpace(req.Config)
	if cfg == "" {
		cfg = "{}"
	}

	// If marking as default, unset all other defaults for the user so runtime
	// provider selection remains deterministic.
	if req.IsDefault {
		h.db.Model(&database.ProviderConfig{}).
			Where("user_id = ?", userID).
			Update("is_default", false)
	}

	var apiKey *string
	if req.APIKey != "" {
		apiKey = &req.APIKey
	}

	prov := database.ProviderConfig{
		UserID:          userID,
		ProviderType:    strings.TrimSpace(req.ProviderType),
		ModelName:       strings.TrimSpace(req.ModelName),
		APIKeyEncrypted: apiKey,
		BaseURL:         req.BaseURL,
		IsDefault:       req.IsDefault,
		Config:          cfg,
	}
	if err := h.db.Create(&prov).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create provider"})
		return
	}

	c.JSON(http.StatusCreated, toProviderDTO(prov))
}

// Update patches a provider configuration.
func (h *ProviderHandler) Update(c *gin.Context) {
	userID := auth.GetUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider id"})
		return
	}

	var prov database.ProviderConfig
	if err := h.db.Where("id = ? AND user_id = ?", id, userID).First(&prov).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	var req UpdateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	updates := make(map[string]interface{})
	if req.ModelName != nil {
		updates["model_name"] = strings.TrimSpace(*req.ModelName)
	}
	if req.APIKey != nil {
		updates["api_key_encrypted"] = *req.APIKey
	}
	if req.BaseURL != nil {
		updates["base_url"] = *req.BaseURL
	}
	if req.Config != nil {
		updates["config"] = *req.Config
	}
	if req.IsDefault != nil {
		if *req.IsDefault {
			// Set this provider as the single default for the user.
			h.db.Model(&database.ProviderConfig{}).
				Where("user_id = ? AND id != ?", userID, id).
				Update("is_default", false)
			updates["is_default"] = true
		} else {
			updates["is_default"] = false
		}
	}

	if len(updates) > 0 {
		if err := h.db.Model(&prov).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update provider"})
			return
		}
	}

	h.db.First(&prov, "id = ?", id)
	c.JSON(http.StatusOK, toProviderDTO(prov))
}

// Delete removes a provider configuration.
func (h *ProviderHandler) Delete(c *gin.Context) {
	userID := auth.GetUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider id"})
		return
	}

	result := h.db.Where("id = ? AND user_id = ?", id, userID).Delete(&database.ProviderConfig{})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "provider deleted"})
}

// TestConnection verifies that the provided credentials can reach the LLM API.
func (h *ProviderHandler) TestConnection(c *gin.Context) {
	userID := auth.GetUserID(c)
	var req TestProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	baseURL := ""
	if req.BaseURL != nil {
		baseURL = *req.BaseURL
	}

	cfg := provider.Config{
		Type:    provider.ProviderType(req.ProviderType),
		APIKey:  req.APIKey,
		BaseURL: baseURL,
		Model:   req.ModelName,
	}

	// If provider_id is supplied, test against stored credentials/config.
	if strings.TrimSpace(req.ProviderID) != "" {
		pid, err := uuid.Parse(req.ProviderID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider id"})
			return
		}

		var stored database.ProviderConfig
		if err := h.db.Where("id = ? AND user_id = ?", pid, userID).First(&stored).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
			return
		}

		cfg.Type = provider.ProviderType(stored.ProviderType)
		cfg.Model = strings.TrimSpace(stored.ModelName)
		if req.ModelName != "" {
			cfg.Model = strings.TrimSpace(req.ModelName)
		}

		cfg.BaseURL = ""
		if stored.BaseURL != nil {
			cfg.BaseURL = strings.TrimSpace(*stored.BaseURL)
		}
		if req.BaseURL != nil {
			cfg.BaseURL = strings.TrimSpace(*req.BaseURL)
		}

		cfg.APIKey = ""
		if stored.APIKeyEncrypted != nil {
			cfg.APIKey = strings.TrimSpace(*stored.APIKeyEncrypted)
		}
		if req.APIKey != "" {
			cfg.APIKey = req.APIKey
		}
	}

	if err := provider.TestConnection(cfg); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Connection successful",
	})
}

// Available returns a list of all provider types with their availability status.
func (h *ProviderHandler) Available(c *gin.Context) {
	c.JSON(http.StatusOK, []provider.ProviderInfo{})
}

