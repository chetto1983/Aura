package documents

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

const (
	retrievalIdentity = "00000000-0000-0000-0000-000000000001"
	retrievalDocument = "10000000-0000-0000-0000-000000000001"
	retrievalVersion  = "20000000-0000-0000-0000-000000000001"
	retrievalPassage  = "30000000-0000-0000-0000-000000000001"
)

type fakeRetrievalControl struct {
	scopeRequest []string
	scope        []string
	cards        []RetrievalCard
	validated    map[string]RevalidatedCandidate
	validateAll  bool
	err          error
}

func (f *fakeRetrievalControl) ResolveDocumentScope(
	_ context.Context, _ string, ids []string,
) ([]string, error) {
	f.scopeRequest = append([]string(nil), ids...)
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.scope...), nil
}

func (f *fakeRetrievalControl) RouteDocumentCards(
	_ context.Context, _ string, _ string, _ []string, _ int,
) ([]RetrievalCard, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]RetrievalCard(nil), f.cards...), nil
}

func (f *fakeRetrievalControl) RevalidateDocumentCandidates(
	_ context.Context, _ string, candidates []arcadedb.PassageCandidate,
) (map[string]RevalidatedCandidate, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]RevalidatedCandidate, len(f.validated)+len(candidates))
	maps.Copy(out, f.validated)
	if f.validateAll {
		for _, candidate := range candidates {
			out[candidateCoordinateKey(candidate)] = RevalidatedCandidate{
				Card: retrievalCard(), Passage: candidate,
			}
		}
	}
	return out, nil
}

type fakeRetrievalProjection struct {
	lexical      []arcadedb.PassageCandidate
	dense        []arcadedb.PassageCandidate
	lexicalErr   error
	denseErr     error
	lexicalQuery arcadedb.LexicalCandidateQuery
	denseQuery   arcadedb.DenseCandidateQuery
}

func (f *fakeRetrievalProjection) LexicalCandidates(
	_ context.Context, query arcadedb.LexicalCandidateQuery,
) ([]arcadedb.PassageCandidate, error) {
	f.lexicalQuery = query
	return append([]arcadedb.PassageCandidate(nil), f.lexical...), f.lexicalErr
}

func (f *fakeRetrievalProjection) DenseCandidates(
	_ context.Context, query arcadedb.DenseCandidateQuery,
) ([]arcadedb.PassageCandidate, error) {
	f.denseQuery = query
	return append([]arcadedb.PassageCandidate(nil), f.dense...), f.denseErr
}

type fakeRetrievalEmbedder struct {
	inputs []string
	vector []float64
	err    error
}

func (f *fakeRetrievalEmbedder) Embed(_ context.Context, inputs []string) ([][]float64, error) {
	f.inputs = append([]string(nil), inputs...)
	if f.err != nil {
		return nil, f.err
	}
	return [][]float64{append([]float64(nil), f.vector...)}, nil
}

func TestHostRetrieverReturnsRevalidatedCitationEvidence(t *testing.T) {
	lexical := retrievalCandidate(arcadedb.RetrievalLegLexical)
	lexical.LexicalScore = new(3.5)
	dense := retrievalCandidate(arcadedb.RetrievalLegDense)
	dense.DenseDistance = new(0.2)
	control := &fakeRetrievalControl{
		scope: []string{retrievalDocument}, cards: []RetrievalCard{retrievalCard()}, validateAll: true,
	}
	projection := &fakeRetrievalProjection{lexical: []arcadedb.PassageCandidate{lexical}, dense: []arcadedb.PassageCandidate{dense}}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}}
	retriever := &HostRetriever{
		ControlPlane: control, Projection: projection, Embedder: embedder,
		Config: RetrievalConfig{CandidateLimit: 20, LexicalMinScore: 2},
	}

	response, err := retriever.Retrieve(t.Context(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "codice cliente WPT", Limit: 3,
		DocumentIDs: []string{"doc_9f2c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Profile != ProductionRetrievalProfile || response.Status != RetrievalComplete ||
		len(response.Documents) != 1 || response.RejectedCandidates != 0 {
		t.Fatalf("response = %#v", response)
	}
	doc := response.Documents[0]
	if doc.RequiresOpen || doc.VersionNumber != 2 || doc.OriginalSHA256 != strings.Repeat("a", 64) ||
		len(doc.Passages) != 1 || len(doc.Passages[0].Evidence) != 2 {
		t.Fatalf("document = %#v", doc)
	}
	passage := doc.Passages[0]
	if passage.CitationToken != "document:doc_9f2c@2#ref=%2Ftexts%2F42;page=7" ||
		passage.CitationLocator != "ref=%2Ftexts%2F42;page=7" ||
		passage.Locator.SelfRef != "/texts/42" ||
		passage.Evidence[0].Rank != 1 || passage.Evidence[1].Rank != 1 {
		t.Fatalf("citation = %#v", passage)
	}
	if !reflect.DeepEqual(control.scopeRequest, []string{"doc_9f2c"}) ||
		!reflect.DeepEqual(projection.lexicalQuery.DocumentIDs, []string{retrievalDocument}) ||
		!reflect.DeepEqual(projection.denseQuery.Embedding, []float64{0.1, 0.2}) {
		t.Fatalf("scope/query not threaded: %#v %#v", projection.lexicalQuery, projection.denseQuery)
	}
	if len(embedder.inputs) != 1 || embedder.inputs[0] != "task: search result | query: codice cliente WPT" {
		t.Fatalf("embedding inputs = %#v", embedder.inputs)
	}
}

func TestHostRetrieverEmitsAuthoritativePostgresPassage(t *testing.T) {
	candidate := retrievalCandidate(arcadedb.RetrievalLegLexical)
	candidate.Text = "stale ArcadeDB text"
	candidate.SelfRef = "/stale/locator"
	candidate.LexicalScore = new(4.0)
	authoritative := candidate
	authoritative.Text = "authoritative PostgreSQL text"
	authoritative.SelfRef = "/texts/authoritative"
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{
			cards: []RetrievalCard{retrievalCard()},
			validated: map[string]RevalidatedCandidate{
				candidateCoordinateKey(candidate): {Card: retrievalCard(), Passage: authoritative},
			},
		},
		Projection: &fakeRetrievalProjection{lexical: []arcadedb.PassageCandidate{candidate}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "WPT cliente"})
	if err != nil {
		t.Fatal(err)
	}
	passage := response.Documents[0].Passages[0]
	if passage.Text != authoritative.Text || passage.Locator.SelfRef != authoritative.SelfRef ||
		passage.CitationLocator != "ref=%2Ftexts%2Fauthoritative;page=7" {
		t.Fatalf("ArcadeDB payload escaped revalidation: %#v", passage)
	}
}

func TestHostRetrieverDegradationIsExplicit(t *testing.T) {
	lexical := retrievalCandidate(arcadedb.RetrievalLegLexical)
	lexical.LexicalScore = new(3.0)
	tests := []struct {
		name       string
		projection *fakeRetrievalProjection
		embedder   *fakeRetrievalEmbedder
		status     RetrievalStatus
		reason     string
		open       bool
	}{
		{
			name: "embedding", projection: &fakeRetrievalProjection{lexical: []arcadedb.PassageCandidate{lexical}},
			embedder: &fakeRetrievalEmbedder{err: errors.New("offline")},
			status:   RetrievalLexicalOnly, reason: DegradationEmbedding,
		},
		{
			name: "dense", projection: &fakeRetrievalProjection{
				lexical: []arcadedb.PassageCandidate{lexical}, denseErr: errors.New("index unavailable"),
			}, embedder: &fakeRetrievalEmbedder{vector: []float64{1}},
			status: RetrievalLexicalOnly, reason: DegradationDense,
		},
		{
			name: "arcade", projection: &fakeRetrievalProjection{lexicalErr: errors.New("server unavailable")},
			embedder: &fakeRetrievalEmbedder{vector: []float64{1}},
			status:   RetrievalCardOnly, reason: DegradationArcade, open: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}, validateAll: true}
			response, err := (&HostRetriever{
				ControlPlane: control, Projection: test.projection, Embedder: test.embedder,
				Config: RetrievalConfig{LexicalMinScore: 2},
			}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "codice cliente"})
			if err != nil {
				t.Fatal(err)
			}
			if response.Status != test.status || response.DegradationReason != test.reason ||
				len(response.Documents) != 1 || response.Documents[0].RequiresOpen != test.open {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestHostRetrieverRejectsStaleArcadeCandidates(t *testing.T) {
	candidate := retrievalCandidate(arcadedb.RetrievalLegLexical)
	candidate.LexicalScore = new(4.0)
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}},
		Projection:   &fakeRetrievalProjection{lexical: []arcadedb.PassageCandidate{candidate}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "WPT cliente"})
	if err != nil {
		t.Fatal(err)
	}
	if response.RejectedCandidates != 1 || len(response.Documents) != 1 ||
		!response.Documents[0].RequiresOpen || len(response.Documents[0].Passages) != 0 {
		t.Fatalf("stale candidate leaked: %#v", response)
	}
}

func TestHostRetrieverValidationAndThresholds(t *testing.T) {
	for _, request := range []RetrievalRequest{
		{}, {IdentityID: retrievalIdentity},
		{IdentityID: retrievalIdentity, Query: "x", Limit: -1},
		{IdentityID: retrievalIdentity, Query: "x", DocumentIDs: []string{" "}},
	} {
		if _, err := (&HostRetriever{}).Retrieve(t.Context(), request); err == nil {
			t.Fatalf("request accepted: %#v", request)
		}
	}
	low := retrievalCandidate(arcadedb.RetrievalLegLexical)
	low.LexicalScore = new(0.5)
	if got := admittedLexical([]arcadedb.PassageCandidate{low}, "single", 2); len(got) != 1 {
		t.Fatalf("single-token positive lexical match rejected: %#v", got)
	}
	if got := admittedLexical([]arcadedb.PassageCandidate{low}, "two terms", 2); len(got) != 0 {
		t.Fatalf("multi-term low lexical match admitted: %#v", got)
	}
	dense := retrievalCandidate(arcadedb.RetrievalLegDense)
	dense.DenseDistance = new(0.56)
	if got := admittedDense([]arcadedb.PassageCandidate{dense}, 0.55); len(got) != 0 {
		t.Fatalf("distant vector admitted: %#v", got)
	}
}

func TestCandidateInputsDeduplicateLegsAndRejectNonUUIDCoordinates(t *testing.T) {
	lexical := retrievalCandidate(arcadedb.RetrievalLegLexical)
	dense := retrievalCandidate(arcadedb.RetrievalLegDense)
	inputs, err := candidateInputs([]arcadedb.PassageCandidate{lexical, dense})
	if err != nil || len(inputs) != 1 || inputs[0].PipelineGeneration != 7 {
		t.Fatalf("inputs = %#v, err = %v", inputs, err)
	}
	lexical.PassageID = "not-a-uuid"
	if _, err := candidateInputs([]arcadedb.PassageCandidate{lexical}); err == nil {
		t.Fatal("invalid candidate coordinates accepted")
	}
}

func retrievalCard() RetrievalCard {
	return RetrievalCard{
		CatalogID: retrievalDocument, DocumentID: "doc_9f2c", Title: "Clienti.xlsx",
		Tags: []string{"clienti"}, Card: "Tabella clienti", Rank: 0.7,
		VersionID: retrievalVersion, VersionNumber: 2, OriginalSHA256: strings.Repeat("a", 64),
		PipelineGeneration: 7,
	}
}

func retrievalCandidate(leg arcadedb.RetrievalLeg) arcadedb.PassageCandidate {
	page := int64(7)
	return arcadedb.PassageCandidate{
		PassageID: retrievalPassage, DocumentID: retrievalDocument,
		SearchDocumentID: "doc_9f2c", VersionID: retrievalVersion, VersionNumber: 2,
		RawSHA256: strings.Repeat("a", 64), PipelineGeneration: "7",
		Ordinal: 42, Text: "Il codice cliente di WPT SRL è C-1042.",
		NormalizedSHA256: strings.Repeat("b", 64), SelfRef: "/texts/42",
		PageNumber: &page, Leg: leg,
	}
}
