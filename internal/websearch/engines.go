package websearch

import (
	"sort"
	"strings"
)

// Supported engine names.
const (
	EngineGoogle = "google"
	EngineBing   = "bing"
)

// registry maps engine names to their implementations.
var registry = map[string]Engine{
	EngineGoogle: googleEngine{},
	EngineBing:   bingEngine{},
}

// EngineByName returns the engine registered under name (case-insensitive).
func EngineByName(name string) (Engine, bool) {
	engine, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	return engine, ok
}

// EngineNames returns the supported engine names, sorted.
func EngineNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SplitEngines parses a comma-separated engine list, dropping empty entries.
func SplitEngines(list string) []string {
	var engines []string
	for _, part := range strings.Split(list, ",") {
		if part = strings.TrimSpace(part); part != "" {
			engines = append(engines, part)
		}
	}
	return engines
}
