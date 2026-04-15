package provider

import "testing"

func TestNewFromConfigOpenRouterUsesOpenRouterDefaults(t *testing.T) {
	llm, err := NewFromConfig(Config{
		Type:   ProviderOpenRouter,
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("NewFromConfig returned error: %v", err)
	}

	adapter, ok := llm.(*OpenRouterAdapter)
	if !ok {
		t.Fatalf("expected *OpenRouterAdapter, got %T", llm)
	}

	if got, want := adapter.Provider(), ProviderOpenRouter; got != want {
		t.Fatalf("Provider() = %q, want %q", got, want)
	}
	if got, want := adapter.ModelName(), "openrouter/auto"; got != want {
		t.Fatalf("ModelName() = %q, want %q", got, want)
	}
	if got, want := adapter.baseURL, "https://openrouter.ai/api/v1"; got != want {
		t.Fatalf("baseURL = %q, want %q", got, want)
	}
	if got, want := adapter.errorLabel, "openrouter"; got != want {
		t.Fatalf("errorLabel = %q, want %q", got, want)
	}
}
