//go:build arcadedb_integration

package arcadedb

import (
	"testing"
)

func graphFixture(t *testing.T) *Client {
	t.Helper()
	client := disposableArcadeClient(t)
	for _, query := range []string{
		"CREATE VERTEX TYPE Entity", "CREATE VERTEX TYPE Object EXTENDS Entity",
		"CREATE VERTEX TYPE Technical", "CREATE EDGE TYPE FACT", "CREATE EDGE TYPE MENTIONS",
		"CREATE VERTEX Entity SET name='A'", "CREATE VERTEX Entity SET name='B'",
		"CREATE VERTEX Entity SET name='C'", "CREATE VERTEX Object SET name='D'",
		"CREATE VERTEX Entity SET name='L'", "CREATE VERTEX Technical SET name='hidden'",
	} {
		if _, err := client.Command(t.Context(), query, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, edge := range [][2]string{{"A", "B"}, {"B", "C"}, {"C", "A"}, {"C", "D"}} {
		_, err := client.Command(t.Context(), "CREATE EDGE FACT FROM (SELECT FROM Entity WHERE name=:source) TO (SELECT FROM Entity WHERE name=:target) SET statement=:statement,predicate='links',fact_key=:key,valid_from='2026-01-01',sources=:sources",
			map[string]any{"source": edge[0], "target": edge[1], "statement": edge[0] + " links " + edge[1], "key": edge[0] + edge[1],
				"sources": []map[string]any{{"run_id": "graph-fixture", "memory_ids": []string{"fixture"}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	return client
}

func TestMemoryGraphLiveNativeKCoreAndSubtypeCoverage(t *testing.T) {
	client := graphFixture(t)
	out, err := client.MemoryGraphDiagnostics(t.Context(), MemoryGraphDiagnosticsRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if out.NodeCount != 5 || out.EdgeCount != 4 || out.IsolatedNodes != 1 || out.ZeroOutDegree != 2 ||
		!out.Truncated || len(out.Nodes) != 2 || len(out.ComponentSizes) != 2 || out.ComponentSizes[0] != 4 ||
		out.CoreHistogram["2"] != 3 || out.CoreHistogram["1"] != 1 || out.CoreHistogram["0"] != 1 {
		t.Fatalf("diagnostics = %+v", out)
	}
}

func TestMemoryGraphLivePathsRespectDirectionDepthAndProvenance(t *testing.T) {
	client := graphFixture(t)
	for _, test := range []struct {
		source, target, direction string
		depth, wantEdges          int
		found                     bool
	}{
		{"A", "D", "BOTH", 2, 2, true},
		{"A", "D", "OUT", 2, 0, false},
		{"A", "D", "OUT", 3, 3, true},
		{"D", "A", "IN", 3, 3, true},
		{"D", "A", "OUT", 6, 0, false},
		{"A", "L", "BOTH", 6, 0, false},
		{"A", "A", "BOTH", 1, 0, true},
		{"A", "missing", "BOTH", 3, 0, false},
	} {
		out, err := client.MemoryGraphPath(t.Context(), MemoryGraphPathRequest{
			Source: test.source, Target: test.target, Direction: test.direction, MaxDepth: test.depth,
		})
		if err != nil || out.Found != test.found || len(out.Edges) != test.wantEdges {
			t.Fatalf("%+v: output=%+v err=%v", test, out, err)
		}
		for _, edge := range out.Edges {
			if edge.Fact == nil || len(edge.Fact.Sources) != 1 || edge.Fact.ValidFrom == "" || edge.From == "" || edge.To == "" {
				t.Fatalf("incomplete edge: %+v", edge)
			}
		}
	}
}

func TestMemoryGraphLiveRelationSelectionAndInputBudget(t *testing.T) {
	client := graphFixture(t)
	_, err := client.Command(t.Context(), "CREATE EDGE MENTIONS FROM (SELECT FROM Entity WHERE name='A') TO (SELECT FROM Entity WHERE name='L') SET fact_key='AB'", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, relations := range []string{"mentions", "combined"} {
		request := MemoryGraphRequest{Relations: relations}
		out, err := client.MemoryGraphPath(t.Context(), MemoryGraphPathRequest{MemoryGraphRequest: request, Source: "A", Target: "L", MaxDepth: 1})
		if err != nil || !out.Found || len(out.Edges) != 1 || out.Edges[0].Type != "MENTIONS" || out.Edges[0].FactKey != "AB" {
			t.Fatalf("%s: %+v, %v", relations, out, err)
		}
		if out.Edges[0].SupportingFact == nil || len(out.Edges[0].SupportingFact.Sources) != 1 || out.Edges[0].SupportMissing {
			t.Fatalf("mention support = %+v", out.Edges[0])
		}
		if _, err := client.MemoryGraphDiagnostics(t.Context(), MemoryGraphDiagnosticsRequest{MemoryGraphRequest: request}); err != nil {
			t.Fatal(err)
		}
	}
	client.limits.GraphMaxRecords = 1
	if _, err := client.MemoryGraphDiagnostics(t.Context(), MemoryGraphDiagnosticsRequest{}); err == nil {
		t.Fatal("oversized graph was accepted")
	}
}
