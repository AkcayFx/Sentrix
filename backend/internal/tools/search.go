package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html"

	"github.com/yourorg/sentrix/internal/config"
	"github.com/yourorg/sentrix/internal/provider"
)

type SearchProvider string

const (
	SearchProviderAuto       SearchProvider = "auto"
	SearchProviderDuckDuckGo SearchProvider = "duckduckgo"
	SearchProviderGoogle     SearchProvider = "google"
	SearchProviderTavily     SearchProvider = "tavily"
	SearchProviderTraversaal SearchProvider = "traversaal"
	SearchProviderPerplexity SearchProvider = "perplexity"
	SearchProviderSploitus   SearchProvider = "sploitus"
	SearchProviderSearxng    SearchProvider = "searxng"
)

const (
	browserActionMarkdown = "markdown"
	browserActionHTML     = "html"
	browserActionLinks    = "links"
)

type SearchOptions struct {
	MaxResults     int
	IncludeDomains []string
	ExcludeDomains []string
	Recency        string
	Category       string
	Sort           string
}

type SearchResult struct {
	Title       string
	URL         string
	Snippet     string
	Source      string
	Provider    SearchProvider
	Score       float64
	PublishedAt string
}

type SearchRun struct {
	Providers []SearchProvider
	Results   []SearchResult
}

type Searcher interface {
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
	IsAvailable() bool
}

type SearchManager struct {
	cfg       *config.Config
	client    *http.Client
	providers map[SearchProvider]Searcher
	priority  []SearchProvider
}

func NewSearchManager(cfg *config.Config) *SearchManager {
	timeout := 30 * time.Second
	if cfg != nil && cfg.Search.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.Search.TimeoutSeconds) * time.Second
	}

	m := &SearchManager{
		cfg:       cfg,
		client:    &http.Client{Timeout: timeout},
		providers: map[SearchProvider]Searcher{},
		priority:  parseProviderPriority(cfg),
	}

	m.providers[SearchProviderDuckDuckGo] = newDuckDuckGoSearcher(cfg, m.client)
	m.providers[SearchProviderGoogle] = newGoogleSearcher(cfg, m.client)
	m.providers[SearchProviderTavily] = newTavilySearcher(cfg, m.client)
	m.providers[SearchProviderTraversaal] = newTraversaalSearcher(cfg, m.client)
	m.providers[SearchProviderPerplexity] = newPerplexitySearcher(cfg, m.client)
	m.providers[SearchProviderSploitus] = newSploitusSearcher(cfg, m.client)
	m.providers[SearchProviderSearxng] = newSearxngSearcher(cfg, m.client)

	return m
}

func parseProviderPriority(cfg *config.Config) []SearchProvider {
	defaults := []SearchProvider{
		SearchProviderDuckDuckGo,
		SearchProviderTavily,
		SearchProviderSearxng,
		SearchProviderGoogle,
		SearchProviderPerplexity,
		SearchProviderTraversaal,
		SearchProviderSploitus,
	}
	if cfg == nil || len(cfg.Search.ProviderPriority) == 0 {
		return defaults
	}

	var out []SearchProvider
	for _, item := range cfg.Search.ProviderPriority {
		if provider := normalizeProvider(item); provider != "" && provider != SearchProviderAuto {
			out = append(out, provider)
		}
	}
	if len(out) == 0 {
		return defaults
	}
	return out
}

func normalizeProvider(value string) SearchProvider {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return SearchProviderAuto
	case "duckduckgo", "ddg":
		return SearchProviderDuckDuckGo
	case "google":
		return SearchProviderGoogle
	case "tavily":
		return SearchProviderTavily
	case "traversaal":
		return SearchProviderTraversaal
	case "perplexity":
		return SearchProviderPerplexity
	case "sploitus":
		return SearchProviderSploitus
	case "searxng":
		return SearchProviderSearxng
	default:
		return ""
	}
}

func (m *SearchManager) anyAvailable() bool {
	for _, provider := range m.priority {
		if searcher, ok := m.providers[provider]; ok && searcher.IsAvailable() {
			return true
		}
	}
	return false
}

func (m *SearchManager) providerAvailable(provider SearchProvider) bool {
	searcher, ok := m.providers[provider]
	return ok && searcher.IsAvailable()
}

func (m *SearchManager) Search(ctx context.Context, query string, providerName string, opts SearchOptions) (SearchRun, error) {
	if strings.TrimSpace(query) == "" {
		return SearchRun{}, fmt.Errorf("search query is required")
	}

	if opts.MaxResults <= 0 {
		opts.MaxResults = 5
		if m.cfg != nil && m.cfg.Search.DefaultMaxResults > 0 {
			opts.MaxResults = m.cfg.Search.DefaultMaxResults
		}
	}

	provider := normalizeProvider(providerName)
	if provider != "" && provider != SearchProviderAuto {
		return m.searchProvider(ctx, provider, query, opts)
	}

	available := m.availableProviders()
	if len(available) == 0 {
		return SearchRun{}, fmt.Errorf("no search providers are configured")
	}

	targetProviders := min(2, len(available))
	var run SearchRun
	var aggregate []SearchResult
	var errs []string

	for _, current := range available {
		currentRun, err := m.searchProvider(ctx, current, query, opts)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", current, err))
		}
		if len(currentRun.Results) > 0 {
			run.Providers = append(run.Providers, current)
			aggregate = append(aggregate, currentRun.Results...)
		}
		if len(run.Providers) >= targetProviders && len(rankAndLimitResults(query, aggregate, m.priority, opts)) >= opts.MaxResults {
			break
		}
	}

	run.Results = rankAndLimitResults(query, aggregate, m.priority, opts)
	if len(run.Results) == 0 && len(errs) > 0 {
		return run, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return run, nil
}

func (m *SearchManager) availableProviders() []SearchProvider {
	var providers []SearchProvider
	for _, current := range m.priority {
		if m.providerAvailable(current) {
			providers = append(providers, current)
		}
	}
	return providers
}

func (m *SearchManager) searchProvider(ctx context.Context, provider SearchProvider, query string, opts SearchOptions) (SearchRun, error) {
	searcher, ok := m.providers[provider]
	if !ok {
		return SearchRun{}, fmt.Errorf("unknown search provider %q", provider)
	}
	if !searcher.IsAvailable() {
		return SearchRun{}, fmt.Errorf("search provider %q is unavailable", provider)
	}

	results, err := searcher.Search(ctx, query, opts)
	filtered := rankAndLimitResults(query, results, m.priority, opts)
	return SearchRun{
		Providers: []SearchProvider{provider},
		Results:   filtered,
	}, err
}

func rankAndLimitResults(query string, results []SearchResult, priority []SearchProvider, opts SearchOptions) []SearchResult {
	filtered := filterSearchResults(results, opts)
	deduped := dedupeSearchResults(filtered)
	assignSearchScores(query, deduped, priority)
	sort.SliceStable(deduped, func(i, j int) bool {
		if deduped[i].Score == deduped[j].Score {
			return deduped[i].Title < deduped[j].Title
		}
		return deduped[i].Score > deduped[j].Score
	})
	if opts.MaxResults > 0 && len(deduped) > opts.MaxResults {
		return deduped[:opts.MaxResults]
	}
	return deduped
}

func filterSearchResults(results []SearchResult, opts SearchOptions) []SearchResult {
	if len(opts.IncludeDomains) == 0 && len(opts.ExcludeDomains) == 0 {
		return append([]SearchResult{}, results...)
	}

	include := normalizeDomains(opts.IncludeDomains)
	exclude := normalizeDomains(opts.ExcludeDomains)
	var filtered []SearchResult
	for _, result := range results {
		host := searchResultHost(result.URL)
		if len(include) > 0 {
			matched := false
			for _, domain := range include {
				if hostMatchesDomain(host, domain) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		skip := false
		for _, domain := range exclude {
			if hostMatchesDomain(host, domain) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

func normalizeDomains(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		domain = strings.TrimPrefix(domain, "https://")
		domain = strings.TrimPrefix(domain, "http://")
		domain = strings.TrimPrefix(domain, "www.")
		domain = strings.TrimSuffix(domain, "/")
		if domain != "" {
			out = append(out, domain)
		}
	}
	return out
}

func searchResultHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func hostMatchesDomain(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	domain = strings.ToLower(strings.TrimSpace(domain))
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func dedupeSearchResults(results []SearchResult) []SearchResult {
	byKey := make(map[string]SearchResult, len(results))
	order := make([]string, 0, len(results))
	for _, result := range results {
		key := canonicalizeSearchKey(result)
		if existing, ok := byKey[key]; ok {
			if result.Score > existing.Score || len(result.Snippet) > len(existing.Snippet) {
				byKey[key] = mergeSearchResults(existing, result)
			}
			continue
		}
		byKey[key] = result
		order = append(order, key)
	}
	out := make([]SearchResult, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	return out
}

func canonicalizeSearchKey(result SearchResult) string {
	if parsed, err := url.Parse(result.URL); err == nil && parsed.Host != "" {
		parsed.Fragment = ""
		if parsed.RawQuery == "" {
			return strings.ToLower(parsed.String())
		}
	}
	return strings.ToLower(strings.TrimSpace(result.Title + "::" + result.Snippet))
}

func mergeSearchResults(left, right SearchResult) SearchResult {
	merged := left
	if len(right.Snippet) > len(merged.Snippet) {
		merged.Snippet = right.Snippet
	}
	if merged.URL == "" {
		merged.URL = right.URL
	}
	if merged.Source == "" {
		merged.Source = right.Source
	}
	if merged.PublishedAt == "" {
		merged.PublishedAt = right.PublishedAt
	}
	if right.Score > merged.Score {
		merged.Score = right.Score
	}
	return merged
}

func assignSearchScores(query string, results []SearchResult, priority []SearchProvider) {
	priorityWeight := make(map[SearchProvider]float64, len(priority))
	for i, provider := range priority {
		priorityWeight[provider] = float64(len(priority)-i) * 0.15
	}

	terms := tokenizeQuery(query)
	for i := range results {
		base := priorityWeight[results[i].Provider]
		if results[i].Score > 0 {
			base += results[i].Score
		}
		base += textMatchScore(terms, results[i].Title) * 2
		base += textMatchScore(terms, results[i].Snippet)
		results[i].Score = base
	}
}

func tokenizeQuery(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	var out []string
	for _, field := range fields {
		if len(field) >= 3 {
			out = append(out, field)
		}
	}
	return out
}

func textMatchScore(terms []string, text string) float64 {
	if len(terms) == 0 {
		return 0
	}
	text = strings.ToLower(text)
	var matches float64
	for _, term := range terms {
		if strings.Contains(text, term) {
			matches++
		}
	}
	return matches / float64(len(terms))
}

func formatSearchRun(query string, run SearchRun) string {
	if len(run.Results) == 0 {
		return fmt.Sprintf("No results found for query: %s", query)
	}

	var b strings.Builder
	b.WriteString("# Search Results\n\n")
	b.WriteString(fmt.Sprintf("Query: %s\n", query))
	if len(run.Providers) > 0 {
		var providers []string
		for _, provider := range run.Providers {
			providers = append(providers, string(provider))
		}
		b.WriteString(fmt.Sprintf("Providers: %s\n", strings.Join(providers, ", ")))
	}
	b.WriteString("\n")

	for i, result := range run.Results {
		fmt.Fprintf(&b, "## %d. %s\n\n", i+1, result.Title)
		fmt.Fprintf(&b, "- Provider: %s\n", result.Provider)
		if result.Source != "" {
			fmt.Fprintf(&b, "- Source: %s\n", result.Source)
		}
		if result.URL != "" {
			fmt.Fprintf(&b, "- URL: %s\n", result.URL)
		}
		if result.PublishedAt != "" {
			fmt.Fprintf(&b, "- Published: %s\n", result.PublishedAt)
		}
		fmt.Fprintf(&b, "- Score: %.2f\n\n", result.Score)
		if snippet := strings.TrimSpace(result.Snippet); snippet != "" {
			b.WriteString(snippet)
			b.WriteString("\n\n")
		}
		if i < len(run.Results)-1 {
			b.WriteString("---\n\n")
		}
	}

	return b.String()
}

func (r *ToolRegistry) registerSearchTools() {
	manager := NewSearchManager(r.cfg)

	r.RegisterTool(newGenericSearchTool(manager, r))
	r.RegisterTool(newProviderSearchTool("duckduckgo_search", "Search DuckDuckGo directly.", SearchProviderDuckDuckGo, manager, r))
	r.RegisterTool(newProviderSearchTool("google_search", "Search Google Custom Search directly.", SearchProviderGoogle, manager, r))
	r.RegisterTool(newProviderSearchTool("tavily_search", "Search Tavily directly.", SearchProviderTavily, manager, r))
	r.RegisterTool(newProviderSearchTool("traversaal_search", "Search Traversaal directly.", SearchProviderTraversaal, manager, r))
	r.RegisterTool(newProviderSearchTool("perplexity_search", "Search Perplexity directly.", SearchProviderPerplexity, manager, r))
	r.RegisterTool(newProviderSearchTool("searxng_search", "Search Searxng directly.", SearchProviderSearxng, manager, r))
	r.RegisterTool(newSploitusSearchTool(manager, r))
	r.RegisterTool(newBrowserTool(r.cfg, r))
}

type searchTool struct {
	name        string
	description string
	manager     *SearchManager
	provider    SearchProvider
	def         provider.ToolDef
	parse       func(json.RawMessage) (string, string, SearchOptions, error)
	registry    *ToolRegistry
}

func (t *searchTool) Name() string { return t.name }

func (t *searchTool) Definition() provider.ToolDef { return t.def }

func (t *searchTool) Binary() string { return "" }

func (t *searchTool) IsAvailable() bool {
	if t.provider == SearchProviderAuto {
		return t.manager != nil && t.manager.anyAvailable()
	}
	return t.manager != nil && t.manager.providerAvailable(t.provider)
}

func (t *searchTool) Handle(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	query, overrideProvider, opts, err := t.parse(rawArgs)
	if err != nil {
		return "", err
	}

	providerName := string(t.provider)
	if t.provider == SearchProviderAuto && overrideProvider != "" {
		providerName = overrideProvider
	}

	run, err := t.manager.Search(ctx, query, providerName, opts)
	if err != nil && len(run.Results) == 0 {
		if t.registry != nil {
			t.registry.logSearch(t.name, providerName, query, "", 0, err.Error(), map[string]interface{}{
				"max_results": opts.MaxResults,
				"recency":     opts.Recency,
			})
		}
		return "", err
	}

	result := formatSearchRun(query, run)
	if t.registry != nil {
		usedProviders := make([]string, 0, len(run.Providers))
		for _, p := range run.Providers {
			usedProviders = append(usedProviders, string(p))
		}
		t.registry.logSearch(t.name, providerName, query, "", len(run.Results), truncateRunes(result, 1500), map[string]interface{}{
			"requested_provider": providerName,
			"used_providers":     usedProviders,
			"max_results":        opts.MaxResults,
			"recency":            opts.Recency,
		})
	}
	if err != nil {
		result += "\nWarnings:\n- " + err.Error()
	}
	return result, nil
}

func newGenericSearchTool(manager *SearchManager, registry *ToolRegistry) Tool {
	return &searchTool{
		name:        "web_search",
		description: "Search the web using the configured provider priority or a specific provider.",
		manager:     manager,
		provider:    SearchProviderAuto,
		def:         webSearchDef(),
		parse:       parseWebSearchArgs,
		registry:    registry,
	}
}

func newProviderSearchTool(name, description string, providerName SearchProvider, manager *SearchManager, registry *ToolRegistry) Tool {
	return &searchTool{
		name:        name,
		description: description,
		manager:     manager,
		provider:    providerName,
		def:         providerSearchDef(name, description),
		parse:       parseProviderSearchArgs,
		registry:    registry,
	}
}

func newSploitusSearchTool(manager *SearchManager, registry *ToolRegistry) Tool {
	return &searchTool{
		name:        "sploitus_search",
		description: "Search Sploitus for public exploits, proof-of-concepts, and offensive tooling.",
		manager:     manager,
		provider:    SearchProviderSploitus,
		def:         sploitusSearchDef(),
		parse:       parseSploitusSearchArgs,
		registry:    registry,
	}
}

func webSearchDef() provider.ToolDef {
	return toolSchema("web_search",
		"Search the web using the configured provider priority or a specific search backend.",
		map[string]interface{}{
			"query":           prop("string", "Search query"),
			"provider":        propEnum("Search provider", []string{"auto", "duckduckgo", "google", "tavily", "traversaal", "perplexity", "sploitus", "searxng"}),
			"max_results":     prop("integer", "Maximum number of ranked results to return"),
			"include_domains": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional domains to include"},
			"exclude_domains": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional domains to exclude"},
			"recency":         propEnum("Optional recency filter", []string{"day", "week", "month", "year"}),
			"message":         prop("string", "Brief purpose of the search"),
		},
		[]string{"query", "message"},
	)
}

func providerSearchDef(name, description string) provider.ToolDef {
	return toolSchema(name,
		description,
		map[string]interface{}{
			"query":           prop("string", "Search query"),
			"max_results":     prop("integer", "Maximum number of results to return"),
			"include_domains": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional domains to include"},
			"exclude_domains": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional domains to exclude"},
			"recency":         propEnum("Optional recency filter", []string{"day", "week", "month", "year"}),
			"message":         prop("string", "Brief purpose of the search"),
		},
		[]string{"query", "message"},
	)
}

func sploitusSearchDef() provider.ToolDef {
	return toolSchema("sploitus_search",
		"Search Sploitus for exploits or offensive tools.",
		map[string]interface{}{
			"query":       prop("string", "Search query"),
			"category":    propEnum("Sploitus search category", []string{"exploits", "tools"}),
			"sort":        propEnum("Sort order", []string{"default", "date", "score"}),
			"max_results": prop("integer", "Maximum number of results to return"),
			"message":     prop("string", "Brief purpose of the search"),
		},
		[]string{"query", "message"},
	)
}

func parseWebSearchArgs(rawArgs json.RawMessage) (string, string, SearchOptions, error) {
	var args WebSearchArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", "", SearchOptions{}, fmt.Errorf("parse web_search args: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", "", SearchOptions{}, fmt.Errorf("web_search query is required")
	}
	if strings.TrimSpace(args.Provider) != "" && normalizeProvider(args.Provider) == "" {
		return "", "", SearchOptions{}, fmt.Errorf("unknown web_search provider %q", args.Provider)
	}
	return args.Query, args.Provider, SearchOptions{
		MaxResults:     args.MaxResults.Int(),
		IncludeDomains: args.IncludeDomains,
		ExcludeDomains: args.ExcludeDomains,
		Recency:        args.Recency,
	}, nil
}

func parseProviderSearchArgs(rawArgs json.RawMessage) (string, string, SearchOptions, error) {
	var args ProviderSearchArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", "", SearchOptions{}, fmt.Errorf("parse provider search args: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", "", SearchOptions{}, fmt.Errorf("search query is required")
	}
	return args.Query, "", SearchOptions{
		MaxResults:     args.MaxResults.Int(),
		IncludeDomains: args.IncludeDomains,
		ExcludeDomains: args.ExcludeDomains,
		Recency:        args.Recency,
	}, nil
}

func parseSploitusSearchArgs(rawArgs json.RawMessage) (string, string, SearchOptions, error) {
	var args SploitusSearchArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", "", SearchOptions{}, fmt.Errorf("parse sploitus_search args: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", "", SearchOptions{}, fmt.Errorf("sploitus_search query is required")
	}
	return args.Query, "", SearchOptions{
		MaxResults: args.MaxResults.Int(),
		Category:   args.Category,
		Sort:       args.Sort,
	}, nil
}

type browserTool struct {
	def       provider.ToolDef
	client    *http.Client
	userAgent string
	registry  *ToolRegistry
	scraper   *scraperClient
}

func newBrowserTool(cfg *config.Config, registry *ToolRegistry) Tool {
	timeout := 30 * time.Second
	userAgent := "Sentrix/0.7"
	if cfg != nil {
		if cfg.Search.TimeoutSeconds > 0 {
			timeout = time.Duration(cfg.Search.TimeoutSeconds) * time.Second
		}
		if cfg.Search.BrowserUserAgent != "" {
			userAgent = cfg.Search.BrowserUserAgent
		}
	}

	var sc *scraperClient
	if cfg != nil {
		sc = newScraperClient(cfg.Scraper, timeout)
		if !sc.enabled() {
			sc = nil
		}
	}

	return &browserTool{
		def:       browserDef(),
		client:    &http.Client{Timeout: timeout},
		userAgent: userAgent,
		registry:  registry,
		scraper:   sc,
	}
}

func (b *browserTool) Name() string { return "browser" }

func (b *browserTool) Definition() provider.ToolDef { return b.def }

func (b *browserTool) Binary() string { return "" }

func (b *browserTool) IsAvailable() bool { return true }

func browserDef() provider.ToolDef {
	return toolSchema("browser",
		"Fetch a web page and return markdown-like text, raw HTML, or extracted links.",
		map[string]interface{}{
			"url":     prop("string", "URL to open"),
			"action":  propEnum("Browser action", []string{"markdown", "html", "links"}),
			"message": prop("string", "Brief purpose of the fetch"),
		},
		[]string{"url", "action", "message"},
	)
}

func (b *browserTool) Handle(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args BrowserArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("parse browser args: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" {
		return "", fmt.Errorf("browser url is required")
	}
	if strings.TrimSpace(args.Action) == "" {
		args.Action = browserActionMarkdown
	}

	pageURL, err := ensureHTTPURL(args.URL)
	if err != nil {
		return "", err
	}

	// Dual-mode: scraper if configured, native HTTP fallback otherwise.
	if b.scraper != nil {
		return b.handleScraper(ctx, args, pageURL.String())
	}
	return b.handleNative(ctx, args, pageURL)
}

// handleScraper runs content fetch and screenshot capture in parallel via the scraper backend.
func (b *browserTool) handleScraper(ctx context.Context, args BrowserArgs, targetURL string) (string, error) {
	action := strings.ToLower(args.Action)
	mode := "public"
	if isPrivateTarget(targetURL) {
		mode = "private"
	}
	logScraperMode(targetURL, mode)

	type contentResult struct {
		data []byte
		err  error
	}
	type screenshotResult struct {
		data []byte
		err  error
	}

	contentCh := make(chan contentResult, 1)
	screenshotCh := make(chan screenshotResult, 1)

	// Fetch content and screenshot in parallel.
	go func() {
		data, err := b.scraper.fetchContent(ctx, targetURL, action)
		contentCh <- contentResult{data, err}
	}()
	go func() {
		data, err := b.scraper.fetchScreenshot(ctx, targetURL)
		screenshotCh <- screenshotResult{data, err}
	}()

	cr := <-contentCh
	sr := <-screenshotCh

	// Content fetch is required for success.
	if cr.err != nil {
		return "", fmt.Errorf("scraper content fetch: %w", cr.err)
	}

	// Screenshot failure is non-fatal.
	var screenshotData []byte
	if sr.err != nil {
		log.WithError(sr.err).Warn("browser: screenshot capture failed, continuing with content only")
	} else {
		screenshotData = sr.data
	}

	// Process content based on action.
	var result string
	var resultCount int
	switch action {
	case browserActionHTML:
		result = string(cr.data)
		resultCount = 1
	case browserActionLinks:
		result, resultCount = formatScraperLinks(targetURL, cr.data)
	case browserActionMarkdown:
		result = string(cr.data)
		resultCount = 1
	default:
		return "", fmt.Errorf("unsupported browser action %q", args.Action)
	}

	// Persist screenshot artifact if we have data and a registry with flow context.
	var screenshotArtifactID string
	var screenshotFilePath string
	if len(screenshotData) > 0 && b.registry != nil {
		aid, fp := b.registry.persistScreenshot(ctx, targetURL, args.Action, screenshotData)
		screenshotArtifactID = aid
		screenshotFilePath = fp
	}

	// Log the search with scraper metadata.
	b.logScraper(args, targetURL, result, resultCount, mode, screenshotArtifactID, screenshotFilePath)

	return result, nil
}

// handleNative runs the original HTTP fetch-based browser behavior.
func (b *browserTool) handleNative(ctx context.Context, args BrowserArgs, pageURL *url.URL) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create browser request: %w", err)
	}
	req.Header.Set("User-Agent", b.userAgent)

	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read page body: %w", err)
	}

	switch strings.ToLower(args.Action) {
	case browserActionHTML:
		result := string(body)
		b.logNative(args, pageURL.String(), result, 1)
		return result, nil
	case browserActionLinks:
		result, err := formatBrowserLinks(pageURL, body)
		if err == nil {
			b.logNative(args, pageURL.String(), result, countBrowserLinks(result))
		}
		return result, err
	case browserActionMarkdown:
		result, err := renderBrowserMarkdown(pageURL, resp.Header.Get("Content-Type"), body)
		if err == nil {
			b.logNative(args, pageURL.String(), result, 1)
		}
		return result, err
	default:
		return "", fmt.Errorf("unsupported browser action %q", args.Action)
	}
}

func (b *browserTool) logNative(args BrowserArgs, target, result string, resultCount int) {
	if b.registry == nil {
		return
	}
	b.registry.logSearch("browser", "native", "", target, resultCount, truncateRunes(result, 1500), map[string]interface{}{
		"mode":       "native",
		"action":     args.Action,
		"target_url": target,
	})
}

func (b *browserTool) logScraper(args BrowserArgs, target, result string, resultCount int, mode, screenshotArtifactID, screenshotFilePath string) {
	if b.registry == nil {
		return
	}
	meta := map[string]interface{}{
		"mode":       mode,
		"action":     args.Action,
		"target_url": target,
	}
	if screenshotArtifactID != "" {
		meta["screenshot_artifact_id"] = screenshotArtifactID
	}
	if screenshotFilePath != "" {
		meta["screenshot_file_path"] = screenshotFilePath
	}
	b.registry.logSearch("browser", "scraper", "", target, resultCount, truncateRunes(result, 1500), meta)
}

func countBrowserLinks(result string) int {
	count := 0
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 2 && unicode.IsDigit(rune(line[0])) && strings.Contains(line, "](") {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

func ensureHTTPURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid url %q", rawURL)
	}
	return parsed, nil
}

func formatBrowserLinks(pageURL *url.URL, body []byte) (string, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("parse html links: %w", err)
	}

	type linkItem struct {
		Title string
		URL   string
	}
	var links []linkItem
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			href := attrValue(node, "href")
			if href != "" {
				target := resolveLink(pageURL, href)
				title := normalizeSpace(nodeText(node))
				if title == "" {
					title = target
				}
				links = append(links, linkItem{Title: title, URL: target})
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	seen := make(map[string]struct{}, len(links))
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Links from %s\n\n", pageURL.String()))
	count := 0
	for _, link := range links {
		if _, ok := seen[link.URL]; ok {
			continue
		}
		seen[link.URL] = struct{}{}
		count++
		fmt.Fprintf(&b, "%d. [%s](%s)\n", count, link.Title, link.URL)
		if count == 100 {
			break
		}
	}
	if count == 0 {
		b.WriteString("No links found.\n")
	}
	return b.String(), nil
}

func renderBrowserMarkdown(pageURL *url.URL, contentType string, body []byte) (string, error) {
	if !strings.Contains(strings.ToLower(contentType), "html") {
		return fmt.Sprintf("# %s\n\n```text\n%s\n```", pageURL.String(), truncateRunes(string(body), 12000)), nil
	}

	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("parse html content: %w", err)
	}

	title := extractTitle(doc)
	text := normalizeSpace(extractReadableText(doc))
	if title == "" {
		title = pageURL.String()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "URL: %s\n\n", pageURL.String())
	if text == "" {
		b.WriteString("No readable page text found.\n")
		return b.String(), nil
	}

	b.WriteString(text)
	b.WriteString("\n")
	return b.String(), nil
}

func extractTitle(doc *html.Node) string {
	var title string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if title != "" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "title" {
			title = normalizeSpace(nodeText(node))
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return title
}

func extractReadableText(doc *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "noscript":
				return
			case "p", "article", "section", "main", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				text := normalizeSpace(nodeText(node))
				if text != "" {
					parts = append(parts, text)
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	if len(parts) == 0 {
		text := normalizeSpace(nodeText(doc))
		return truncateRunes(text, 12000)
	}

	return truncateRunes(strings.Join(uniqueStrings(parts, 80), "\n\n"), 12000)
}

func nodeText(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(nodeText(child))
		if child.Type == html.ElementNode && (child.Data == "p" || child.Data == "br" || child.Data == "li") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func normalizeSpace(text string) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.TrimSpace(text)
	return strings.Join(strings.Fields(text), " ")
}

func attrValue(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func resolveLink(base *url.URL, href string) string {
	parsed, err := url.Parse(strings.TrimSpace(href))
	if err != nil || href == "" {
		return href
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	parsed.Fragment = ""
	return parsed.String()
}
