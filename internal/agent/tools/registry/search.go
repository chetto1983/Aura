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
//   - action=lessons — approved operational lessons
//   - action=user_facts — approved user facts/preferences
//   - action=god_nodes — most-connected wiki graph nodes
//   - action=path — shortest wiki graph path
//   - action=subgraph — query-seeded wiki subgraph capsule
type SearchTool struct {
	searchMem         *SearchMemoryTool
	wikiReader        wiki.PageReader
	sourceReader      *ReadSourceTool // nil when source store not wired
	operationalRecall *RecallOperationalTool
	userMemoryRecall  *RecallUserMemoryTool
	godNodes          *RecallGodNodesTool
	wikiPath          *WikiPathTool
	wikiSubgraph      *WikiSubgraphTool
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

// WithRecallAndGraphActions attaches implementation delegates for the folded
// recall/wiki graph actions. The old tool names stay as internal types; only
// the LLM-facing surface is consolidated into search(action=...).
func (t *SearchTool) WithRecallAndGraphActions(
	operational *RecallOperationalTool,
	userMemory *RecallUserMemoryTool,
	godNodes *RecallGodNodesTool,
	wikiPath *WikiPathTool,
	wikiSubgraph *WikiSubgraphTool,
) *SearchTool {
	if t == nil {
		return nil
	}
	t.operationalRecall = operational
	t.userMemoryRecall = userMemory
	t.godNodes = godNodes
	t.wikiPath = wikiPath
	t.wikiSubgraph = wikiSubgraph
	return t
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
	return `Read-only. Unified knowledge lookup over wiki pages, sources, and conversation archive.

EXAMPLES — copy the shape exactly:

  search({"action":"search","query":"corso base robot","top_k":6})
  search({"action":"search","query":"davide preferences","zone":"wiki"})
  search({"action":"list","slug_prefix":"robot"})
  search({"action":"read","slug":"davide-marchetto"})
  search({"action":"read","slug":"src_0ec1b02e112f0ca4"})
  search({"action":"lessons","tool_name":"web","limit":5})
  search({"action":"user_facts","category":"preference","limit":5})
  search({"action":"god_nodes","top_k":8})
  search({"action":"subgraph","query":"robot calibration","depth":2})
  search({"action":"path","from_slug":"robot","to_slug":"frame"})

action REQUIRED; valid: "search", "list", "read", "lessons", "user_facts", "god_nodes", "subgraph", "path".

Per-action required:
  • search → query
  • list   → nothing (slug_prefix optional filter)
  • read   → slug (wiki slug OR src_<16hex>)
  • lessons → nothing (query/tool_name/error_class optional)
  • user_facts → nothing (query/category optional)
  • god_nodes → nothing (top_k optional)
  • subgraph → query
  • path → from_slug AND to_slug

Optional: zone ("wiki"/"source"/"all"), top_k (1-12 default 6), limit (1-20 for recall actions).`
}

func (t *SearchTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"search", "list", "read", "lessons", "user_facts", "god_nodes", "subgraph", "path"},
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
				"description": "action=search/god_nodes max results (1-12 for search, up to 50 for god_nodes).",
				"minimum":     1,
				"maximum":     50,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "action=lessons/user_facts max results (1-20, default 5).",
				"minimum":     1,
				"maximum":     20,
			},
			"slug_prefix": map[string]any{
				"type":        "string",
				"description": "action=list: return only slugs starting with this prefix.",
			},
			"tool_name": map[string]any{
				"type":        "string",
				"description": "action=lessons: restrict operational lessons to this tool name.",
			},
			"error_class": map[string]any{
				"type":        "string",
				"description": "action=lessons: restrict operational lessons to this error class.",
			},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"person", "preference", "fact", "todo"},
				"description": "action=user_facts: restrict results to a memory category.",
			},
			"from_slug": map[string]any{
				"type":        "string",
				"description": "action=path: start wiki slug.",
			},
			"to_slug": map[string]any{
				"type":        "string",
				"description": "action=path: destination wiki slug.",
			},
			"max_hops": map[string]any{
				"type":        "integer",
				"description": "action=path: maximum graph hops (default 5, max 20).",
				"minimum":     1,
				"maximum":     20,
			},
			"depth": map[string]any{
				"type":        "integer",
				"description": "action=subgraph: BFS expansion depth (1-3, default 2).",
				"minimum":     1,
				"maximum":     3,
			},
			"budget_tokens": map[string]any{
				"type":        "integer",
				"description": "action=subgraph: approximate token budget (default 1500, max 4000).",
				"minimum":     1,
				"maximum":     4000,
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "search", RequiredKeys: []string{"query"}},
			{Name: "list", RequiredKeys: nil},
			{Name: "read", RequiredKeys: []string{"slug"}},
			{Name: "lessons", RequiredKeys: nil},
			{Name: "user_facts", RequiredKeys: nil},
			{Name: "god_nodes", RequiredKeys: nil},
			{Name: "subgraph", RequiredKeys: []string{"query"}},
			{Name: "path", RequiredKeys: []string{"from_slug", "to_slug"}},
		}),
		"examples": []any{
			map[string]any{"action": "search", "query": "corso base robot", "top_k": 6},
			map[string]any{"action": "search", "query": "davide preferences", "zone": "wiki"},
			map[string]any{"action": "list", "slug_prefix": "robot"},
			map[string]any{"action": "read", "slug": "davide-marchetto"},
			map[string]any{"action": "read", "slug": "src_0ec1b02e112f0ca4"},
			map[string]any{"action": "lessons", "tool_name": "web", "limit": 5},
			map[string]any{"action": "user_facts", "category": "preference", "limit": 5},
			map[string]any{"action": "god_nodes", "top_k": 8},
			map[string]any{"action": "subgraph", "query": "robot calibration", "depth": 2},
			map[string]any{"action": "path", "from_slug": "robot", "to_slug": "frame"},
		},
	}
}

var (
	searchToolValidActions = []string{"search", "list", "read", "lessons", "user_facts", "god_nodes", "subgraph", "path"}
	searchToolActionHints  = []ActionHint{
		{Name: "search", RequiredKeys: []string{"query"}},
		{Name: "read", RequiredKeys: []string{"slug"}},
		{Name: "path", RequiredKeys: []string{"from_slug", "to_slug"}},
	}
)

func (t *SearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if rewritten, ok := RewriteVerbKeyAsAction(args, searchToolValidActions, searchToolActionHints); ok {
		args = rewritten
	}
	action := strings.TrimSpace(stringArg(args, "action"))
	switch action {
	case "search":
		return t.doSearch(ctx, args)
	case "list":
		return t.doList(ctx, args)
	case "read":
		return t.doRead(ctx, args)
	case "lessons":
		return t.doLessons(ctx, args)
	case "user_facts":
		return t.doUserFacts(ctx, args)
	case "god_nodes":
		return t.doGodNodes(ctx, args)
	case "subgraph":
		return t.doSubgraph(ctx, args)
	case "path":
		return t.doPath(ctx, args)
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

func (t *SearchTool) doLessons(ctx context.Context, args map[string]any) (string, error) {
	if t.operationalRecall == nil {
		return "", fmt.Errorf("search lessons: operational recall unavailable")
	}
	delegateArgs := searchDelegateArgs(args, "query", "tool_name", "error_class", "limit")
	searchCopyTopKToLimit(delegateArgs, args)
	return t.operationalRecall.Execute(ctx, delegateArgs)
}

func (t *SearchTool) doUserFacts(ctx context.Context, args map[string]any) (string, error) {
	if t.userMemoryRecall == nil {
		return "", fmt.Errorf("search user_facts: user memory recall unavailable")
	}
	delegateArgs := searchDelegateArgs(args, "query", "category", "limit")
	searchCopyTopKToLimit(delegateArgs, args)
	return t.userMemoryRecall.Execute(ctx, delegateArgs)
}

func (t *SearchTool) doGodNodes(ctx context.Context, args map[string]any) (string, error) {
	if t.godNodes == nil {
		return "", fmt.Errorf("search god_nodes: wiki graph unavailable")
	}
	return t.godNodes.Execute(ctx, searchDelegateArgs(args, "top_k"))
}

func (t *SearchTool) doSubgraph(ctx context.Context, args map[string]any) (string, error) {
	if t.wikiSubgraph == nil {
		return "", fmt.Errorf("search subgraph: wiki subgraph unavailable")
	}
	return t.wikiSubgraph.Execute(ctx, searchDelegateArgs(args, "query", "depth", "budget_tokens"))
}

func (t *SearchTool) doPath(ctx context.Context, args map[string]any) (string, error) {
	if t.wikiPath == nil {
		return "", fmt.Errorf("search path: wiki graph unavailable")
	}
	return t.wikiPath.Execute(ctx, searchDelegateArgs(args, "from_slug", "to_slug", "max_hops"))
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

func searchDelegateArgs(args map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := args[key]; ok {
			out[key] = value
		}
	}
	return out
}

func searchCopyTopKToLimit(delegateArgs, args map[string]any) {
	if _, ok := delegateArgs["limit"]; ok {
		return
	}
	if topK, ok := args["top_k"]; ok {
		delegateArgs["limit"] = topK
	}
}
