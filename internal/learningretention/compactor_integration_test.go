package learningretention

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLearningGraphStoreQueriesAreBoundedAndExcludePinnedSeeds(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	graph := &queryGraph{readRows: []map[string]any{{
		"hash": "h", "bucket": "high", "updated_at": now.Format(time.RFC3339Nano),
		"quality": 2.0, "novelty": -1.0, "policy_version": "learning-v1",
	}}}
	store := NewReasoningGraphStore(graph)
	candidates, err := store.Candidates(context.Background(), "high", 513)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("Candidates() = %+v, %v", candidates, err)
	}
	for _, fragment := range []string{":ReasoningExample", "source: 'learned'", "ORDER BY e.updated_at DESC, e.hash ASC", "LIMIT $limit"} {
		if !strings.Contains(graph.query, fragment) {
			t.Errorf("candidate query missing %q: %s", fragment, graph.query)
		}
	}
	if strings.Contains(graph.query, "ReasoningSeed") || graph.params["limit"] != 513 {
		t.Fatalf("pinned/unbounded candidate query: %s / %#v", graph.query, graph.params)
	}

	graph.readRows = []map[string]any{{"count": int64(10_001)}}
	if count, err := store.TotalCount(context.Background()); err != nil || count != 10_001 {
		t.Fatalf("TotalCount() = %d, %v", count, err)
	}
	if !strings.Contains(graph.query, "count(e)") || strings.Contains(graph.query, "Seed") {
		t.Fatalf("count query = %s", graph.query)
	}
}

func TestLearningGraphStoreDeleteUsesHashSourceAndTimestampVersion(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 123, time.UTC)
	graph := &queryGraph{writeRows: []map[string]any{{"deleted": int64(1)}}, readRows: []map[string]any{{"remaining": int64(0)}}}
	store := NewToolSelectionGraphStore(graph)
	deleted, err := store.Delete(context.Background(), []Candidate{{Hash: "h", UpdatedAt: now}})
	if err != nil || deleted != 1 {
		t.Fatalf("Delete() = %d, %v", deleted, err)
	}
	for _, fragment := range []string{":ToolSelectionExample", "source: 'learned'", "hash: row.hash", "e.updated_at = datetime(row.updated_at)"} {
		if !strings.Contains(graph.query, fragment) {
			t.Errorf("delete query missing %q: %s", fragment, graph.query)
		}
	}
	if strings.Contains(graph.query, "ToolSelectionSeed") {
		t.Fatalf("delete can touch pinned seeds: %s", graph.query)
	}
	rows := graph.params["rows"].([]map[string]any)
	if rows[0]["updated_at"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("delete version = %#v", rows[0])
	}
}

func TestLearningGraphStoreDeleteVerifiesBoundedHashPageWhenWriteAckHasNoRows(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	graph := &queryGraph{readRows: []map[string]any{{"remaining": float64(0)}}}
	store := NewReasoningGraphStore(graph)
	deleted, err := store.Delete(context.Background(), []Candidate{{Hash: "h", UpdatedAt: now}})
	if err != nil || deleted != 1 {
		t.Fatalf("Delete() = %d, %v", deleted, err)
	}
	if !strings.Contains(graph.query, "UNWIND $hashes") || !strings.Contains(graph.query, "source: 'learned'") {
		t.Fatalf("unbounded delete verification: %s", graph.query)
	}
	if hashes := graph.params["hashes"].([]string); len(hashes) != 1 || hashes[0] != "h" {
		t.Fatalf("verification hashes = %#v", hashes)
	}
}

type queryGraph struct {
	query     string
	params    map[string]any
	readRows  []map[string]any
	writeRows []map[string]any
}

func (g *queryGraph) Read(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	g.query, g.params = query, params
	return g.readRows, nil
}

func (g *queryGraph) Write(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	g.query, g.params = query, params
	return g.writeRows, nil
}
