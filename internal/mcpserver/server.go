// Package mcpserver wires the gsearch client into an MCP server exposing a
// single "google_search" tool.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/demid/mcp-google-search/internal/websearch"
)

// ServerName is the implementation name advertised to MCP clients.
const ServerName = "mcp-google-search"

// SearchInput is the input schema for the google_search tool. Fields without
// the "omitempty" option are required (the SDK derives the JSON schema from
// these struct tags).
type SearchInput struct {
	Query    string `json:"query" jsonschema:"The search query to send to Google. Prefer specific, natural-language queries."`
	Num      int    `json:"num,omitempty" jsonschema:"Number of results to return, 1-10. Defaults to 5."`
	Start    int    `json:"start,omitempty" jsonschema:"Zero-based index of the first result to return, for pagination. Defaults to 0."`
	Language string `json:"language,omitempty" jsonschema:"Result language as a Google hl code, e.g. 'en', 'de', 'fr'. Defaults to 'en'."`
	Country  string `json:"country,omitempty" jsonschema:"Country code used to localize results, e.g. 'us', 'gb', 'de'."`
	Safe     string `json:"safe,omitempty" jsonschema:"SafeSearch setting: 'off' or 'active'."`
	Site     string `json:"site,omitempty" jsonschema:"Restrict results to a single site, e.g. 'example.com'."`
}

// SearchResult is a single organic result returned to the caller.
type SearchResult struct {
	Rank    int    `json:"rank"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Source  string `json:"source,omitempty"`
}

// SearchOutput is the structured output of the google_search tool.
type SearchOutput struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

// New builds the MCP server and registers the google_search tool.
func New(client *websearch.Client, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    ServerName,
		Version: version,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "google_search",
		Description: "Search the web and return ranked results with a title, URL and snippet for each hit. Uses Google first and falls back to Bing when Google requires JavaScript.",
	}, searchHandler(client))

	return server
}

// searchHandler adapts the websearch client to the MCP tool handler signature.
func searchHandler(client *websearch.Client) mcp.ToolHandlerFor[SearchInput, SearchOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, SearchOutput{}, fmt.Errorf("query must not be empty")
		}
		if site := strings.TrimSpace(in.Site); site != "" && !strings.Contains(query, "site:") {
			query += " site:" + site
		}

		num := in.Num
		if num == 0 {
			num = 5
		}

		results, err := client.Search(ctx, websearch.Request{
			Query:    query,
			Num:      num,
			Start:    in.Start,
			Language: in.Language,
			Country:  in.Country,
			Safe:     in.Safe,
		})
		if err != nil {
			return nil, SearchOutput{}, err
		}

		out := SearchOutput{
			Query:   query,
			Results: make([]SearchResult, 0, len(results)),
		}
		for _, r := range results {
			out.Results = append(out.Results, SearchResult{
				Rank:    r.Rank,
				Title:   r.Title,
				URL:     r.URL,
				Snippet: r.Snippet,
				Source:  r.Source,
			})
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: render(out)}},
		}, out, nil
	}
}

// render produces the plain-text rendering of the results that the model sees.
func render(out SearchOutput) string {
	if len(out.Results) == 0 {
		return fmt.Sprintf("No results found for %q.", out.Query)
	}

	header := fmt.Sprintf("Top %d results for %q", len(out.Results), out.Query)
	if source := out.Results[0].Source; source != "" {
		header += " [via " + source + "]"
	}

	var b strings.Builder
	b.WriteString(header + ":")
	for _, r := range out.Results {
		fmt.Fprintf(&b, "\n\n%d. %s\n   %s", r.Rank, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "\n   %s", r.Snippet)
		}
	}
	return b.String()
}
