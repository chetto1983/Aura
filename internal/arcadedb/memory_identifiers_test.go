package arcadedb

import (
	"strings"
	"testing"
	"time"
)

func TestMemoryIdentifiersRequireCompleteTechnicalNames(t *testing.T) {
	for _, test := range []struct {
		query, text string
		want        bool
	}{
		{"Quale database usa ZQX-947?", "Aura usa ArcadeDB", false},
		{"ZQX-947", "ZQX-9470 ZQX-947-old", false},
		{"ZQX-947", "(zqx-947).", true},
		{"memory_merge_entities", "memory_merge_entities_extra", false},
		{"memory_merge_entities", "`memory_merge_entities` falliva", true},
		{"PHASE49_HISTORY_f2104bedf146", "PHASE49_HISTORY_other", false},
		{"Dove usa Neo4j?", "Uso NEO4J.", true},
		{"Neo4j", "Neo4jé", false},
		{"Dove usa il database?", "A database is used here", true},
		{"long-term memory in 2026", "memoria a lungo termine", true},
		{"A1 B2", "A1 soltanto", false},
		{"A1 B2", "B2 poi A1", true},
	} {
		t.Run(test.query+"/"+test.text, func(t *testing.T) {
			if got := memoryIdentifiersMatch(memoryQueryIdentifiers(test.query), test.text); got != test.want {
				t.Fatalf("match = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSearchFactsIdentifierGateSurvivesLexicalFallback(t *testing.T) {
	rows := `{"result":[{"statement":"Aura stores memory","subject":"Aura"},` +
		`{"statement":"ZQX-947 stores memory","subject":"ZQX-947"}]}`
	client, _ := recordingClient(t, rows)
	result, err := client.SearchFactsHybrid(t.Context(), "ZQX-947", 5, time.Time{})
	if err != nil || len(result.Facts) != 1 || result.Facts[0].Subject != "ZQX-947" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestSearchFactsIdentifierGateBeforeAdmission(t *testing.T) {
	ranks := `{"result":[{"rid":"#1:0"},{"rid":"#1:1"}]}`
	rows := `{"result":[{"@rid":"#1:0","statement":"ZQX-9470 uses a database"},` +
		`{"@rid":"#1:1","statement":"The project uses ArcadeDB","subject":"ZQX-947"}]}`
	client, _ := recordingClient(t, ranks, rows)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.SearchFactsHybrid(t.Context(), "ZQX-947", 1, time.Time{})
	if err != nil || len(result.Facts) != 1 || result.Facts[0].Subject != "ZQX-947" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestRecallIdentifierRejectsUnrelatedFactsAndTurns(t *testing.T) {
	facts := `{"result":[{"@rid":"#1:0","statement":"Aura uses ArcadeDB","subject":"Aura"}]}`
	turns := `{"result":[{"@rid":"#2:0","identity_id":"identity-a","conversation_id":"c1",` +
		`"turn_seq":1,"role":"user","content":"PHASE49_HISTORY_other","content_hash":"h",` +
		`"occurred_at":"2026-09-03T10:00:00Z","source_ref":"turn:1"}]}`
	for _, path := range []string{retrievalPathHybrid, retrievalPathLexical} {
		client, rec := recordingClient(t, facts, turns)
		result, err := client.hydrateRecallRanking(t.Context(), RecallRequest{
			IdentityID: "identity-a", Query: "ZQX-947", AsOf: time.Now(),
		}, []recallRankedRID{{rid: "#1:0", score: 0.9}, {rid: "#2:0", score: 0.8}}, 5, path, "")
		if err != nil || !result.Abstained || len(result.Evidence) != 0 {
			t.Fatalf("%s result = %+v, err = %v", path, result, err)
		}
		if strings.Contains(rec.joined(), "BETWEEN :from_seq") {
			t.Fatal("rejected conversation consumed a window read")
		}
	}
}

func TestRecallExpansionKeepsOnlyNewFactKeys(t *testing.T) {
	rows := `{"result":[{"fact_key":"ranked","statement":"ranked fact"},` +
		`{"fact_key":"new","statement":"new fact","subject":"Aura","subject_kind":"System"}]}`
	client, _ := recordingClient(t, rows, rows)
	nodes := client.expandRecallEntities(t.Context(), RecallRequest{}, []RecallEvidence{{
		Kind: RecallEvidenceFact, Rank: 1, Fact: &FactHit{FactKey: "ranked", Subject: "Aura", Object: "ArcadeDB"},
	}})
	if len(nodes) != 2 || len(nodes[0].Facts) != 1 || nodes[0].Facts[0].FactKey != "new" || len(nodes[1].Facts) != 0 {
		t.Fatalf("nodes = %+v", nodes)
	}
}
