package reasoningstore

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
)

type fakeGraph struct {
	rows      []map[string]any
	readErr   error
	written   []map[string]any
	lastQuery string
}

func (f *fakeGraph) Read(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return f.rows, f.readErr
}

func (f *fakeGraph) Write(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.lastQuery = query
	f.written = append(f.written, params)
	return nil, nil
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
	if p["tier"] != "none" || p["source"] != "oracle" {
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
