package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AnthropicAdapter implements the LLM interface for the Anthropic Messages API.
type AnthropicAdapter struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// NewAnthropic creates an adapter for Anthropic's Claude models.
func NewAnthropic(cfg Config) *AnthropicAdapter {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &AnthropicAdapter{
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(base, "/"),
		model:   model,
		client:  &http.Client{},
	}
}

func (a *AnthropicAdapter) ModelName() string      { return a.model }
func (a *AnthropicAdapter) Provider() ProviderType { return ProviderAnthropic }

// ── Internal structures ─────────────────────────────────────────────

type claudeRequest struct {
	Model     string           `json:"model"`
	Messages  []claudeMessage  `json:"messages"`
	System    string           `json:"system,omitempty"`
	MaxTokens int              `json:"max_tokens"`
	Tools     []claudeTool     `json:"tools,omitempty"`
	Stream    bool             `json:"stream,omitempty"`
	Temp      *float64         `json:"temperature,omitempty"`
	TopP      *float64         `json:"top_p,omitempty"`
	Stop      []string         `json:"stop_sequences,omitempty"`
}

type claudeMessage struct {
	Role    string              `json:"role"`
	Content json.RawMessage     `json:"content"` // string or []block
}

type claudeBlock struct {
	Type      string      `json:"type"`
	Text      string      `json:"text,omitempty"`
	ID        string      `json:"id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Input     interface{} `json:"input,omitempty"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   string      `json:"content,omitempty"`
}

type claudeTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

type claudeResponse struct {
	ID           string        `json:"id"`
	Model        string        `json:"model"`
	Content      []claudeBlock `json:"content"`
	StopReason   string        `json:"stop_reason"`
	Usage        claudeUsage   `json:"usage"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type claudeErrorResp struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// ── Complete ────────────────────────────────────────────────────────

func (a *AnthropicAdapter) Complete(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (*Response, error) {
	body := a.buildRequest(messages, tools, params, false)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

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
		var errResp claudeErrorResp
		json.Unmarshal(respData, &errResp)
		return nil, fmt.Errorf("anthropic api error (%d): %s", resp.StatusCode, errResp.Error.Message)
	}

	var cResp claudeResponse
	if err := json.Unmarshal(respData, &cResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	result := &Response{
		Model:        cResp.Model,
		TokensIn:     cResp.Usage.InputTokens,
		TokensOut:    cResp.Usage.OutputTokens,
		FinishReason: cResp.StopReason,
	}

	for _, block := range cResp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   block.ID,
				Name: block.Name,
				Args: string(argsJSON),
			})
		}
	}

	return result, nil
}

// ── Stream (simplified — returns channel) ───────────────────────────

func (a *AnthropicAdapter) Stream(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (<-chan StreamChunk, error) {
	body := a.buildRequest(messages, tools, params, true)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respData, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var errResp claudeErrorResp
		json.Unmarshal(respData, &errResp)
		return nil, fmt.Errorf("anthropic api error (%d): %s", resp.StatusCode, errResp.Error.Message)
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		a.readAnthropicStream(ctx, resp.Body, ch)
	}()

	return ch, nil
}

func (a *AnthropicAdapter) readAnthropicStream(ctx context.Context, body io.Reader, ch chan<- StreamChunk) {
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
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				payload := strings.TrimPrefix(line, "data: ")

				var event map[string]interface{}
				if jsonErr := json.Unmarshal([]byte(payload), &event); jsonErr != nil {
					continue
				}

				eventType, _ := event["type"].(string)
				switch eventType {
				case "content_block_delta":
					delta, _ := event["delta"].(map[string]interface{})
					if text, ok := delta["text"].(string); ok {
						ch <- StreamChunk{Delta: text}
					}
				case "message_stop":
					ch <- StreamChunk{Done: true}
					return
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

func (a *AnthropicAdapter) buildRequest(messages []Message, tools []ToolDef, params *CompletionParams, stream bool) claudeRequest {
	maxTokens := 4096
	if params != nil && params.MaxTokens != nil {
		maxTokens = *params.MaxTokens
	}

	cr := claudeRequest{
		Model:     a.model,
		MaxTokens: maxTokens,
		Stream:    stream,
	}

	if params != nil {
		cr.Temp = params.Temperature
		cr.TopP = params.TopP
		cr.Stop = params.Stop
	}

	// Extract system message
	for _, m := range messages {
		if m.Role == RoleSystem {
			cr.System = m.Content
			break
		}
	}

	// Convert messages (skip system — it goes into the system field)
	for _, m := range messages {
		if m.Role == RoleSystem {
			continue
		}

		role := m.Role
		if role == RoleTool {
			role = RoleUser
		}

		if m.ToolCallID != "" {
			// Tool result → content array with tool_result block
			block := claudeBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}
			blockJSON, _ := json.Marshal([]claudeBlock{block})
			cr.Messages = append(cr.Messages, claudeMessage{
				Role:    role,
				Content: blockJSON,
			})
		} else if len(m.ToolCalls) > 0 {
			// Assistant with tool_use blocks
			var blocks []claudeBlock
			if m.Content != "" {
				blocks = append(blocks, claudeBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input interface{}
				json.Unmarshal([]byte(tc.Args), &input)
				blocks = append(blocks, claudeBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			blockJSON, _ := json.Marshal(blocks)
			cr.Messages = append(cr.Messages, claudeMessage{
				Role:    role,
				Content: blockJSON,
			})
		} else {
			contentJSON, _ := json.Marshal(m.Content)
			cr.Messages = append(cr.Messages, claudeMessage{
				Role:    role,
				Content: contentJSON,
			})
		}
	}

	for _, t := range tools {
		cr.Tools = append(cr.Tools, claudeTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	return cr
}
