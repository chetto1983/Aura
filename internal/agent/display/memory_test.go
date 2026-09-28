package display

import (
	"strings"
	"testing"
)

func memoryInput(tool, result string) PreviewInput {
	return PreviewInput{ToolCallID: "m1", ToolName: "alias__" + tool, ResultPreview: result,
		TrustedMCP: &TrustedMCP{Recipe: "recipe:memory", Tool: tool}}
}

func TestMemoryFactsUseBoundedTable(t *testing.T) {
	in := memoryInput("memory_search", `{"facts":[{"statement":"<img src=x onerror=alert(1)>","subject":"A","predicate":"knows","object":"B","valid_from":"2026-01-01","sources":[{"run_id":"r1","memory_ids":["m1"]}]}],"retrieval":{"path":"lexical","abstained":false}}`)
	p, ok := NormalizeToolPreview(in, NewRegistry())
	if !ok || p.Type != KindTable || p.Table == nil || len(p.Table.Rows) != 1 {
		t.Fatalf("memory facts = %+v, ok=%v", p, ok)
	}
	if got := p.Table.Rows[0][0]; got != "<img src=x onerror=alert(1)>" {
		t.Fatalf("fact text mutated = %q", got)
	}
	if !strings.Contains(strings.Join(p.Table.Rows[0], " "), "r1") || !strings.Contains(strings.Join(p.Table.Rows[0], " "), "2026-01-01") {
		t.Fatalf("source or validity missing: %v", p.Table.Rows[0])
	}
	for _, tool := range []string{"memory_facts_about", "memory_recall"} {
		in.ToolName = "alias__" + tool
		in.TrustedMCP = &TrustedMCP{Recipe: "recipe:memory", Tool: tool}
		if got, valid := NormalizeToolPreview(in, NewRegistry()); !valid || got.Table == nil {
			t.Fatalf("%s facts = %+v, %v", tool, got, valid)
		}
	}
}

func TestMemoryReadShapesAndFallback(t *testing.T) {
	cases := []struct {
		tool string
		body string
		kind Kind
	}{
		{"memory_search", `{"facts":[],"retrieval":{"path":"lexical","abstained":true}}`, KindTable},
		{"memory_entities", `{"entities":[{"name":"A","pole":"Person","kind":"person","facts":2}],"total":1}`, KindTable},
		{"graph_diagnostics", `{"semantics":"stored_topology_all_validity_windows","relations":"FACT","node_count":3,"edge_count":2,"isolated_nodes":1,"zero_out_degree":1,"component_sizes":[2,1],"core_histogram":{"1":2},"nodes":[],"truncated":false}`, KindStats},
		{"graph_schema", `{"vertices":[{"name":"Entity","kind":"vertex","records":2}],"edges":[]}`, KindCode},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			p, ok := NormalizeToolPreview(memoryInput(tc.tool, tc.body), NewRegistry())
			if !ok || p.Type != tc.kind {
				t.Fatalf("display = %+v, ok=%v", p, ok)
			}
		})
	}
	for _, tc := range []struct{ tool, body string }{
		{"memory_search", `{"facts":{}}`},
		{"memory_search", `{"facts":[{"statement":"x"}],"retrieval":{"path":"lexical"}}`},
		{"memory_recall", `{"facts":[],"evidence":[{"kind":"conversation"}]}`},
		{"memory_recall", `{"facts":[],"entities":[{"name":"A","facts":[]}]}`},
		{"memory_recall", `{"facts":[],"next_cursor":"opaque"}`},
		{"graph_diagnostics", `{"node_count":-1,"edge_count":2,"isolated_nodes":0,"zero_out_degree":0}`},
		{"graph_path", `{"found":true,"nodes":["A","B"]}`},
		{"memory_digest", `{"covered":true}`},
		{"memory_upsert_fact", `{"facts":[]}`},
	} {
		if p, ok := NormalizeToolPreview(memoryInput(tc.tool, tc.body), NewRegistry()); ok {
			t.Fatalf("%s promoted unsupported shape: %+v", tc.tool, p)
		}
	}
	untrusted := memoryInput("memory_search", cases[0].body)
	untrusted.TrustedMCP = nil
	if p, ok := NormalizeToolPreview(untrusted, NewRegistry()); ok {
		t.Fatalf("untrusted name promoted: %+v", p)
	}
}

func TestMemoryOversizedResultFallsBack(t *testing.T) {
	in := memoryInput("memory_search", `{"facts":[]}`+strings.Repeat(" ", 70*1024))
	if p, ok := NormalizeToolPreview(in, NewRegistry()); ok {
		t.Fatalf("oversized result promoted: %+v", p)
	}
}
