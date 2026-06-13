package reasoningstore

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
)

// errGraph injects a Read error to exercise the LoadExamples error path.
type errGraph struct{ readErr error }

func (e *errGraph) Read(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return nil, e.readErr
}

func (e *errGraph) Write(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func TestLoadExamples_PropagatesReadError(t *testing.T) {
	sentinel := errors.New("cypher read failed")
	s := &Store{Client: &errGraph{readErr: sentinel}}

	got, err := s.LoadExamples(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel read error propagated, got %v", err)
	}
	if got != nil {
		t.Errorf("want nil slice on error, got %+v", got)
	}
}

// TestLoadExamples_AsStringNonStringTier covers asString's non-string fallback:
// a non-string `tier` column coerces to "" → invalid tier → the row is skipped.
func TestLoadExamples_AsStringNonStringTier(t *testing.T) {
	g := &fakeGraph{rows: []map[string]any{
		{"tier": int64(7), "embedding": []any{0.1, 0.2}}, // non-string tier → "" → skipped
		{"tier": "none", "embedding": []any{0.3, 0.4}},   // valid, kept
	}}
	s := &Store{Client: g}

	got, err := s.LoadExamples(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d examples, want 1 (non-string tier skipped): %+v", len(got), got)
	}
	if got[0].Tier != prompt.ReasoningTierNone || len(got[0].Vec) != 2 {
		t.Errorf("kept row = %+v", got[0])
	}
}

// TestLoadExamples_EmbeddingVariants exercises every asFloats branch through the
// real LoadExamples path: APOC JSON string, int64/int element coercion, and the
// non-string/non-list default (→ empty vec → skipped).
func TestLoadExamples_EmbeddingVariants(t *testing.T) {
	g := &fakeGraph{rows: []map[string]any{
		// asFloats string case, successful json.Unmarshal (the live APOC path).
		{"tier": "none", "embedding": "[-0.02,0.5,0.25]"},
		// asFloats []any with int64 + int elements → coerced to float64.
		{"tier": "low", "embedding": []any{int64(1), int(2), 0.5}},
		// asFloats default case: not a string, not a []any → nil → empty → skipped.
		{"tier": "high", "embedding": int64(42)},
		// asFloats string case, malformed JSON → nil → empty → skipped.
		{"tier": "high", "embedding": "{not json"},
	}}
	s := &Store{Client: g}

	got, err := s.LoadExamples(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d examples, want 2 (json-string + int-list kept; default + bad-json skipped): %+v", len(got), got)
	}

	// Row 0: APOC JSON string decoded to a 3d vector.
	if got[0].Tier != prompt.ReasoningTierNone {
		t.Errorf("row 0 tier = %s, want none", got[0].Tier)
	}
	wantVec := []float64{-0.02, 0.5, 0.25}
	if len(got[0].Vec) != len(wantVec) {
		t.Fatalf("row 0 vec len = %d, want %d", len(got[0].Vec), len(wantVec))
	}
	for i, v := range wantVec {
		if got[0].Vec[i] != v {
			t.Errorf("row 0 vec[%d] = %v, want %v", i, got[0].Vec[i], v)
		}
	}

	// Row 1: int64/int elements coerced to float64, order preserved.
	if got[1].Tier != prompt.ReasoningTierLow {
		t.Errorf("row 1 tier = %s, want low", got[1].Tier)
	}
	wantVec1 := []float64{1, 2, 0.5}
	if len(got[1].Vec) != len(wantVec1) {
		t.Fatalf("row 1 vec len = %d, want %d", len(got[1].Vec), len(wantVec1))
	}
	for i, v := range wantVec1 {
		if got[1].Vec[i] != v {
			t.Errorf("row 1 vec[%d] = %v, want %v", i, got[1].Vec[i], v)
		}
	}
}

// TestLoadExamples_Empty proves an empty result set returns an empty (non-nil),
// no-error slice rather than panicking on the make/append path.
func TestLoadExamples_Empty(t *testing.T) {
	s := &Store{Client: &fakeGraph{rows: nil}}
	got, err := s.LoadExamples(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 examples from empty graph, got %d", len(got))
	}
}
