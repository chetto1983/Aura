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
