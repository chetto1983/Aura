package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/wiki"
)

// SearchTool is the unified read-only knowledge lookup surface that replaces
// the fragmented search_memory / list_memory / read_memory / read_source pattern.
//
//   - action=search — hybrid wiki+compact search (delegates to SearchMemoryTool)
//   - action=list   — enumerate wiki page slugs with optional prefix filter
//   - action=read   — fetch a wiki page body or source extract.md by slug
//
// The deprecated tools remain registered for backward compatibility. Their
// descriptions and result envelopes now carry DEPRECATED redirect hints.
type SearchTool struct {
	searchMem    *SearchMemoryTool
	wikiReader   wiki.PageReader
	sourceReader *ReadSourceTool // nil when source store not wired
}

// NewSearchTool wires the unified search tool. Returns nil when all three
// dependencies are nil so callers can skip Register cleanly.
func NewSearchTool(searchMem *SearchMemoryTool, wikiReader wiki.PageReader, sourceReader *ReadSourceTool) *SearchTool {
	if searchMem == nil && wikiReader == nil && sourceReader == nil {
		return nil
	}
	return &SearchTool{
		searchMem:    searchMem,
		wikiReader:   wikiReader,
		sourceReader: sourceReader,
	}
}

func (t *SearchTool) Name() string { return "search" }

func (t *SearchTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		OpenWorldHint:  true, // wiki content drifts as pages are written
		IdempotentHint: true,
		VisibilityTier: VisibilityActiveTurn,
	}
}

func (t *SearchTool) Description() string {
	return `Read-only. Unified knowledge lookup — wiki pages, ingested sources, conversation archive.
Use INSTEAD of search_memory/list_memory/read_memory/read_source which are deprecated.

action=search returns top-{top_k} hybrid hits across wiki + sources.
action=list returns matching slugs (default zone=wiki, top 20).
action=read returns the body of one page or source by slug.

REQUIRED PARAMETERS BY ACTION:
  • action="search": query (required), zone (wiki|source|all, default all), top_k (1-12, default 6)
  • action="list":   (none) — optional: zone (default wiki), slug_prefix
  • action="read":   slug (wiki slug or src_<16hex> for source)`
}

func (t *SearchTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"search", "list", "read"},
				"description": "Which operation: search (hybrid lookup), list (enumerate slugs), read (fetch body).",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language query. Required for action=search.",
			},
			"slug": map[string]any{
				"type":        "string",
				"description": "Page slug or source_id (src_<16hex>). Required for action=read.",
			},
			"zone": map[string]any{
				"type":        "string",
				"enum":        []string{"wiki", "source", "all"},
				"description": "Zone filter: wiki = wiki/*.md only, source = src_* only, all = both. Default: all for search, wiki for list.",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": "action=search max results (1-12, default 6).",
				"minimum":     1,
				"maximum":     12,
			},
			"slug_prefix": map[string]any{
				"type":        "string",
				"description": "action=list: return only slugs starting with this prefix.",
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "search", RequiredKeys: []string{"query"}},
			{Name: "list", RequiredKeys: nil},
			{Name: "read", RequiredKeys: []string{"slug"}},
		}),
	}
}

var (
	searchToolValidActions = []string{"search", "list", "read"}
	searchToolActionHints  = []ActionHint{
		{Name: "search", RequiredKeys: []string{"query"}},
		{Name: "read", RequiredKeys: []string{"slug"}},
	}
)

func (t *SearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	action := strings.TrimSpace(stringArg(args, "action"))
	switch action {
	case "search":
		return t.doSearch(ctx, args)
	case "list":
		return t.doList(ctx, args)
	case "read":
		return t.doRead(ctx, args)
	case "":
		return "", ActionRequiredError("search", searchToolValidActions, args, searchToolActionHints, "search")
	default:
		return "", UnknownActionError("search", action, searchToolValidActions, args)
	}
}

// doSearch delegates to SearchMemoryTool after translating zone → scope.
// chat_id is passed through when present so archive results are scoped to
// the current conversation (injected by ToolArgumentsForTool upstream).
func (t *SearchTool) doSearch(ctx context.Context, args map[string]any) (string, error) {
	if t.searchMem == nil {
		return "", fmt.Errorf("search: search backend unavailable")
	}
	zone := searchToolZoneArg(args)
	delegateArgs := map[string]any{
		"query": args["query"],
		"scope": searchToolZoneToScope(zone),
	}
	if topK, ok := args["top_k"]; ok {
		delegateArgs["limit"] = topK
	}
	if chatID, ok := args["chat_id"]; ok {
		delegateArgs["chat_id"] = chatID
	}
	return t.searchMem.Execute(ctx, delegateArgs)
}

// searchListMatch is one entry in the action=list result.
type searchListMatch struct {
	Slug        string `json:"slug"`
	Title       string `json:"title,omitempty"`
	Kind        string `json:"kind"`
	LastUpdated string `json:"last_updated,omitempty"`
}

// doList enumerates wiki page slugs filtered by zone and optional prefix.
func (t *SearchTool) doList(_ context.Context, args map[string]any) (string, error) {
	zone := searchToolZoneArg(args)
	if zone == "" {
		zone = "wiki"
	}
	prefix := strings.ToLower(strings.TrimSpace(stringArg(args, "slug_prefix")))

	var matches []searchListMatch
	if (zone == "wiki" || zone == "all") && t.wikiReader != nil {
		slugs, err := t.wikiReader.ListPages()
		if err != nil {
			return "", fmt.Errorf("search list: list wiki pages: %w", err)
		}
		for _, slug := range slugs {
			if prefix != "" && !strings.HasPrefix(slug, prefix) {
				continue
			}
			entry := searchListMatch{Slug: slug, Kind: "wiki"}
			if page, readErr := t.wikiReader.ReadPage(slug); readErr == nil && page != nil {
				entry.Title = page.Title
				entry.LastUpdated = page.UpdatedAt
			}
			matches = append(matches, entry)
			if len(matches) >= 20 {
				break
			}
		}
	}

	out := struct {
		Matches    []searchListMatch `json:"matches"`
		NextCursor string            `json:"next_cursor"`
	}{
		NextCursor: "",
	}
	if len(matches) > 0 {
		out.Matches = matches
	} else {
		out.Matches = make([]searchListMatch, 0)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("search list: marshal: %w", err)
	}
	return string(b), nil
}

// doRead fetches the body of a wiki page or source extract by slug.
func (t *SearchTool) doRead(ctx context.Context, args map[string]any) (string, error) {
	slug, err := requiredString(args, "slug")
	if err != nil {
		return "", fmt.Errorf("search read: %w", err)
	}
	slug = strings.TrimSpace(slug)
	if strings.HasPrefix(slug, "src_") {
		return t.readSource(ctx, slug)
	}
	return t.readWikiPage(slug)
}

// readWikiPage returns JSON {slug, title, body, frontmatter} for a wiki page.
func (t *SearchTool) readWikiPage(slug string) (string, error) {
	if t.wikiReader == nil {
		return "", fmt.Errorf("search read: wiki reader unavailable")
	}
	page, err := t.wikiReader.ReadPage(slug)
	if err != nil {
		return "", fmt.Errorf("search read wiki: %w", err)
	}
	type fmResult struct {
		Category      string   `json:"category,omitempty"`
		Tags          []string `json:"tags,omitempty"`
		UpdatedAt     string   `json:"updated_at,omitempty"`
		SchemaVersion int      `json:"schema_version,omitempty"`
	}
	result := struct {
		Slug        string   `json:"slug"`
		Title       string   `json:"title"`
		Body        string   `json:"body"`
		Frontmatter fmResult `json:"frontmatter"`
	}{
		Slug:  slug,
		Title: page.Title,
		Body:  page.Body,
		Frontmatter: fmResult{
			Category:      page.Category,
			Tags:          page.Tags,
			UpdatedAt:     page.UpdatedAt,
			SchemaVersion: page.SchemaVersion,
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("search read wiki: marshal: %w", err)
	}
	return string(b), nil
}

// readSource delegates to ReadSourceTool for source extract markdown.
func (t *SearchTool) readSource(ctx context.Context, id string) (string, error) {
	if t.sourceReader == nil {
		return "", fmt.Errorf("search read: source reader unavailable for %s", id)
	}
	return t.sourceReader.Execute(ctx, map[string]any{
		"source_id": id,
		"mode":      "ocr",
	})
}

func searchToolZoneArg(args map[string]any) string {
	return strings.ToLower(strings.TrimSpace(stringArg(args, "zone")))
}

// searchToolZoneToScope maps the zone enum to the search_memory scope parameter.
func searchToolZoneToScope(zone string) string {
	switch zone {
	case "wiki":
		return "wiki"
	case "source":
		return "sources"
	default: // "all" or empty
		return "all"
	}
}
