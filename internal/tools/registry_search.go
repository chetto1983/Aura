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
