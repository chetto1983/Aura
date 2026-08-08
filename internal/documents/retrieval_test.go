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
	fused      []arcadedb.PassageCandidate
	fusedErr   error
	fusedQuery arcadedb.FusedCandidateQuery
}

func (f *fakeRetrievalProjection) FusedCandidates(
	_ context.Context, query arcadedb.FusedCandidateQuery,
) ([]arcadedb.PassageCandidate, error) {
	f.fusedQuery = query
	return append([]arcadedb.PassageCandidate(nil), f.fused...), f.fusedErr
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
	// One fused candidate, not one per leg: the engine returns a single ranking.
	fused := retrievalCandidate(arcadedb.RetrievalLegFused)
	fused.FusedScore = new(0.031)
	control := &fakeRetrievalControl{
		scope: []string{retrievalDocument}, cards: []RetrievalCard{retrievalCard()},
	}
	projection := &fakeRetrievalProjection{fused: []arcadedb.PassageCandidate{fused}}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}}
	retriever := &HostRetriever{
		ControlPlane: control, Projection: projection, Embedder: embedder,
		Config: RetrievalConfig{CandidateLimit: 20},
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
		len(doc.Passages) != 1 || len(doc.Passages[0].Evidence) != 1 || doc.Score != 0.031 {
		t.Fatalf("document = %#v", doc)
	}
	passage := doc.Passages[0]
	if passage.CitationToken != "document:doc_9f2c@aaaaaaaaaaaa#ref=%2Ftexts%2F42;page=7" ||
		passage.CitationLocator != "ref=%2Ftexts%2F42;page=7" ||
		passage.Locator.SelfRef != "/texts/42" ||
		passage.Evidence[0].Rank != 1 {
		t.Fatalf("citation = %#v", passage)
	}
	if !reflect.DeepEqual(control.scopeRequest, []string{"doc_9f2c"}) ||
		!reflect.DeepEqual(projection.fusedQuery.DocumentIDs, []string{retrievalDocument}) ||
		!reflect.DeepEqual(projection.fusedQuery.Embedding, []float64{0.1, 0.2}) {
		t.Fatalf("scope/query not threaded: %#v", projection.fusedQuery)
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
	candidate := retrievalCandidate(arcadedb.RetrievalLegFused)
	candidate.Text = "il codice cliente WPT-4417 e' attivo"
	candidate.SelfRef = "/texts/42"
	candidate.FusedScore = new(0.031)
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}},
		Projection:   &fakeRetrievalProjection{fused: []arcadedb.PassageCandidate{candidate}},
		Embedder:     &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
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
	candidate := retrievalCandidate(arcadedb.RetrievalLegFused)
	candidate.FusedScore = new(0.031)
	candidate.SourceKind, candidate.SourceKey = "s3", "fatture/2026/q1/fattura-acme.pdf"
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{},
		Projection:   &fakeRetrievalProjection{fused: []arcadedb.PassageCandidate{candidate}},
		Embedder:     &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
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
	// With one fused read there are two ways to lose it -- no embedding to fuse with, or
	// the engine refusing -- and both leave only the cards.
	tests := []struct {
		name       string
		projection *fakeRetrievalProjection
		embedder   *fakeRetrievalEmbedder
		reason     string
	}{
		{"embedding", &fakeRetrievalProjection{}, &fakeRetrievalEmbedder{err: errors.New("offline")}, DegradationEmbedding},
		{"arcade", &fakeRetrievalProjection{fusedErr: errors.New("server unavailable")},
			&fakeRetrievalEmbedder{vector: []float64{1}}, DegradationArcade},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
			response, err := (&HostRetriever{
				ControlPlane: control, Projection: test.projection, Embedder: test.embedder,
			}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "codice cliente"})
			if err != nil {
				t.Fatal(err)
			}
			if response.Status != RetrievalCardOnly || response.DegradationReason != test.reason ||
				len(response.Documents) != 1 || !response.Documents[0].RequiresOpen {
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
}

func retrievalCard() RetrievalCard {
	return RetrievalCard{
		DocumentID: "doc_9f2c", Title: "Clienti.xlsx",
		SourceKind: "s3", SourceKey: "contabilita/Clienti.xlsx",
		Card: "Tabella clienti", Rank: 0.7,
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
