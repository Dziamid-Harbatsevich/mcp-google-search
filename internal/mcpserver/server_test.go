package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/demid/mcp-google-search/internal/websearch"
)

// serpHTML is a minimal, representative Google results page used to exercise
// the tool end-to-end without touching the network.
const serpHTML = `<!doctype html>
<html><head><title>golang mcp - Google Search</title></head>
<body><div id="rso">
  <div class="MjjYud"><div class="g tF2Cxc"><div class="yuRUbf">
    <a href="/url?q=https://example.com/go-mcp&amp;sa=U"><h3>Go MCP SDK</h3></a>
  </div><div class="VwiC3b">The official Model Context Protocol SDK for Go.</div></div></div>
  <div class="MjjYud"><div class="g tF2Cxc"><div class="yuRUbf">
    <a href="https://example.org/scraping"><h3>Google Search Scraping</h3></a>
  </div><div class="VwiC3b">How to scrape Google search results with Go.</div></div></div>
</div></body></html>`

// newSession wires a gsearch client (backed by an httptest server serving
// serpHTML) into an MCP server and connects an in-memory client session.
func newSession(t *testing.T) *mcp.ClientSession {
	t.Helper()

	google := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(serpHTML))
	}))
	t.Cleanup(google.Close)

	client, err := websearch.New(
		[]string{websearch.EngineGoogle},
		websearch.WithBaseURL(google.URL),
		websearch.WithMinInterval(time.Microsecond),
	)
	if err != nil {
		t.Fatalf("websearch.New: %v", err)
	}
	server := New(client, "test")

	ctx := context.Background()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	session, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func TestGoogleSearchToolListsTool(t *testing.T) {
	session := newSession(t)

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var found bool
	for _, tool := range res.Tools {
		if tool.Name == "google_search" {
			found = true
		}
	}
	if !found {
		t.Fatalf("google_search tool not listed; got %d tools", len(res.Tools))
	}
}

func TestGoogleSearchToolReturnsResults(t *testing.T) {
	session := newSession(t)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "google_search",
		Arguments: map[string]any{"query": "golang mcp", "num": 2},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported an error: %s", textContent(res))
	}

	text := textContent(res)
	if !strings.Contains(text, "Go MCP SDK") {
		t.Errorf("text content missing expected result:\n%s", text)
	}
	if !strings.Contains(text, "https://example.com/go-mcp") {
		t.Errorf("text content missing expected URL:\n%s", text)
	}
}

func TestGoogleSearchToolRejectsEmptyQuery(t *testing.T) {
	session := newSession(t)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "google_search",
		Arguments: map[string]any{"query": ""},
	})
	if err != nil {
		t.Fatalf("CallTool returned a transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for an empty query, got: %s", textContent(res))
	}
}

// textContent concatenates the text parts of a tool result.
func textContent(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
