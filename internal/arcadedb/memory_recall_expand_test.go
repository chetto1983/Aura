package arcadedb

import (
	"strings"
	"testing"
)

// Seeds come from the top of a score-ordered list, so what the picker must get
// right is agreement over rank: an entity two ranked facts lean on outranks one a
// single higher-scoring fact mentions in passing.
func TestRecallEntitySeedsPrefersAgreementThenRank(t *testing.T) {
	evidence := []RecallEvidence{
		{Kind: RecallEvidenceFact, Rank: 1, Fact: &FactHit{Subject: "Aura", Object: "Memory Management"}},
		{Kind: RecallEvidenceConversation, Rank: 2},
		{Kind: RecallEvidenceFact, Rank: 3, Fact: &FactHit{Subject: "Aura", Object: "ArcadeDB"}},
		{Kind: RecallEvidenceFact, Rank: 4, Fact: &FactHit{Subject: "ArcadeDB", Object: "Cypher"}},
	}
	seeds := recallEntitySeeds(evidence)
	if strings.Join(seeds, ",") != "Aura,ArcadeDB,Memory Management" {
		t.Fatalf("seeds = %#v, want agreement first then best rank", seeds)
	}
}

// A question ranked deep must not drag its tail into the graph: the seeds stop at
// the top hits, and the traversal stops at three entities however many they name.
func TestRecallEntitySeedsAreBounded(t *testing.T) {
	evidence := make([]RecallEvidence, 0, 12)
	for index := range 12 {
		evidence = append(evidence, RecallEvidence{
			Kind: RecallEvidenceFact, Rank: index + 1,
			Fact: &FactHit{Subject: "subject-" + string(rune('a'+index)), Object: "object-" + string(rune('a'+index))},
		})
	}
	seeds := recallEntitySeeds(evidence)
	if len(seeds) != recallMaxEntitySeeds {
		t.Fatalf("seeds = %#v, want %d", seeds, recallMaxEntitySeeds)
	}
	// Only the top hits may seed: the fifth fact's endpoints must not appear.
	for _, seed := range seeds {
		if strings.HasSuffix(seed, "-e") || strings.HasSuffix(seed, "-f") {
			t.Fatalf("seed %q came from past the top hits", seed)
		}
	}
}

// Evidence that names nothing expands to nothing, rather than to a traversal of
// whatever happens to be adjacent.
func TestRecallEntitySeedsIgnoreNonFactEvidence(t *testing.T) {
	if seeds := recallEntitySeeds([]RecallEvidence{
		{Kind: RecallEvidenceConversation, Rank: 1},
		{Kind: RecallEvidenceFact, Rank: 2, Fact: &FactHit{Subject: "  ", Object: ""}},
	}); len(seeds) != 0 {
		t.Fatalf("seeds = %#v, want none", seeds)
	}
}

// A semantic recall must come back carrying the nodes its own evidence named --
// the question that measured this ("che cosa usa Aura per la memoria a lungo
// termine", 2026-09-03) ranked `Aura -usa_memoria_a_lungo_termine-> ArcadeDB`
// second and still exposed no node the caller could expand.
func TestRecallSemanticReturnsSeededEntityNodes(t *testing.T) {
	ranking := `{"result":[{"rid":"#1:0","score":0.78}]}`
	factRow := `{"result":[{"@rid":"#1:0","statement":"Aura usa_memoria_a_lungo_termine ArcadeDB",` +
		`"predicate":"usa_memoria_a_lungo_termine","subject":"Aura","subject_kind":"System",` +
		`"object":"ArcadeDB","object_kind":"System","valid_from":"2026-09-01T00:00:00Z","sources":[]}]}`
	empty := `{"result":[]}`
	// ranking, facts, turns, then one FactsAbout per seed.
	client, rec := recordingClient(t, ranking, factRow, empty, factRow, factRow)
	result, err := client.RecallMemory(t.Context(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeSemantic, Query: "memoria a lungo termine", Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if len(result.Entities) == 0 {
		t.Fatalf("recall returned no graph nodes:\n%s", rec.joined())
	}
	if result.Retrieval.EntityCount != len(result.Entities) {
		t.Fatalf("entity count = %d, nodes = %d", result.Retrieval.EntityCount, len(result.Entities))
	}
	names := make([]string, 0, len(result.Entities))
	for _, node := range result.Entities {
		if len(node.Facts) == 0 {
			t.Fatalf("node %q carries no facts", node.Name)
		}
		names = append(names, node.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "ArcadeDB") {
		t.Fatalf("seeded nodes = %#v, want the object the top fact named", names)
	}
	// The ranked evidence must survive the addition untouched.
	if len(result.Evidence) != 1 || result.Evidence[0].Kind != RecallEvidenceFact {
		t.Fatalf("expansion disturbed the ranked evidence: %#v", result.Evidence)
	}
}
