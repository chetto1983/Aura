package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	toolSearchDefaultLimit  = 5
	toolSearchAbsoluteLimit = 20
)

// toolCatalogReader is the read-side subset of *Registry that tool_search
// needs. Satisfied by *Registry without modification.
type toolCatalogReader interface {
	FullDefinitions() []ToolDefinition
}

// ToolSearchTool is the LLM's entry point for loading full JSON schemas of
// deferred tools on demand. It is always-on itself (VisibilityAlwaysOn) so
// the LLM can always call it without a prior search step.
//
// Three query forms (mirror Claude Code's ToolSearch verbatim):
//
//	select:name1,name2  — exact fetch; missing names produce a clear error
//	keyword keyword     — heuristic search: name_match*2 + desc_match
//	+required keyword   — require term in name/desc; rest for ranking
type ToolSearchTool struct {
	catalog toolCatalogReader
}

// NewToolSearchTool returns a ToolSearchTool backed by the registry.
// Returns nil when catalog is nil so callers can compose unconditionally.
func NewToolSearchTool(catalog toolCatalogReader) *ToolSearchTool {
	if catalog == nil {
		return nil
	}
	return &ToolSearchTool{catalog: catalog}
}

func (t *ToolSearchTool) Name() string { return "tool_search" }

func (t *ToolSearchTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		VisibilityTier: VisibilityAlwaysOn,
		ReadOnlyHint:   true,
		IdempotentHint: true,
	}
}

func (t *ToolSearchTool) Description() string {
	return "Fetches full schema definitions for deferred tools so they can be called. Use query=\"select:<name>\" for exact fetch, or keyword query for heuristic search."
}

func (t *ToolSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"query"},
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query. Forms: \"select:name1,name2\" for exact fetch; \"keyword keyword\" for heuristic; \"+required_term keyword\" to require a term in name/description.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum results to return (default 5, max 20).",
				"minimum":     1,
				"maximum":     toolSearchAbsoluteLimit,
			},
		},
	}
}

// toolSearchEntry is one entry in the JSON result envelope.
type toolSearchEntry struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// toolSearchResponse is the JSON envelope returned by Execute.
type toolSearchResponse struct {
	Tools  []toolSearchEntry `json:"tools"`
	Errors []string          `json:"errors,omitempty"`
}

func (t *ToolSearchTool) Execute(_ context.Context, args map[string]any) (string, error) {
	query, err := requiredString(args, "query")
	if err != nil {
		return "", fmt.Errorf("tool_search: %w", err)
	}
	limit := intArg(args, "max_results", toolSearchDefaultLimit, 1, toolSearchAbsoluteLimit)

	defs := t.catalog.FullDefinitions()

	var resp toolSearchResponse
	if strings.HasPrefix(query, "select:") {
		resp = t.execSelect(query, defs)
	} else {
		resp = t.execKeyword(query, defs, limit)
	}

	data, jsonErr := json.Marshal(resp)
	if jsonErr != nil {
		return "", fmt.Errorf("tool_search: marshal: %w", jsonErr)
	}
	return string(data), nil
}

// execSelect handles "select:name1,name2" — exact fetch by name.
func (t *ToolSearchTool) execSelect(query string, defs []ToolDefinition) toolSearchResponse {
	namesRaw := strings.TrimPrefix(query, "select:")
	requestedNames := strings.Split(namesRaw, ",")

	byName := make(map[string]ToolDefinition, len(defs))
	for _, d := range defs {
		byName[strings.ToLower(d.Name)] = d
	}

	var tools []toolSearchEntry
	var missing []string
	for _, raw := range requestedNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if d, ok := byName[strings.ToLower(name)]; ok {
			tools = append(tools, toolDefToEntry(d))
		} else {
			missing = append(missing, name)
		}
	}

	var errs []string
	if len(tools) == 0 && len(missing) > 0 {
		errs = append(errs, "no tools matched: "+strings.Join(missing, ", "))
	} else {
		for _, name := range missing {
			errs = append(errs, "no tool named: "+name)
		}
	}
	if tools == nil {
		tools = []toolSearchEntry{}
	}
	return toolSearchResponse{Tools: tools, Errors: errs}
}

// execKeyword handles keyword and +required queries.
// Scoring: name_match_score * 2 + desc_match_score per the story spec.
func (t *ToolSearchTool) execKeyword(query string, defs []ToolDefinition, limit int) toolSearchResponse {
	required, optional := parseToolSearchQuery(query)
	reqTerms := searchTerms(strings.Join(required, " "))
	terms := searchTerms(strings.Join(optional, " "))

	type scored struct {
		def   ToolDefinition
		score int
	}
	var candidates []scored

	for _, d := range defs {
		if !toolMeetsRequired(d, reqTerms) {
			continue
		}
		score := toolSearchScore(terms, d.Name, d.Description)
		if score == 0 && len(reqTerms) > 0 {
			score = 1 // required-match baseline when no optional terms score
		}
		if score > 0 || len(terms) == 0 {
			candidates = append(candidates, scored{def: d, score: score})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].def.Name < candidates[j].def.Name
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	tools := make([]toolSearchEntry, 0, len(candidates))
	for _, c := range candidates {
		tools = append(tools, toolDefToEntry(c.def))
	}
	return toolSearchResponse{Tools: tools}
}

// parseToolSearchQuery splits tokens into required (+term) and optional buckets.
func parseToolSearchQuery(query string) (required, optional []string) {
	for tok := range strings.FieldsSeq(query) {
		if strings.HasPrefix(tok, "+") && len(tok) > 1 {
			required = append(required, tok[1:])
		} else {
			optional = append(optional, tok)
		}
	}
	return required, optional
}

// toolMeetsRequired returns true when all required terms appear in the tool's
// name or description (case-insensitive substring match).
func toolMeetsRequired(d ToolDefinition, reqTerms []string) bool {
	if len(reqTerms) == 0 {
		return true
	}
	text := strings.ToLower(d.Name + " " + d.Description)
	for _, term := range reqTerms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

// toolSearchScore returns name_match_count*2 + desc_match_count for ranking.
func toolSearchScore(terms []string, name, desc string) int {
	nameLower := strings.ToLower(name)
	descLower := strings.ToLower(desc)
	score := 0
	for _, term := range terms {
		if strings.Contains(nameLower, term) {
			score += 2
		}
		if strings.Contains(descLower, term) {
			score += 1
		}
	}
	return score
}

func toolDefToEntry(d ToolDefinition) toolSearchEntry {
	return toolSearchEntry{
		Name:        d.Name,
		Description: d.Description,
		Parameters:  d.Parameters,
	}
}
