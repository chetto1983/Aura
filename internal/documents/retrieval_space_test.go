package documents

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
)

func spaceRetriever(control *fakeRetrievalControl, embedder *fakeRetrievalEmbedder, index *fakePassageIndex) *HostRetriever {
	return &HostRetriever{
		ControlPlane: control, PassageIndex: index, Embedder: embedder,
		Config: RetrievalConfig{CandidateLimit: 20},
	}
}

func retrieveCodice(t *testing.T, retriever *HostRetriever) RetrievalResponse {
	t.Helper()
	response, err := retriever.Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "codice cliente"})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	return response
}

// A closed gate is answered before the query is embedded, so it costs no embedding request.
func TestRetrieveAsksTheGateBeforeEmbedding(t *testing.T) {
	control := &fakeRetrievalControl{closed: true, cards: []RetrievalCard{retrievalCard()}}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}}
	response := retrieveCodice(t, spaceRetriever(control, embedder, &fakePassageIndex{}))
	if response.DegradationReason != DegradationSpaceMismatch || embedder.inputs != nil ||
		control.gateSpace != "es1-docs" || control.cardEmbedding != nil {
		t.Fatalf("response = %#v inputs = %v gate = %q", response, embedder.inputs, control.gateSpace)
	}
}

func TestRetrieveBindsTheDenseLegsToTheQuerysSpace(t *testing.T) {
	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	index := &fakePassageIndex{}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}, spaces: []string{"es1-a"}}
	response := retrieveCodice(t, spaceRetriever(control, embedder, index))
	if control.gateSpace != "es1-a" || control.cardSpace != "es1-a" || index.fusedQuery.Space != "es1-a" {
		t.Fatalf("gate %q cards %q passages %q, want es1-a everywhere",
			control.gateSpace, control.cardSpace, index.fusedQuery.Space)
	}
	if response.DegradationReason != "" || response.FloorsReason != arcadedb.ReasonUncalibratedFloors {
		t.Fatalf("response = %#v, want a dense answer naming its uncalibrated floors", response)
	}
}

func TestRetrieveNamesNoFloorsReasonInTheMeasuredSpace(t *testing.T) {
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}, spaces: []string{"es1-e0aa6accf0b79c6b"}}
	response := retrieveCodice(t, spaceRetriever(&fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}},
		embedder, &fakePassageIndex{}))
	if response.FloorsReason != "" {
		t.Fatalf("floors reason = %q in the space the floors were measured in", response.FloorsReason)
	}
}

// Review Focus 2: a model swapped between the gate and the embedding would rank the new
// model's vector against a library checked in the old space.
func TestRetrieveRefusesADenseReadWhoseSpaceMovedDuringTheEmbedding(t *testing.T) {
	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	index := &fakePassageIndex{}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}, spaces: []string{"es1-a", "es1-b"}}
	response := retrieveCodice(t, spaceRetriever(control, embedder, index))
	if response.DegradationReason != DegradationSpaceMismatch || control.cardEmbedding != nil ||
		index.fusedQuery.Space != "" {
		t.Fatalf("response = %#v; a dense leg ran after the space moved", response)
	}
}

func TestRetrieveNamesAGateItCouldNotRead(t *testing.T) {
	control := &fakeRetrievalControl{gateErr: errors.New("count refused"), cards: []RetrievalCard{retrievalCard()}}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}}
	response := retrieveCodice(t, spaceRetriever(control, embedder, &fakePassageIndex{}))
	if response.DegradationReason != DegradationSpaceCheck || embedder.inputs != nil {
		t.Fatalf("response = %#v", response)
	}
}

// Review Focus 5: a hosted route with no key names no space; that is the embedder being
// unavailable, not a failed call.
func TestRetrieveWithoutACredentialIsAnEmbeddingDegradation(t *testing.T) {
	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	embedder := &fakeRetrievalEmbedder{spaceErr: embeddings.ErrNoCredential}
	response := retrieveCodice(t, spaceRetriever(control, embedder, &fakePassageIndex{}))
	if response.DegradationReason != DegradationEmbedding || control.gateSpace != "" {
		t.Fatalf("response = %#v gate = %q", response, control.gateSpace)
	}
}
