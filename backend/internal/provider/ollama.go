package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OllamaAdapter implements the LLM interface for locally-hosted Ollama models.
type OllamaAdapter struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllama creates an adapter for Ollama's chat API.
func NewOllama(cfg Config) *OllamaAdapter {
	base := cfg.BaseURL
	if base == "" {
		base = "http://localhost:11434"
	}
	model := cfg.Model
	if model == "" {
		model = "llama3.1"
	}
	return &OllamaAdapter{
		baseURL: strings.TrimRight(base, "/"),
		model:   model,
		client:  &http.Client{},
	}
}

func (a *OllamaAdapter) ModelName() string      { return a.model }
func (a *OllamaAdapter) Provider() ProviderType { return ProviderOllama }

// ── Internal structures ─────────────────────────────────────────────

type ollamaRequest struct {
	Model    string           `json:"model"`
	Messages []ollamaMessage  `json:"messages"`
	Tools    []ollamaTool     `json:"tools,omitempty"`
	Stream   bool             `json:"stream"`
	Options  *ollamaOptions   `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaTool struct {
	Type     string         `json:"type"`
	Function ollamaFunction `json:"function"`
}

type ollamaFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

type ollamaToolCall struct {
	Function ollamaToolCallFunc `json:"function"`
}

type ollamaToolCallFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ollamaOptions struct {
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	NumPredict  *int     `json:"num_predict,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

type ollamaResponse struct {
	Model   string        `json:"model"`
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// ── Complete ────────────────────────────────────────────────────────

func (a *OllamaAdapter) Complete(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (*Response, error) {
	body := a.buildRequest(messages, tools, params, false)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request (is Ollama running at %s?): %w", a.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respData, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama error (%d): %s", resp.StatusCode, string(respData))
	}

	var olResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&olResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	result := &Response{
		Content: olResp.Message.Content,
		Model:   olResp.Model,
	}

	for _, tc := range olResp.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			Name: tc.Function.Name,
			Args: string(tc.Function.Arguments),
		})
	}

	return result, nil
}

// ── Stream ──────────────────────────────────────────────────────────

func (a *OllamaAdapter) Stream(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (<-chan StreamChunk, error) {
	body := a.buildRequest(messages, tools, params, true)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respData, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama error (%d): %s", resp.StatusCode, string(respData))
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			var chunk ollamaResponse
			if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
				continue
			}

			if chunk.Done {
				ch <- StreamChunk{Done: true}
				return
			}

			ch <- StreamChunk{Delta: chunk.Message.Content}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Err: err}
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

// ── Helpers ─────────────────────────────────────────────────────────

func (a *OllamaAdapter) buildRequest(messages []Message, tools []ToolDef, params *CompletionParams, stream bool) ollamaRequest {
	req := ollamaRequest{
		Model:  a.model,
		Stream: stream,
	}

	for _, m := range messages {
		req.Messages = append(req.Messages, ollamaMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	for _, t := range tools {
		req.Tools = append(req.Tools, ollamaTool{
			Type: "function",
			Function: ollamaFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	if params != nil {
		req.Options = &ollamaOptions{
			Temperature: params.Temperature,
			TopP:        params.TopP,
			NumPredict:  params.MaxTokens,
			Stop:        params.Stop,
		}
	}

	return req
}
