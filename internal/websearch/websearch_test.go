package websearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// jsShell mimics the JavaScript-only page Google serves to plain HTTP clients.
const jsShell = `<!DOCTYPE html><html><head><title>Google Search</title></head>` +
	`<body><noscript>Please click here if you are not redirected.</noscript></body></html>`

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func newClient(t *testing.T, engines []string, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(engines, WithBaseURL(server.URL), WithMinInterval(time.Microsecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestGoogleSearchParsesResults(t *testing.T) {
	fixture := readFixture(t, "serp_google.html")
	client := newClient(t, []string{EngineGoogle}, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if got := query.Get("q"); got != "golang mcp" {
			t.Errorf("q = %q, want %q", got, "golang mcp")
		}
		if got := query.Get("num"); got != "3" {
			t.Errorf("num = %q, want 3", got)
		}
		if got := query.Get("hl"); got != "en" {
			t.Errorf("hl = %q, want en", got)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("User-Agent header is empty")
		}
		_, _ = w.Write(fixture)
	})

	results, err := client.Search(context.Background(), Request{Query: "golang mcp", Num: 3})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(results), results)
	}

	want0 := Result{
		Rank:    1,
		Title:   "Go MCP SDK",
		URL:     "https://example.com/go-mcp",
		Snippet: "The official Model Context Protocol SDK for Go.",
		Source:  EngineGoogle,
	}
	if results[0] != want0 {
		t.Errorf("results[0] = %+v, want %+v", results[0], want0)
	}
	if results[2].URL != "https://example.net/third" {
		t.Errorf("results[2].URL = %q, want https://example.net/third", results[2].URL)
	}
	if results[2].Snippet != "A third organic result found via a snippet fallback selector." {
		t.Errorf("results[2].Snippet = %q", results[2].Snippet)
	}
}

func TestBingSearchParsesResults(t *testing.T) {
	fixture := readFixture(t, "serp_bing.html")
	client := newClient(t, []string{EngineBing}, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("form"); got != "QBLH" {
			t.Errorf("form = %q, want QBLH", got)
		}
		_, _ = w.Write(fixture)
	})

	results, err := client.Search(context.Background(), Request{Query: "test query", Num: 2})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(results), results)
	}
	if results[0].URL != "https://example.com/bing-one" {
		t.Errorf("results[0].URL = %q, want https://example.com/bing-one (redirect not decoded)", results[0].URL)
	}
	if results[0].Title != "First Bing Result" {
		t.Errorf("results[0].Title = %q", results[0].Title)
	}
	if results[0].Source != EngineBing {
		t.Errorf("results[0].Source = %q, want bing", results[0].Source)
	}
	if !strings.Contains(results[1].Snippet, "second organic") {
		t.Errorf("results[1].Snippet = %q", results[1].Snippet)
	}
}

func TestFallsBackToSecondEngine(t *testing.T) {
	bingFixture := readFixture(t, "serp_bing.html")
	client := newClient(t, []string{EngineGoogle, EngineBing}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("form") == "QBLH" {
			_, _ = w.Write(bingFixture)
			return
		}
		_, _ = w.Write([]byte(jsShell)) // Google serves the JS-only shell
	})

	results, err := client.Search(context.Background(), Request{Query: "anything"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected fallback results from Bing")
	}
	if results[0].Source != EngineBing {
		t.Errorf("results[0].Source = %q, want bing", results[0].Source)
	}
}

func TestBlockedPageReturnsErrBlocked(t *testing.T) {
	fixture := readFixture(t, "blocked.html")
	client := newClient(t, []string{EngineGoogle}, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture)
	})

	_, err := client.Search(context.Background(), Request{Query: "anything"})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("got error %v, want ErrBlocked", err)
	}
}

func TestTooManyRequestsReturnsErrBlocked(t *testing.T) {
	client := newClient(t, []string{EngineGoogle}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := client.Search(context.Background(), Request{Query: "anything"})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("got error %v, want ErrBlocked", err)
	}
}

func TestEmptyQueryIsRejected(t *testing.T) {
	client, err := New([]string{EngineGoogle}, WithMinInterval(time.Microsecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Search(context.Background(), Request{Query: "   "}); err == nil {
		t.Fatal("expected an error for an empty query")
	}
}

func TestUnknownEngineIsRejected(t *testing.T) {
	if _, err := New([]string{"nope"}); err == nil {
		t.Fatal("expected an error for an unknown engine")
	}
}

func TestSplitEngines(t *testing.T) {
	got := SplitEngines(" google , bing ,, ")
	want := []string{"google", "bing"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("SplitEngines() = %v, want %v", got, want)
	}
}

func TestSearchURLParameters(t *testing.T) {
	google, ok := EngineByName("google")
	if !ok {
		t.Fatal("google engine not registered")
	}
	got := google.SearchURL("https://g.example", Request{Query: "go mcp", Num: 99, Start: 10, Language: "de", Country: "de", Safe: "active"})
	for _, want := range []string{"q=go+mcp", "num=10", "start=10", "hl=de", "gl=de", "safe=active"} {
		if !strings.Contains(got, want) {
			t.Errorf("google SearchURL %q missing %q", got, want)
		}
	}

	bing, ok := EngineByName("bing")
	if !ok {
		t.Fatal("bing engine not registered")
	}
	got = bing.SearchURL("https://b.example", Request{Query: "go mcp", Num: 5, Start: 10})
	for _, want := range []string{"q=go+mcp", "count=5", "first=11", "form=QBLH"} {
		if !strings.Contains(got, want) {
			t.Errorf("bing SearchURL %q missing %q", got, want)
		}
	}
}
