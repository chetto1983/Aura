//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/embeddings"
)

// The exact lane over a live ArcadeDB (spec 2026-10-25-document-exactness-and-judge, decision 1,
// "Memory"): a fact that names the font, its vector away from the question's, is dropped by the
// reranked cosine and its floor, and the quoted name admits it first, in recall and in the fact
// search alike.

// axisEmbedder puts every query on one axis and every stored text naming the font on another, so
// the floor drops that fact for a reason the test controls.
type axisEmbedder struct{}

func (axisEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: "es1-exact-live"}, nil
}

func (axisEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, text := range texts {
		axis := 0
		if !strings.HasPrefix(text, embeddings.QueryPrefix) && strings.Contains(text, "Ubuntu Mono") {
			axis = 1
		}
		out[i] = make([]float64, vectorDimensions)
		out[i][axis] = 1
	}
	return out, nil
}

const fontStatement = "The calibre demo embeds the Ubuntu Mono font."

func recalledStatements(result RecallResult) []string {
	var statements []string
	for _, evidence := range result.Evidence {
		if evidence.Fact != nil {
			statements = append(statements, evidence.Fact.Statement)
		}
	}
	return statements
}

func TestMemoryExactLiveQuotedNameAdmitsTheFactTheFloorDropped(t *testing.T) {
	client := disposableMemoryClient(t).WithEmbedder(axisEmbedder{})
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	source := FactSource{RunID: "memory-exact-live", MemoryIDs: []string{"fact-source"}, WriterRole: WriterParent}
	write(t, client, Fact{Subject: "calibre demo", Predicate: "embeds", Object: "Ubuntu Mono font",
		Statement: fontStatement, Source: source}, now)
	write(t, client, Fact{Subject: "slide deck", Predicate: "compares", Object: "monospaced fonts",
		Statement: "The slide deck compares monospaced fonts.", Source: source}, now)
	recall := func(query string) []string {
		result, err := client.RecallMemory(ctx, RecallRequest{IdentityID: "identity-memory-exact-live", Query: query, Limit: 5})
		if err != nil {
			t.Fatalf("RecallMemory(%s): %v", query, err)
		}
		if result.Retrieval.Path != retrievalPathHybrid {
			t.Fatalf("%s: path = %s, want the dense path the floor belongs to", query, result.Retrieval.Path)
		}
		return recalledStatements(result)
	}

	if got := recall("quale font mostra Ubuntu Mono?"); len(got) == 0 || strings.Contains(strings.Join(got, "\n"), fontStatement) {
		t.Fatalf("unquoted recall = %q: the floor kept the font, so this memory does not reproduce the burial", got)
	}
	if got := recall(`quale font mostra "Ubuntu Mono"?`); len(got) == 0 || got[0] != fontStatement {
		t.Fatalf("quoted recall = %q, want the font's fact first", got)
	}
	searched, err := client.SearchFactsHybrid(ctx, `quale font mostra "Ubuntu Mono"?`, 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if searched.RetrievalPath != retrievalPathHybrid || len(searched.Facts) == 0 || searched.Facts[0].Statement != fontStatement {
		t.Fatalf("fact search = %+v, want the font's fact first on the hybrid path", searched)
	}
}
