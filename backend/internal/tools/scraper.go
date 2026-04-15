package tools

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/yourorg/sentrix/internal/config"
)

// scraperClient wraps HTTP calls to a scraper-compatible API.
type scraperClient struct {
	publicURL  string
	privateURL string
	client     *http.Client
}

func newScraperClient(cfg config.ScraperConfig, timeout time.Duration) *scraperClient {
	return &scraperClient{
		publicURL:  strings.TrimRight(cfg.PublicURL, "/"),
		privateURL: strings.TrimRight(cfg.PrivateURL, "/"),
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// enabled returns true when at least one scraper URL is configured.
func (sc *scraperClient) enabled() bool {
	return sc.publicURL != "" || sc.privateURL != ""
}

// scraperURLForTarget returns the best scraper base URL for the given target URL.
// Private targets prefer the private scraper; public targets prefer the public one.
func (sc *scraperClient) scraperURLForTarget(targetURL string) string {
	if isPrivateTarget(targetURL) {
		if sc.privateURL != "" {
			return sc.privateURL
		}
		return sc.publicURL
	}
	if sc.publicURL != "" {
		return sc.publicURL
	}
	return sc.privateURL
}

// fetchContent calls the scraper content endpoint (markdown, html, or links).
func (sc *scraperClient) fetchContent(ctx context.Context, targetURL, action string) ([]byte, error) {
	base := sc.scraperURLForTarget(targetURL)
	if base == "" {
		return nil, fmt.Errorf("no scraper URL configured")
	}

	endpoint := fmt.Sprintf("%s/%s?url=%s", base, action, url.QueryEscape(targetURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create scraper request: %w", err)
	}

	resp, err := sc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraper fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scraper returned %d: %s", resp.StatusCode, truncateRunes(string(body), 200))
	}

	return io.ReadAll(resp.Body)
}

// fetchScreenshot calls the scraper screenshot endpoint and returns the PNG bytes.
func (sc *scraperClient) fetchScreenshot(ctx context.Context, targetURL string) ([]byte, error) {
	base := sc.scraperURLForTarget(targetURL)
	if base == "" {
		return nil, fmt.Errorf("no scraper URL configured")
	}

	endpoint := fmt.Sprintf("%s/screenshot?url=%s&fullPage=true", base, url.QueryEscape(targetURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create screenshot request: %w", err)
	}

	resp, err := sc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("screenshot fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("screenshot returned %d: %s", resp.StatusCode, truncateRunes(string(body), 200))
	}

	return io.ReadAll(resp.Body)
}

// scraperLinksResponse represents a link from the scraper JSON output.
type scraperLinksResponse struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// formatScraperLinks converts scraper JSON link output to Sentrix's human-readable list.
func formatScraperLinks(targetURL string, data []byte) (string, int) {
	var links []scraperLinksResponse
	if err := json.Unmarshal(data, &links); err != nil {
		// If not JSON, return raw output.
		return string(data), 1
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Links from %s\n\n", targetURL))
	seen := make(map[string]struct{}, len(links))
	count := 0
	for _, link := range links {
		if link.URL == "" {
			continue
		}
		if _, ok := seen[link.URL]; ok {
			continue
		}
		seen[link.URL] = struct{}{}
		count++
		title := link.Title
		if title == "" {
			title = link.Text
		}
		if title == "" {
			title = link.URL
		}
		fmt.Fprintf(&b, "%d. [%s](%s)\n", count, title, link.URL)
		if count >= 100 {
			break
		}
	}
	if count == 0 {
		b.WriteString("No links found.\n")
	}
	return b.String(), count
}

// isPrivateTarget classifies a URL as targeting a private/internal host.
// Rules: loopback, RFC1918, localhost, no-dot hostnames, private suffixes.
func isPrivateTarget(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return false
	}

	// Localhost.
	if strings.EqualFold(hostname, "localhost") {
		return true
	}

	// Hostnames without a dot (single-label) are treated as internal.
	if !strings.Contains(hostname, ".") {
		return true
	}

	// Private suffixes.
	lower := strings.ToLower(hostname)
	privateSuffixes := []string{".local", ".internal", ".test", ".localhost", ".lan", ".home", ".corp"}
	for _, suffix := range privateSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}

	// IP address checks.
	ip := net.ParseIP(hostname)
	if ip == nil {
		return false
	}

	// Loopback.
	if ip.IsLoopback() {
		return true
	}

	// Link-local.
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// RFC1918 private ranges.
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"fc00::/7",
	}
	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// redactScraperURL strips credentials from a scraper URL for safe logging.
func redactScraperURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	if parsed.User != nil {
		parsed.User = url.User("***")
	}
	return parsed.String()
}

// logScraperMode logs scraper selection at debug level without exposing credentials.
func logScraperMode(targetURL, mode string) {
	log.WithFields(log.Fields{
		"tool":       "browser",
		"provider":   "scraper",
		"mode":       mode,
		"target_url": targetURL,
	}).Debug("browser: scraper mode selected")
}
