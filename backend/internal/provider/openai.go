package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
)

// OpenAIAdapter implements the LLM interface for OpenAI-compatible APIs.
// Works with OpenAI, Azure OpenAI, and any service exposing the same chat completions API.
type OpenAIAdapter struct {
	apiKey       string
	baseURL      string
	model        string
	errorLabel   string
	extraHeaders map[string]string
	client       *http.Client
}

// NewOpenAI creates an adapter for the OpenAI chat completions API.
func NewOpenAI(cfg Config) *OpenAIAdapter {
	return newOpenAICompatibleAdapter(cfg, "https://api.openai.com/v1", "gpt-4o", "openai", nil)
}

func newOpenAICompatibleAdapter(
	cfg Config,
	defaultBase string,
	defaultModel string,
	errorLabel string,
	extraHeaders map[string]string,
) *OpenAIAdapter {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBase
	}
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	return &OpenAIAdapter{
		apiKey:       cfg.APIKey,
		baseURL:      strings.TrimRight(base, "/"),
		model:        model,
		errorLabel:   errorLabel,
		extraHeaders: extraHeaders,
		client:       &http.Client{},
	}
}

func (a *OpenAIAdapter) ModelName() string      { return a.model }
func (a *OpenAIAdapter) Provider() ProviderType { return ProviderOpenAI }

// ── Internal request/response structures ────────────────────────────

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Tools       []oaiTool    `json:"tools,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	MaxTokens   *int         `json:"max_tokens,omitempty"`
	TopP        *float64     `json:"top_p,omitempty"`
	Stop        []string     `json:"stop,omitempty"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Index    int             `json:"index"`
	Type     string          `json:"type"`
	Function oaiToolCallFunc `json:"function"`
}

type oaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiResponse struct {
	ID      string      `json:"id"`
	Model   string      `json:"model"`
	Choices []oaiChoice `json:"choices"`
	Usage   oaiUsage    `json:"usage"`
}

type oaiChoice struct {
	Message      oaiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type oaiStreamChunk struct {
	Choices []oaiStreamChoice `json:"choices"`
}

type oaiStreamChoice struct {
	Delta        oaiStreamDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type oaiStreamDelta struct {
	Content   string        `json:"content"`
	ToolCalls []oaiToolCall `json:"tool_calls"`
}

type oaiErrorResp struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ── Complete ────────────────────────────────────────────────────────

func (a *OpenAIAdapter) Complete(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (*Response, error) {
	body := a.buildRequest(messages, tools, params, false)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	a.applyHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp oaiErrorResp
		json.Unmarshal(respData, &errResp)
		return nil, fmt.Errorf("%s api error (%d): %s", a.errorLabel, resp.StatusCode, errResp.Error.Message)
	}

	var oaiResp oaiResponse
	if err := json.Unmarshal(respData, &oaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}

	choice := oaiResp.Choices[0]
	result := &Response{
		Content:      choice.Message.Content,
		Model:        oaiResp.Model,
		TokensIn:     oaiResp.Usage.PromptTokens,
		TokensOut:    oaiResp.Usage.CompletionTokens,
		FinishReason: choice.FinishReason,
	}

	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: tc.Function.Arguments,
		})
	}

	return result, nil
}

// ── Stream ──────────────────────────────────────────────────────────

func (a *OpenAIAdapter) Stream(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (<-chan StreamChunk, error) {
	body := a.buildRequest(messages, tools, params, true)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	a.applyHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respData, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var errResp oaiErrorResp
		json.Unmarshal(respData, &errResp)
		return nil, fmt.Errorf("%s api error (%d): %s", a.errorLabel, resp.StatusCode, errResp.Error.Message)
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		a.readSSEStream(ctx, resp.Body, ch)
	}()

	return ch, nil
}

func (a *OpenAIAdapter) readSSEStream(ctx context.Context, body io.Reader, ch chan<- StreamChunk) {
	buf := make([]byte, 4096)
	var partial string

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := body.Read(buf)
		if n > 0 {
			partial += string(buf[:n])
			lines := strings.Split(partial, "\n")
			partial = lines[len(lines)-1]

			for _, line := range lines[:len(lines)-1] {
				line = strings.TrimSpace(line)
				if line == "" || line == "data: [DONE]" {
					if line == "data: [DONE]" {
						ch <- StreamChunk{Done: true}
						return
					}
					continue
				}
				if !strings.HasPrefix(line, "data: ") {
					continue
				}

				payload := strings.TrimPrefix(line, "data: ")
				var chunk oaiStreamChunk
				if jsonErr := json.Unmarshal([]byte(payload), &chunk); jsonErr != nil {
					log.Warnf("stream parse error: %v", jsonErr)
					continue
				}

				if len(chunk.Choices) > 0 {
					delta := chunk.Choices[0].Delta
					sc := StreamChunk{Delta: delta.Content}
					for _, tc := range delta.ToolCalls {
						sc.ToolCalls = append(sc.ToolCalls, ToolCall{
							ID:    tc.ID,
							Index: tc.Index,
							Name:  tc.Function.Name,
							Args:  tc.Function.Arguments,
						})
					}
					ch <- sc
				}
			}
		}

		if err != nil {
			if err != io.EOF {
				ch <- StreamChunk{Err: err}
			}
			ch <- StreamChunk{Done: true}
			return
		}
	}
}

// ── Helpers ─────────────────────────────────────────────────────────

func (a *OpenAIAdapter) buildRequest(messages []Message, tools []ToolDef, params *CompletionParams, stream bool) oaiRequest {
	req := oaiRequest{
		Model:  a.model,
		Stream: stream,
	}

	for _, m := range messages {
		om := oaiMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			om.ToolCalls = append(om.ToolCalls, oaiToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: oaiToolCallFunc{
					Name:      tc.Name,
					Arguments: tc.Args,
				},
			})
		}
		req.Messages = append(req.Messages, om)
	}

	for _, t := range tools {
		req.Tools = append(req.Tools, oaiTool{
			Type: "function",
			Function: oaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	if params != nil {
		req.Temperature = params.Temperature
		req.MaxTokens = params.MaxTokens
		req.TopP = params.TopP
		req.Stop = params.Stop
	}

	return req
}

func (a *OpenAIAdapter) applyHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	for key, value := range a.extraHeaders {
		if strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}
