package arcadedb

import "testing"

func TestMemoryGraphRelationAllowlist(t *testing.T) {
	for selection, want := range map[string]string{"": "FACT", "facts": "FACT", "mentions": "MENTIONS", "combined": "FACT,MENTIONS"} {
		got, err := memoryGraphRelations(MemoryGraphRequest{Relations: selection})
		if err != nil || got != want {
			t.Fatalf("%q: %q, %v", selection, got, err)
		}
	}
}

func TestMemoryGraphRejectsUnboundedOrInvalidTemporalRequests(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	for _, request := range []MemoryGraphPathRequest{
		{Source: "A", Target: "B", MaxDepth: 7},
		{Source: "A", Target: "B", MaxDepth: -1},
		{Source: "A", Target: "B", Direction: "OUT; DELETE"},
		{Source: "A", Target: "B", MemoryGraphRequest: MemoryGraphRequest{AsOf: "not-an-instant"}},
		{Source: "A", Target: "B", MemoryGraphRequest: MemoryGraphRequest{AsOf: "0001-01-01T00:00:00Z"}},
		{Source: "A", Target: "B", MemoryGraphRequest: MemoryGraphRequest{Relations: "HAS_TURN"}},
		{Source: "A"},
	} {
		if _, err := client.MemoryGraphPath(t.Context(), request); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
	if rec.joined() != "" {
		t.Fatal("invalid request reached the database")
	}
}

func TestMemoryGraphPreflightRejectsBeforeAlgorithms(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[{"name":"Technical","type":"vertex","records":10001}]}`)
	if _, err := client.MemoryGraphDiagnostics(t.Context(), MemoryGraphDiagnosticsRequest{}); err == nil {
		t.Fatal("unbounded graph accepted")
	}
	if rec.joined() != schemaStatement {
		t.Fatalf("queries = %s", rec.joined())
	}
}

func TestMemoryGraphEmptyAndMissingEntities(t *testing.T) {
	for _, path := range []bool{false, true} {
		client, _ := recordingClient(t, `{"result":[]}`, `{"result":[]}`)
		if path {
			out, err := client.MemoryGraphPath(t.Context(), MemoryGraphPathRequest{Source: "A", Target: "B"})
			if err != nil || out.Found || out.Reason != "entity_not_found" {
				t.Fatalf("%+v %v", out, err)
			}
		} else {
			out, err := client.MemoryGraphDiagnostics(t.Context(), MemoryGraphDiagnosticsRequest{})
			if err != nil || out.NodeCount != 0 || out.Semantics != graphTopologySemantics {
				t.Fatalf("%+v %v", out, err)
			}
		}
	}
}

func TestMemoryGraphMentionWithoutSupportIsExplicit(t *testing.T) {
	client, _ := recordingClient(t, `{"result":[]}`)
	edges := []MemoryGraphPathEdge{{Type: mentionsEdgeType, FactKey: "removed"}, {Type: factEdgeType}}
	if err := client.memoryGraphPathSupport(t.Context(), edges); err != nil {
		t.Fatal(err)
	}
	if !edges[0].SupportMissing || edges[0].SupportingFact != nil || edges[1].SupportMissing {
		t.Fatalf("edges = %+v", edges)
	}
}
