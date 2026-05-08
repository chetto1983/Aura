package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/aura/aura/internal/memoryindex"
	"github.com/aura/aura/internal/search"
)

const (
	searchMemoryDefaultLimit   = 6
	searchMemoryMaxLimit       = 12
	searchMemorySnippetLimit   = 260
	searchMemoryDefaultTimeout = 5 * time.Second
)

type compactMemorySearcher interface {
	Search(ctx context.Context, query string, filter memoryindex.Filter) ([]memoryindex.Document, error)
}

type SearchMemoryTool struct {
	wiki    search.Searcher
	compact compactMemorySearcher
	timeout time.Duration
}

func NewSearchMemoryTool(wiki search.Searcher, compact compactMemorySearcher) *SearchMemoryTool {
	return NewSearchMemoryToolWithTimeout(wiki, compact, searchMemoryDefaultTimeout)
}

func NewSearchMemoryToolWithTimeout(wiki search.Searcher, compact compactMemorySearcher, timeout time.Duration) *SearchMemoryTool {
	if wiki == nil && compact == nil {
		return nil
	}
	return &SearchMemoryTool{wiki: wiki, compact: compact, timeout: timeout}
}

func (t *SearchMemoryTool) Name() string { return "search_memory" }

func (t *SearchMemoryTool) Description() string {
	return "Search Aura memory across wiki pages, stored sources/OCR, and the conversation archive. Returns compact readable evidence plus a JSON evidence envelope with wiki slugs, source IDs, conversation turn IDs, snippets, and source page numbers when available."
}

func (t *SearchMemoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language query or keywords to search in Aura memory.",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "Optional memory scope. Defaults to all.",
				"enum":        []string{"all", "wiki", "sources", "archive", "proposals"},
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum evidence items to return (default 6, max 12).",
				"minimum":     1,
				"maximum":     searchMemoryMaxLimit,
			},
			"chat_id": map[string]any{
				"type":        "integer",
				"description": "Optional chat ID to restrict conversation archive search.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchMemoryTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", errors.New("search_memory: tool unavailable")
	}
	query, err := requiredString(args, "query")
	if err != nil {
		return "", fmt.Errorf("search_memory: %w", err)
	}
	scopes, err := memoryScopes(stringArg(args, "scope"))
	if err != nil {
		return "", err
	}
	limit := intArg(args, "limit", searchMemoryDefaultLimit, 1, searchMemoryMaxLimit)
	chatID := int64Arg(args, "chat_id")
	searchCtx, cancel := t.searchContext(ctx)
	defer cancel()

	var warnings []string
	results := make([]memoryResult, 0, limit*3)
	if scopes["wiki"] {
		wikiResults, wikiWarnings := t.searchWiki(searchCtx, query, limit)
		results = append(results, wikiResults...)
		warnings = append(warnings, wikiWarnings...)
	}
	if scopes["sources"] {
		compactResults, compactWarnings := t.searchCompact(searchCtx, query, []string{memoryindex.KindSource}, chatID, limit)
		results = append(results, compactResults...)
		warnings = append(warnings, compactWarnings...)
	}
	if scopes["archive"] {
		compactResults, compactWarnings := t.searchCompact(searchCtx, query, []string{memoryindex.KindArchive}, chatID, limit)
		results = append(results, compactResults...)
		warnings = append(warnings, compactWarnings...)
	}
	if scopes["proposals"] {
		compactResults, compactWarnings := t.searchCompact(searchCtx, query, []string{memoryindex.KindProposal}, chatID, limit)
		results = append(results, compactResults...)
		warnings = append(warnings, compactWarnings...)
	}

	calibrateMemoryScores(results)
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Identifier < results[j].Identifier
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return formatMemoryResults(query, results, warnings), nil
}

func (t *SearchMemoryTool) searchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if t == nil || t.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, t.timeout)
}

type memoryResult struct {
	Kind       string
	Identifier string
	Title      string
	Role       string
	Snippet    string
	Page       int
	Score      float64
	Handle     string
}

type memoryEvidenceEnvelope struct {
	Query    string               `json:"query"`
	Items    []memoryEvidenceItem `json:"items"`
	Warnings []string             `json:"warnings,omitempty"`
}

type memoryEvidenceItem struct {
	Kind    string  `json:"kind"`
	ID      string  `json:"id"`
	Title   string  `json:"title,omitempty"`
	Role    string  `json:"role,omitempty"`
	Page    int     `json:"page,omitempty"`
	Score   float64 `json:"score"`
	Handle  string  `json:"handle,omitempty"`
	Snippet string  `json:"snippet,omitempty"`
}

func (t *SearchMemoryTool) searchWiki(ctx context.Context, query string, limit int) ([]memoryResult, []string) {
	if t.wiki == nil {
		return nil, []string{"wiki search unavailable"}
	}
	if !t.wiki.IsIndexed() {
		return nil, []string{"wiki index not ready"}
	}
	results, err := t.wiki.Search(ctx, query, limit)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, []string{"wiki search timed out"}
		}
		return nil, []string{"wiki search failed: " + err.Error()}
	}
	out := make([]memoryResult, 0, len(results))
	for _, r := range results {
		snippet, _ := snippetAround(r.Content, query, searchMemorySnippetLimit)
		kind := strings.TrimSpace(r.Kind)
		if kind == "" || kind == "wiki_page" {
			kind = "wiki"
		}
		identifier := "[[" + r.Slug + "]]"
		if kind == "graph_index" {
			identifier = r.Slug
		}
		out = append(out, memoryResult{
			Kind:       kind,
			Identifier: identifier,
			Title:      r.Title,
			Snippet:    snippet,
			Score:      float64(r.Score),
			Handle:     identifier,
		})
	}
	return out, nil
}

func (t *SearchMemoryTool) searchCompact(ctx context.Context, query string, kinds []string, chatID int64, limit int) ([]memoryResult, []string) {
	if t.compact == nil {
		return nil, []string{"compact memory index unavailable"}
	}
	docs, err := t.compact.Search(ctx, query, memoryindex.Filter{
		Kinds:  kinds,
		ChatID: chatID,
		Limit:  limit,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, []string{"compact memory search timed out"}
		}
		return nil, []string{"compact memory search failed: " + err.Error()}
	}
	out := make([]memoryResult, 0, len(docs))
	for _, doc := range docs {
		snippet, _ := snippetAround(doc.Body, query, searchMemorySnippetLimit)
		identifier := compactIdentifier(doc)
		out = append(out, memoryResult{
			Kind:       doc.Kind,
			Identifier: identifier,
			Title:      doc.Title,
			Snippet:    snippet,
			Page:       doc.Page,
			Score:      doc.Score,
			Handle:     doc.Handle,
		})
	}
	return out, nil
}

func compactIdentifier(doc memoryindex.Document) string {
	if doc.Handle != "" {
		switch doc.Kind {
		case memoryindex.KindArchive, memoryindex.KindProposal:
			return doc.Handle
		}
	}
	if doc.Kind == memoryindex.KindSource && doc.SourceID != "" {
		return doc.SourceID
	}
	return doc.ID
}

func formatMemoryResults(query string, results []memoryResult, warnings []string) string {
	var sb strings.Builder
	cleanedWarnings := cleanWarnings(warnings)
	fmt.Fprintf(&sb, "Memory evidence for %q", query)
	if len(results) == 0 {
		sb.WriteString(": no matching evidence found.")
		if len(cleanedWarnings) > 0 {
			sb.WriteString("\nWarnings:")
			for _, warning := range cleanedWarnings {
				fmt.Fprintf(&sb, "\n- %s", warning)
			}
		}
		appendMemoryEvidenceEnvelope(&sb, query, results, cleanedWarnings)
		return sb.String()
	}
	fmt.Fprintf(&sb, " (%d result(s)):", len(results))
	for _, r := range results {
		fmt.Fprintf(&sb, "\n- [%s] %s", r.Kind, r.Identifier)
		if r.Title != "" {
			fmt.Fprintf(&sb, " - %s", compactMemoryLine(r.Title))
		}
		if r.Role != "" {
			fmt.Fprintf(&sb, " - role=%s", r.Role)
		}
		if r.Page > 0 {
			fmt.Fprintf(&sb, " - page=%d", r.Page)
		}
		if r.Handle != "" && r.Handle != r.Identifier {
			fmt.Fprintf(&sb, " - handle=%s", r.Handle)
		}
		fmt.Fprintf(&sb, " - score=%.2f", r.Score)
		if r.Snippet != "" {
			fmt.Fprintf(&sb, "\n  %s", r.Snippet)
		}
	}
	if len(cleanedWarnings) > 0 {
		sb.WriteString("\nWarnings:")
		for _, warning := range cleanedWarnings {
			fmt.Fprintf(&sb, "\n- %s", warning)
		}
	}
	appendMemoryEvidenceEnvelope(&sb, query, results, cleanedWarnings)
	return truncateForToolContext(sb.String(), maxSourceToolChars)
}

func appendMemoryEvidenceEnvelope(sb *strings.Builder, query string, results []memoryResult, warnings []string) {
	envelope := memoryEvidenceEnvelope{
		Query:    query,
		Items:    make([]memoryEvidenceItem, 0, len(results)),
		Warnings: warnings,
	}
	for _, r := range results {
		envelope.Items = append(envelope.Items, memoryEvidenceItem{
			Kind:    r.Kind,
			ID:      r.Identifier,
			Title:   r.Title,
			Role:    r.Role,
			Page:    r.Page,
			Score:   r.Score,
			Handle:  r.Handle,
			Snippet: compactMemoryLine(r.Snippet),
		})
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	sb.WriteString("\nEvidence envelope:\n")
	sb.Write(payload)
}

func memoryScopes(raw string) (map[string]bool, error) {
	scope := strings.ToLower(strings.TrimSpace(raw))
	if scope == "" {
		scope = "all"
	}
	out := map[string]bool{}
	switch scope {
	case "all":
		out["wiki"] = true
		out["sources"] = true
		out["archive"] = true
		out["proposals"] = true
	case "wiki":
		out["wiki"] = true
	case "source", "sources":
		out["sources"] = true
	case "archive", "conversations", "conversation":
		out["archive"] = true
	case "proposal", "proposals":
		out["proposals"] = true
	default:
		return nil, fmt.Errorf("search_memory: unsupported scope %q", raw)
	}
	return out, nil
}

func calibrateMemoryScores(results []memoryResult) {
	for i := range results {
		results[i].Score = calibratedMemoryScore(results[i].Kind, results[i].Score)
	}
}

func calibratedMemoryScore(kind string, raw float64) float64 {
	if raw <= 0 {
		return 0
	}
	switch strings.TrimSpace(kind) {
	case "wiki", "wiki_page", "graph_node", "graph_index":
		if raw <= 1 {
			return 0.55 + raw*0.40
		}
		return 0.95
	case "source":
		if raw <= 1 {
			return raw
		}
		return 0.45 + cappedRatio(raw, 12)*0.40
	case "archive":
		if raw <= 1 {
			return raw
		}
		return 0.35 + cappedRatio(raw, 12)*0.35
	case "proposal":
		if raw <= 1 {
			return raw
		}
		return 0.40 + cappedRatio(raw, 12)*0.35
	default:
		return cappedRatio(raw, 12) * 0.70
	}
}

func cappedRatio(value, cap float64) float64 {
	if value <= 0 || cap <= 0 {
		return 0
	}
	if value > cap {
		return 1
	}
	return value / cap
}

func queryTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len(field) < 2 || seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
	}
	return out
}

func snippetAround(text, query string, limit int) (string, int) {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return "", -1
	}
	offset := findQueryOffset(clean, query)
	if limit <= 0 || len(clean) <= limit {
		return compactMemoryLine(clean), offset
	}
	if offset < 0 {
		offset = 0
	}
	start := offset - limit/3
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(clean) {
		end = len(clean)
		start = end - limit
		if start < 0 {
			start = 0
		}
	}
	snippet := strings.TrimSpace(clean[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(clean) {
		snippet += "..."
	}
	return compactMemoryLine(snippet), offset
}

func findQueryOffset(text, query string) int {
	lower := strings.ToLower(text)
	phrase := strings.ToLower(strings.TrimSpace(query))
	if phrase != "" {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			return idx
		}
	}
	for _, term := range queryTerms(query) {
		if idx := strings.Index(lower, term); idx >= 0 {
			return idx
		}
	}
	return -1
}

func int64Arg(args map[string]any, key string) int64 {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	default:
		return 0
	}
}

func cleanWarnings(warnings []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" || seen[warning] {
			continue
		}
		seen[warning] = true
		out = append(out, warning)
	}
	return out
}

func compactMemoryLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
