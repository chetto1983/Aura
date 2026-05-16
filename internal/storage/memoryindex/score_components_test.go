package memoryindex

import (
	"testing"
)

// TestMergeDocumentsRRFPreservesScoreComponents verifies that each document
// that appears in exactly one retrieval channel has only that channel's score
// populated (non-zero), while the other two remain zero, and that the fused
// Score is non-zero for all docs.
func TestMergeDocumentsRRFPreservesScoreComponents(t *testing.T) {
	docExact := Document{ID: "doc-exact", Kind: KindArchive, Title: "Exact Only"}
	docFTS := Document{ID: "doc-fts", Kind: KindArchive, Title: "FTS Only"}
	docVector := Document{ID: "doc-vector", Kind: KindArchive, Title: "Vector Only"}

	exact := []Document{docExact}
	fts := []Document{docFTS}
	vector := []Document{docVector}

	results := mergeDocumentsRRF(exact, fts, vector, 10)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	byID := map[string]Document{}
	for _, d := range results {
		byID[d.ID] = d
	}

	t.Run("exact-only doc", func(t *testing.T) {
		d, ok := byID["doc-exact"]
		if !ok {
			t.Fatal("doc-exact missing from results")
		}
		if d.Score == 0 {
			t.Error("fused Score must be non-zero for exact-only doc")
		}
		if d.ScoreExact == 0 {
			t.Error("ScoreExact must be non-zero for exact-only doc")
		}
		if d.ScoreFTS != 0 {
			t.Errorf("ScoreFTS must be zero for exact-only doc, got %v", d.ScoreFTS)
		}
		if d.ScoreVector != 0 {
			t.Errorf("ScoreVector must be zero for exact-only doc, got %v", d.ScoreVector)
		}
	})

	t.Run("fts-only doc", func(t *testing.T) {
		d, ok := byID["doc-fts"]
		if !ok {
			t.Fatal("doc-fts missing from results")
		}
		if d.Score == 0 {
			t.Error("fused Score must be non-zero for fts-only doc")
		}
		if d.ScoreExact != 0 {
			t.Errorf("ScoreExact must be zero for fts-only doc, got %v", d.ScoreExact)
		}
		if d.ScoreFTS == 0 {
			t.Error("ScoreFTS must be non-zero for fts-only doc")
		}
		if d.ScoreVector != 0 {
			t.Errorf("ScoreVector must be zero for fts-only doc, got %v", d.ScoreVector)
		}
	})

	t.Run("vector-only doc", func(t *testing.T) {
		d, ok := byID["doc-vector"]
		if !ok {
			t.Fatal("doc-vector missing from results")
		}
		if d.Score == 0 {
			t.Error("fused Score must be non-zero for vector-only doc")
		}
		if d.ScoreExact != 0 {
			t.Errorf("ScoreExact must be zero for vector-only doc, got %v", d.ScoreExact)
		}
		if d.ScoreFTS != 0 {
			t.Errorf("ScoreFTS must be zero for vector-only doc, got %v", d.ScoreFTS)
		}
		if d.ScoreVector == 0 {
			t.Error("ScoreVector must be non-zero for vector-only doc")
		}
	})
}

// TestMergeDocumentsRRFMultiChannelAccumulatesComponents verifies that a doc
// appearing in multiple channels accumulates each channel's contribution
// independently, and that the fused Score equals the sum of all channel scores.
func TestMergeDocumentsRRFMultiChannelAccumulatesComponents(t *testing.T) {
	doc := Document{ID: "doc-all", Kind: KindSource, Title: "All Channels"}

	results := mergeDocumentsRRF(
		[]Document{doc},
		[]Document{doc},
		[]Document{doc},
		10,
	)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	d := results[0]
	if d.ScoreExact == 0 {
		t.Error("ScoreExact must be non-zero for multi-channel doc")
	}
	if d.ScoreFTS == 0 {
		t.Error("ScoreFTS must be non-zero for multi-channel doc")
	}
	if d.ScoreVector == 0 {
		t.Error("ScoreVector must be non-zero for multi-channel doc")
	}
	// Fused Score must equal sum of channel scores (within float64 precision).
	wantSum := d.ScoreExact + d.ScoreFTS + d.ScoreVector
	if d.Score != wantSum {
		t.Errorf("Score=%v != ScoreExact+ScoreFTS+ScoreVector=%v", d.Score, wantSum)
	}
}
