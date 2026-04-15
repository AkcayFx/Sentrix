package provider

import (
	"context"
	"fmt"
)

// ProviderType enumerates supported LLM backend types.
type ProviderType string

const (
	ProviderOpenAI     ProviderType = "openai"
	ProviderOpenRouter ProviderType = "openrouter"
	ProviderAnthropic  ProviderType = "anthropic"
	ProviderGemini     ProviderType = "gemini"
	ProviderOllama     ProviderType = "ollama"
	ProviderDeepSeek   ProviderType = "deepseek"
	ProviderBedrock    ProviderType = "bedrock"
	ProviderCustom     ProviderType = "custom"
)

// Role constants for chat messages.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message represents a single message in a conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID    string `json:"id"`
	Index int    `json:"index"` // stream-chunk index for correlating incremental deltas
	Name  string `json:"name"`
	Args  string `json:"arguments"`
}

// ToolDef defines a tool the model can invoke (OpenAI function-calling schema).
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // JSON Schema object
}

// Response is the result of a completion request.
type Response struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Model        string     `json:"model"`
	TokensIn     int        `json:"tokens_in"`
	TokensOut    int        `json:"tokens_out"`
	FinishReason string     `json:"finish_reason"`
}

// StreamChunk is a piece of a streaming completion.
type StreamChunk struct {
	Delta     string     `json:"delta"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Done      bool       `json:"done"`
	Err       error      `json:"-"`
}

// CompletionParams holds optional parameters for completion requests.
type CompletionParams struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// LLM is the core interface every provider adapter must implement.
type LLM interface {
	// Complete sends messages to the model and returns a single response.
	Complete(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (*Response, error)

	// Stream sends messages and returns a channel of incremental chunks.
	Stream(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (<-chan StreamChunk, error)

	// ModelName returns the identifier of the model in use.
	ModelName() string

	// Provider returns the adapter type.
	Provider() ProviderType
}

// Config holds connection details for a provider instance.
type Config struct {
	Type    ProviderType `json:"type"`
	APIKey  string       `json:"api_key"`
	BaseURL string       `json:"base_url"`
	Model   string       `json:"model"`
}

// Validate checks that the config has the minimum required fields.
func (c Config) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("provider type is required")
	}
	if c.Type != ProviderOllama && c.APIKey == "" {
		return fmt.Errorf("api key is required for %s", c.Type)
	}
	return nil
}
