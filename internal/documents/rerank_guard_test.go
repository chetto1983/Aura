package documents

import (
	"testing"

	"github.com/chetto1983/aura/internal/rerank"
)

func mkHit(chunkID string, score float64) SearchHit {
	return SearchHit{DocumentID: "doc-1", ChunkID: chunkID, Text: chunkID, Score: score}
}

// TestApplyRerankGuardConfidentReorders: a confident rerank (top score >= threshold,
// order differs) returns the reranked order and attaches the rerank scores.
func TestApplyRerankGuardConfidentReorders(t *testing.T) {
	seed := []SearchHit{mkHit("chunk-0", 0.9), mkHit("chunk-1", 0.5), mkHit("chunk-2", 0.1)}
	scored := []rerank.Scored{{Index: 2, Score: 0.99}, {Index: 0, Score: 0.80}, {Index: 1, Score: 0.20}}

	got := applyRerankGuard(seed, scored, 0.50, false)
	if ids := chunkIDs(got); ids != "chunk-2,chunk-0,chunk-1" {
		t.Fatalf("order = %s, want reranked chunk-2,chunk-0,chunk-1", ids)
	}
	if got[0].Score != 0.99 {
		t.Fatalf("winner score = %v, want rerank score 0.99", got[0].Score)
	}
}

// TestApplyRerankGuardBelowThresholdKeepsSeed: the back-to-box guard — a top rerank
// score below the threshold keeps the seed order and the seed scores (spike 070).
func TestApplyRerankGuardBelowThresholdKeepsSeed(t *testing.T) {
	seed := []SearchHit{mkHit("chunk-0", 0.9), mkHit("chunk-1", 0.5), mkHit("chunk-2", 0.1)}
	// The reranker WANTS to reorder, but its top score 0.30 is below the 0.50 threshold.
	scored := []rerank.Scored{{Index: 2, Score: 0.30}, {Index: 0, Score: 0.20}, {Index: 1, Score: 0.10}}

	got := applyRerankGuard(seed, scored, 0.50, false)
	if ids := chunkIDs(got); ids != "chunk-0,chunk-1,chunk-2" {
		t.Fatalf("order = %s, want seed order kept below threshold", ids)
	}
	if got[0].Score != 0.9 {
		t.Fatalf("score = %v, want seed score 0.9 preserved", got[0].Score)
	}
}

// TestApplyRerankGuardIdentityKeepsSeed: an identity reranker result (the fail-soft
// rerank-off contract) keeps the seed order and the seed scores.
func TestApplyRerankGuardIdentityKeepsSeed(t *testing.T) {
	seed := []SearchHit{mkHit("chunk-0", 0.9), mkHit("chunk-1", 0.5), mkHit("chunk-2", 0.1)}
	scored := []rerank.Scored{{Index: 0, Score: 0}, {Index: 1, Score: 0}, {Index: 2, Score: 0}}

	got := applyRerankGuard(seed, scored, 0, false)
	if ids := chunkIDs(got); ids != "chunk-0,chunk-1,chunk-2" {
		t.Fatalf("order = %s, want seed order on identity", ids)
	}
	if got[0].Score != 0.9 {
		t.Fatalf("score = %v, want seed score 0.9 preserved on identity", got[0].Score)
	}
}

// TestApplyRerankGuardLengthMismatchKeepsSeed: a result/seed length mismatch keeps the
// seed order verbatim (defensive — a degraded reranker should never corrupt the order).
func TestApplyRerankGuardLengthMismatchKeepsSeed(t *testing.T) {
	seed := []SearchHit{mkHit("chunk-0", 0.9), mkHit("chunk-1", 0.5), mkHit("chunk-2", 0.1)}
	scored := []rerank.Scored{{Index: 2, Score: 0.99}, {Index: 0, Score: 0.80}} // short

	if ids := chunkIDs(applyRerankGuard(seed, scored, 0, false)); ids != "chunk-0,chunk-1,chunk-2" {
		t.Fatalf("order = %s, want seed order on length mismatch", ids)
	}
}

// TestApplyRerankGuardOutOfRangeIndexKeepsSeed: an out-of-range Scored.Index keeps the
// seed order rather than panicking or dropping a hit.
func TestApplyRerankGuardOutOfRangeIndexKeepsSeed(t *testing.T) {
	seed := []SearchHit{mkHit("chunk-0", 0.9), mkHit("chunk-1", 0.5), mkHit("chunk-2", 0.1)}
	scored := []rerank.Scored{{Index: 5, Score: 0.99}, {Index: 0, Score: 0.80}, {Index: 1, Score: 0.70}}

	if ids := chunkIDs(applyRerankGuard(seed, scored, 0, false)); ids != "chunk-0,chunk-1,chunk-2" {
		t.Fatalf("order = %s, want seed order on out-of-range index", ids)
	}
}

// TestApplyRerankGuardSingleElementKeepsSeed: a pool too small to reorder is returned
// unchanged.
func TestApplyRerankGuardSingleElementKeepsSeed(t *testing.T) {
	seed := []SearchHit{mkHit("chunk-0", 0.9)}
	scored := []rerank.Scored{{Index: 0, Score: 0.99}}

	if ids := chunkIDs(applyRerankGuard(seed, scored, 0, false)); ids != "chunk-0" {
		t.Fatalf("order = %s, want the single seed hit unchanged", ids)
	}
}

// TestBlendNeverBuriesStrongSeedHit: with blend enabled, a strong seed hit (#1 in the
// seed) that the reranker demotes to the very bottom on a single low-confidence call is
// NOT buried — RRF keeps its rank-0 seed term dominant. The hard threshold mode would
// bury it last; the blend keeps it in the middle of the field.
func TestBlendNeverBuriesStrongSeedHit(t *testing.T) {
	seed := []SearchHit{
		mkHit("chunk-0", 0.95), // the strong seed hit (#1), the clean answer
		mkHit("chunk-1", 0.40),
		mkHit("chunk-2", 0.30),
		mkHit("chunk-3", 0.20),
		mkHit("chunk-4", 0.10),
	}
	// The reranker demotes chunk-0 (seed #1) to dead last (the spike-070 back-to-box case).
	scored := []rerank.Scored{
		{Index: 1, Score: 0.90}, {Index: 2, Score: 0.80}, {Index: 3, Score: 0.70},
		{Index: 4, Score: 0.60}, {Index: 0, Score: 0.50},
	}

	// Hard threshold mode trusts the reorder and BURIES the strong seed hit last.
	hard := chunkIDs(applyRerankGuard(seed, scored, 0, false))
	if hard != "chunk-1,chunk-2,chunk-3,chunk-4,chunk-0" {
		t.Fatalf("hard order = %s, want the reranked order (chunk-0 buried last)", hard)
	}

	// Blend mode fuses seed rank + rerank rank, keeping the strong seed hit out of the
	// bottom: RRF places chunk-0 in the middle, never last.
	blended := applyRerankGuard(seed, scored, 0, true)
	gotIDs := chunkIDs(blended)
	if gotIDs != "chunk-1,chunk-2,chunk-0,chunk-3,chunk-4" {
		t.Fatalf("blend order = %s, want chunk-1,chunk-2,chunk-0,chunk-3,chunk-4", gotIDs)
	}
	if blended[len(blended)-1].ChunkID == "chunk-0" {
		t.Fatal("blend buried the strong seed hit chunk-0 in the last position")
	}
}

// TestBlendOutOfRangeIndexKeepsSeed: a corrupt rerank index in blend mode falls back to
// the seed order verbatim.
func TestBlendOutOfRangeIndexKeepsSeed(t *testing.T) {
	seed := []SearchHit{mkHit("chunk-0", 0.9), mkHit("chunk-1", 0.5)}
	scored := []rerank.Scored{{Index: 9, Score: 0.99}, {Index: 0, Score: 0.80}}

	if ids := chunkIDs(applyRerankGuard(seed, scored, 0, true)); ids != "chunk-0,chunk-1" {
		t.Fatalf("order = %s, want seed order on out-of-range blend index", ids)
	}
}
