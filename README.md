# mcp-google-search

A Model Context Protocol (MCP) server that exposes a web search tool to LM Studio.

No API key is required. It scrapes search-engine results pages, tries each configured engine in order, and returns the first engine that yields hits.

## Install

```bash
make build          # produces ./mcp-google-search
```

## Configure in LM Studio

LM Studio reads MCP servers from `~/.lmstudio/mcp.json` (create it if missing):

```json
{
  "mcpServers": {
    "google-search": {
      "command": "/your/local/path",
      "args": ["-transport", "stdio"],
      "env": {
        "SEARCH_ENGINES": "duckduckgo,google,bing"
      }
    }
  }
}
```

Use the absolute path to the binary. Then restart LM Studio, or toggle the server in the MCP developer panel.

## The tool

One tool is exposed: `google_search`.

| Field | Type | Description |
| --- | --- | --- |
| `query` | string | **Required.** The search query. |
| `num` | int | Results to return, 1-10. Clamped. Defaults to 5. |
| `start` | int | Zero-based index of the first hit, for pagination. Defaults to 0. |
| `language` | string | Result language, e.g. `en`, `de`, `fr`. |
| `country` | string | Country code for localization, e.g. `us`, `gb`, `de`. |
| `safe` | string | SafeSearch: `off` or `active`. |
| `site` | string | Restrict to one site, e.g. `github.com`. |

Output is a `query`, the `engine` that answered, and a ranked `results` list of `{rank, title, url, snippet, source}`.

## Engines

Set the order with `-engine` or the `SEARCH_ENGINES` env var (comma-separated).

| Engine | Notes |
| --- | --- |
| `duckduckgo` | The default first choice. The `/html/` endpoint renders results server-side. |
| `google` | **Usually returns nothing over plain HTTP** — see below. |
| `bing` | Renders results server-side, but frequently answers with off-topic hits. |

### Why Google is not first

Google answers non-browser clients with a JavaScript bootstrap shell: HTTP 200 with no organic hits. Scraping it requires a headless browser, which this keyless server does not use. A request that gets the shell is reported as `ErrUnsupported` rather than as a successful search with zero results, so the model is never told "no results exist" when the real cause is that Google simply cannot be scraped this way.

Bing and several other engines were observed returning results unrelated to the query (ads, regional pages, or unrelated topics) from the same network. Bing remains in the default list as a last resort, but DuckDuckGo is preferred.

If you have a Google Custom Search JSON API key, or a paid scraping proxy, prefer wiring that in over relying on the plain Google endpoint.

## Failure behaviour

- If **every** engine fails (blocked, CAPTCHA, or JavaScript-only), the tool returns an error. It does not report "no results found", which would mislead the model.
- A genuine "no matches anywhere" is a successful call with an empty list.
- A CAPTCHA or rate-limit page yields `ErrBlocked` suggesting you retry later.

Engines rate-limit aggressively. The default minimum spacing between requests is 1s; raise `SEARCH_MIN_INTERVAL` if you see `ErrBlocked`.

## Configuration

Every flag has an environment-variable equivalent, so LM Studio can supply them in the `env` block.

| Flag | Env | Default |
| --- | --- | --- |
| `-transport` | `MCP_TRANSPORT` | `stdio` (`http` also supported) |
| `-addr` | `MCP_ADDR` | `:8080` (http transport) |
| `-engine` | `SEARCH_ENGINES` | `duckduckgo,google,bing` |
| `-base-url` | `SEARCH_BASE_URL` | engine default |
| `-user-agent` | `SEARCH_USER_AGENT` | a desktop Chrome string |
| `-min-interval` | `SEARCH_MIN_INTERVAL` | `1s` |
| `-timeout` | `SEARCH_TIMEOUT` | `15s` |
| `-log-level` | `LOG_LEVEL` | `info` |

All logs go to **stderr**; stdout is reserved for the MCP protocol.

## Development

```bash
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -w .
make run     # build and start over stdio
```

Tests use HTML fixtures in `internal/websearch/testdata` and `httptest`, so the suite never touches the network.

Layout:

- `main.go` — flags, env parsing, transport selection.
- `internal/websearch` — engine registry, per-engine scrapers, rate limiting.
- `internal/mcpserver` — the MCP tool definition and result rendering.

### Adding an engine

Implement `websearch.Engine` in a new file (name, base URL, URL builder, parser, block detection, `NeedsJavaScript`), then register it in `engines.go`. Add a fixture captured from the live page and a parse test.