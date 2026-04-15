package provider

// OpenRouterAdapter wraps OpenRouter's OpenAI-compatible chat completions API.
type OpenRouterAdapter struct {
	*OpenAIAdapter
}

// NewOpenRouter creates an adapter that speaks to OpenRouter's API.
func NewOpenRouter(cfg Config) *OpenRouterAdapter {
	inner := newOpenAICompatibleAdapter(cfg, "https://openrouter.ai/api/v1", "openrouter/auto", "openrouter", nil)
	return &OpenRouterAdapter{OpenAIAdapter: inner}
}

func (a *OpenRouterAdapter) ModelName() string      { return a.model }
func (a *OpenRouterAdapter) Provider() ProviderType { return ProviderOpenRouter }
