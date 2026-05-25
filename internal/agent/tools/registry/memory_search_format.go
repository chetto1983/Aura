package tools

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aura/aura/internal/storage/memoryindex"
)

// formatMemoryResults renders the result list as plain markdown. No JSON
// envelope, no "Evidence envelope:" appendix. Each hit is two lines max
// so the agent can scan a top-K list without drowning in curator
// metadata.
//
// Format (per hit):
//
//	- [kind] identifier — title  (age)  score=0.92
//	  snippet text from the page
//
// Optional extras only when they add signal:
//   - page=N / span=bytes=A-B   when the hit is a chunked source
//   - handle=…                  when the precise re-read handle differs
//   - follow_up=…               when the follow-up command differs from identifier
//   - ⚠ stale freshness=indexed_at=…  ONLY when the index is degraded
//
// Removed in 2026-05-25 (chat 1148481707 turn 263 over-search bug): the
// per-hit dump used to include schema_version, prompt_version, created_at,
// unversioned, file=wiki/…, category, tags, related[], sources[], and the
// [exact=… fts=… vector=…] component breakdown. With a top-K of 6 that
// added ~1.5 KB of curator noise per call and the agent treated low raw
// scores as "nothing matched", over-firing search to compensate. Snippet
// + identifier + score is all the agent needs to decide cite-or-dig.
func formatMemoryResults(query string, results []memoryResult, warnings []string, now time.Time) string {
	var sb strings.Builder
	cleanedWarnings := cleanWarnings(warnings)
	if len(results) == 0 {
		fmt.Fprintf(&sb, "No memory found for %q.", query)
		appendWarnings(&sb, cleanedWarnings)
		return sb.String()
	}
	fmt.Fprintf(&sb, "%d memory hit(s) for %q:", len(results), query)
	for _, r := range results {
		writeMemoryHit(&sb, r, now)
	}
	appendWarnings(&sb, cleanedWarnings)
	return truncateForToolContext(sb.String(), maxSourceToolChars)
}

// writeMemoryHit renders one search hit with the slim 2026-05-25 contract.
func writeMemoryHit(sb *strings.Builder, r memoryResult, now time.Time) {
	fmt.Fprintf(sb, "\n- [%s] %s", r.Kind, r.Identifier)
	if r.Title != "" {
		fmt.Fprintf(sb, " — %s", compactMemoryLine(r.Title))
	}
	if r.Role != "" {
		fmt.Fprintf(sb, " role=%s", r.Role)
	}
	if r.Page > 0 {
		fmt.Fprintf(sb, " page=%d", r.Page)
	}
	if r.ByteEnd > r.ByteStart {
		fmt.Fprintf(sb, " span=bytes=%d-%d", r.ByteStart, r.ByteEnd)
		if r.ChunkIndex > 0 {
			fmt.Fprintf(sb, " chunk=%d", r.ChunkIndex)
		}
	}
	if r.Handle != "" && r.Handle != r.Identifier {
		fmt.Fprintf(sb, " handle=%s", r.Handle)
	}
	if age := formatAge(now, r.UpdatedAt); age != "" {
		fmt.Fprintf(sb, " (%s)", age)
	}
	fmt.Fprintf(sb, " score=%.2f", r.Score)
	if fu := followUpHandle(r); fu != "" && fu != r.Identifier {
		fmt.Fprintf(sb, " follow_up=%s", fu)
	}
	if r.DegradedRead {
		sb.WriteString(" degraded_read=true")
		if freshness := formatFreshnessAnnotation(r); freshness != "" {
			fmt.Fprintf(sb, " freshness=%s", freshness)
		}
	}
	if r.Snippet != "" {
		fmt.Fprintf(sb, "\n  %s", r.Snippet)
	}
}

// formatFreshnessAnnotation builds the freshness= value when the hit is
// degraded. Emits only operationally-useful tokens (indexed_at, model,
// build) — collapsed into a single comma list to keep the line short.
func formatFreshnessAnnotation(r memoryResult) string {
	var parts []string
	if !r.IndexedAt.IsZero() {
		parts = append(parts, "indexed_at="+r.IndexedAt.UTC().Format(time.RFC3339))
	}
	if r.EmbeddingModelID != "" {
		parts = append(parts, "model="+truncateForToolContext(r.EmbeddingModelID, 12))
	}
	if r.IndexBuildID != "" {
		parts = append(parts, "build="+truncateForToolContext(r.IndexBuildID, 12))
	}
	if r.DegradedRead {
		parts = append(parts, "stale")
	}
	return strings.Join(parts, ",")
}

func appendWarnings(sb *strings.Builder, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	sb.WriteString("\nWarnings:")
	for _, warning := range warnings {
		fmt.Fprintf(sb, "\n- %s", warning)
	}
}

func formatAge(now, updated time.Time) string {
	if updated.IsZero() {
		return ""
	}
	d := max(now.Sub(updated), 0)
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
	offset = max(offset, 0)
	start := max(offset-limit/3, 0)
	end := start + limit
	if end > len(clean) {
		end = len(clean)
		start = max(end-limit, 0)
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

// formatIndexedDocumentResults renders a memoryindex.Document slice as plain
// markdown. Called by formatOperationalResults and formatUserMemoryResults with
// per-kind labels, so the two tools share identical formatting logic.
func formatIndexedDocumentResults(docs []memoryindex.Document, degradedRead bool, now time.Time, kindTag, countLabel string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d %s:", len(docs), countLabel)
	for _, doc := range docs {
		fmt.Fprintf(&sb, "\n- [%s] %s — %s", kindTag, doc.Handle, compactMemoryLine(doc.Title))
		if age := formatAge(now, doc.UpdatedAt); age != "" {
			fmt.Fprintf(&sb, " (%s)", age)
		}
		if doc.EmbeddingModelID != "" || doc.IndexBuildID != "" {
			var parts []string
			if !doc.UpdatedAt.IsZero() {
				parts = append(parts, "indexed_at="+doc.UpdatedAt.UTC().Format(time.RFC3339))
			}
			if doc.EmbeddingModelID != "" {
				model := doc.EmbeddingModelID
				if len(model) > 12 {
					model = model[:12]
				}
				parts = append(parts, "model="+model)
			}
			if doc.IndexBuildID != "" {
				build := doc.IndexBuildID
				if len(build) > 12 {
					build = build[:12]
				}
				parts = append(parts, "build="+build)
			}
			if len(parts) > 0 {
				fmt.Fprintf(&sb, " freshness=%s", strings.Join(parts, ","))
			}
		}
		if degradedRead {
			sb.WriteString(" degraded_read=true")
		}
		if doc.Body != "" {
			snippet, _ := snippetAround(doc.Body, "", 200)
			if snippet != "" {
				fmt.Fprintf(&sb, "\n  %s", snippet)
			}
		}
	}
	return sb.String()
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
