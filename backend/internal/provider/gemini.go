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

// GeminiAdapter implements the LLM interface for Google's Gemini REST API.
type GeminiAdapter struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// NewGemini creates an adapter for the Google Gemini generateContent API.
func NewGemini(cfg Config) *GeminiAdapter {
	base := cfg.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	model := cfg.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return &GeminiAdapter{
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(base, "/"),
		model:   model,
		client:  &http.Client{},
	}
}

func (a *GeminiAdapter) ModelName() string      { return a.model }
func (a *GeminiAdapter) Provider() ProviderType { return ProviderGemini }

// ── Internal structures ─────────────────────────────────────────────

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	SystemInstruct   *geminiContent         `json:"systemInstruction,omitempty"`
	Tools            []geminiToolDecl       `json:"tools,omitempty"`
	GenerationConfig *geminiGenerationCfg   `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall   `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse   `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type geminiFuncResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type geminiToolDecl struct {
	FunctionDeclarations []geminiFuncDecl `json:"functionDeclarations"`
}

type geminiFuncDecl struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type geminiGenerationCfg struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

type geminiErrorResp struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// ── Complete ────────────────────────────────────────────────────────

func (a *GeminiAdapter) Complete(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (*Response, error) {
	body := a.buildRequest(messages, tools, params)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", a.baseURL, a.model, a.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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
		var errResp geminiErrorResp
		json.Unmarshal(respData, &errResp)
		return nil, fmt.Errorf("gemini api error (%d): %s", resp.StatusCode, errResp.Error.Message)
	}

	var gResp geminiResponse
	if err := json.Unmarshal(respData, &gResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(gResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates returned")
	}

	candidate := gResp.Candidates[0]
	result := &Response{
		Model:        a.model,
		FinishReason: candidate.FinishReason,
	}

	if gResp.UsageMetadata != nil {
		result.TokensIn = gResp.UsageMetadata.PromptTokenCount
		result.TokensOut = gResp.UsageMetadata.CandidatesTokenCount
	}

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			result.Content += part.Text
		}
		if part.FunctionCall != nil {
			argsJSON, _ := json.Marshal(part.FunctionCall.Args)
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   fmt.Sprintf("call_%s", part.FunctionCall.Name),
				Name: part.FunctionCall.Name,
				Args: string(argsJSON),
			})
		}
	}

	return result, nil
}

// ── Stream ──────────────────────────────────────────────────────────

func (a *GeminiAdapter) Stream(ctx context.Context, messages []Message, tools []ToolDef, params *CompletionParams) (<-chan StreamChunk, error) {
	body := a.buildRequest(messages, tools, params)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", a.baseURL, a.model, a.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
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
		var errResp geminiErrorResp
		json.Unmarshal(respData, &errResp)
		return nil, fmt.Errorf("gemini api error (%d): %s", resp.StatusCode, errResp.Error.Message)
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		a.readGeminiStream(ctx, resp.Body, ch)
	}()

	return ch, nil
}

func (a *GeminiAdapter) readGeminiStream(ctx context.Context, body io.Reader, ch chan<- StreamChunk) {
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

				var chunk geminiResponse
				if jsonErr := json.Unmarshal([]byte(payload), &chunk); jsonErr != nil {
					continue
				}

				if len(chunk.Candidates) > 0 {
					for _, part := range chunk.Candidates[0].Content.Parts {
						if part.Text != "" {
							ch <- StreamChunk{Delta: part.Text}
						}
						if part.FunctionCall != nil {
							argsJSON, _ := json.Marshal(part.FunctionCall.Args)
							ch <- StreamChunk{
								ToolCalls: []ToolCall{{
									ID:   fmt.Sprintf("call_%s", part.FunctionCall.Name),
									Name: part.FunctionCall.Name,
									Args: string(argsJSON),
								}},
							}
						}
					}

					if chunk.Candidates[0].FinishReason != "" && chunk.Candidates[0].FinishReason != "STOP" {
						// Non-normal finish
					}
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

func (a *GeminiAdapter) buildRequest(messages []Message, tools []ToolDef, params *CompletionParams) geminiRequest {
	req := geminiRequest{}

	// Generation config
	if params != nil {
		req.GenerationConfig = &geminiGenerationCfg{
			Temperature:     params.Temperature,
			MaxOutputTokens: params.MaxTokens,
			TopP:            params.TopP,
			StopSequences:   params.Stop,
		}
	}

	// Extract system instruction
	for _, m := range messages {
		if m.Role == RoleSystem {
			req.SystemInstruct = &geminiContent{
				Parts: []geminiPart{{Text: m.Content}},
			}
			break
		}
	}

	// Convert messages (Gemini uses "user" and "model" roles)
	for _, m := range messages {
		if m.Role == RoleSystem {
			continue
		}

		role := m.Role
		switch role {
		case RoleAssistant:
			role = "model"
		case RoleTool:
			role = "user"
		}

		gc := geminiContent{Role: role}

		if m.ToolCallID != "" {
			// Tool result → functionResponse part
			gc.Parts = append(gc.Parts, geminiPart{
				FunctionResponse: &geminiFuncResponse{
					Name:     m.ToolCallID,
					Response: map[string]interface{}{"result": m.Content},
				},
			})
		} else if len(m.ToolCalls) > 0 {
			// Model response with function calls
			if m.Content != "" {
				gc.Parts = append(gc.Parts, geminiPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args map[string]interface{}
				json.Unmarshal([]byte(tc.Args), &args)
				gc.Parts = append(gc.Parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						Name: tc.Name,
						Args: args,
					},
				})
			}
		} else {
			gc.Parts = append(gc.Parts, geminiPart{Text: m.Content})
		}

		req.Contents = append(req.Contents, gc)
	}

	// Tools
	if len(tools) > 0 {
		var decls []geminiFuncDecl
		for _, t := range tools {
			decls = append(decls, geminiFuncDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
		req.Tools = []geminiToolDecl{{FunctionDeclarations: decls}}
	}

	return req
}
