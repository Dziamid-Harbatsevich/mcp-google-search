// Package websearch implements a small, dependency-light web search client
// that scrapes search-engine results pages.
//
// It needs no API key, which means it is subject to the engines' rate limiting
// and to changes in their markup. Every CSS selector therefore lives in the
// per-engine files (google.go, bing.go), each of which also provides a purely
// structural fallback so a class rename does not break parsing.
//
// Google currently serves a JavaScript-only "enable JS" shell to plain HTTP
// clients, so a Client may be configured with an ordered list of engines: the
// first engine that yields results wins. See New.
package websearch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/time/rate"
)

const (
	// DefaultUserAgent mimics a current desktop Chrome build.
	DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

	// DefaultTimeout bounds a single HTTP request.
	DefaultTimeout = 15 * time.Second

	// DefaultMinInterval is the minimum spacing between consecutive requests.
	DefaultMinInterval = time.Second

	// maxResults caps how many results we ask for per page.
	maxResults = 10
)

// ErrBlocked is returned when an engine answers with a rate-limit or CAPTCHA
// page instead of results.
var ErrBlocked = errors.New("search engine blocked the request (rate limit or CAPTCHA); reduce request frequency and try again later")

// Result is a single organic search result.
type Result struct {
	Rank    int    `json:"rank"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Source  string `json:"source,omitempty"`
}

// Request describes a web search.
type Request struct {
	Query    string // required
	Num      int    // results per page (1-10); zero means the engine default
	Start    int    // index of the first result, for pagination
	Language string // result language, e.g. "en"
	Country  string // country code, e.g. "us"
	Safe     string // SafeSearch: "off" or "active"
}

// Engine scrapes a single search engine's results page.
type Engine interface {
	// Name returns the engine's short identifier, e.g. "google".
	Name() string
	// DefaultBaseURL returns the engine's default base URL.
	DefaultBaseURL() string
	// SearchURL builds the results URL for a request, given a base URL.
	SearchURL(baseURL string, req Request) string
	// Parse extracts organic results from a results page.
	Parse(doc *goquery.Document) []Result
	// IsBlocked reports whether the page is a rate-limit/CAPTCHA page.
	IsBlocked(doc *goquery.Document) bool
}

// Client performs web searches over HTTP using one or more engines.
type Client struct {
	engines    []Engine
	baseURL    string
	userAgent  string
	httpClient *http.Client
	limiter    *rate.Limiter
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the base URL used for every engine. It is primarily
// useful for pointing tests at a local server.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.baseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

// WithUserAgent overrides the User-Agent header sent with each request.
func WithUserAgent(userAgent string) Option {
	return func(c *Client) {
		if userAgent != "" {
			c.userAgent = userAgent
		}
	}
}

// WithHTTPClient overrides the underlying HTTP client (and its timeout).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithMinInterval sets the minimum spacing enforced between consecutive
// requests. Non-positive values keep the default.
func WithMinInterval(d time.Duration) Option {
	return func(c *Client) {
		if d <= 0 {
			d = DefaultMinInterval
		}
		c.limiter = rate.NewLimiter(rate.Every(d), 1)
	}
}

// New creates a Client that tries the given engines in order, returning the
// first engine's non-empty result set. Engine names are case-insensitive; see
// EngineByName for the supported values.
func New(engineNames []string, opts ...Option) (*Client, error) {
	c := &Client{
		userAgent:  DefaultUserAgent,
		httpClient: &http.Client{Timeout: DefaultTimeout},
		limiter:    rate.NewLimiter(rate.Every(DefaultMinInterval), 1),
	}

	for _, name := range engineNames {
		engine, ok := EngineByName(name)
		if !ok {
			return nil, fmt.Errorf("unknown search engine %q (supported: %s)", name, strings.Join(EngineNames(), ", "))
		}
		c.engines = append(c.engines, engine)
	}
	if len(c.engines) == 0 {
		return nil, errors.New("no search engines configured")
	}

	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Engines returns the names of the configured engines, in order.
func (c *Client) Engines() []string {
	names := make([]string, 0, len(c.engines))
	for _, engine := range c.engines {
		names = append(names, engine.Name())
	}
	return names
}

// Search runs the query against each configured engine until one returns
// results. An empty result set (no matches) is not an error.
func (c *Client) Search(ctx context.Context, req Request) ([]Result, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("query must not be empty")
	}

	var firstErr error
	for _, engine := range c.engines {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("waiting for rate limiter: %w", err)
		}

		base := engine.DefaultBaseURL()
		if c.baseURL != "" {
			base = c.baseURL
		}

		results, err := c.search(ctx, engine, base, req)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(results) > 0 {
			return results, nil
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	return nil, nil
}

// search performs a single request/parse cycle for one engine.
func (c *Client) search(ctx context.Context, engine Engine, base string, req Request) ([]Result, error) {
	body, err := c.fetch(ctx, engine.SearchURL(base, req))
	if err != nil {
		return nil, err
	}
	defer body.Close()

	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("parse %s results: %w", engine.Name(), err)
	}
	if engine.IsBlocked(doc) {
		return nil, fmt.Errorf("%s: %w", engine.Name(), ErrBlocked)
	}

	results := engine.Parse(doc)
	for i := range results {
		results[i].Rank = i + 1
		results[i].Source = engine.Name()
	}
	return results, nil
}

// fetch issues the HTTP request and returns the response body for the caller
// to close.
func (c *Client) fetch(ctx context.Context, requestURL string) (io.ReadCloser, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("User-Agent", c.userAgent)
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	httpReq.Header.Set("Accept-Language", "en-US,en;q=0.9")
	// Skips the EU cookie/consent interstitial for most regions.
	httpReq.Header.Set("Cookie", "CONSENT=YES+; gpc=1")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Body, nil
	case http.StatusTooManyRequests:
		resp.Body.Close()
		return nil, ErrBlocked
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("search engine returned HTTP %d", resp.StatusCode)
	}
}

// cleanText collapses whitespace runs into single spaces and trims.
func cleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// firstText returns the cleaned text of the first non-empty matching
// descendant, or "" if none match.
func firstText(s *goquery.Selection, selectors ...string) string {
	for _, selector := range selectors {
		if text := cleanText(s.Find(selector).First().Text()); text != "" {
			return text
		}
	}
	return ""
}

// clampNum returns n clamped to [1, maxResults]. Zero means "engine default"
// and is returned unchanged.
func clampNum(n int) int {
	if n <= 0 {
		return 0
	}
	if n > maxResults {
		return maxResults
	}
	return n
}

// normalizeURL converts a raw href into an absolute http(s) URL, dropping
// engine-internal navigation and "#".
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "#" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return parsed.String()
}

// hostOf returns the lower-cased hostname of raw, or "" if it is not a URL.
func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// isInternalHost reports whether host belongs to domain or a subdomain of it.
func isInternalHost(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}
