package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/web"
)

// searchEngine is the search seam the adapter delegates to (mirrors
// Execute.Runner sandbox.Runner — an injectable interface, not a concrete type,
// so the adapter is unit-testable with a double). *web.Client satisfies it.
type searchEngine interface {
	Search(ctx context.Context, params web.SearchParams) ([]web.Result, error)
}

// WebSearch is the thin Deferred:true adapter over the shared web.Client search
// engine (D-01). It marshals the model-supplied args into web.SearchParams,
// delegates to the SSRF-hardened engine, maps a sanitized *web.WebError to an
// inline {error,reason,message} object via NewResult so the model self-corrects
// (D-41), and on success returns the ranked result list. It re-implements no
// security boundary — every dial/classify/pin lives in internal/web.
type WebSearch struct {
	Engine searchEngine
}

// webSearchArgs exposes ONLY the D-09 public controls. There is deliberately no
// engines/safesearch/pageno pass-through (D-10/T-07-31): the schema is the only
// surface the model can drive, and the engine maps category through its own
// enum→raw table.
type webSearchArgs struct {
	Query           string   `json:"query"`
	MaxResults      int      `json:"max_results"`
	Category        string   `json:"category"`
	Language        string   `json:"language"`
	TimeRange       string   `json:"time_range"`
	Domains         []string `json:"domains"`
	IncludeMetadata bool     `json:"include_metadata"`
}

func (e *WebSearch) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "The search query. Plain words; no engine-specific operators are needed."},
    "max_results": {"type": "integer", "minimum": 1, "maximum": 25, "description": "Optional cap on the number of results returned (default: the server cap). Omit to use the default."},
    "category": {"type": "string", "enum": ["general", "news"], "description": "Result category. general (default): broad web results, reference pages, and current structured facts (a live price, a definition). news: recent reporting — use it WITH time_range:day|week for the latest developments, breaking events, or 'what happened recently' queries."},
    "language": {"type": "string", "description": "Optional BCP-47-ish language hint (e.g. en, it, de). Omit for the engine default."},
    "time_range": {"type": "string", "enum": ["day", "week", "month", "year"], "description": "Optional recency window for engines that support it. Use day or week for breaking/recent topics. Omit for no time filter."},
    "domains": {"type": "array", "items": {"type": "string"}, "description": "Optional list of bare hostnames (e.g. wikipedia.org) to restrict results to. Hostnames only — no scheme or path."},
    "include_metadata": {"type": "boolean", "description": "When true, attach a metadata tier (engine, score, category, published_at, thumbnail) to each result. Default false keeps the lean {title,url,snippet} shape."}
  },
  "required": ["query"]
}`)
	return Spec{
		Name:    "web_search",
		Summary: "Search the public web via the configured SearXNG instance.",
		Description: "Search the public web through the configured private SearXNG meta-search instance and get back a ranked list of {title, url, snippet} results. " +
			"Use it to find pages, recent news, or candidate URLs you can then read with web_fetch. " +
			"Freshness matters: the general category returns broad web results (good for reference and current structured facts like a live price or a definition) but its snippets can be cached and undated. " +
			"For the latest developments, breaking news, or anything time-sensitive ('today', 'latest', 'recent'), set category:news together with time_range:day or week, and pass include_metadata:true to read each result's published_at so you can prefer the freshest. " +
			"You can also narrow with a language hint and a domains allowlist of bare hostnames. " +
			"Example (reference): {\"query\":\"orbital period of Titan\",\"max_results\":5}. " +
			"Example (latest news): {\"query\":\"EU AI Act\",\"category\":\"news\",\"time_range\":\"day\",\"include_metadata\":true}. " +
			"Example (domain-scoped): {\"query\":\"site reliability\",\"domains\":[\"wikipedia.org\"]}.",
		Parameters: params,
		Deferred:   true,
	}
}

// Execute unmarshals the args, delegates to the hardened engine, and routes the
// outcome through NewResult. A *web.WebError (e.g. searxng unavailable / not
// configured) is sanitized to inline {error,reason,message} JSON so the model
// self-corrects (D-41) — it is NOT a Go error. Only a genuine non-web infra
// fault propagates as a Go error to the loop.
func (e *WebSearch) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a webSearchArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("web_search args: %w", err)
	}
	a.Query = strings.TrimSpace(a.Query)
	if a.Query == "" {
		return ToolResult{}, fmt.Errorf("web_search: query is required")
	}

	results, err := e.Engine.Search(ctx, web.SearchParams{
		Query:           a.Query,
		MaxResults:      a.MaxResults,
		Category:        a.Category,
		Language:        a.Language,
		TimeRange:       a.TimeRange,
		Domains:         a.Domains,
		IncludeMetadata: a.IncludeMetadata,
	})
	if err != nil {
		return webErrorResult(ctx, err)
	}

	out, mErr := json.Marshal(map[string]any{"results": results})
	if mErr != nil {
		return ToolResult{}, fmt.Errorf("web_search: marshal results: %w", mErr)
	}
	return NewResult(ctx, string(out))
}
