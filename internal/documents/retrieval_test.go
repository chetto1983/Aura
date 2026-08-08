package documents

import (
	"context"
	"errors"
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
		scope: []string{retrievalDocument}, cards: []RetrievalCard{retrievalCard()},
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
	if doc.RequiresOpen || doc.OriginalSHA256 != strings.Repeat("a", 64) ||
		len(doc.Passages) != 1 || len(doc.Passages[0].Evidence) != 2 {
		t.Fatalf("document = %#v", doc)
	}
	passage := doc.Passages[0]
	if passage.CitationToken != "document:doc_9f2c@aaaaaaaaaaaa#ref=%2Ftexts%2F42;page=7" ||
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

func TestHostRetrieverReturnsTheProjectionPassageDirectly(t *testing.T) {
	// This test asserted the OPPOSITE until 2026-08-08: that a Postgres copy fetched by
	// RevalidateDocumentCandidates superseded the ArcadeDB payload wholesale, and it was
	// named EmitsAuthoritativePostgresPassage. That step is gone -- there is no second copy
	// of the text in Postgres to be authoritative, and its own preconditions (uuid ids, an
	// integer generation) rejected every row the reconciler writes. The projection payload
	// is the answer now, which is what comparable systems do.
	candidate := retrievalCandidate(arcadedb.RetrievalLegLexical)
	candidate.Text = "il codice cliente WPT-4417 e' attivo"
	candidate.SelfRef = "/texts/42"
	candidate.LexicalScore = new(4.0)
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}},
		Projection:   &fakeRetrievalProjection{lexical: []arcadedb.PassageCandidate{candidate}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "WPT cliente"})
	if err != nil {
		t.Fatal(err)
	}
	passage := response.Documents[0].Passages[0]
	if passage.Text != candidate.Text || passage.Locator.SelfRef != candidate.SelfRef {
		t.Fatalf("projection payload was altered on the way out: %#v", passage)
	}
	if response.RejectedCandidates != 0 {
		t.Fatalf("nothing revalidates any more, so nothing can be rejected: %d", response.RejectedCandidates)
	}
}

// A document reconciled from the bucket has no catalog row, so no card. Before the passage
// became the ranking spine its passages matched and were then discarded, because a document
// could only be created by a card -- which is the whole reason bucket-ingested documents
// were unreachable through document_search.
func TestHostRetrieverReturnsDocumentsThatHaveNoCard(t *testing.T) {
	candidate := retrievalCandidate(arcadedb.RetrievalLegLexical)
	candidate.LexicalScore = new(4.0)
	candidate.SourceKind, candidate.SourceKey = "s3", "fatture/2026/q1/fattura-acme.pdf"
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{},
		Projection:   &fakeRetrievalProjection{lexical: []arcadedb.PassageCandidate{candidate}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "fattura"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Documents) != 1 {
		t.Fatalf("a passage with no card produced %d documents, want 1", len(response.Documents))
	}
	doc := response.Documents[0]
	if doc.DocumentID != candidate.SearchDocumentID || doc.SourceKey != candidate.SourceKey {
		t.Fatalf("document not built from the passage: %#v", doc)
	}
	// The filename is the title when no card supplies one -- enough for the agent to name
	// what it found, and the key is what document_open needs.
	if doc.Title != "fattura-acme.pdf" {
		t.Fatalf("title = %q, want the object's base name", doc.Title)
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
			control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
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

func retrievalCard() RetrievalCard {
	return RetrievalCard{
		CatalogID: retrievalDocument, DocumentID: "doc_9f2c", Title: "Clienti.xlsx",
		Tags: []string{"clienti"}, Card: "Tabella clienti", Rank: 0.7,
		OriginalSHA256: strings.Repeat("a", 64),
	}
}

func retrievalCandidate(leg arcadedb.RetrievalLeg) arcadedb.PassageCandidate {
	page := int64(7)
	return arcadedb.PassageCandidate{
		PassageID: retrievalPassage, DocumentID: retrievalDocument,
		SearchDocumentID: "doc_9f2c", SourceKind: "s3", SourceKey: "clienti/Clienti.xlsx",
		RawSHA256: strings.Repeat("a", 64), PipelineGeneration: "7",
		Ordinal: 42, Text: "Il codice cliente di WPT SRL è C-1042.",
		NormalizedSHA256: strings.Repeat("b", 64), SelfRef: "/texts/42",
		PageNumber: &page, Leg: leg,
	}
}
