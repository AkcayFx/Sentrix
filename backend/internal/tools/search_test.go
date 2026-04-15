package tools

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

type fakeSearcher struct {
	available bool
	results   []SearchResult
	err       error
}

func (f fakeSearcher) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	return append([]SearchResult{}, f.results...), f.err
}

func (f fakeSearcher) IsAvailable() bool {
	return f.available
}

func TestSearchManagerAutoSearchUsesPriorityAndDedupes(t *testing.T) {
	manager := &SearchManager{
		providers: map[SearchProvider]Searcher{
			SearchProviderDuckDuckGo: fakeSearcher{
				available: true,
				results: []SearchResult{
					{Title: "Open redirect guide", URL: "https://example.com/a", Snippet: "short", Provider: SearchProviderDuckDuckGo},
					{Title: "Another result", URL: "https://example.com/b", Snippet: "details", Provider: SearchProviderDuckDuckGo},
				},
			},
			SearchProviderTavily: fakeSearcher{
				available: true,
				results: []SearchResult{
					{Title: "Open redirect guide", URL: "https://example.com/a", Snippet: "longer snippet with mitigation details", Provider: SearchProviderTavily, Score: 0.8},
					{Title: "Tavily result", URL: "https://example.com/c", Snippet: "query specific content", Provider: SearchProviderTavily, Score: 0.7},
				},
			},
		},
		priority: []SearchProvider{SearchProviderDuckDuckGo, SearchProviderTavily},
	}

	run, err := manager.Search(context.Background(), "open redirect", "", SearchOptions{MaxResults: 3})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(run.Providers) != 2 {
		t.Fatalf("expected 2 providers to be used, got %v", run.Providers)
	}
	if len(run.Results) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(run.Results))
	}
	if run.Results[0].URL != "https://example.com/a" {
		t.Fatalf("expected duplicate URL to be preserved once, got first result %q", run.Results[0].URL)
	}
	if run.Results[0].Snippet != "longer snippet with mitigation details" {
		t.Fatalf("expected duplicate merge to keep richer snippet, got %q", run.Results[0].Snippet)
	}
}

func TestRenderBrowserMarkdown(t *testing.T) {
	pageURL, _ := url.Parse("https://example.com/docs")
	htmlDoc := []byte(`<!doctype html><html><head><title>Example Docs</title></head><body><main><h1>Example Docs</h1><p>First paragraph.</p><p>Second paragraph.</p></main></body></html>`)

	out, err := renderBrowserMarkdown(pageURL, "text/html", htmlDoc)
	if err != nil {
		t.Fatalf("renderBrowserMarkdown returned error: %v", err)
	}

	if !containsAll(out, []string{"# Example Docs", "URL: https://example.com/docs", "First paragraph.", "Second paragraph."}) {
		t.Fatalf("unexpected markdown output: %s", out)
	}
}

func TestFormatBrowserLinksResolvesRelativeLinks(t *testing.T) {
	pageURL, _ := url.Parse("https://example.com/base/")
	htmlDoc := []byte(`<html><body><a href="/docs">Docs</a><a href="https://other.example/x">Other</a></body></html>`)

	out, err := formatBrowserLinks(pageURL, htmlDoc)
	if err != nil {
		t.Fatalf("formatBrowserLinks returned error: %v", err)
	}

	if !containsAll(out, []string{"https://example.com/docs", "https://other.example/x"}) {
		t.Fatalf("unexpected links output: %s", out)
	}
}

func containsAll(haystack string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
