package provider

// DeepSeekAdapter wraps the OpenAI-compatible API exposed by DeepSeek.
// Since DeepSeek's chat/completions endpoint follows the exact OpenAI format,
// we embed OpenAIAdapter and override only the metadata.
type DeepSeekAdapter struct {
	*OpenAIAdapter
}

// NewDeepSeek creates an adapter that speaks to DeepSeek's OpenAI-compatible endpoint.
func NewDeepSeek(cfg Config) *DeepSeekAdapter {
	inner := newOpenAICompatibleAdapter(cfg, "https://api.deepseek.com/v1", "deepseek-chat", "deepseek", nil)
	return &DeepSeekAdapter{OpenAIAdapter: inner}
}

func (a *DeepSeekAdapter) ModelName() string      { return a.model }
func (a *DeepSeekAdapter) Provider() ProviderType { return ProviderDeepSeek }
