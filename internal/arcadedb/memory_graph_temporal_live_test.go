//go:build arcadedb_integration

package arcadedb

import "testing"

func temporalGraphFixture(t *testing.T) *Client {
	t.Helper()
	client := disposableMemoryClient(t)
	for _, name := range []string{"Origin", "Middle", "Bridge", "Future", "Orphan", "Aux"} {
		if _, err := client.Command(t.Context(), "CREATE VERTEX Entity SET name=:name", map[string]any{"name": name}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Command(t.Context(), "CREATE VERTEX Object SET name='Destination'", nil); err != nil {
		t.Fatal(err)
	}
	for _, edge := range []struct{ key, from, to, vf, vt string }{
		{"past", "Origin", "Destination", "2026-01-01 00:00:00", "2026-06-01 00:00:00"},
		{"first", "Origin", "Middle", "2026-06-01 00:00:00", ""},
		{"second", "Middle", "Destination", "2026-06-01 00:00:00", ""},
		{"future", "Origin", "Future", "2026-10-01 00:00:00", ""},
		{"a-neighbor", "Destination", "Aux", "2026-06-01 00:00:00", ""},
	} {
		var until any
		if edge.vt != "" {
			until = edge.vt
		}
		_, err := client.Command(t.Context(), "CREATE EDGE FACT FROM (SELECT FROM Entity WHERE name=:source) TO (SELECT FROM Entity WHERE name=:target) SET fact_key=:key,statement=:statement,predicate='connects',valid_from=:vf,valid_to=:vt,sources=:sources",
			map[string]any{"source": edge.from, "target": edge.to, "key": edge.key, "statement": edge.from + " connects " + edge.to, "vf": edge.vf, "vt": until, "sources": []map[string]any{{"run_id": "temporal-graph-fixture", "memory_ids": []string{"observed-support"}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, edge := range [][3]string{{"Origin", "Destination", "past"}, {"Origin", "Bridge", "first"}, {"Bridge", "Destination", "second"}, {"Origin", "Orphan", "missing"}} {
		if _, err := client.Command(t.Context(), "CREATE EDGE MENTIONS FROM (SELECT FROM Entity WHERE name=:source) TO (SELECT FROM Entity WHERE name=:target) SET fact_key=:key", map[string]any{"source": edge[0], "target": edge[1], "key": edge[2]}); err != nil {
			t.Fatal(err)
		}
	}
	return client
}

func TestMemoryGraphTemporalLive(t *testing.T) {
	client := temporalGraphFixture(t)
	for _, tc := range []struct {
		name, at, relations, source, target, direction string
		depth, edges                                   int
		found                                          bool
	}{
		{"current_alternative", "2026-07-01T00:00:00Z", "facts", "Origin", "Destination", "OUT", 4, 2, true},
		{"past_shortcut", "2026-02-01T00:00:00Z", "facts", "Origin", "Destination", "OUT", 4, 1, true},
		{"boundary", "2026-06-01T02:00:00+02:00", "facts", "Origin", "Destination", "OUT", 4, 2, true},
		{"support", "2026-07-01T00:00:00Z", "mentions", "Origin", "Destination", "OUT", 4, 2, true},
		{"mixed", "2026-07-01T00:00:00Z", "combined", "Origin", "Bridge", "OUT", 4, 1, true},
		{"unsupported", "2026-07-01T00:00:00Z", "combined", "Origin", "Orphan", "OUT", 4, 0, false},
		{"future", "2026-07-01T00:00:00Z", "facts", "Origin", "Future", "OUT", 4, 0, false},
		{"depth", "2026-07-01T00:00:00Z", "facts", "Origin", "Destination", "OUT", 1, 0, false},
		{"reverse", "2026-07-01T00:00:00Z", "facts", "Destination", "Origin", "IN", 4, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := client.MemoryGraphPath(t.Context(), MemoryGraphPathRequest{MemoryGraphRequest: MemoryGraphRequest{AsOf: tc.at, Relations: tc.relations}, Source: tc.source, Target: tc.target, Direction: tc.direction, MaxDepth: tc.depth})
			if err != nil {
				t.Fatal(err)
			}
			if out.Found != tc.found || len(out.Edges) != tc.edges {
				t.Fatalf("path=%+v", out)
			}
			for _, edge := range out.Edges {
				fact := edge.Fact
				if edge.Type == mentionsEdgeType {
					fact = edge.SupportingFact
				}
				if fact == nil || len(fact.Sources) != 1 || fact.FactKey == "" {
					t.Fatalf("missing temporal provenance: %+v", edge)
				}
			}
		})
	}
}
