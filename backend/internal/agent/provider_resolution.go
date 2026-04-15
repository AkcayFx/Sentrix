package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/provider"
)

type flowProviderSelection struct {
	ProviderType string `json:"provider_type"`
	ModelName    string `json:"model_name"`
}

func (o *Orchestrator) resolveLLMForFlow(ctx context.Context, flow *database.Flow) (provider.LLM, error) {
	return resolveLLMForFlow(ctx, o.db, flow)
}

func resolveLLMForFlow(
	ctx context.Context,
	db *gorm.DB,
	flow *database.Flow,
) (provider.LLM, error) {
	if flow == nil {
		return nil, fmt.Errorf("flow is required")
	}
	override := parseFlowProviderSelection(flow.Config)
	llm, err := resolveUserConfiguredLLM(ctx, db, flow.UserID, override)
	if err == nil && llm != nil {
		return llm, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("no user provider configured; add one in Settings > LLM Providers")
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no user provider configured; add one in Settings > LLM Providers")
}

func resolveUserConfiguredLLM(
	ctx context.Context,
	db *gorm.DB,
	userID uuid.UUID,
	override flowProviderSelection,
) (provider.LLM, error) {
	if db == nil {
		return nil, fmt.Errorf("db unavailable")
	}

	query := db.WithContext(ctx).Model(&database.ProviderConfig{}).Where("user_id = ?", userID)
	providerType := strings.TrimSpace(override.ProviderType)
	if providerType != "" {
		query = query.Where("provider_type = ?", providerType)
	}

	var cfg database.ProviderConfig
	err := query.
		Order("is_default DESC, updated_at DESC, created_at DESC").
		First(&cfg).
		Error
	if err != nil {
		return nil, err
	}

	modelName := strings.TrimSpace(override.ModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(cfg.ModelName)
	}

	baseURL := ""
	if cfg.BaseURL != nil {
		baseURL = strings.TrimSpace(*cfg.BaseURL)
	}

	apiKey := ""
	if cfg.APIKeyEncrypted != nil {
		apiKey = strings.TrimSpace(*cfg.APIKeyEncrypted)
	}

	return provider.NewFromConfig(provider.Config{
		Type:    provider.ProviderType(cfg.ProviderType),
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   modelName,
	})
}

func parseFlowProviderSelection(raw string) flowProviderSelection {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return flowProviderSelection{}
	}

	var selection flowProviderSelection
	_ = json.Unmarshal([]byte(raw), &selection)
	return selection
}
