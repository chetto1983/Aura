//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"strings"
	"testing"
)

func TestDocumentProjectionRetrievalAndTombstoneLive(t *testing.T) {
	client := disposableArcadeClient(t)
	index, err := NewDocumentIndex(tenantResolverFunc(func(context.Context, string) (*Client, error) {
		return client, nil
	}), DocumentIndexConfig{Dimensions: 3})
	if err != nil {
		t.Fatalf("NewDocumentIndex: %v", err)
	}
	generation := ProjectionGeneration{
		IdentityID: documentTestIdentity, DocumentID: "live-document", SearchDocumentID: "live-search",
		VersionID: "live-version", VersionNumber: 1, RawSHA256: strings.Repeat("a", 64),
		PipelineGeneration: "live-generation",
		Passages: []ProjectedPassage{
			{PassageID: "live-1", Ordinal: 1, Text: "unicorno industriale torinese", NormalizedSHA256: strings.Repeat("b", 64), Embedding: []float64{1, 0, 0}},
			{PassageID: "live-2", Ordinal: 2, Text: "bilancio annuale milanese", NormalizedSHA256: strings.Repeat("c", 64), Embedding: []float64{0, 1, 0}},
		},
	}
	projected, err := index.ProjectGeneration(t.Context(), generation)
	if err != nil {
		t.Fatalf("ProjectGeneration: %v", err)
	}
	if !projected.Created || projected.PassageCount != 2 {
		t.Fatalf("projected = %+v", projected)
	}
	lexical, err := index.LexicalCandidates(t.Context(), LexicalCandidateQuery{
		CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 2}, Query: "unicorno",
	})
	if err != nil {
		t.Fatalf("LexicalCandidates: %v", err)
	}
	if len(lexical) != 1 || lexical[0].PassageID != "live-1" {
		t.Fatalf("lexical = %+v", lexical)
	}
	dense, err := index.DenseCandidates(t.Context(), DenseCandidateQuery{
		CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 1}, Embedding: []float64{1, 0, 0},
	})
	if err != nil {
		t.Fatalf("DenseCandidates: %v", err)
	}
	if len(dense) != 1 || dense[0].PassageID != "live-1" {
		t.Fatalf("dense = %+v", dense)
	}
	if _, err := index.TombstoneGeneration(
		t.Context(), documentTestIdentity, generation.DocumentID, generation.PipelineGeneration,
	); err != nil {
		t.Fatalf("TombstoneGeneration: %v", err)
	}
	lexical, err = index.LexicalCandidates(t.Context(), LexicalCandidateQuery{
		CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 2}, Query: "unicorno",
	})
	if err != nil {
		t.Fatalf("LexicalCandidates after tombstone: %v", err)
	}
	if len(lexical) != 0 {
		t.Fatalf("tombstoned passages remained searchable: %+v", lexical)
	}
	deleted, err := index.DeleteGeneration(
		t.Context(), documentTestIdentity, generation.DocumentID, generation.PipelineGeneration,
	)
	if err != nil {
		t.Fatalf("DeleteGeneration: %v", err)
	}
	if !deleted.Found || deleted.PassageCount != 2 {
		t.Fatalf("deleted = %+v", deleted)
	}
}
