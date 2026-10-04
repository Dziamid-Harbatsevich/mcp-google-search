package websearch

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const googleDefaultBaseURL = "https://www.google.com"

// containerSelectors lists the CSS classes Google has used to wrap organic
// results over time. Selectors are tried in order and results are de-duplicated
// by URL, so it is safe for them to overlap.
var googleContainers = []string{
	".MjjYud", // general result wrapper (2025)
	".tF2Cxc", // result block (2021-2025)
	".g",      // long-lived result class (retired March 2025, kept as fallback)
	".hlcw0c", // top result with sitelinks
	".wHYlTd", // replacement for ".g" from March 2025
	".sATSHe", // alternate result wrapper (2025)
}

// googleSnippets lists, in priority order, elements that hold a description.
var googleSnippets = []string{".VwiC3b", ".IsZvec", ".aCOpRe"}

// googleEngine scrapes the Google web results page.
type googleEngine struct{}

func (googleEngine) Name() string           { return EngineGoogle }
func (googleEngine) DefaultBaseURL() string { return googleDefaultBaseURL }

// NeedsJavaScript reports true: Google answers plain HTTP clients with a
// bootstrap shell and renders organic results only in the browser.
func (googleEngine) NeedsJavaScript() bool { return true }

// SearchURL builds a Google /search URL.
func (googleEngine) SearchURL(base string, req Request) string {
	params := url.Values{}
	params.Set("q", req.Query)
	if n := clampNum(req.Num); n > 0 {
		params.Set("num", strconv.Itoa(n))
	}
	if req.Start > 0 {
		params.Set("start", strconv.Itoa(req.Start))
	}
	hl := req.Language
	if hl == "" {
		hl = "en"
	}
	params.Set("hl", hl)
	if req.Country != "" {
		params.Set("gl", req.Country)
	}
	if req.Safe != "" {
		params.Set("safe", req.Safe)
	}
	return base + "/search?" + params.Encode()
}

// IsBlocked reports whether Google served a CAPTCHA / rate-limit page.
func (googleEngine) IsBlocked(doc *goquery.Document) bool {
	if doc.Find("#captcha-form, form#captcha-form, #recaptcha, .g-recaptcha").Length() > 0 {
		return true
	}
	text := strings.ToLower(doc.Text())
	return strings.Contains(text, "unusual traffic") ||
		strings.Contains(text, "our systems have detected")
}

// Parse extracts organic results from a Google results page.
func (googleEngine) Parse(doc *goquery.Document) []Result {
	seen := make(map[string]bool)
	var results []Result

	add := func(title, link, snippet string) {
		link = normalizeGoogleURL(link)
		title = cleanText(title)
		if title == "" || link == "" || seen[link] {
			return
		}
		seen[link] = true
		results = append(results, Result{Title: title, URL: link, Snippet: cleanText(snippet)})
	}

	for _, selector := range googleContainers {
		doc.Find(selector).Each(func(_ int, block *goquery.Selection) {
			title := block.Find("h3").First().Text()
			link, _ := block.Find("a[href]").First().Attr("href")
			add(title, link, firstText(block, googleSnippets...))
		})
	}

	// Structural fallback: if the known containers are gone, treat any anchor
	// that wraps a heading as a result. This survives class renames.
	if len(results) == 0 {
		doc.Find("a").Each(func(_ int, anchor *goquery.Selection) {
			heading := anchor.Find("h3").First()
			if heading.Length() == 0 {
				return
			}
			link, _ := anchor.Attr("href")
			add(heading.Text(), link, firstText(anchor.Parent(), googleSnippets...))
		})
	}

	return results
}

// normalizeGoogleURL resolves Google's "/url?q=<target>" redirect wrapper and
// drops Google-internal links.
func normalizeGoogleURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "/url?") || strings.HasPrefix(raw, "/url/") {
		if parsed, err := url.Parse(raw); err == nil {
			if target := parsed.Query().Get("q"); target != "" {
				raw = target
			} else if target := parsed.Query().Get("url"); target != "" {
				raw = target
			}
		}
	}

	normalized := normalizeURL(raw)
	if normalized == "" {
		return ""
	}
	if isInternalHost(hostOf(normalized), "google.com") {
		return ""
	}
	return normalized
}
