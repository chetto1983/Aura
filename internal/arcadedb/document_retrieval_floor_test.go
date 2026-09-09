package arcadedb

import (
	"strings"
	"testing"
)

// vector.neighbors returns its k nearest however far away they are, so without a distance
// bound the dense leg always produces candidates and retrieval can never answer "nothing
// here". Measured 2026-09-09 on the live 1079-passage corpus: the nearest neighbour for
// five questions the corpus COULD answer sat at 0.247/0.400/0.420/0.651/0.712, and for five
// it could not at 0.706/0.731/0.794/0.798/0.799. 0.72 keeps all five true matches and drops
// four of the five others.
func TestFusedStatementBoundsTheDenseLegByDistance(t *testing.T) {
	statement := fusedStatement("identity_id = :identity_id", FusionRRF, 20)
	if !strings.Contains(statement, "maxDistance: :max_distance") {
		t.Fatalf("fused statement has no dense distance bound, so it can never abstain:\n%s", statement)
	}
}

// The dense bound above is the RECALL half and it cannot abstain on its own: it bounds one
// leg, so a lexical hit on an incidental word still enters with nothing left to reject it.
// `vector.fuse` emits an RRF pseudo-score of 1/(60+rank), identical for every source's rank
// 1 -- measured 2026-09-09, all five out-of-corpus questions scored exactly 0.016393442 --
// so no threshold on it carries information. `vector.rerank` replaces it with a real cosine.
func TestFusedStatementReranksBeforeScoring(t *testing.T) {
	statement := fusedStatement("identity_id = :identity_id", FusionRRF, 20)
	if !strings.Contains(statement, "`vector.rerank`") {
		t.Fatalf("fused statement scores by RRF rank, which cannot separate relevance:\n%s", statement)
	}
	if !strings.Contains(statement, "score >= :min_relevance") {
		t.Fatalf("fused statement has no relevance floor, so it can never abstain:\n%s", statement)
	}
}

func TestDocumentConfigDefaultsTheRelevanceFloor(t *testing.T) {
	cfg, err := DocumentIndexConfig{Dimensions: 768}.normalized()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RelevanceFloor != defaultDocumentRelevanceFloor {
		t.Fatalf("relevance floor = %v, want %v", cfg.RelevanceFloor, defaultDocumentRelevanceFloor)
	}
}

// Grouping by the object id spends the candidate budget twice on one file: the same bytes
// under two source keys are two IndexedDocument rows by design, and the fusion cannot tell
// them apart by id. The manual's §6.4.19 is explicit that this belongs in the traversal --
// "deduped at the index level rather than over-fetched and post-partitioned in the
// application" -- so the group key is the content hash, not the document id.
func TestFusedStatementGroupsByContentHash(t *testing.T) {
	statement := fusedStatement("identity_id = :identity_id", FusionRRF, 20)
	if !strings.Contains(statement, "groupBy: 'raw_sha256'") {
		t.Fatalf("fusion groups copies of one file as separate documents:\n%s", statement)
	}
}

// The card leg ranked with BM25 while the passage leg ranked with a reranked cosine, so the
// two could not be compared and rankDocuments fell back to a precedence rule. Measured
// 2026-09-09: gi_comuni_cap.xlsx, whose card names every one of its seventeen columns, did
// not come back even when searched by its exact filename, because it has no passages.
// Memory already solved the shape of this — recall runs one reranked query per type and
// merges on the score — so the card leg gets the same wrapper, not a new mechanism.
func TestCardStatementScoresOnTheSameScaleAsPassages(t *testing.T) {
	statement := documentCardStatement("identity_id = :identity_id", 20)
	for _, want := range []string{"`vector.rerank`", "score >= :min_relevance", "card_score"} {
		if !strings.Contains(statement, want) {
			t.Fatalf("card statement is missing %q, so its score cannot meet a passage's:\n%s",
				want, statement)
		}
	}
}
