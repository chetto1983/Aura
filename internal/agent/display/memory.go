package display

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxMemoryPreviewBytes = 64 << 10
const maxMemoryRows = 50
const maxMemoryCellRunes = 256

type memoryFactSource struct {
	RunID     string   `json:"run_id"`
	MemoryIDs []string `json:"memory_ids"`
}

type memoryFact struct {
	Statement string             `json:"statement"`
	Subject   string             `json:"subject"`
	Predicate string             `json:"predicate"`
	Object    string             `json:"object"`
	ValidFrom string             `json:"valid_from"`
	ValidTo   string             `json:"valid_to"`
	FactKey   string             `json:"fact_key"`
	Sources   []memoryFactSource `json:"sources"`
}

func normalizeMemoryPreview(in PreviewInput) (Payload, bool) {
	marker := in.TrustedMCP
	if marker == nil || marker.Recipe != "recipe:memory" || !strings.HasSuffix(in.ToolName, "__"+marker.Tool) || len(in.ResultPreview) > maxMemoryPreviewBytes {
		return Payload{}, false
	}
	switch marker.Tool {
	case "memory_search", "memory_facts_about", "memory_recall":
		return memoryFactsPreview(in)
	case "memory_entities":
		return memoryEntitiesPreview(in)
	case "graph_diagnostics":
		return memoryDiagnosticsPreview(in)
	case "graph_schema":
		return memorySchemaPreview(in)
	default:
		return Payload{}, false
	}
}

func memoryFactsPreview(in PreviewInput) (Payload, bool) {
	var body struct {
		Facts      *[]memoryFact     `json:"facts"`
		Entities   []json.RawMessage `json:"entities"`
		NextCursor string            `json:"next_cursor"`
		Evidence   []struct {
			Kind string `json:"kind"`
		} `json:"evidence"`
	}
	if json.Unmarshal([]byte(in.ResultPreview), &body) != nil || body.Facts == nil {
		return Payload{}, false
	}
	if in.TrustedMCP.Tool == "memory_recall" {
		if len(body.Entities) > 0 || body.NextCursor != "" {
			return Payload{}, false
		}
		for _, item := range body.Evidence {
			if item.Kind != "fact" {
				return Payload{}, false
			}
		}
	}
	facts := *body.Facts
	rows := make([][]string, 0, min(len(facts), maxMemoryRows))
	for _, fact := range facts[:min(len(facts), maxMemoryRows)] {
		if fact.Statement == "" || fact.Subject == "" || fact.Predicate == "" || fact.Object == "" {
			return Payload{}, false
		}
		sources := make([]string, 0, min(len(fact.Sources), 8))
		for _, source := range fact.Sources[:min(len(fact.Sources), 8)] {
			if source.RunID == "" {
				return Payload{}, false
			}
			entry := source.RunID
			if len(source.MemoryIDs) > 0 {
				entry += " (" + strings.Join(source.MemoryIDs[:min(len(source.MemoryIDs), 4)], ", ") + ")"
			}
			sources = append(sources, entry)
		}
		validity := fact.ValidFrom
		if fact.ValidTo != "" {
			validity += " → " + fact.ValidTo
		}
		rows = append(rows, []string{
			memoryCell(fact.Statement), memoryCell(fact.Subject), memoryCell(fact.Predicate),
			memoryCell(fact.Object), memoryCell(validity), memoryCell(strings.Join(sources, "; ")),
			memoryCell(fact.FactKey),
		})
	}
	return Payload{Type: KindTable, ToolCallID: in.ToolCallID, Title: "memory_facts", Table: &Table{
		Columns: []string{"Fact", "Subject", "Relation", "Object", "Valid", "Sources", "Fact key"},
		Rows:    rows, OmittedRows: max(0, len(facts)-len(rows)),
	}}, true
}

func memoryEntitiesPreview(in PreviewInput) (Payload, bool) {
	var body struct {
		Entities *[]struct {
			Name  string `json:"name"`
			Pole  string `json:"pole"`
			Kind  string `json:"kind"`
			Facts int    `json:"facts"`
		} `json:"entities"`
		Total *int `json:"total"`
	}
	if json.Unmarshal([]byte(in.ResultPreview), &body) != nil || body.Entities == nil || body.Total == nil || *body.Total < len(*body.Entities) {
		return Payload{}, false
	}
	seen := make(map[string]bool)
	rows := make([][]string, 0, min(len(*body.Entities), maxMemoryRows))
	for _, entity := range *body.Entities {
		if entity.Name == "" || entity.Facts < 0 || seen[entity.Name] {
			return Payload{}, false
		}
		seen[entity.Name] = true
		if len(rows) < maxMemoryRows {
			rows = append(rows, []string{memoryCell(entity.Name), memoryCell(entity.Pole), memoryCell(entity.Kind), fmt.Sprint(entity.Facts)})
		}
	}
	return Payload{Type: KindTable, ToolCallID: in.ToolCallID, Title: "memory_entities", Table: &Table{
		Columns: []string{"Entity", "Class", "Kind", "Facts"}, Rows: rows,
		OmittedRows: max(0, *body.Total-len(rows)),
	}}, true
}

func memoryDiagnosticsPreview(in PreviewInput) (Payload, bool) {
	var body struct {
		NodeCount     *int64 `json:"node_count"`
		EdgeCount     *int64 `json:"edge_count"`
		IsolatedNodes *int64 `json:"isolated_nodes"`
		ZeroOutDegree *int64 `json:"zero_out_degree"`
	}
	if json.Unmarshal([]byte(in.ResultPreview), &body) != nil || body.NodeCount == nil || body.EdgeCount == nil || body.IsolatedNodes == nil || body.ZeroOutDegree == nil ||
		*body.NodeCount < 0 || *body.EdgeCount < 0 || *body.IsolatedNodes < 0 || *body.ZeroOutDegree < 0 || *body.IsolatedNodes > *body.NodeCount || *body.ZeroOutDegree > *body.NodeCount {
		return Payload{}, false
	}
	return Payload{Type: KindStats, ToolCallID: in.ToolCallID, Title: "memory_graph", Stats: &Stats{Items: []StatItem{
		{Label: "nodes", Value: *body.NodeCount}, {Label: "edges", Value: *body.EdgeCount},
		{Label: "isolated_nodes", Value: *body.IsolatedNodes}, {Label: "zero_out_degree", Value: *body.ZeroOutDegree},
	}}}, true
}

func memorySchemaPreview(in PreviewInput) (Payload, bool) {
	var body struct {
		Vertices *[]struct {
			Name    string `json:"name"`
			Records int64  `json:"records"`
		} `json:"vertices"`
		Edges *[]struct {
			Name    string `json:"name"`
			Records int64  `json:"records"`
		} `json:"edges"`
	}
	if json.Unmarshal([]byte(in.ResultPreview), &body) != nil || body.Vertices == nil || body.Edges == nil {
		return Payload{}, false
	}
	for _, entry := range *body.Vertices {
		if entry.Name == "" || entry.Records < 0 {
			return Payload{}, false
		}
	}
	for _, entry := range *body.Edges {
		if entry.Name == "" || entry.Records < 0 {
			return Payload{}, false
		}
	}
	var formatted bytes.Buffer
	if json.Indent(&formatted, []byte(in.ResultPreview), "", "  ") != nil {
		return Payload{}, false
	}
	return Payload{Type: KindCode, ToolCallID: in.ToolCallID, Title: "memory_schema", Code: &Code{Body: formatted.String(), Lang: "json"}}, true
}

func memoryCell(value string) string {
	if utf8.RuneCountInString(value) <= maxMemoryCellRunes {
		return value
	}
	return string([]rune(value)[:maxMemoryCellRunes-1]) + "…"
}
