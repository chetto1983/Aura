package governance

import (
	"errors"
	"net/url"
	"strings"
	"sync"
)

// MaxRepeats is the number of identical external lookups allowed per turn
// before subsequent calls are short-circuited. Production-validated at 2
// by nanobot utils/runtime.py:68-102.
const MaxRepeats = 2

// ErrRepeatedLookup is returned by RepeatedLookupCounter.Check when a
// tracked tool has been called more than MaxRepeats times with the same
// normalized target in a single turn.
var ErrRepeatedLookup = errors.New("repeated external lookup blocked. Use the results you already have to answer, or try a meaningfully different source")

// RepeatedLookupCounter tracks identical external lookup calls within a
// single agent turn. Covered tools: web_search (by query), web_fetch (by
// URL), search_memory (by query). After MaxRepeats identical targets, the
// next call returns ErrRepeatedLookup so the LLM sees it as a tool result.
//
// Safe for concurrent use: parallel tool dispatches in the same iteration
// may call Check simultaneously.
type RepeatedLookupCounter struct {
	mu     sync.Mutex
	counts map[string]int
	max    int
}

// NewRepeatedLookupCounter returns a counter with the default MaxRepeats cap.
func NewRepeatedLookupCounter() *RepeatedLookupCounter {
	return &RepeatedLookupCounter{
		counts: make(map[string]int),
		max:    MaxRepeats,
	}
}

// Check atomically increments the call count for the lookup target and
// returns ErrRepeatedLookup when the same target has been called more than
// max times this turn. Returns nil for untracked tools or calls within budget.
func (c *RepeatedLookupCounter) Check(toolName string, args map[string]any) error {
	sig, tracked := buildLookupSignature(toolName, args)
	if !tracked {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[sig]++
	if c.counts[sig] > c.max {
		return ErrRepeatedLookup
	}
	return nil
}

// buildLookupSignature returns a stable string key for the lookup target and
// whether toolName is a tracked external lookup. Returns ("", false) for
// untracked tools.
func buildLookupSignature(toolName string, args map[string]any) (string, bool) {
	switch toolName {
	case "web_search":
		q, _ := args["query"].(string)
		return "web_search:" + strings.ToLower(strings.TrimSpace(q)), true
	case "web_fetch":
		u, _ := args["url"].(string)
		return "web_fetch:" + normalizeFetchURL(u), true
	case "search_memory":
		q, _ := args["query"].(string)
		return "search_memory:" + strings.ToLower(strings.TrimSpace(q)), true
	default:
		return "", false
	}
}

// normalizeFetchURL normalizes a web_fetch URL for dedup:
// trim whitespace, lowercase host, strip trailing slash from path,
// preserve query params (callers use specific params intentionally).
func normalizeFetchURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil {
		return strings.ToLower(trimmed)
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}
