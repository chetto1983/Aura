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
	scopeRequest      []string
	scope             []string
	cardDocumentScope []string
	cardEmbedding     []float64
	cardSourceScope   []SourceScope
	cards             []RetrievalCard
	names             map[string]string
	namesRequest      []string
	namesErr          error
	err               error
}

func (f *fakeRetrievalControl) DocumentNames(
	_ context.Context, _ string, ids []string,
) (map[string]string, error) {
	f.namesRequest = append([]string(nil), ids...)
	if f.namesErr != nil {
		return nil, f.namesErr
	}
	return f.names, nil
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
	_ context.Context, _ string, _ string, embedding []float64,
	documentIDs []string, sourceScopes []SourceScope, _ int,
) ([]RetrievalCard, error) {
	f.cardEmbedding = append([]float64(nil), embedding...)
	f.cardDocumentScope = append([]string(nil), documentIDs...)
	f.cardSourceScope = append([]SourceScope(nil), sourceScopes...)
	if f.err != nil {
		return nil, f.err
	}
	return append([]RetrievalCard(nil), f.cards...), nil
}

type fakePassageIndex struct {
	fused      []arcadedb.PassageCandidate
	fusedErr   error
	fusedQuery arcadedb.FusedCandidateQuery
	at         []arcadedb.PassageCandidate
	atErr      error
	atRefs     []arcadedb.PassageRef
}

func (f *fakePassageIndex) PassagesAt(
	_ context.Context, _ string, refs []arcadedb.PassageRef,
) ([]arcadedb.PassageCandidate, error) {
	f.atRefs = append(f.atRefs, refs...)
	return append([]arcadedb.PassageCandidate(nil), f.at...), f.atErr
}

func (f *fakePassageIndex) FusedCandidates(
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

func TestHostRetrieverReturnsCitationEvidence(t *testing.T) {
	// One fused candidate, not one per leg: the engine returns a single ranking.
	fused := retrievalCandidate(arcadedb.RetrievalLegFused)
	// A cosine, like the card's rank below: both legs are reranked now, so the document's
	// score is the best evidence for it and the two can actually be compared.
	fused.FusedScore = new(0.62)
	control := &fakeRetrievalControl{
		scope: []string{retrievalDocument}, cards: []RetrievalCard{retrievalCard()},
	}
	passageIndex := &fakePassageIndex{fused: []arcadedb.PassageCandidate{fused}}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}}
	retriever := &HostRetriever{
		ControlPlane: control, PassageIndex: passageIndex, Embedder: embedder,
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
		len(response.Documents) != 1 {
		t.Fatalf("response = %#v", response)
	}
	doc := response.Documents[0]
	if doc.RequiresOpen || doc.OriginalSHA256 != strings.Repeat("a", 64) ||
		len(doc.Passages) != 1 || len(doc.Passages[0].Evidence) != 1 || doc.Score != 0.62 {
		t.Fatalf("document = %#v", doc)
	}
	passage := doc.Passages[0]
	if passage.CitationToken != "document:doc_9f2c@aaaaaaaaaaaa#chars=10-25" ||
		passage.CitationLocator != "chars=10-25" ||
		passage.Locator.CharStart == nil || *passage.Locator.CharStart != 10 ||
		passage.Evidence[0].Rank != 1 {
		t.Fatalf("citation = %#v", passage)
	}
	if !reflect.DeepEqual(control.scopeRequest, []string{"doc_9f2c"}) ||
		!reflect.DeepEqual(passageIndex.fusedQuery.DocumentIDs, []string{retrievalDocument}) ||
		!reflect.DeepEqual(passageIndex.fusedQuery.Embedding, []float64{0.1, 0.2}) {
		t.Fatalf("scope/query not threaded: %#v", passageIndex.fusedQuery)
	}
	if len(embedder.inputs) != 1 || embedder.inputs[0] != "task: search result | query: codice cliente WPT" {
		t.Fatalf("embedding inputs = %#v", embedder.inputs)
	}
}

func TestHostRetrieverThreadsGarageScopeToCardsAndBothPassageLegs(t *testing.T) {
	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	passages := &fakePassageIndex{}
	scopes := []SourceScope{
		{Kind: SourceScopeFolder, Path: "/finance/2026/"},
		{Kind: SourceScopeFile, Path: "/manual.pdf"},
	}
	_, err := (&HostRetriever{
		ControlPlane: control,
		PassageIndex: passages,
		Embedder:     &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
	}).Retrieve(t.Context(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "revenue", SourceScopes: scopes,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantScopes := []SourceScope{
		{Kind: SourceScopeFolder, Path: "finance/2026"},
		{Kind: SourceScopeFile, Path: "manual.pdf"},
	}
	if !reflect.DeepEqual(control.cardSourceScope, wantScopes) {
		t.Fatalf("card source scope = %#v, want %#v", control.cardSourceScope, wantScopes)
	}
	if !reflect.DeepEqual(passages.fusedQuery.SourceKeys, []string{"manual.pdf"}) ||
		!reflect.DeepEqual(passages.fusedQuery.SourcePrefixes, []string{"finance/2026/"}) {
		t.Fatalf("passage source scope = %#v", passages.fusedQuery.CandidateFilter)
	}
}

func TestHostRetrieverNeverWidensAnEmptyResolvedDocumentScope(t *testing.T) {
	control := &fakeRetrievalControl{}
	_, err := (&HostRetriever{ControlPlane: control}).Retrieve(t.Context(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "secret", DocumentIDs: []string{"not-visible"},
	})
	if !errors.Is(err, ErrInvalidDocumentScope) {
		t.Fatalf("error = %v, want ErrInvalidDocumentScope", err)
	}
	if control.cardDocumentScope != nil {
		t.Fatalf("card query still ran with %#v", control.cardDocumentScope)
	}
}

func TestHostRetrieverReturnsTheIndexedPassageDirectly(t *testing.T) {
	candidate := retrievalCandidate(arcadedb.RetrievalLegFused)
	candidate.Text = "il codice cliente WPT-4417 e' attivo"
	candidate.HeadingPath = []string{"Clienti"}
	candidate.FusedScore = new(0.031)
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}},
		PassageIndex: &fakePassageIndex{fused: []arcadedb.PassageCandidate{candidate}},
		Embedder:     &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "WPT cliente"})
	if err != nil {
		t.Fatal(err)
	}
	passage := response.Documents[0].Passages[0]
	if passage.Text != candidate.Text || !reflect.DeepEqual(passage.Locator.HeadingPath, candidate.HeadingPath) {
		t.Fatalf("indexed passage was altered on the way out: %#v", passage)
	}
}

// Passage hits remain searchable even when the document-card leg has no matching row.
func TestHostRetrieverReturnsDocumentsThatHaveNoCard(t *testing.T) {
	candidate := retrievalCandidate(arcadedb.RetrievalLegFused)
	candidate.FusedScore = new(0.031)
	candidate.SourceKind, candidate.SourceKey = "s3", "fatture/2026/q1/fattura-acme.pdf"
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{},
		PassageIndex: &fakePassageIndex{fused: []arcadedb.PassageCandidate{candidate}},
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

// A chat attachment's key is `chat/<assetID>.pdf` on purpose, so its base name is a uuid.
// Titling a passage-leg hit with it showed the agent an id where a name belongs: measured
// 2026-08-16 on the live stack, document_search answered
// "bc4c9304-7729-4b1e-9009-0882a03ea1a5.pdf" and the agent had to spend a document_open
// call to learn the file was called colm2025_conference.pdf.
func TestHostRetrieverTitlesPassageOnlyHitsWithTheirRealName(t *testing.T) {
	carded := retrievalCandidate(arcadedb.RetrievalLegFused)
	uncarded := retrievalCandidate(arcadedb.RetrievalLegFused)
	uncarded.SearchDocumentID, uncarded.PassageID = "doc_bucket", "40000000-0000-0000-0000-000000000001"
	uncarded.SourceKey = "chat/bc4c9304-7729-4b1e-9009-0882a03ea1a5.pdf"
	// Its own bytes, because retrieval now collapses copies by content hash: leaving the
	// helper's hash here would make two unrelated documents a SHA-256 collision.
	uncarded.RawSHA256 = "b17e0f5c9a2d43e8b6c1f0a7d5e3948266cbb01f9a7d2e4c8b3a5f6071829d4e"
	control := &fakeRetrievalControl{
		cards: []RetrievalCard{retrievalCard()},
		names: map[string]string{"doc_bucket": "colm2025_conference.pdf"},
	}
	response, err := (&HostRetriever{
		ControlPlane: control,
		PassageIndex: &fakePassageIndex{
			fused: []arcadedb.PassageCandidate{carded, uncarded},
		},
		Embedder: &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "footnotes"})
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, doc := range response.Documents {
		titles[doc.DocumentID] = doc.Title
	}
	if titles["doc_bucket"] != "colm2025_conference.pdf" {
		t.Fatalf("passage-only title = %q, want the indexed name", titles["doc_bucket"])
	}
	// The card already carries the name of everything it ranked, so asking again would be a
	// second answer to a settled question -- and a needlessly wider statement.
	if !reflect.DeepEqual(control.namesRequest, []string{"doc_bucket"}) {
		t.Fatalf("names requested for %#v, want only the uncarded hit", control.namesRequest)
	}
	if titles["doc_9f2c"] != "Clienti.xlsx" {
		t.Fatalf("carded title = %q, want the card's own", titles["doc_9f2c"])
	}
}

// The name is enrichment: losing it must cost the title, never the passages. This is the
// one silenced failure in the cascade that does NOT set a DegradationReason, because the
// answer itself is complete.
func TestHostRetrieverKeepsTheAnswerWhenTheNameLookupFails(t *testing.T) {
	candidate := retrievalCandidate(arcadedb.RetrievalLegFused)
	candidate.SearchDocumentID = "doc_bucket"
	candidate.SourceKey = "chat/bc4c9304-7729-4b1e-9009-0882a03ea1a5.pdf"
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{namesErr: errors.New("arcadedb unreachable")},
		PassageIndex: &fakePassageIndex{fused: []arcadedb.PassageCandidate{candidate}},
		Embedder:     &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "footnotes"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != RetrievalComplete || response.DegradationReason != "" {
		t.Fatalf("a missing title degraded the whole response: %#v", response)
	}
	doc := response.Documents[0]
	if doc.Title != "bc4c9304-7729-4b1e-9009-0882a03ea1a5.pdf" || len(doc.Passages) != 1 {
		t.Fatalf("document = %#v, want the key-derived title and its passage intact", doc)
	}
}

func TestHostRetrieverDegradationIsExplicit(t *testing.T) {
	// With one fused read there are two ways to lose it -- no embedding to fuse with, or
	// the engine refusing -- and both leave only the cards.
	tests := []struct {
		name         string
		passageIndex *fakePassageIndex
		embedder     *fakeRetrievalEmbedder
		reason       string
	}{
		{"embedding", &fakePassageIndex{}, &fakeRetrievalEmbedder{err: errors.New("offline")}, DegradationEmbedding},
		{"arcade", &fakePassageIndex{fusedErr: errors.New("server unavailable")},
			&fakeRetrievalEmbedder{vector: []float64{1}}, DegradationArcade},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
			response, err := (&HostRetriever{
				ControlPlane: control, PassageIndex: test.passageIndex, Embedder: test.embedder,
			}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "codice cliente"})
			if err != nil {
				t.Fatal(err)
			}
			if response.Status != RetrievalCardOnly || response.DegradationReason != test.reason {
				t.Fatalf("response = %#v", response)
			}
			// An embedder outage now costs BOTH legs, not one. The card leg is ranked
			// against the query vector like the passage leg is -- that is what lets the two
			// be compared at all -- so with no vector there is no honest card ranking to
			// serve either, and answering from an unranked list would be a guess wearing a
			// degradation label. Every other degradation still serves its cards.
			if test.reason == DegradationEmbedding {
				if len(response.Documents) != 0 {
					t.Fatalf("documents = %d, want none: an unscored card is not an answer",
						len(response.Documents))
				}
				return
			}
			if len(response.Documents) != 1 || !response.Documents[0].RequiresOpen {
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
		Card: "Tabella clienti", Rank: 0.41,
		OriginalSHA256: strings.Repeat("a", 64),
	}
}

func retrievalCandidate(leg arcadedb.RetrievalLeg) arcadedb.PassageCandidate {
	return arcadedb.PassageCandidate{
		PassageID:        retrievalPassage,
		SearchDocumentID: "doc_9f2c", SourceKind: "s3", SourceKey: "clienti/Clienti.xlsx",
		RawSHA256: strings.Repeat("a", 64),
		Ordinal:   42, Text: "Il codice cliente di WPT SRL è C-1042.",
		NormalizedSHA256: strings.Repeat("b", 64),
		CharacterSpan:    &arcadedb.CharacterSpan{Start: 10, End: 25}, Leg: leg,
	}
}

// A query the corpus cannot answer must come back empty and say so, not hand the agent
// whatever the card index happened to match. Measured 2026-09-09 on the live corpus:
// "ricetta della carbonara" returned three E2E worker reports at status "complete" with an
// empty degradation reason, and the agent had nothing on the wire telling it to stop.
func TestHostRetrieverAbstainsWhenNoPassageQualifies(t *testing.T) {
	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	retriever := &HostRetriever{
		ControlPlane: control,
		PassageIndex: &fakePassageIndex{}, // the floor admitted nothing
		Embedder:     &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
		Config:       RetrievalConfig{CandidateLimit: 20},
	}

	response, err := retriever.Retrieve(t.Context(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "ricetta della carbonara", Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Abstained {
		t.Error("Abstained = false, want true when no passage qualified")
	}
	if response.AbstentionReason != AbstainedNoQualifiedPassage {
		t.Errorf("AbstentionReason = %q, want %q", response.AbstentionReason, AbstainedNoQualifiedPassage)
	}
	if len(response.Documents) != 0 {
		t.Errorf("documents = %d, want 0: a card match alone is not evidence", len(response.Documents))
	}
}

// The card-only DEGRADATIONS are not abstentions: the passage leg never ran, so the cards
// are the best honest answer rather than a claim that the corpus has nothing.
func TestHostRetrieverDoesNotAbstainWhileDegradedToCards(t *testing.T) {
	retriever := &HostRetriever{
		ControlPlane: &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}},
		// The passage leg is what is missing here, not the embedder: the cards are ranked
		// against the query vector, so a degradation that keeps them has to keep it too.
		Embedder: &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
		Config:   RetrievalConfig{CandidateLimit: 20},
	}

	response, err := retriever.Retrieve(t.Context(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "clienti", Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Abstained {
		t.Error("Abstained = true while card-only degraded, want false")
	}
	if len(response.Documents) == 0 {
		t.Error("documents = 0, want the cards the degradation exists to serve")
	}
}
