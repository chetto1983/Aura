//go:build arcadedb_integration

// Does retrieving over EXTRACTED FACTS beat retrieving over raw turns?
//
// This is the last architectural difference with Mem0. Everything measured so
// far retrieves conversation turns as they were spoken; Mem0 retrieves facts an
// LLM distilled out of them, which is a different corpus with different
// statistics: shorter, denser, and free of the filler that dominates a chat log.
//
// The facts were extracted by hand for conversation 1 -- 130 facts over 419
// turns -- and each carries the dia_ids it came from, which raw Mem0 facts do
// not: they score answer accuracy, and evidence provenance is what makes this
// scoreable for free. Extraction is otherwise the same shape as their
// FACT_RETRIEVAL_PROMPT: short natural-language statements about the speakers.
//
// The comparison is controlled: same 199 questions, same retrieval pipeline,
// same BM25 re-ranker. Only the indexed corpus changes.
//
// Run: go test -tags arcadedb_integration -run LocomoFacts -v ./internal/arcadedb/
package arcadedb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/bm25"
)

const factType = "LocomoFact"

type extractedFact struct {
	Subject   string   `json:"s"`
	Predicate string   `json:"p"`
	Object    string   `json:"o"`
	Statement string   `json:"st"`
	DiaIDs    []string `json:"d"`
}

func loadExtractedFacts(t *testing.T) []extractedFact {
	t.Helper()
	path := envOr("AURA_LOCOMO_FACTS", filepath.Join(
		"/mnt/c/Users/Davide/AppData/Local/Temp/claude/d--Aura",
		"23893a75-ceb5-4bf5-92be-3a72e5b4afab/scratchpad/locomo1_facts.json"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("extracted facts unavailable at %s: %v", path, err)
	}
	var facts []extractedFact
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatalf("decode facts: %v", err)
	}
	return facts
}

// factDocument is what gets indexed. The subject and predicate are repeated
// because BM25 has no notion of fields and repetition is how a flat index
// expresses one -- the same trick tool search uses to weight a name over a
// schema. A question names its subject far more reliably than it quotes the
// sentence.
func factDocument(fact extractedFact) string {
	return fact.Subject + " " + fact.Subject + " " +
		fact.Predicate + " " + fact.Object + " " + fact.Statement
}

const factWriteBatch = `
UNWIND $facts AS fact
CREATE (f:LocomoFact)
SET f.id = fact.id, f.conversation = fact.conversation,
    f.text = fact.text, f.dia_ids = fact.dia_ids
RETURN count(f) AS written
`

func TestLocomoFactsLoad(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	facts := loadExtractedFacts(t)

	for _, ddl := range []string{
		"CREATE VERTEX TYPE " + factType + " IF NOT EXISTS",
		"CREATE PROPERTY " + factType + ".id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + factType + ".conversation IF NOT EXISTS STRING",
		"CREATE PROPERTY " + factType + ".text IF NOT EXISTS STRING",
		"CREATE PROPERTY " + factType + ".dia_ids IF NOT EXISTS LIST",
		"CREATE INDEX IF NOT EXISTS ON " + factType + " (id) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + factType + " (text) FULL_TEXT " +
			"METADATA {analyzer:'org.apache.lucene.analysis.en.EnglishAnalyzer'}",
	} {
		if _, err := client.Command(ctx, ddl, nil); err != nil {
			t.Fatalf("ddl %q: %v", ddl, err)
		}
	}
	if _, err := client.Command(ctx, "DELETE FROM "+factType, nil); err != nil {
		t.Fatalf("clear facts: %v", err)
	}

	payload := make([]any, 0, len(facts))
	for i, fact := range facts {
		payload = append(payload, map[string]any{
			"id":           fmt.Sprintf("locomo-1#f%03d", i),
			"conversation": "locomo-1",
			"text":         factDocument(fact),
			"dia_ids":      fact.DiaIDs,
		})
	}
	started := time.Now()
	for start := 0; start < len(payload); start += locomoBatchSize {
		end := min(start+locomoBatchSize, len(payload))
		if _, err := client.Write(ctx, factWriteBatch,
			map[string]any{"facts": payload[start:end]}); err != nil {
			t.Fatalf("write facts %d-%d: %v", start, end, err)
		}
	}
	covered := map[string]bool{}
	for _, fact := range facts {
		for _, diaID := range fact.DiaIDs {
			covered[diaID] = true
		}
	}
	t.Logf("%d facts covering %d distinct turns, written in %s",
		len(facts), len(covered), time.Since(started).Round(time.Millisecond))
}

// TestLocomoFactsVersusTurns is the controlled A/B.
func TestLocomoFactsVersusTurns(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	samples := loadLocomo(t)
	facts := loadExtractedFacts(t)

	rows, err := client.Query(ctx,
		"SELECT count(*) AS n FROM "+factType+" WHERE conversation = 'locomo-1'", nil)
	if err != nil || len(rows) == 0 || rowInt(rows[0], "n") == 0 {
		t.Skip("facts not loaded; run TestLocomoFactsLoad first")
	}

	// One BM25 index per corpus, both over conversation 1 only.
	factDocs := make([]string, 0, len(facts))
	for _, fact := range facts {
		factDocs = append(factDocs, factDocument(fact))
	}
	factIndex := bm25.New(factDocs)
	turnRows, err := client.Query(ctx,
		"SELECT dia_id, text FROM "+locomoEnglishType+
			" WHERE conversation = :c LIMIT 5000", map[string]any{"c": "locomo-1"})
	if err != nil {
		t.Fatalf("read locomo-1 turns: %v", err)
	}
	turnIDs := make([]string, 0, len(turnRows))
	turnDocs := make([]string, 0, len(turnRows))
	for _, row := range turnRows {
		turnIDs = append(turnIDs, rowString(row, "dia_id"))
		turnDocs = append(turnDocs, rowString(row, "text"))
	}
	turnIndex := bm25.New(turnDocs)

	questions := 0
	turnScore, factScore := &recallScore{}, &recallScore{}
	factCeiling := 0
	turnStart := time.Now()

	for _, qa := range samples[0].QA {
		if qa.Category == adversarialClass || len(qa.Evidence) == 0 {
			continue
		}
		questions++

		// Raw turns: the same in-process BM25 ranker used for the fact corpus.
		rank := -1
		for position, hit := range turnIndex.Rank(qa.Question) {
			if position >= 10 {
				break
			}
			if slices.Contains(qa.Evidence, turnIDs[hit.Doc]) {
				rank = position
				break
			}
		}
		turnScore.add(rank)

		// Facts: ranked entirely in process, since 130 documents do not need a
		// database round trip to shortlist. A fact hit counts when ANY dia_id it
		// was derived from is evidence for the question.
		rank = -1
		for position, hit := range factIndex.Rank(qa.Question) {
			if position >= 10 {
				break
			}
			for _, diaID := range facts[hit.Doc].DiaIDs {
				if slices.Contains(qa.Evidence, diaID) {
					rank = position
					break
				}
			}
			if rank >= 0 {
				break
			}
		}
		factScore.add(rank)

		// The ceiling: is the evidence reachable through ANY extracted fact? A
		// question whose evidence no fact covers is lost to extraction, not to
		// ranking, and no retriever can recover it.
		for _, fact := range facts {
			reachable := false
			for _, diaID := range fact.DiaIDs {
				if slices.Contains(qa.Evidence, diaID) {
					reachable = true
					break
				}
			}
			if reachable {
				factCeiling++
				break
			}
		}
	}

	t.Logf("conversation 1: %d scorable questions, %d turns vs %d facts, %s",
		questions, len(turnDocs), len(facts),
		time.Since(turnStart).Round(time.Millisecond))
	t.Logf("%-24s %8s %8s %8s", "corpus", "R@1", "R@5", "R@10")
	t.Logf("%-24s %7.1f%% %7.1f%% %7.1f%%", "raw turns (BM25)",
		pct(turnScore.hit1, turnScore.asked), pct(turnScore.hit5, turnScore.asked),
		pct(turnScore.hit10, turnScore.asked))
	t.Logf("%-24s %7.1f%% %7.1f%% %7.1f%%", "extracted facts (BM25)",
		pct(factScore.hit1, factScore.asked), pct(factScore.hit5, factScore.asked),
		pct(factScore.hit10, factScore.asked))
	t.Logf("%-24s %7.1f%%", "fact-coverage ceiling", pct(factCeiling, questions))
}

// TestLocomoFactsRecallCurve answers the only question that makes the 92.5 on
// Mem0's leaderboard comparable to anything here.
//
// That number is ANSWER accuracy judged by an LLM, not retrieval recall, and an
// answer does not need its evidence at rank 1 -- it needs it somewhere in the
// context the model is handed. Mem0 reports ~7.0K tokens per query, which is
// dozens of memories, not ten. So the honest comparison is recall at the k they
// actually feed, and the curve below says where retrieval stops being the
// binding constraint.
func TestLocomoFactsRecallCurve(t *testing.T) {
	client := documentClient(t)
	samples := loadLocomo(t)
	facts := loadExtractedFacts(t)
	_ = client

	documents := make([]string, 0, len(facts))
	for _, fact := range facts {
		documents = append(documents, factDocument(fact))
	}
	index := bm25.New(documents)

	cutoffs := []int{1, 3, 5, 10, 20, 30, 50}
	hits := make([]int, len(cutoffs))
	asked, reachable := 0, 0
	tokens := 0

	started := time.Now()
	for _, qa := range samples[0].QA {
		if qa.Category == adversarialClass || len(qa.Evidence) == 0 {
			continue
		}
		asked++
		found := -1
		for position, hit := range index.Rank(qa.Question) {
			if evidenceIn(facts[hit.Doc].DiaIDs, qa.Evidence) {
				found = position
				break
			}
		}
		for i, cutoff := range cutoffs {
			if found >= 0 && found < cutoff {
				hits[i]++
			}
		}
		for _, fact := range facts {
			if evidenceIn(fact.DiaIDs, qa.Evidence) {
				reachable++
				break
			}
		}
	}
	// What 30 facts actually cost to hand a model, in rough tokens.
	for _, document := range documents {
		tokens += len(bm25.Tokenize(document))
	}
	perFact := float64(tokens) / float64(max(len(documents), 1))

	t.Logf("%d questions over %d facts, %s", asked, len(facts),
		time.Since(started).Round(time.Millisecond))
	for i, cutoff := range cutoffs {
		t.Logf("   R@%-3d %6.1f%%", cutoff, pct(hits[i], asked))
	}
	t.Logf("   ceiling (evidence reachable through any extracted fact): %.1f%%",
		pct(reachable, asked))
	t.Logf("   a 30-fact context is roughly %.0f tokens; Mem0 reports ~7000",
		perFact*30*1.4)
}

func evidenceIn(diaIDs, evidence []string) bool {
	for _, diaID := range diaIDs {
		if slices.Contains(evidence, diaID) {
			return true
		}
	}
	return false
}
