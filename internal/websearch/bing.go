package websearch

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const bingDefaultBaseURL = "https://www.bing.com"

// bingSnippets lists, in priority order, elements that hold a description.
var bingSnippets = []string{".b_caption p", ".b_caption", ".b_snippet", ".b_lineclamp"}

// bingEngine scrapes the Bing web results page.
type bingEngine struct{}

func (bingEngine) Name() string           { return EngineBing }
func (bingEngine) DefaultBaseURL() string { return bingDefaultBaseURL }

// SearchURL builds a Bing /search URL.
func (bingEngine) SearchURL(base string, req Request) string {
	params := url.Values{}
	params.Set("q", req.Query)
	params.Set("form", "QBLH")
	if n := clampNum(req.Num); n > 0 {
		params.Set("count", strconv.Itoa(n))
	}
	if req.Start > 0 {
		params.Set("first", strconv.Itoa(req.Start+1)) // Bing's "first" is 1-based
	}
	if req.Language != "" {
		params.Set("setlang", req.Language)
	}
	if req.Country != "" {
		params.Set("cc", req.Country)
	}
	if req.Safe != "" {
		params.Set("adlt", req.Safe)
	}
	return base + "/search?" + params.Encode()
}

// IsBlocked reports whether Bing served a CAPTCHA page.
func (bingEngine) IsBlocked(doc *goquery.Document) bool {
	title := strings.ToLower(cleanText(doc.Find("title").First().Text()))
	if strings.Contains(title, "captcha") {
		return true
	}
	return doc.Find("#b_captcha, .b_captcha").Length() > 0
}

// Parse extracts organic results from a Bing results page.
func (bingEngine) Parse(doc *goquery.Document) []Result {
	seen := make(map[string]bool)
	var results []Result

	add := func(title, link, snippet string) {
		link = decodeBingURL(link)
		title = cleanText(title)
		if title == "" || link == "" || seen[link] || isInternalHost(hostOf(link), "bing.com") {
			return
		}
		seen[link] = true
		results = append(results, Result{Title: title, URL: link, Snippet: cleanText(snippet)})
	}

	doc.Find("li.b_algo").Each(func(_ int, block *goquery.Selection) {
		title := block.Find("h2 a").First().Text()
		if strings.TrimSpace(title) == "" {
			title = block.Find("h2").First().Text()
		}
		link, _ := block.Find("h2 a[href]").First().Attr("href")
		add(title, link, firstText(block, bingSnippets...))
	})

	// Structural fallback: any list item with a second-level heading link.
	if len(results) == 0 {
		doc.Find("li").Each(func(_ int, block *goquery.Selection) {
			heading := block.Find("h2 a[href]").First()
			if heading.Length() == 0 {
				return
			}
			link, _ := heading.Attr("href")
			add(heading.Text(), link, firstText(block, bingSnippets...))
		})
	}

	return results
}

// decodeBingURL unwraps Bing's "ck/a" click-tracking redirect, whose "u"
// parameter carries the target as base64url prefixed with "a1".
func decodeBingURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if isInternalHost(strings.ToLower(parsed.Hostname()), "bing.com") && strings.Contains(parsed.Path, "/ck/a") {
		if encoded := parsed.Query().Get("u"); strings.HasPrefix(encoded, "a1") {
			if decoded, err := base64.RawURLEncoding.DecodeString(encoded[2:]); err == nil {
				return normalizeURL(string(decoded))
			}
		}
	}
	return normalizeURL(raw)
}
