package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/yourorg/sentrix/internal/config"
)

const (
	duckDuckGoSearchURL = "https://html.duckduckgo.com/html/"
	googleSearchURL     = "https://www.googleapis.com/customsearch/v1"
	tavilySearchURL     = "https://api.tavily.com/search"
	traversaalSearchURL = "https://api-ares.traversaal.ai/live/predict"
	perplexitySearchURL = "https://api.perplexity.ai/chat/completions"
	sploitusSearchURL   = "https://sploitus.com/search"
)

type duckDuckGoSearcher struct {
	enabled   bool
	client    *http.Client
	userAgent string
}

type googleSearcher struct {
	apiKey string
	cx     string
	client *http.Client
}

type tavilySearcher struct {
	apiKey string
	client *http.Client
}

type traversaalSearcher struct {
	apiKey string
	client *http.Client
}

type perplexitySearcher struct {
	apiKey string
	model  string
	client *http.Client
}

type sploitusSearcher struct {
	enabled bool
	client  *http.Client
}

type searxngSearcher struct {
	baseURL    string
	language   string
	categories string
	safeSearch string
	client     *http.Client
}

func newDuckDuckGoSearcher(cfg *config.Config, client *http.Client) Searcher {
	enabled := true
	if cfg != nil {
		enabled = cfg.Search.DuckDuckGoEnabled
	}
	return &duckDuckGoSearcher{
		enabled:   enabled,
		client:    client,
		userAgent: "Mozilla/5.0 (compatible; Sentrix/0.7; +https://sentrix.local)",
	}
}

func newGoogleSearcher(cfg *config.Config, client *http.Client) Searcher {
	searchCfg := config.SearchConfig{}
	if cfg != nil {
		searchCfg = cfg.Search
	}
	return &googleSearcher{
		apiKey: searchCfg.GoogleAPIKey,
		cx:     searchCfg.GoogleCX,
		client: client,
	}
}

func newTavilySearcher(cfg *config.Config, client *http.Client) Searcher {
	searchCfg := config.SearchConfig{}
	if cfg != nil {
		searchCfg = cfg.Search
	}
	return &tavilySearcher{
		apiKey: searchCfg.TavilyAPIKey,
		client: client,
	}
}

func newTraversaalSearcher(cfg *config.Config, client *http.Client) Searcher {
	searchCfg := config.SearchConfig{}
	if cfg != nil {
		searchCfg = cfg.Search
	}
	return &traversaalSearcher{
		apiKey: searchCfg.TraversaalAPIKey,
		client: client,
	}
}

func newPerplexitySearcher(cfg *config.Config, client *http.Client) Searcher {
	searchCfg := config.SearchConfig{}
	if cfg != nil {
		searchCfg = cfg.Search
	}
	model := searchCfg.PerplexityModel
	if model == "" {
		model = "sonar"
	}
	return &perplexitySearcher{
		apiKey: searchCfg.PerplexityAPIKey,
		model:  model,
		client: client,
	}
}

func newSploitusSearcher(cfg *config.Config, client *http.Client) Searcher {
	enabled := true
	if cfg != nil {
		enabled = cfg.Search.SploitusEnabled
	}
	return &sploitusSearcher{
		enabled: enabled,
		client:  client,
	}
}

func newSearxngSearcher(cfg *config.Config, client *http.Client) Searcher {
	searchCfg := config.SearchConfig{}
	if cfg != nil {
		searchCfg = cfg.Search
	}
	return &searxngSearcher{
		baseURL:    strings.TrimSpace(searchCfg.SearxngURL),
		language:   defaultString(searchCfg.SearxngLanguage, "en"),
		categories: defaultString(searchCfg.SearxngCategories, "general"),
		safeSearch: defaultString(searchCfg.SearxngSafeSearch, "1"),
		client:     client,
	}
}

func (s *duckDuckGoSearcher) IsAvailable() bool { return s.enabled }

func (s *duckDuckGoSearcher) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	form := url.Values{}
	form.Set("q", query)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, duckDuckGoSearchURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create duckduckgo request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", s.userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("duckduckgo request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duckduckgo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read duckduckgo response: %w", err)
	}
	return parseDuckDuckGoResults(string(body)), nil
}

func (s *googleSearcher) IsAvailable() bool {
	return s.apiKey != "" && s.cx != ""
}

func (s *googleSearcher) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	reqURL, _ := url.Parse(googleSearchURL)
	params := reqURL.Query()
	params.Set("key", s.apiKey)
	params.Set("cx", s.cx)
	params.Set("q", query)
	params.Set("num", strconv.Itoa(clampResults(opts.MaxResults, 10)))
	reqURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create google request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google returned status %d", resp.StatusCode)
	}

	var payload struct {
		Items []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode google response: %w", err)
	}

	results := make([]SearchResult, 0, len(payload.Items))
	for _, item := range payload.Items {
		results = append(results, SearchResult{
			Title:    item.Title,
			URL:      item.Link,
			Snippet:  item.Snippet,
			Source:   "Google Custom Search",
			Provider: SearchProviderGoogle,
		})
	}
	return results, nil
}

func (s *tavilySearcher) IsAvailable() bool { return s.apiKey != "" }

func (s *tavilySearcher) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	payload := map[string]any{
		"api_key":         s.apiKey,
		"query":           query,
		"topic":           "general",
		"search_depth":    "advanced",
		"include_answer":  false,
		"include_images":  false,
		"max_results":     clampResults(opts.MaxResults, 10),
		"include_domains": opts.IncludeDomains,
		"exclude_domains": opts.ExcludeDomains,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal tavily request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilySearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create tavily request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tavily returned status %d", resp.StatusCode)
	}

	var payloadResp struct {
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payloadResp); err != nil {
		return nil, fmt.Errorf("decode tavily response: %w", err)
	}

	results := make([]SearchResult, 0, len(payloadResp.Results))
	for _, item := range payloadResp.Results {
		results = append(results, SearchResult{
			Title:    item.Title,
			URL:      item.URL,
			Snippet:  item.Content,
			Source:   "Tavily",
			Provider: SearchProviderTavily,
			Score:    item.Score,
		})
	}
	return results, nil
}

func (s *traversaalSearcher) IsAvailable() bool { return s.apiKey != "" }

func (s *traversaalSearcher) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, fmt.Errorf("marshal traversaal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, traversaalSearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create traversaal request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("traversaal request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("traversaal returned status %d", resp.StatusCode)
	}

	var payload struct {
		Data struct {
			Response string   `json:"response_text"`
			Links    []string `json:"web_url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode traversaal response: %w", err)
	}

	results := []SearchResult{{
		Title:    "Traversaal answer",
		URL:      firstNonEmpty(payload.Data.Links),
		Snippet:  payload.Data.Response,
		Source:   "Traversaal",
		Provider: SearchProviderTraversaal,
		Score:    0.75,
	}}
	for _, link := range payload.Data.Links {
		results = append(results, SearchResult{
			Title:    "Traversaal source",
			URL:      link,
			Snippet:  truncateRunes(payload.Data.Response, 280),
			Source:   "Traversaal",
			Provider: SearchProviderTraversaal,
			Score:    0.5,
		})
	}
	return results, nil
}

func (s *perplexitySearcher) IsAvailable() bool { return s.apiKey != "" }

func (s *perplexitySearcher) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	payload := map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "user", "content": query},
		},
		"max_tokens":               1500,
		"temperature":              0.2,
		"top_p":                    0.9,
		"search_context_size":      "medium",
		"return_images":            false,
		"return_related_questions": false,
		"stream":                   false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal perplexity request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, perplexitySearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create perplexity request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perplexity request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("perplexity returned status %d", resp.StatusCode)
	}

	var payloadResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Citations []string `json:"citations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payloadResp); err != nil {
		return nil, fmt.Errorf("decode perplexity response: %w", err)
	}

	answer := ""
	if len(payloadResp.Choices) > 0 {
		answer = payloadResp.Choices[0].Message.Content
	}
	results := []SearchResult{{
		Title:    "Perplexity answer",
		URL:      firstNonEmpty(payloadResp.Citations),
		Snippet:  answer,
		Source:   "Perplexity",
		Provider: SearchProviderPerplexity,
		Score:    0.85,
	}}
	for _, citation := range payloadResp.Citations {
		results = append(results, SearchResult{
			Title:    "Perplexity citation",
			URL:      citation,
			Snippet:  truncateRunes(answer, 280),
			Source:   "Perplexity",
			Provider: SearchProviderPerplexity,
			Score:    0.6,
		})
	}
	return results, nil
}

func (s *sploitusSearcher) IsAvailable() bool { return s.enabled }

func (s *sploitusSearcher) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	category := strings.ToLower(strings.TrimSpace(opts.Category))
	if category == "" {
		category = "exploits"
	}
	sortOrder := strings.ToLower(strings.TrimSpace(opts.Sort))
	if sortOrder == "" {
		sortOrder = "default"
	}
	body, err := json.Marshal(map[string]any{
		"query":  query,
		"type":   category,
		"sort":   sortOrder,
		"title":  false,
		"offset": 0,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal sploitus request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sploitusSearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create sploitus request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://sploitus.com")
	req.Header.Set("Referer", "https://sploitus.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Sentrix/0.7)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sploitus request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sploitus returned status %d", resp.StatusCode)
	}

	var payload struct {
		Exploits []struct {
			Title     string  `json:"title"`
			Href      string  `json:"href"`
			Type      string  `json:"type"`
			Score     float64 `json:"score"`
			Published string  `json:"published"`
			Source    string  `json:"source"`
			Language  string  `json:"language"`
		} `json:"exploits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode sploitus response: %w", err)
	}

	limit := clampResults(opts.MaxResults, 10)
	results := make([]SearchResult, 0, min(limit, len(payload.Exploits)))
	for _, item := range payload.Exploits {
		results = append(results, SearchResult{
			Title:       item.Title,
			URL:         item.Href,
			Snippet:     truncateRunes(strings.TrimSpace(item.Source), 400),
			Source:      defaultString(item.Language, item.Type),
			Provider:    SearchProviderSploitus,
			Score:       item.Score,
			PublishedAt: item.Published,
		})
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

func (s *searxngSearcher) IsAvailable() bool { return s.baseURL != "" }

func (s *searxngSearcher) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	base, err := url.Parse(s.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid searxng url: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/search"
	params := base.Query()
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("language", s.language)
	params.Set("categories", s.categories)
	params.Set("safesearch", s.safeSearch)
	params.Set("limit", strconv.Itoa(clampResults(opts.MaxResults, 10)))
	base.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create searxng request: %w", err)
	}
	req.Header.Set("User-Agent", "Sentrix/0.7")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned status %d", resp.StatusCode)
	}

	var payload struct {
		Results []struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			Author        string `json:"author"`
			PublishedDate string `json:"publishedDate"`
			Engine        string `json:"engine"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode searxng response: %w", err)
	}

	results := make([]SearchResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		results = append(results, SearchResult{
			Title:       item.Title,
			URL:         item.URL,
			Snippet:     item.Content,
			Source:      firstNonEmpty([]string{item.Engine, item.Author}),
			Provider:    SearchProviderSearxng,
			PublishedAt: item.PublishedDate,
		})
	}
	return results, nil
}

func parseDuckDuckGoResults(body string) []SearchResult {
	blockPattern := regexp.MustCompile(`(?s)<div class="result results_links[^"]*">.*?<div class="clear"></div>\s*</div>\s*</div>`)
	titlePattern := regexp.MustCompile(`<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	snippetPattern := regexp.MustCompile(`(?s)<a[^>]+class="result__snippet"[^>]+href="[^"]*">(.+?)</a>`)

	var results []SearchResult
	for _, block := range blockPattern.FindAllString(body, -1) {
		titleMatch := titlePattern.FindStringSubmatch(block)
		if len(titleMatch) < 3 {
			continue
		}
		snippet := ""
		if snippetMatch := snippetPattern.FindStringSubmatch(block); len(snippetMatch) > 1 {
			snippet = cleanHTMLText(snippetMatch[1])
		}
		results = append(results, SearchResult{
			Title:    cleanHTMLText(titleMatch[2]),
			URL:      cleanDuckDuckGoURL(titleMatch[1]),
			Snippet:  snippet,
			Source:   "DuckDuckGo",
			Provider: SearchProviderDuckDuckGo,
		})
	}
	return results
}

func cleanHTMLText(text string) string {
	tagPattern := regexp.MustCompile(`<[^>]+>`)
	text = tagPattern.ReplaceAllString(text, " ")
	replacements := map[string]string{
		"&amp;":  "&",
		"&lt;":   "<",
		"&gt;":   ">",
		"&quot;": `"`,
		"&#39;":  "'",
		"&nbsp;": " ",
	}
	for old, newValue := range replacements {
		text = strings.ReplaceAll(text, old, newValue)
	}
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func cleanDuckDuckGoURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if uddg := parsed.Query().Get("uddg"); uddg != "" {
		if decoded, err := url.QueryUnescape(uddg); err == nil {
			return decoded
		}
	}
	return raw
}

func clampResults(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	if value > 10 {
		return 10
	}
	return value
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
