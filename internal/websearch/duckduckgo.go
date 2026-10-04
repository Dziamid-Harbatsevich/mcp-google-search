package websearch

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const duckduckgoDefaultBaseURL = "https://html.duckduckgo.com"

// duckduckgoContainers lists the wrappers DuckDuckGo places around each organic
// hit on the no-JavaScript endpoint.
var duckduckgoContainers = []string{
	"div.result.results_links",
	"div.result",
	"div.web-result",
}

// duckduckgoSnippets lists, in priority order, elements holding a description.
var duckduckgoSnippets = []string{
	".result__snippet",
	"td.result__snippet",
	".result__extras__url",
}

// duckduckgoEngine scrapes DuckDuckGo's no-JavaScript HTML endpoint.
//
// Google now answers plain HTTP clients with a JavaScript-only shell, and Bing
// has been observed answering with results unrelated to the query, so this
// engine is the most reliable keyless source and is tried early in the default
// engine order.
type duckduckgoEngine struct{}

func (duckduckgoEngine) Name() string           { return EngineDuckDuckGo }
func (duckduckgoEngine) DefaultBaseURL() string { return duckduckgoDefaultBaseURL }

// NeedsJavaScript reports false: the /html/ endpoint renders results server-side.
func (duckduckgoEngine) NeedsJavaScript() bool { return false }

// SearchURL builds a DuckDuckGo /html/ URL. Pagination uses the "s" offset, in
// steps of duckduckgoPageSize.
func (duckduckgoEngine) SearchURL(base string, req Request) string {
	params := url.Values{}
	params.Set("q", req.Query)
	params.Set("kl", duckduckgoRegion(req))
	if req.Start > 0 {
		params.Set("s", strconv.Itoa(req.Start))
	}
	// "df" restricts by age; SafeSearch has no counterpart on this endpoint and
	// is honoured by the engines that do support it.
	return base + "/html/?" + params.Encode()
}

// duckduckgoRegion maps the language/country pair onto a "kl" region code,
// e.g. ("de", "de") -> "de-de".
func duckduckgoRegion(req Request) string {
	lang := strings.ToLower(strings.TrimSpace(req.Language))
	country := strings.ToLower(strings.TrimSpace(req.Country))
	switch {
	case lang != "" && country != "":
		return lang + "-" + country
	case lang != "":
		return lang + "-" + lang
	default:
		return "wt-wt"
	}
}

// IsBlocked reports whether DuckDuckGo answered with its anti-bot challenge
// instead of results. The challenge is served with HTTP 202 and mentions
// "challenge" in the body, so both signals are checked.
func (duckduckgoEngine) IsBlocked(doc *goquery.Document) bool {
	text := strings.ToLower(cleanText(doc.Text()))
	for _, marker := range []string{
		"challenge to confirm this search was made by a human",
		"bots use duckduckgo too",
		"complete the following challenge",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return doc.Find("form.anomaly-form, #anomaly-form, .anomaly-modal").Length() > 0
}

// Parse extracts organic results from a DuckDuckGo HTML page.
func (duckduckgoEngine) Parse(doc *goquery.Document) []Result {
	seen := make(map[string]bool)
	var results []Result

	add := func(title, link, snippet string) {
		link = normalizeDuckDuckGoURL(link)
		title = cleanText(title)
		if title == "" || link == "" || seen[link] {
			return
		}
		if isInternalHost(hostOf(link), "duckduckgo.com") {
			return
		}
		seen[link] = true
		results = append(results, Result{Title: title, URL: link, Snippet: cleanText(snippet)})
	}

	for _, selector := range duckduckgoContainers {
		doc.Find(selector).Each(func(_ int, block *goquery.Selection) {
			anchor := block.Find("a.result__a").First()
			if anchor.Length() == 0 {
				anchor = block.Find("a[href^='//duckduckgo.com/l/'], a[href*='uddg='], a[href]").First()
			}
			if anchor.Length() == 0 {
				return
			}
			link, _ := anchor.Attr("href")
			add(anchor.Text(), link, firstText(block, duckduckgoSnippets...))
		})
		if len(results) > 0 {
			break
		}
	}

	return results
}

// normalizeDuckDuckGoURL unwraps the "/l/?uddg=<encoded>" redirect wrapper and
// resolves the protocol-relative form ("//duckduckgo.com/...") that this
// endpoint emits.
func normalizeDuckDuckGoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "#" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	// "uddg" is only read on the redirect endpoint; the "kl" param is mirrored
	// onto some result links and must be ignored.
	if isInternalHost(strings.ToLower(parsed.Hostname()), "duckduckgo.com") || strings.HasPrefix(raw, "//duckduckgo.com/") {
		if target := parsed.Query().Get("uddg"); target != "" {
			return normalizeURL(target)
		}
	}

	// Protocol-relative URLs still need a scheme before normalisation.
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	return normalizeURL(raw)
}
