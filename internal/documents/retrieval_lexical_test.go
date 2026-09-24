package documents

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
)

func lexicalCandidate(documentID, raw string, score float64) arcadedb.PassageCandidate {
	candidate := retrievalCandidate(arcadedb.RetrievalLegLexical)
	candidate.SearchDocumentID, candidate.PassageID, candidate.RawSHA256 = documentID, documentID+":0", raw
	candidate.FusedScore, candidate.LexicalScore = nil, new(score)
	return candidate
}

// Every reason the dense legs cannot run is answered from the full-text indexes, never with
// zero documents or an error (spec §8; Review Focus 5 for the credential and attestation rows).
func TestLexicalModeAnswersEveryDenseFailure(t *testing.T) {
	vector := []float64{0.1, 0.2}
	tests := []struct {
		name     string
		control  *fakeRetrievalControl
		embedder QueryEmbedder
		reason   string
	}{
		{"library in another space", &fakeRetrievalControl{closed: true}, &fakeRetrievalEmbedder{vector: vector}, DegradationSpaceMismatch},
		{"gate unreadable", &fakeRetrievalControl{gateErr: errors.New("count refused")}, &fakeRetrievalEmbedder{vector: vector}, DegradationSpaceCheck},
		{"embedding refused", &fakeRetrievalControl{}, &fakeRetrievalEmbedder{err: errors.New("offline")}, DegradationEmbedding},
		{"no credential", &fakeRetrievalControl{}, &fakeRetrievalEmbedder{spaceErr: embeddings.ErrNoCredential}, DegradationEmbedding},
		{"attestation fails", &fakeRetrievalControl{},
			&fakeRetrievalEmbedder{spaceErr: errors.New("attest local embedder: /v1/models returned HTTP 503")}, DegradationEmbedding},
		{"space moved", &fakeRetrievalControl{}, &fakeRetrievalEmbedder{vector: vector, spaces: []string{"es1-a", "es1-b"}}, DegradationSpaceMismatch},
		{"no embedder", &fakeRetrievalControl{}, nil, DegradationEmbedding},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.control.cards = []RetrievalCard{retrievalCard()}
			index := &fakePassageIndex{lexical: []arcadedb.PassageCandidate{
				lexicalCandidate("doc_9f2c", strings.Repeat("a", 64), 4.2),
			}}
			response := retrieveCodice(t, &HostRetriever{
				ControlPlane: test.control, PassageIndex: index, Embedder: test.embedder,
			})
			if response.Status != RetrievalLexicalOnly || response.DegradationReason != test.reason ||
				response.Abstained || response.FloorsReason != "" {
				t.Fatalf("response = %#v", response)
			}
			if len(response.Documents) != 1 || len(response.Documents[0].Passages) != 1 ||
				response.Documents[0].Passages[0].Evidence[0].Leg != string(arcadedb.RetrievalLegLexical) ||
				response.Documents[0].Score != 4.2 {
				t.Fatalf("documents = %#v", response.Documents)
			}
			if test.control.lexicalQuery != "codice cliente" || index.lexicalQuery != "codice cliente" ||
				index.fusedQuery.Query != "" || test.control.cardEmbedding != nil {
				t.Fatalf("a dense leg ran, or a lexical one did not: cards %q passages %q fused %q",
					test.control.lexicalQuery, index.lexicalQuery, index.fusedQuery.Query)
			}
		})
	}
}

func TestLexicalModeAbstainsWhenNothingMatches(t *testing.T) {
	response := retrieveCodice(t, &HostRetriever{
		ControlPlane: &fakeRetrievalControl{closed: true}, PassageIndex: &fakePassageIndex{},
		Embedder: &fakeRetrievalEmbedder{},
	})
	if !response.Abstained || response.AbstentionReason != AbstainedNoQualifiedPassage ||
		response.Status != RetrievalLexicalOnly || len(response.Documents) != 0 {
		t.Fatalf("response = %#v", response)
	}
}

// Measured: five identical transcripts took the top five places for an unrelated question.
func TestLexicalModeCountsIdenticalFilesOnce(t *testing.T) {
	raw := strings.Repeat("c", 64)
	response := retrieveCodice(t, &HostRetriever{
		ControlPlane: &fakeRetrievalControl{closed: true},
		PassageIndex: &fakePassageIndex{lexical: []arcadedb.PassageCandidate{
			lexicalCandidate("doc_video1", raw, 7.5), lexicalCandidate("doc_video2", raw, 7.5),
			lexicalCandidate("doc_video3", raw, 7.5),
		}},
		Embedder: &fakeRetrievalEmbedder{},
	})
	if len(response.Documents) != 1 {
		t.Fatalf("documents = %d, want the three copies as one", len(response.Documents))
	}
}

func TestLexicalModeKeepsTheCallersScope(t *testing.T) {
	control := &fakeRetrievalControl{closed: true, scope: []string{retrievalDocument}}
	index := &fakePassageIndex{}
	scopes := []SourceScope{{Kind: SourceScopeFolder, Path: "/finance/2026/"}}
	_, err := (&HostRetriever{ControlPlane: control, PassageIndex: index, Embedder: &fakeRetrievalEmbedder{}}).
		Retrieve(t.Context(), RetrievalRequest{
			IdentityID: retrievalIdentity, Query: "fatturato", DocumentIDs: []string{retrievalDocument}, SourceScopes: scopes,
		})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(control.cardDocumentScope, []string{retrievalDocument}) ||
		!reflect.DeepEqual(index.lexicalFilter.DocumentIDs, []string{retrievalDocument}) ||
		!reflect.DeepEqual(index.lexicalFilter.SourcePrefixes, []string{"finance/2026/"}) ||
		len(control.cardSourceScope) != 1 {
		t.Fatalf("scope lost: cards %v %v passages %#v", control.cardDocumentScope, control.cardSourceScope, index.lexicalFilter)
	}
}

// A passage index that refuses in lexical mode still leaves the cards, as it does in dense mode.
func TestLexicalModeServesItsCardsWhenThePassageIndexRefuses(t *testing.T) {
	response := retrieveCodice(t, &HostRetriever{
		ControlPlane: &fakeRetrievalControl{closed: true, cards: []RetrievalCard{retrievalCard()}},
		PassageIndex: &fakePassageIndex{lexicalErr: errors.New("engine refused")},
		Embedder:     &fakeRetrievalEmbedder{},
	})
	if response.Status != RetrievalCardOnly || response.DegradationReason != DegradationArcade ||
		len(response.Documents) != 1 || !response.Documents[0].RequiresOpen {
		t.Fatalf("response = %#v", response)
	}
}
