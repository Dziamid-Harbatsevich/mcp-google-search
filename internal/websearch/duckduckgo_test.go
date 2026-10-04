package websearch

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestDuckDuckGoParsesResults(t *testing.T) {
	fixture := readFixture(t, "serp_duckduckgo.html")
	client := newClient(t, []string{EngineDuckDuckGo}, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "golang json unmarshal" {
			t.Errorf("q = %q", got)
		}
		_, _ = w.Write(fixture)
	})

	results, err := client.Search(t.Context(), Request{Query: "golang json unmarshal"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results parsed from DuckDuckGo fixture")
	}

	got := results[0]
	if !strings.Contains(got.URL, "pkg.go.dev") {
		t.Errorf("results[0].URL = %q, want the decoded pkg.go.dev target", got.URL)
	}
	if strings.Contains(got.URL, "duckduckgo.com") {
		t.Errorf("results[0].URL = %q, still a DuckDuckGo redirect", got.URL)
	}
	if got.Title == "" {
		t.Error("results[0].Title is empty")
	}
	if got.Snippet == "" {
		t.Error("results[0].Snippet is empty")
	}
	if got.Source != EngineDuckDuckGo {
		t.Errorf("results[0].Source = %q, want duckduckgo", got.Source)
	}
	if got.Rank != 1 {
		t.Errorf("results[0].Rank = %d, want 1", got.Rank)
	}
}

func TestDuckDuckGoUnwruddgRedirect(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2Fencoding%2Fjson&rut=abc",
			want: "https://pkg.go.dev/encoding/json",
		},
		{
			// A protocol-relative absolute link still needs a scheme.
			in:   "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa%3Fb%3D1",
			want: "https://example.com/a?b=1",
		},
		{
			// The "kl" param is mirrored onto links and must not be mistaken for
			// the redirect target.
			in:   "//duckduckgo.com/l/?kl=wt-wt&uddg=https%3A%2F%2Fexample.org%2Fx",
			want: "https://example.org/x",
		},
		{
			// No wrapper: an ordinary absolute link passes through.
			in:   "https://example.com/plain",
			want: "https://example.com/plain",
		},
		{
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		if got := normalizeDuckDuckGoURL(tc.in); got != tc.want {
			t.Errorf("normalizeDuckDuckGoURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDuckDuckGoChallengeIsBlocked(t *testing.T) {
	fixture := readFixture(t, "blocked_ddg.html")
	client := newClient(t, []string{EngineDuckDuckGo}, func(w http.ResponseWriter, r *http.Request) {
		// DuckDuckGo serves its challenge with 202, not 200.
		w.WriteHeader(202)
		_, _ = w.Write(fixture)
	})

	_, err := client.Search(t.Context(), Request{Query: "anything"})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("got error %v, want ErrBlocked", err)
	}
}

func TestDuckDuckGoSearchURLParameters(t *testing.T) {
	engine, ok := EngineByName("duckduckgo")
	if !ok {
		t.Fatal("duckduckgo engine not registered")
	}

	got := engine.SearchURL("https://ddg.example", Request{Query: "go mcp", Start: 20, Language: "de", Country: "de"})
	for _, want := range []string{"q=go+mcp", "s=20", "kl=de-de", "/html/"} {
		if !strings.Contains(got, want) {
			t.Errorf("duckduckgo SearchURL %q missing %q", got, want)
		}
	}

	// No region hints must yield the worldwide default rather than an empty kl.
	got = engine.SearchURL("https://ddg.example", Request{Query: "go mcp"})
	if !strings.Contains(got, "kl=wt-wt") {
		t.Errorf("duckduckgo SearchURL %q missing kl=wt-wt default", got)
	}
}

// A Google query that returns the JavaScript shell must be reported as an
// unsupported page, not as a successful search with zero results. Reporting
// zero results would tell the model the web has nothing on the topic.
func TestGoogleJSShellIsReportedAsUnsupported(t *testing.T) {
	client := newClient(t, []string{EngineGoogle}, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(jsShell))
	})

	_, err := client.Search(t.Context(), Request{Query: "golang mcp"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got error %v, want ErrUnsupported", err)
	}
}

// When every engine yields the JavaScript shell the caller must get an error,
// never a silent empty result set.
func TestAllEnginesFailingReturnsError(t *testing.T) {
	client := newClient(t, []string{EngineGoogle, EngineBing}, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(jsShell))
	})

	_, err := client.Search(t.Context(), Request{Query: "golang mcp"})
	if err == nil {
		t.Fatal("expected an error when every engine returns no results and no matches")
	}
	if !strings.Contains(err.Error(), "all search engines failed") {
		t.Errorf("error %q should name the failure", err)
	}
}

// A genuine "no matches anywhere" must stay a successful, empty result set.
func TestNoMatchesIsNotAnError(t *testing.T) {
	empty := `<!doctype html><html><head><title>no results</title></head><body><p>0 results</p></body></html>`
	client := newClient(t, []string{EngineDuckDuckGo}, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(empty))
	})

	results, err := client.Search(t.Context(), Request{Query: "zzqqxx nonsense token"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}

func TestBingMapsSafeSearchToAdultLevels(t *testing.T) {
	cases := map[string]string{
		"active":   "strict",
		"strict":   "strict",
		"off":      "off",
		"moderate": "moderate",
		"":         "moderate",
		"weird":    "moderate",
	}
	for safe, want := range cases {
		if got := bingAdultLevel(safe); got != want {
			t.Errorf("bingAdultLevel(%q) = %q, want %q", safe, got, want)
		}
	}

	// The URL must carry Bing's vocabulary, not Google's.
	engine, _ := EngineByName(EngineBing)
	got := engine.SearchURL("https://b.example", Request{Query: "x", Safe: "active"})
	if !strings.Contains(got, "adlt=strict") {
		t.Errorf("bing SearchURL %q missing adlt=strict", got)
	}
}

func TestEngineRegistryListsDuckDuckGo(t *testing.T) {
	names := EngineNames()
	want := []string{EngineBing, EngineDuckDuckGo, EngineGoogle}
	if len(names) != len(want) {
		t.Fatalf("EngineNames() = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("EngineNames()[%d] = %q, want %q", i, names[i], want[i])
		}
	}
	// Lookup must stay case-insensitive so a hand-edited LM Studio config works.
	if _, ok := EngineByName("DuckDuckGo"); !ok {
		t.Error("EngineByName should match case-insensitively")
	}
}

// DuckDuckGo ignores the page-size hint and always returns a fixed-size page, so
// the client must cap the result set itself. Returning a full page regardless of
// what the caller asked for would silently blow past their context budget.
func TestSearchCapsResultsToRequestedNum(t *testing.T) {
	var page strings.Builder
	for i := 0; i < 10; i++ {
		page.WriteString(`<div class="result results_links"><h2 class="result__title">` +
			`<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2F` +
			strconv.Itoa(i) + `">Hit ` + strconv.Itoa(i) + `</a></h2></div>`)
	}
	html := `<!doctype html><html><body>` + page.String() + `</body></html>`

	client := newClient(t, []string{EngineDuckDuckGo}, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	})

	for _, num := range []int{1, 3, 10} {
		results, err := client.Search(t.Context(), Request{Query: "x", Num: num})
		if err != nil {
			t.Fatalf("Search(num=%d): %v", num, err)
		}
		if len(results) != num {
			t.Errorf("Num=%d returned %d results, want %d", num, len(results), num)
		}
	}

	// Num=0 means "engine default" and must not truncate to nothing.
	results, err := client.Search(t.Context(), Request{Query: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 10 {
		t.Errorf("Num=0 returned %d results, want the full page of 10", len(results))
	}
}

// Pagination must offset into a later page rather than repeat the first.
func TestSearchPaginates(t *testing.T) {
	var seenOffsets []string
	client := newClient(t, []string{EngineDuckDuckGo}, func(w http.ResponseWriter, r *http.Request) {
		seenOffsets = append(seenOffsets, r.URL.Query().Get("s"))
		_, _ = w.Write([]byte(readFixture(t, "serp_duckduckgo.html")))
	})

	if _, err := client.Search(t.Context(), Request{Query: "x", Start: 20}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(seenOffsets) == 0 || seenOffsets[0] != "20" {
		t.Errorf("expected the request to carry s=20, got %v", seenOffsets)
	}
}
