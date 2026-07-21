package reasoningstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
)

type fakeGraph struct {
	rows           []map[string]any
	readErr        error
	written        []map[string]any
	lastQuery      string
	lastReadQuery  string
	lastReadParams map[string]any
	writeRows      []map[string]any
}

func (f *fakeGraph) Read(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.lastReadQuery, f.lastReadParams = query, params
	return f.rows, f.readErr
}

func (f *fakeGraph) Write(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.lastQuery = query
	f.written = append(f.written, params)
	if f.writeRows != nil {
		return f.writeRows, nil
	}
	return []map[string]any{{"status": "created"}}, nil
}

func TestLoadExamples_UsesServerSideTTLPerBucketAndGlobalLimits(t *testing.T) {
	g := &fakeGraph{}
	s := &Store{Client: g, Now: func() time.Time { return time.Unix(1_800_000_000, 0) }}
	if _, err := s.LoadExamples(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"source = 'learned'", "updated_at >= datetime($cutoff)", "ORDER BY e.updated_at DESC, e.hash ASC", "LIMIT $bucket_limit", "LIMIT $store_limit"} {
		if !strings.Contains(g.lastReadQuery, fragment) {
			t.Errorf("bounded load query missing %q: %s", fragment, g.lastReadQuery)
		}
	}
	if g.lastReadParams["bucket_limit"] != 512 || g.lastReadParams["store_limit"] != 10_000 {
		t.Fatalf("load limits = %#v", g.lastReadParams)
	}
}

func TestSaveLearned_ReportsCapAndPreservesCreationTimestamp(t *testing.T) {
	g := &fakeGraph{writeRows: []map[string]any{{"status": "at_capacity"}}}
	s := &Store{Client: g, Now: func() time.Time { return time.Unix(1_800_000_000, 0) }}
	result, err := s.SaveLearned(context.Background(), "text", []float64{1}, prompt.ReasoningTierLow, 2, -1)
	if err != nil {
		t.Fatal(err)
	}
	if result != SaveAtCapacity {
		t.Fatalf("result = %q, want %q", result, SaveAtCapacity)
	}
	for _, fragment := range []string{"existing.created_at", "bucket_count < row.bucket_cap", "store_count < row.store_cap", "quality", "novelty"} {
		if !strings.Contains(g.lastQuery, fragment) {
			t.Errorf("atomic save query missing %q", fragment)
		}
	}
}

func TestSave_AtCapacityIsACommittedDropNotARetryError(t *testing.T) {
	g := &fakeGraph{writeRows: []map[string]any{{"status": "at_capacity"}}}
	s := &Store{Client: g}
	if err := s.Save(context.Background(), "text", []float64{1}, prompt.ReasoningTierLow); err != nil {
		t.Fatalf("capacity should be a committed drop, got retry error: %v", err)
	}
}

func TestPinnedSeedsUseSeparateLabelAndBypassLearnedCaps(t *testing.T) {
	g := &fakeGraph{}
	s := &Store{Client: g}
	if err := s.SavePinned(context.Background(), "seed", []float64{1}, prompt.ReasoningTierHigh); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.lastQuery, ":ReasoningSeed") || strings.Contains(g.lastQuery, "bucket_count") {
		t.Fatalf("pinned save is not isolated: %s", g.lastQuery)
	}
	if _, err := s.LoadPinnedExamples(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.lastReadQuery, ":ReasoningSeed") || !strings.Contains(g.lastReadQuery, "LIMIT $limit") {
		t.Fatalf("pinned load is not separately bounded: %s", g.lastReadQuery)
	}
}

func TestLoadExamples_ParsesRowsAndSkipsBad(t *testing.T) {
	g := &fakeGraph{rows: []map[string]any{
		{"tier": "none", "embedding": []any{0.1, 0.2, 0.3}},
		{"tier": "high", "embedding": []any{float64(1), float64(0)}},
		{"tier": "bogus", "embedding": []any{0.5}},    // invalid tier → skipped
		{"tier": "low", "embedding": "not-a-list"},    // bad embedding → skipped
		{"tier": "low", "embedding": []any{0.4, "x"}}, // bad element → skipped
	}}
	s := &Store{Client: g}
	got, err := s.LoadExamples(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d examples, want 2 (2 valid, 3 skipped): %+v", len(got), got)
	}
	if got[0].Tier != prompt.ReasoningTierNone || len(got[0].Vec) != 3 {
		t.Errorf("row 0 = %+v", got[0])
	}
	if got[1].Tier != prompt.ReasoningTierHigh || len(got[1].Vec) != 2 {
		t.Errorf("row 1 = %+v", got[1])
	}
}

func TestSave_MergesByContentHash(t *testing.T) {
	g := &fakeGraph{}
	s := &Store{Client: g}
	if err := s.Save(context.Background(), "qual e la capitale dell'Italia?", []float64{0.1, 0.2}, prompt.ReasoningTierNone); err != nil {
		t.Fatal(err)
	}
	if len(g.written) != 1 {
		t.Fatalf("want 1 write, got %d", len(g.written))
	}
	row := func(params map[string]any) map[string]any {
		return params["rows"].([]map[string]any)[0]
	}
	p := row(g.written[0])
	if p["tier"] != "none" || p["source"] != "learned" {
		t.Errorf("row = %+v", p)
	}
	h1 := p["hash"].(string)
	// Same text → same hash (idempotent dedup via MERGE).
	_ = s.Save(context.Background(), "qual e la capitale dell'Italia?", []float64{0.9}, prompt.ReasoningTierLow)
	if row(g.written[1])["hash"].(string) != h1 {
		t.Error("same text must produce the same MERGE hash key")
	}
	if len(h1) != 64 {
		t.Errorf("hash len = %d, want 64 (sha256 hex)", len(h1))
	}
}
