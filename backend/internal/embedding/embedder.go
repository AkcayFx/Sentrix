package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/yourorg/sentrix/internal/config"
)

// Embedder generates dense vector representations of text.
type Embedder interface {
	// Embed produces a single embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch produces embedding vectors for multiple texts in one call.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the expected vector length.
	Dimensions() int

	// Available reports whether the embedder is functional.
	Available() bool
}

// NewEmbedder creates the appropriate Embedder from configuration.
func NewEmbedder(cfg config.EmbeddingConfig) (Embedder, error) {
	switch cfg.Provider {
	case "openai":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("embedding: openai provider requires EMBEDDING_API_KEY")
		}
		return newOpenAIEmbedder(cfg), nil

	case "ollama":
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return newOllamaEmbedder(cfg, baseURL), nil

	case "none", "":
		log.Info("embedding: provider set to 'none', using noop embedder")
		return &noopEmbedder{dims: cfg.Dimensions}, nil

	default:
		return nil, fmt.Errorf("embedding: unsupported provider %q", cfg.Provider)
	}
}

// ═══════════════════════════════════════════════════════════════
//  OpenAI Embedder
// ═══════════════════════════════════════════════════════════════

type openAIEmbedder struct {
	apiKey     string
	model      string
	baseURL    string
	dims       int
	batchSize  int
	httpClient *http.Client
}

// openAI request/response types.
type oaiEmbedRequest struct {
	Input      interface{} `json:"input"` // string or []string
	Model      string      `json:"model"`
	Dimensions int         `json:"dimensions,omitempty"`
}

type oaiEmbedResponse struct {
	Data  []oaiEmbedData `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *oaiError `json:"error,omitempty"`
}

type oaiEmbedData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type oaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func newOpenAIEmbedder(cfg config.EmbeddingConfig) *openAIEmbedder {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := cfg.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 64
	}

	return &openAIEmbedder{
		apiKey:    cfg.APIKey,
		model:     model,
		baseURL:   base,
		dims:      cfg.Dimensions,
		batchSize: batchSize,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (e *openAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := e.callAPI(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("embedding: openai returned empty result")
	}
	return results[0], nil
}

func (e *openAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Process in chunks to respect batch size limits.
	all := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.batchSize {
		end := start + e.batchSize
		if end > len(texts) {
			end = len(texts)
		}

		batch, err := e.callAPI(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embedding: batch %d-%d failed: %w", start, end, err)
		}
		all = append(all, batch...)
	}

	return all, nil
}

func (e *openAIEmbedder) Dimensions() int { return e.dims }
func (e *openAIEmbedder) Available() bool  { return e.apiKey != "" }

func (e *openAIEmbedder) callAPI(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := oaiEmbedRequest{
		Model: e.model,
	}
	if len(texts) == 1 {
		reqBody.Input = texts[0]
	} else {
		reqBody.Input = texts
	}
	if e.dims > 0 {
		reqBody.Dimensions = e.dims
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embedding: marshal request: %w", err)
	}

	url := e.baseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("embedding: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding: http call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding: openai returned %d: %s", resp.StatusCode, truncateBytes(body, 500))
	}

	var oaiResp oaiEmbedResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("embedding: unmarshal response: %w", err)
	}
	if oaiResp.Error != nil {
		return nil, fmt.Errorf("embedding: openai error: %s", oaiResp.Error.Message)
	}

	embeddings := make([][]float32, len(oaiResp.Data))
	for _, d := range oaiResp.Data {
		embeddings[d.Index] = d.Embedding
	}

	log.WithFields(log.Fields{
		"model":  e.model,
		"count":  len(texts),
		"tokens": oaiResp.Usage.TotalTokens,
	}).Debug("embedding: openai call completed")

	return embeddings, nil
}

// ═══════════════════════════════════════════════════════════════
//  Ollama Embedder
// ═══════════════════════════════════════════════════════════════

type ollamaEmbedder struct {
	baseURL    string
	model      string
	dims       int
	httpClient *http.Client
	mu         sync.Mutex
	available  *bool
}

type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"` // string or []string
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func newOllamaEmbedder(cfg config.EmbeddingConfig, baseURL string) *ollamaEmbedder {
	model := cfg.Model
	if model == "" {
		model = "nomic-embed-text"
	}

	return &ollamaEmbedder{
		baseURL: baseURL,
		model:   model,
		dims:    cfg.Dimensions,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (e *ollamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := e.callAPI(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("embedding: ollama returned empty result")
	}
	return results[0], nil
}

func (e *ollamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return e.callAPI(ctx, texts)
}

func (e *ollamaEmbedder) Dimensions() int { return e.dims }

func (e *ollamaEmbedder) Available() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.available != nil {
		return *e.available
	}

	// Check connectivity once.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+"/api/tags", nil)
	if err != nil {
		avail := false
		e.available = &avail
		return false
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		avail := false
		e.available = &avail
		log.Warnf("embedding: ollama not reachable at %s: %v", e.baseURL, err)
		return false
	}
	resp.Body.Close()

	avail := resp.StatusCode == http.StatusOK
	e.available = &avail
	return avail
}

func (e *ollamaEmbedder) callAPI(ctx context.Context, input any) ([][]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model: e.model,
		Input: input,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embedding: marshal ollama request: %w", err)
	}

	url := e.baseURL + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("embedding: create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding: ollama http call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding: read ollama response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding: ollama returned %d: %s", resp.StatusCode, truncateBytes(body, 500))
	}

	var ollamaResp ollamaEmbedResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return nil, fmt.Errorf("embedding: unmarshal ollama response: %w", err)
	}

	return ollamaResp.Embeddings, nil
}

// ═══════════════════════════════════════════════════════════════
//  Noop Embedder (provider = "none")
// ═══════════════════════════════════════════════════════════════

type noopEmbedder struct {
	dims int
}

func (e *noopEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, e.dims), nil
}

func (e *noopEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = make([]float32, e.dims)
	}
	return out, nil
}

func (e *noopEmbedder) Dimensions() int { return e.dims }
func (e *noopEmbedder) Available() bool  { return false }

// ═══════════════════════════════════════════════════════════════
//  Helpers
// ═══════════════════════════════════════════════════════════════

func truncateBytes(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
