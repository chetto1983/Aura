//go:build retrieval_eval

// What the SHIPPED retrieval actually scores, against a question set with gold answers.
//
// It drives the production path -- DocumentCardsScoped, FusedCandidates, DocumentNames and
// rankDocuments, as HostRetriever.Retrieve calls them -- so what is measured is what ships
// and not a reimplementation that could flatter it. Gold is matched on the document TITLE,
// which is the file's real name; a chat attachment's key is a uuid on purpose, which is why
// the names lookup below is not optional.
//
// Baseline on the 118-document corpus of 2026-09-09: recall@1 0.875, recall@3 0.938,
// MRR 0.912 over 16 questions. The one question missed at every rank is
// sheet-caraglio-codes, where the file holding the answer has no passages at all and every
// document that merely NAMES the place outscores its card.
//
//	go test -tags retrieval_eval ./internal/documents/ -run TestProductionRetrievalRecall -v
package documents

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
)

func TestProductionRetrievalRecall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	identity := benchEnv(t, "AURA_BENCH_IDENTITY")
	raw, err := os.ReadFile(benchEnv(t, "AURA_BENCH_PILOT"))
	if err != nil {
		t.Fatalf("read pilot: %v", err)
	}
	var pilot benchPilot
	if err := json.Unmarshal(raw, &pilot); err != nil {
		t.Fatalf("decode pilot: %v", err)
	}
	if pilot.SchemaID != "aura.document-retrieval-eval/v1" || len(pilot.Questions) == 0 {
		t.Fatalf("pilot contract is missing or empty: schema=%q", pilot.SchemaID)
	}

	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("tenant credentials: %v", err)
	}
	admin, err := arcadedb.New(arcadedb.Config{
		BaseURL: benchEnv(t, "AURA_ARCADEDB_URL"), Database: benchEnv(t, "AURA_ARCADEDB_DATABASE"),
		User: benchEnv(t, "AURA_ARCADEDB_ADMIN_USER"), Password: benchEnv(t, "AURA_ARCADEDB_ADMIN_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	embedder := arcadedb.NewSidecarEmbedder(benchEnv(t, "AURA_EMBED_BASE_URL"), "embeddinggemma", "", 2*time.Minute)
	tenants := arcadedb.NewTenantClients(arcadedb.Config{BaseURL: benchEnv(t, "AURA_ARCADEDB_URL")}, admin, embedder, credentials)
	index, err := arcadedb.NewDocumentIndex(tenants, arcadedb.DocumentIndexConfig{Dimensions: 768})
	if err != nil {
		t.Fatalf("document index: %v", err)
	}

	cfg := normalizedRetrievalConfig(RetrievalConfig{})
	runs := []benchRun{
		{Arm: "production", Production: true, Ranking: map[string][]string{}},
		{Arm: "reserve-one-card-lane", Ranking: map[string][]string{}},
	}

	for _, question := range pilot.Questions {
		vectors, err := embedder.Embed(ctx, embeddings.RetrievalQueries([]string{question.Query}))
		if err != nil || len(vectors) != 1 {
			t.Fatalf("embed %q: %v", question.QID, err)
		}
		filter := arcadedb.CandidateFilter{IdentityID: identity, Limit: cfg.CandidateLimit}
		// The card leg is the same for both arms: only the passage ordering is under test,
		// and a card-only document like a spreadsheet reaches the ranking through it.
		found, err := index.DocumentCardsScoped(ctx, filter, question.Query, vectors[0])
		if err != nil {
			t.Fatalf("cards %q: %v", question.QID, err)
		}
		cards := make([]RetrievalCard, 0, len(found))
		for _, card := range found {
			cards = append(cards, RetrievalCard{
				DocumentID: card.SearchDocumentID, Title: card.FileName,
				SourceKind: card.SourceKind, SourceKey: card.SourceKey, Card: card.Card,
				Rank: card.Score, OriginalSHA256: card.RawSHA256,
				NormalizedSHA256: card.NormalizedSHA256,
			})
		}
		fused, err := index.FusedCandidates(ctx, arcadedb.FusedCandidateQuery{
			CandidateFilter: filter, Query: question.Query,
			Embedding: vectors[0], Strategy: cfg.FusionStrategy,
		})
		if err != nil {
			t.Fatalf("fuse %q: %v", question.QID, err)
		}
		{
			// The names map is what production resolves for the hits the card leg did not
			// rank: a chat attachment's key is chat/<uuid> on purpose, so without it a
			// passage-only document is titled with a uuid and no gold name can match it.
			names := map[string]string{}
			ids := make([]string, 0, len(fused))
			for _, candidate := range fused {
				ids = append(ids, candidate.SearchDocumentID)
			}
			if len(ids) > 0 {
				resolved, err := index.DocumentNames(ctx, identity, ids)
				if err != nil {
					t.Fatalf("names %q: %v", question.QID, err)
				}
				names = resolved
			}
			ranked := rankDocuments(cards, fused, names, cfg.CandidateLimit, cfg.TopPassages, false)
			for _, doc := range ranked {
				runs[0].Ranking[question.QID] = append(runs[0].Ranking[question.QID], doc.Title)
			}
			for _, doc := range reserveOneCardLane(ranked) {
				runs[1].Ranking[question.QID] = append(runs[1].Ranking[question.QID], doc.Title)
			}
		}
	}

	for i := range runs {
		runs[i].Metrics = scoreBenchmark(pilot.Questions, runs[i].Ranking)
		t.Logf("%-16s recall@1=%.3f recall@3=%.3f mrr=%.3f over %d queries",
			runs[i].Arm, runs[i].Metrics.RecallAt1, runs[i].Metrics.RecallAt3,
			runs[i].Metrics.MRR, runs[i].Metrics.Queries)
	}
	for _, question := range pilot.Questions {
		t.Logf("%-28s %v", question.QID, firstFew(runs[0].Ranking[question.QID]))
	}
	report := benchReport{SchemaID: "aura.document-retrieval-eval-report/v1", Runs: runs}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	if err := os.WriteFile(benchEnv(t, "AURA_BENCH_OUT"), encoded, 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}
}

func firstFew(ranking []string) []string {
	if len(ranking) > 3 {
		return ranking[:3]
	}
	return ranking
}

// reserveOneCardLane is the rule under measurement: a document with NO passages cannot
// compete on passage evidence, only on its card, so it is not allowed to be crowded out of
// the answer entirely by passage-bearing documents. Exactly ONE slot is reserved, and only
// when the best such document is not already near the top -- the same diversification the
// engine's groupBy does for source files, applied to the KIND of evidence instead.
//
// It is here and not in retrieval_rank.go because it is a hypothesis, not a decision.
func reserveOneCardLane(ranked []RetrievalDocument) []RetrievalDocument {
	const lane = 1 // the position the reserved document takes, zero-based
	best := -1
	for index, doc := range ranked {
		if len(doc.Passages) == 0 {
			best = index
			break
		}
	}
	if best <= lane {
		return ranked
	}
	promoted := make([]RetrievalDocument, 0, len(ranked))
	promoted = append(promoted, ranked[:lane]...)
	promoted = append(promoted, ranked[best])
	promoted = append(promoted, ranked[lane:best]...)
	return append(promoted, ranked[best+1:]...)
}
