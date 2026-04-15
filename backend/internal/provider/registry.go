package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Registry currently exposes provider metadata for API handlers.
// Environment-based system providers are intentionally disabled; only user
// providers persisted via the API are used for runtime selection.
type Registry struct {
	mu       sync.RWMutex
	defaults map[ProviderType]LLM
}

// ProviderInfo describes a provider that is available for use.
type ProviderInfo struct {
	Type      ProviderType `json:"type"`
	Label     string       `json:"label"`
	Model     string       `json:"model,omitempty"`
	Source    string       `json:"source"` // "system" or "user"
	Available bool         `json:"available"`
}

// NewRegistry builds a registry. System providers from environment are ignored.
func NewRegistry(cfg *EnvLLMConfig) *Registry {
	_ = cfg
	r := &Registry{
		defaults: make(map[ProviderType]LLM),
	}
	return r
}

// EnvLLMConfig holds LLM-related environment variable values.
type EnvLLMConfig struct {
	OpenAIKey       string
	OpenRouterKey   string
	AnthropicKey    string
	GeminiKey       string
	DeepSeekKey     string
	OllamaURL       string
	CustomURL       string
	CustomModel     string
	CustomAPIKey    string
	DefaultProvider string
}

// NewFromConfig is a factory that builds the correct adapter from a Config.
func NewFromConfig(cfg Config) (LLM, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid provider config: %w", err)
	}

	switch cfg.Type {
	case ProviderOpenAI:
		return NewOpenAI(cfg), nil
	case ProviderOpenRouter:
		return NewOpenRouter(cfg), nil
	case ProviderAnthropic:
		return NewAnthropic(cfg), nil
	case ProviderGemini:
		return NewGemini(cfg), nil
	case ProviderDeepSeek:
		return NewDeepSeek(cfg), nil
	case ProviderOllama:
		return NewOllama(cfg), nil
	case ProviderCustom:
		return NewCustom(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", cfg.Type)
	}
}

// GetDefault returns the system-level default provider for the given type.
// Returns nil if no default is configured.
func (r *Registry) GetDefault(pt ProviderType) (LLM, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.defaults[pt]
	return p, ok
}

// GetAnyDefault returns the first available system-level provider,
// prioritising the preferredType if it exists.
func (r *Registry) GetAnyDefault(preferredType ProviderType) (LLM, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if preferredType != "" {
		if p, ok := r.defaults[preferredType]; ok {
			return p, nil
		}
	}

	// Fall back to any available provider (stable order)
	priority := []ProviderType{
		ProviderOpenAI, ProviderOpenRouter, ProviderAnthropic, ProviderGemini,
		ProviderDeepSeek, ProviderOllama, ProviderCustom,
	}
	for _, pt := range priority {
		if p, ok := r.defaults[pt]; ok {
			return p, nil
		}
	}

	return nil, fmt.Errorf("no system providers configured")
}

// ListDefaults returns info about every default (system-level) provider.
func (r *Registry) ListDefaults() []ProviderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	labels := map[ProviderType]string{
		ProviderOpenAI:     "OpenAI",
		ProviderOpenRouter: "OpenRouter",
		ProviderAnthropic:  "Anthropic",
		ProviderGemini:     "Google Gemini",
		ProviderDeepSeek:   "DeepSeek",
		ProviderOllama:     "Ollama (Local)",
		ProviderCustom:     "Custom HTTP",
	}

	allTypes := []ProviderType{
		ProviderOpenAI, ProviderOpenRouter, ProviderAnthropic, ProviderGemini,
		ProviderDeepSeek, ProviderOllama, ProviderCustom,
	}

	var infos []ProviderInfo
	for _, pt := range allTypes {
		info := ProviderInfo{
			Type:   pt,
			Label:  labels[pt],
			Source: "system",
		}
		if p, ok := r.defaults[pt]; ok {
			info.Available = true
			info.Model = p.ModelName()
		}
		infos = append(infos, info)
	}

	return infos
}

// TestConnection verifies that a provider configuration can reach the API
// by sending a minimal "hello" prompt. Returns nil on success.
func TestConnection(cfg Config) error {
	adapter, err := NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("create adapter: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []Message{
		{
			Role:    RoleSystem,
			Content: "Respond with exactly one plain word in the assistant message content. Do not use tools.",
		},
		{Role: RoleUser, Content: "Say hello in one word."},
	}

	resp, err := adapter.Complete(ctx, messages, nil, &CompletionParams{
		MaxTokens: intPtr(16),
	})
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}

	// Some reasoning models may still return empty assistant content for smoke
	// tests even with explicit system instructions. Reaching this point means
	// connectivity and auth are valid, so treat it as a successful test.
	if strings.TrimSpace(resp.Content) == "" && len(resp.ToolCalls) == 0 {
		return nil
	}

	return nil
}

func intPtr(v int) *int { return &v }
