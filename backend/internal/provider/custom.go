package provider

// CustomAdapter wraps the OpenAI-compatible API for any user-provided endpoint.
// Works with vLLM, LM Studio, text-generation-inference, LocalAI, etc.
type CustomAdapter struct {
	*OpenAIAdapter
}

// NewCustom creates an adapter that speaks to a custom OpenAI-compatible server.
func NewCustom(cfg Config) *CustomAdapter {
	inner := newOpenAICompatibleAdapter(cfg, "http://localhost:8000/v1", "default", "custom", nil)
	return &CustomAdapter{OpenAIAdapter: inner}
}

func (a *CustomAdapter) ModelName() string      { return a.model }
func (a *CustomAdapter) Provider() ProviderType { return ProviderCustom }
