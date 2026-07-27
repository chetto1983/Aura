package adaptive

import (
	"strings"
	"testing"
)

// contract_retrieval.go shipped with no test of its own: the whole file was uncovered,
// which is what pushed the owned-surface floor under 85% in the db_integration-only
// Skills gate. Its job is to refuse anything the caller did not freeze BEFORE execution —
// an unregistered chunk, a repeated one, a revision outside the catalog — so every
// rejection below is a contract guarantee, not a line-count exercise.

func validRetrievalRevisionValues() RetrievalRevisionValues {
	return RetrievalRevisionValues{
		CorpusEpoch: 42,
		Parser:      "parser-v2",
		Chunker:     "chunker-v1",
		Embedding:   "granite-v3",
		Index:       "hnsw-v1",
		Reranker:    "bge-v2",
		Retriever:   "hybrid-v4",
	}
}

func TestNewRetrievalDeliveryMetadataKeepsDeliveredChunkOrder(t *testing.T) {
	results, revisions, err := NewRetrievalDeliveryMetadata(
		[]string{"chunk-a", "chunk-b", "chunk-c"},
		[]string{"chunk-c", "chunk-a"},
		validRetrievalRevisionValues(),
	)
	if err != nil {
		t.Fatalf("NewRetrievalDeliveryMetadata: %v", err)
	}

	// Delivered order is the ranking under evaluation — reordering it would silently
	// rewrite the exposure being measured.
	want := []string{"chunk-c", "chunk-a"}
	if len(results) != len(want) {
		t.Fatalf("results = %d, want %d", len(results), len(want))
	}
	for i, id := range want {
		if results[i].id != id {
			t.Errorf("results[%d].id = %q, want %q", i, results[i].id, id)
		}
		if results[i].kind != ResultChunk {
			t.Errorf("results[%d].kind = %q, want %q", i, results[i].kind, ResultChunk)
		}
	}

	// Corpus epoch plus the six fixed stack revisions.
	if len(revisions.values) != 7 {
		t.Fatalf("revisions = %d, want 7", len(revisions.values))
	}
	if got := revisions.values[RevisionCorpus].value; got != "42" {
		t.Errorf("corpus revision = %q, want %q", got, "42")
	}
	for kind, want := range map[RevisionKind]string{
		RevisionParser:    "parser-v2",
		RevisionChunker:   "chunker-v1",
		RevisionEmbedding: "granite-v3",
		RevisionIndex:     "hnsw-v1",
		RevisionReranker:  "bge-v2",
		RevisionRetriever: "hybrid-v4",
	} {
		if got := revisions.values[kind].value; got != want {
			t.Errorf("%s revision = %q, want %q", kind, got, want)
		}
	}
	if err := revisions.validate(); err != nil {
		t.Errorf("revision set does not validate: %v", err)
	}
}

func TestNewRetrievalDeliveryMetadataAcceptsAnEmptyDelivery(t *testing.T) {
	results, revisions, err := NewRetrievalDeliveryMetadata(
		[]string{"chunk-a"},
		nil,
		validRetrievalRevisionValues(),
	)
	if err != nil {
		t.Fatalf("NewRetrievalDeliveryMetadata: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
	// A retrieval that returned nothing is still a measurable exposure, so the stack
	// revisions must be recorded exactly as for a non-empty one.
	if len(revisions.values) != 7 {
		t.Errorf("revisions = %d, want 7", len(revisions.values))
	}
}

func TestNewRetrievalDeliveryMetadataRejections(t *testing.T) {
	tests := []struct {
		name       string
		registered []string
		delivered  []string
		mutate     func(*RetrievalRevisionValues)
		wantErr    string
	}{
		{
			name:       "delivered chunk was never registered",
			registered: []string{"chunk-a"},
			delivered:  []string{"chunk-b"},
			wantErr:    "not registered",
		},
		{
			name:       "the same chunk is delivered twice",
			registered: []string{"chunk-a"},
			delivered:  []string{"chunk-a", "chunk-a"},
			wantErr:    "duplicated",
		},
		{
			name:       "registered chunk ID is not a safe slug",
			registered: []string{"Chunk-A"},
			delivered:  nil,
			wantErr:    "invalid",
		},
		{
			name:       "registered chunk ID is duplicated in the catalog",
			registered: []string{"chunk-a", "chunk-a"},
			delivered:  nil,
			wantErr:    "duplicated",
		},
		{
			name:       "revision carries no immutable version segment",
			registered: []string{"chunk-a"},
			delivered:  []string{"chunk-a"},
			mutate:     func(v *RetrievalRevisionValues) { v.Embedding = "latest" },
			wantErr:    "invalid",
		},
		{
			name:       "corpus epoch is unset",
			registered: []string{"chunk-a"},
			delivered:  []string{"chunk-a"},
			mutate:     func(v *RetrievalRevisionValues) { v.CorpusEpoch = 0 },
			wantErr:    "corpus revision epoch is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := validRetrievalRevisionValues()
			if tt.mutate != nil {
				tt.mutate(&values)
			}

			results, revisions, err := NewRetrievalDeliveryMetadata(
				tt.registered, tt.delivered, values,
			)

			if err == nil {
				t.Fatalf("accepted %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
			// A rejected delivery must hand back nothing usable: a caller that ignores
			// the error would otherwise record an unvalidated exposure.
			if results != nil {
				t.Errorf("results = %#v, want nil on rejection", results)
			}
			if len(revisions.values) != 0 {
				t.Errorf("revisions = %#v, want the zero set on rejection", revisions.values)
			}
		})
	}
}
