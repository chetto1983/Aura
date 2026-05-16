package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

type ToolSearchResult struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Tags        []string          `json:"tags,omitempty"`
	InputSchema map[string]any    `json:"input_schema,omitempty"`
	Examples    []ToolCallExample `json:"examples,omitempty"`
	Score       int               `json:"score,omitempty"`
}

func (r *Registry) Search(query string, limit int, excluded ...string) []ToolSearchResult {
	query = strings.TrimSpace(query)
	if r == nil || query == "" {
		return nil
	}
	if limit <= 0 || limit > 5 {
		limit = 5
	}
	exclude := make(map[string]bool, len(excluded))
	for _, name := range excluded {
		name = strings.TrimSpace(name)
		if name != "" {
			exclude[name] = true
		}
	}
	terms := searchTerms(query)
	if len(terms) == 0 {
		return nil
	}

	r.mu.RLock()
	lexResults := make([]ToolSearchResult, 0, len(r.tools))
	for name, tool := range r.tools {
		if exclude[name] {
			continue
		}
		def := definitionForTool(tool)
		tags := toolCategories(tool)
		text := searchableToolText(def, tags)
		score := scoreToolText(terms, def.Name, text)
		if score <= 0 {
			continue
		}
		lexResults = append(lexResults, ToolSearchResult{
			Name:        def.Name,
			Description: def.Description,
			Tags:        tags,
			InputSchema: def.Parameters,
			Examples:    append([]ToolCallExample(nil), def.Examples...),
			Score:       score,
		})
	}
	vectorIdx := r.vectorIndex
	r.mu.RUnlock()

	lexByName := make(map[string]ToolSearchResult, len(lexResults))
	for _, res := range lexResults {
		lexByName[res.Name] = res
	}

	merged := make(map[string]ToolSearchResult)
	for name, lr := range lexByName {
		merged[name] = lr
	}

	if vectorIdx != nil {
		ctx := context.Background()
		if vecResults, vecErr := vectorIdx.Search(ctx, query, limit, excluded...); vecErr == nil {
			for _, vr := range vecResults {
				if lr, ok := lexByName[vr.Name]; ok {
					lr.Score = lr.Score + vr.Score
					merged[vr.Name] = lr
				} else {
					merged[vr.Name] = vr
				}
			}
		}
	}

	results := make([]ToolSearchResult, 0, len(merged))
	for _, res := range merged {
		results = append(results, res)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Name < results[j].Name
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func searchTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && !(r >= 'à' && r <= 'ÿ')
	})
	out := make([]string, 0, len(fields))
	seen := make(map[string]bool, len(fields))
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

// searchableToolEmbeddingText returns the text the embedding model sees per
// tool. Format borrowed from ToolRAG
// (https://github.com/antl3x/ToolRAG, packages/@antl3x-toolrag/source/ToolRAG.ts):
//
//	<tool name with _ and - turned into spaces>: <description>
//	    <param1> [<type>]: <param1 description>
//	    <param2> [<type>]: <param2 description>
//
// Why this shape:
//   - Replacing underscores/dashes with spaces lets the embed model tokenize
//     `web_fetch` as the natural-language phrase "web fetch" instead of a
//     single rare identifier — multilingual queries ("analizza l'URL", "leggi
//     la pagina") then anchor on the same surface area.
//   - Including per-parameter descriptions captures usage intent that a
//     terse top-line description omits. For tools like web_fetch whose
//     description is just "Fetch a web page by URL...", the `url` parameter
//     carries the bulk of the searchable signal.
//   - Case is preserved (no lowercase). Modern multilingual sentence-embedding
//     models case-fold internally; collapsing here just discards the few
//     entity-like tokens the embedder uses (URL, HTTP, README, OCR).
//
// Lex search (searchableToolText below) keeps a broader corpus — that path
// is BM25-style and benefits from more tokens.
// maxEmbeddingTextChars bounds the rendered embedding input so it fits within
// the embedding model's context window. embeddinggemma-300m caps at 1024 tokens
// and refuses ("input is larger than the max context size") above that. English
// encodes at ~3.5–4 chars/token, so 3500 chars leaves a safety margin under
// 1024 tokens regardless of language mix. Action-dispatch tools (wiki_page,
// propose_patch, subagent_dispatch) used to blow past this because their
// description carries the full "REQUIRED PARAMETERS BY ACTION" prose plus the
// oneOf schema in parameters; this cap keeps embedding I/O healthy without
// dropping the prose from the LLM-facing schema.
const maxEmbeddingTextChars = 3500

func searchableToolEmbeddingText(def ToolDefinition) string {
	var b strings.Builder
	b.WriteString(toolNameForEmbedding(def.Name))
	b.WriteString(": ")
	b.WriteString(def.Description)
	if props, ok := def.Parameters["properties"].(map[string]any); ok && len(props) > 0 {
		// Sort parameter keys so the embedding is deterministic across
		// Build runs — Go map iteration order would otherwise produce a
		// different hash and bust warm-cache invalidation logic.
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, name := range keys {
			schema, ok := props[name].(map[string]any)
			if !ok {
				continue
			}
			paramType, _ := schema["type"].(string)
			paramDesc, _ := schema["description"].(string)
			b.WriteString("\n    ")
			b.WriteString(name)
			if paramType != "" {
				b.WriteString(" [")
				b.WriteString(paramType)
				b.WriteByte(']')
			}
			if paramDesc != "" {
				b.WriteString(": ")
				b.WriteString(paramDesc)
			}
		}
	}
	return truncateEmbeddingText(b.String(), maxEmbeddingTextChars)
}

// truncateEmbeddingText slices text at a rune boundary at or before maxChars.
// Returns text unchanged when within the budget. The cut is plain length-based
// — we keep the prefix (tool name + description start) because that carries
// the strongest semantic signal for retrieval; trailing parameter docs are
// the most expendable.
func truncateEmbeddingText(text string, maxChars int) string {
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	// Slice on a rune boundary at or before maxChars so we never produce
	// invalid UTF-8 (some param descriptions contain multi-byte chars).
	cut := maxChars
	for cut > 0 && (text[cut]&0xC0) == 0x80 {
		cut--
	}
	return text[:cut]
}

// toolNameForEmbedding renders a tool name in the form best suited for
// natural-language semantic similarity: underscores and dashes become spaces.
// `mcp_mail_imap_search` → `mcp mail imap search`.
func toolNameForEmbedding(name string) string {
	return strings.NewReplacer("_", " ", "-", " ").Replace(name)
}

func searchableToolText(def ToolDefinition, tags []string) string {
	var b strings.Builder
	b.WriteString(def.Name)
	b.WriteByte(' ')
	b.WriteString(def.Description)
	for _, tag := range tags {
		b.WriteByte(' ')
		b.WriteString(tag)
	}
	if len(def.Examples) > 0 {
		data, _ := json.Marshal(def.Examples)
		b.WriteByte(' ')
		b.Write(data)
	}
	if len(def.Parameters) > 0 {
		data, _ := json.Marshal(def.Parameters)
		b.WriteByte(' ')
		b.Write(data)
	}
	return strings.ToLower(b.String())
}

func scoreToolText(terms []string, name, text string) int {
	name = strings.ToLower(name)
	score := 0
	for _, term := range terms {
		if strings.Contains(name, term) {
			score += 12
		}
		if strings.Contains(text, term) {
			score += 3
		}
	}
	return score
}
