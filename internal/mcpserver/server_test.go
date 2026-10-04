package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// newSessionWithHTML is newSession against a caller-supplied page body, so the
// failure paths can be exercised without touching the network.
func newSessionWithHTML(t *testing.T, body string) *mcp.ClientSession {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	client, err := websearch.New(
		[]string{websearch.EngineDuckDuckGo},
		websearch.WithBaseURL(srv.URL),
		websearch.WithMinInterval(time.Microsecond),
	)
	if err != nil {
		t.Fatalf("websearch.New: %v", err)
	}
	return connectSession(t, client)
}

// newSessionWithResultCount serves a SERP holding exactly count hits, and
// returns the session plus a pointer to the last "q" the server observed.
func newSessionWithResultCount(t *testing.T, count int) (*mcp.ClientSession, *string) {
	t.Helper()
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		for i := 0; i < count; i++ {
			_, _ = w.Write([]byte(`<div class="MjjYud"><a href="https://example.com/` +
				strings.Repeat("x", i+1) + `"><h3>Hit ` + strconv.Itoa(i) + `</h3></a>` +
				`<div class="VwiC3b">Snippet ` + strconv.Itoa(i) + `</div></div>`))
		}
	}))
	t.Cleanup(srv.Close)

	client, err := websearch.New([]string{websearch.EngineGoogle},
		websearch.WithBaseURL(srv.URL), websearch.WithMinInterval(time.Microsecond))
	if err != nil {
		t.Fatalf("websearch.New: %v", err)
	}
	return connectSession(t, client), &lastQuery
}

// connectSession wires a websearch client into an MCP server and returns an
// in-memory client session.
func connectSession(t *testing.T, client *websearch.Client) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := New(client, "test")
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

// The tool description is the only guidance the model gets before calling, so
// it must not claim Google is the primary source.
func TestToolDescriptionNamesDefaultEngineOrder(t *testing.T) {
	session := newSession(t)
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "google_search" {
			continue
		}
		if !strings.Contains(tool.Description, "DuckDuckGo") {
			t.Errorf("tool description should name the default engine order, got: %s", tool.Description)
		}
		return
	}
	t.Fatal("google_search tool not listed")
}

// An out-of-range "num" must be clamped rather than rejected, and must never
// return more hits than the server offered.
func TestSearchClampsNum(t *testing.T) {
	const serverHits = 3
	session, _ := newSessionWithResultCount(t, serverHits)

	for _, num := range []int{99, -3, 0, 2} {
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "google_search",
			Arguments: map[string]any{"query": "golang mcp", "num": num},
		})
		if err != nil {
			t.Fatalf("CallTool(num=%d) returned a transport error: %v", num, err)
		}
		if res.IsError {
			t.Errorf("num=%d should be clamped, not rejected: %s", num, textContent(res))
		}
		if got := strings.Count(textContent(res), "https://example.com/"); got > serverHits {
			t.Errorf("num=%d returned %d hits, more than the %d on the page", num, got, serverHits)
		}
	}
}

// A negative "start" must not be forwarded as a pagination offset.
func TestSearchClampsStart(t *testing.T) {
	session, lastQuery := newSessionWithResultCount(t, 2)
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "google_search",
		Arguments: map[string]any{"query": "golang mcp", "start": -5},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if *lastQuery != "golang mcp" {
		t.Errorf("last query = %q, want %q", *lastQuery, "golang mcp")
	}
}

// "site" must be folded into the query rather than passed as a separate field.
func TestSearchAppendsSiteOperator(t *testing.T) {
	session, lastQuery := newSessionWithResultCount(t, 2)
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "google_search",
		Arguments: map[string]any{"query": "mcp sdk", "site": "github.com"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if *lastQuery != "mcp sdk site:github.com" {
		t.Errorf("last query = %q, want %q", *lastQuery, "mcp sdk site:github.com")
	}

	// An explicit site: in the query must not be duplicated.
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "google_search",
		Arguments: map[string]any{"query": "mcp site:golang.org", "site": "github.com"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if *lastQuery != "mcp site:golang.org" {
		t.Errorf("last query = %q, want it unchanged", *lastQuery)
	}
}

// A page with no hits must be surfaced as a tool error rather than as a
// confident "No results found", which would mislead the model.
func TestSearchSurfacesEngineFailureAsToolError(t *testing.T) {
	session := newSessionWithHTML(t, `<!doctype html><html><head><title>DuckDuckGo</title></head>
<body><p>Unfortunately, bots use DuckDuckGo too. Please complete the following challenge
to confirm this search was made by a human.</p></body></html>`)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "google_search",
		Arguments: map[string]any{"query": "golang mcp"},
	})
	if err != nil {
		t.Fatalf("CallTool returned a transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError when every engine is blocked, got: %s", textContent(res))
	}
}
