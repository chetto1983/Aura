//go:build arcadedb_integration

package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func TestAgentMemoryMCPLiveGraphDiagnosticsAndPath(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	sessions, identities, _ := newAgentMemoryLiveMCP(t, 1, "")
	session := sessions[identities[0]]
	for _, edge := range [][2]string{{"GraphProbeA", "GraphProbeB"}, {"GraphProbeB", "GraphProbeC"}, {"GraphProbeC", "GraphProbeA"}, {"GraphProbeC", "GraphProbeD"}} {
		callAgentMemoryLiveJSON[MemoryUpsertFactOutput](t, t.Context(), session, "memory_upsert_fact", map[string]any{
			"subject": edge[0], "object": edge[1], "predicate": "links",
			"statement": edge[0] + " links " + edge[1], "source": map[string]any{"memory_ids": []string{"graph-fixture"}},
		})
	}
	diagnostics := callAgentMemoryLiveJSON[arcadedb.MemoryGraphDiagnostics](t, t.Context(), session, "graph_diagnostics", map[string]any{"relations": "facts", "limit": 10})
	if diagnostics.NodeCount != 4 || diagnostics.CoreHistogram["2"] != 3 || diagnostics.CoreHistogram["1"] != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	path := callAgentMemoryLiveJSON[arcadedb.MemoryGraphPath](t, t.Context(), session, "graph_path", map[string]any{
		"source": "GraphProbeA", "target": "GraphProbeD", "max_depth": 2,
	})
	if !path.Found || len(path.Edges) != 2 || path.Edges[0].Fact == nil || len(path.Edges[0].Fact.Sources) == 0 {
		t.Fatalf("path = %+v", path)
	}
}
