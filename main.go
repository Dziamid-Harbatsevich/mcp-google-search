// Command mcp-web-search is a Model Context Protocol (MCP) server that
// exposes a Google web search tool. It is designed to be run by an MCP host
// such as LM Studio over the stdio transport.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/demid/mcp-web-search/internal/mcpserver"
	"github.com/demid/mcp-web-search/internal/websearch"
)

// version is reported to MCP clients during initialization.
var version = "v0.1.0"

func main() {
	var (
		transport   = flag.String("transport", envOrDefault("MCP_TRANSPORT", "stdio"), "transport to use: stdio or http")
		addr        = flag.String("addr", envOrDefault("MCP_ADDR", ":8080"), "listen address for the http transport")
		engine      = flag.String("engine", envOrDefault("SEARCH_ENGINES", "duckduckgo,google,bing"), "comma-separated search engines, tried in order (supported: duckduckgo, google, bing)")
		baseURL     = flag.String("base-url", os.Getenv("SEARCH_BASE_URL"), "override the base URL of every search engine (optional)")
		userAgent   = flag.String("user-agent", envOrDefault("SEARCH_USER_AGENT", websearch.DefaultUserAgent), "User-Agent header sent to search engines")
		minInterval = flag.Duration("min-interval", envDuration("SEARCH_MIN_INTERVAL", websearch.DefaultMinInterval), "minimum spacing between search requests")
		timeout     = flag.Duration("timeout", envDuration("SEARCH_TIMEOUT", websearch.DefaultTimeout), "per-request timeout")
		logLevel    = flag.String("log-level", envOrDefault("LOG_LEVEL", "info"), "log level: debug, info, warn, error")
	)
	flag.Parse()

	// The stdio transport owns stdout, so all logs must go to stderr.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLevel(*logLevel)}))
	slog.SetDefault(logger)

	client, err := websearch.New(
		websearch.SplitEngines(*engine),
		websearch.WithBaseURL(*baseURL),
		websearch.WithUserAgent(*userAgent),
		websearch.WithMinInterval(*minInterval),
		websearch.WithHTTPClient(&http.Client{Timeout: *timeout}),
	)
	if err != nil {
		logger.Error("invalid configuration", "err", err)
		os.Exit(2)
	}
	logger.Info("configured search engines", "engines", client.Engines())
	server := mcpserver.New(client, version)

	ctx := context.Background()
	switch *transport {
	case "stdio":
		logger.Info("starting MCP server", "transport", "stdio", "version", version)
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			logger.Error("server stopped", "err", err)
			os.Exit(1)
		}
	case "http":
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
		logger.Info("starting MCP server", "transport", "http", "addr", *addr, "version", version)
		if err := http.ListenAndServe(*addr, handler); err != nil {
			logger.Error("http server stopped", "err", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown transport %q (want \"stdio\" or \"http\")\n", *transport)
		os.Exit(2)
	}
}

// envOrDefault returns the value of the environment variable key, or fallback
// when it is unset or empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envDuration parses key as a Go duration, falling back when unset or invalid.
func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

// parseLevel converts a textual log level into a slog.Level.
func parseLevel(s string) slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return level
}
