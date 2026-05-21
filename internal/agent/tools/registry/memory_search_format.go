package tools

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aura/aura/internal/storage/memoryindex"
)

// formatMemoryResults renders the result list as plain markdown. No JSON
// envelope, no "Evidence envelope:" appendix. Each line is short enough for
// the model to scan, and a snippet (one line, trimmed) follows when present.
//
// Format (per hit):
//
//   - [kind] handle — title (age) score=0.92
//     snippet text from the page
func formatMemoryResults(query string, results []memoryResult, warnings []string, now time.Time) string {
	var sb strings.Builder
	cleanedWarnings := cleanWarnings(warnings)
	if len(results) == 0 {
		fmt.Fprintf(&sb, "No memory found for %q.", query)
		if len(cleanedWarnings) > 0 {
			sb.WriteString("\nWarnings:")
			for _, warning := range cleanedWarnings {
				fmt.Fprintf(&sb, "\n- %s", warning)
			}
		}
		return sb.String()
	}
	fmt.Fprintf(&sb, "%d memory hit(s) for %q:", len(results), query)
	for _, r := range results {
		fmt.Fprintf(&sb, "\n- [%s] %s", r.Kind, r.Identifier)
		if r.Title != "" {
			fmt.Fprintf(&sb, " — %s", compactMemoryLine(r.Title))
		}
		if r.Role != "" {
			fmt.Fprintf(&sb, " role=%s", r.Role)
		}
		if r.Page > 0 {
			fmt.Fprintf(&sb, " page=%d", r.Page)
		}
		if r.ByteEnd > r.ByteStart {
			fmt.Fprintf(&sb, " span=bytes=%d-%d", r.ByteStart, r.ByteEnd)
			if r.ChunkIndex > 0 {
				fmt.Fprintf(&sb, " chunk=%d", r.ChunkIndex)
			}
		}
		// Surface the handle when it is a richer follow-up token than the
		// identifier alone (e.g. a source's page-targeted "source:src_xxx#page=2").
		// This is what source(action=read) consumes for precise re-reads.
		if r.Handle != "" && r.Handle != r.Identifier {
			fmt.Fprintf(&sb, " handle=%s", r.Handle)
		}
		if age := formatAge(now, r.UpdatedAt); age != "" {
			fmt.Fprintf(&sb, " (%s)", age)
		}
		fmt.Fprintf(&sb, " score=%.2f", r.Score)
		if r.ScoreExact != 0 || r.ScoreFTS != 0 || r.ScoreVector != 0 {
			fmt.Fprintf(&sb, " [exact=%.2f fts=%.2f vector=%.2f]", r.ScoreExact, r.ScoreFTS, r.ScoreVector)
		}
		if r.SchemaVersion > 0 {
			fmt.Fprintf(&sb, " schema=%d", r.SchemaVersion)
		}
		if r.PromptVersion != "" {
			fmt.Fprintf(&sb, " prompt=%s", r.PromptVersion)
		}
		if !r.CreatedAt.IsZero() {
			fmt.Fprintf(&sb, " created=%s", r.CreatedAt.UTC().Format(time.RFC3339))
		}
		if r.Unversioned {
			fmt.Fprintf(&sb, " unversioned=true")
		}
		if fu := followUpHandle(r); fu != "" {
			fmt.Fprintf(&sb, " follow_up=%s", fu)
		}
		// Surface the on-disk file path so the model can read the page
		// directly (read_file) without a second list_files round-trip
		// (gap the model itself flagged in 2026-05-11 turn 2393).
		if r.FilePath != "" {
			fmt.Fprintf(&sb, " file=wiki/%s", r.FilePath)
		}
		if r.Category != "" {
			fmt.Fprintf(&sb, " category=%s", r.Category)
		}
		if len(r.Tags) > 0 {
			fmt.Fprintf(&sb, " tags=%s", strings.Join(r.Tags, ","))
		}
		if len(r.Related) > 0 {
			// Cap the inline related list — pages with 20+ links would
			// otherwise dwarf the snippet. The full list is reachable via
			// read_file when the model wants the graph.
			rel := r.Related
			if len(rel) > 5 {
				rel = rel[:5]
			}
			fmt.Fprintf(&sb, " related=[%s]", strings.Join(rel, ","))
		}
		if len(r.Sources) > 0 {
			sources := r.Sources
			if len(sources) > 5 {
				sources = sources[:5]
			}
			fmt.Fprintf(&sb, " sources=[%s]", strings.Join(sources, ","))
		}
		// Freshness annotation: emitted when any freshness column is non-empty or
		// the hit is marked degraded. Only compact-memory hits carry these fields.
		{
			var parts []string
			if !r.IndexedAt.IsZero() {
				parts = append(parts, "indexed_at="+r.IndexedAt.UTC().Format(time.RFC3339))
			}
			if r.EmbeddingModelID != "" {
				model := r.EmbeddingModelID
				if len(model) > 12 {
					model = model[:12]
				}
				parts = append(parts, "model="+model)
			}
			if r.IndexBuildID != "" {
				build := r.IndexBuildID
				if len(build) > 12 {
					build = build[:12]
				}
				parts = append(parts, "build="+build)
			}
			if r.DegradedRead {
				parts = append(parts, "stale")
			}
			if len(parts) > 0 {
				fmt.Fprintf(&sb, " freshness=%s", strings.Join(parts, ","))
			}
		}
		if r.DegradedRead {
			fmt.Fprintf(&sb, " degraded_read=true")
		}
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
	return truncateForToolContext(sb.String(), maxSourceToolChars)
}

func formatAge(now, updated time.Time) string {
	if updated.IsZero() {
		return ""
	}
	d := now.Sub(updated)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	}
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

// followUpHandle returns a tool invocation or wiki-link that the LLM can use
// to expand a memory hit into its full content. Only tool names that exist in
// the registry are used — no invented names.
//
// Mapping:
//   - wiki / wiki_page / graph_node: [[slug]] (the identifier already is [[slug]])
//   - graph_index: [[slug]] (identifier is bare slug for graph_index kind)
//   - source: source(action=read,source_id=<id>,mode=ocr[,byte_start=<n>,byte_end=<n>])
//   - archive: search_memory(scope=archive) — read_memory does not exist in registry
//   - proposal: search_memory(scope=proposals) — read_memory does not exist in registry
func followUpHandle(r memoryResult) string {
	switch r.Kind {
	case "wiki", "wiki_page", "graph_node":
		return r.Identifier
	case "graph_index":
		return "[[" + r.Identifier + "]]"
	case memoryindex.KindSource:
		if r.ByteEnd > r.ByteStart {
			return fmt.Sprintf("source(action=read,source_id=%s,mode=ocr,byte_start=%d,byte_end=%d)", r.Identifier, r.ByteStart, r.ByteEnd)
		}
		return "source(action=read,source_id=" + r.Identifier + ",mode=ocr)"
	case memoryindex.KindArchive:
		return "search_memory(scope=archive)"
	case memoryindex.KindProposal:
		return "search_memory(scope=proposals)"
	default:
		return ""
	}
}
