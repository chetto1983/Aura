// Package reasoningstore persists oracle-labeled reasoning-tier examples in
// Neo4j (the existing vector store) and loads them for the embedding classifier
// to fold into its centroids — the self-improvement substrate validated in
// spike 053. It rides the existing mcp-neo4j-cypher client (knowledge.Client);
// no new migration: a `:ReasoningExample` node is created on first MERGE.
package reasoningstore

import (
	"context"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/neostore"
)

// Store reads/writes :ReasoningExample nodes. The content hash is the MERGE key,
// so re-labeling the same text is idempotent (free dedup). The Cypher seam and the
// hash/column coercers are the canonical neostore leaf (QUAL-03).
type Store struct {
	Client neostore.GraphClient
}

const (
	// The mcp-neo4j-cypher read tool returns NULL for list-valued columns
	// (scalars and indexed elements come back fine, full lists do not), so we
	// serialize the embedding to a JSON string with APOC and parse it in Go.
	loadQuery = `MATCH (e:ReasoningExample) WHERE e.embedding IS NOT NULL
RETURN e.tier AS tier, apoc.convert.toJson(e.embedding) AS embedding`
	// A top-level list param ($embedding) is dropped by the mcp-neo4j-cypher write
	// tool; the proven path (documents.Indexer) nests the embedding in an UNWIND'd
	// map, so we mirror that exactly with a single-row list.
	saveQuery = `UNWIND $rows AS row
MERGE (e:ReasoningExample {hash: row.hash})
SET e.tier = row.tier, e.embedding = row.embedding, e.text = row.text, e.source = row.source`
)

// LoadExamples returns every stored example as a prompt.LabeledVec. Implements
// prompt.ExampleStore.
func (s *Store) LoadExamples(ctx context.Context) ([]prompt.LabeledVec, error) {
	rows, err := s.Client.Read(ctx, loadQuery, nil)
	if err != nil {
		return nil, err
	}
	out := make([]prompt.LabeledVec, 0, len(rows))
	for _, row := range rows {
		tier := prompt.ReasoningTier(neostore.AsString(row["tier"]))
		if !tier.Valid() {
			continue
		}
		vec := neostore.AsFloats(row["embedding"])
		if len(vec) == 0 {
			continue
		}
		out = append(out, prompt.LabeledVec{Tier: tier, Vec: vec})
	}
	return out, nil
}

// Save upserts one oracle-labeled example keyed by the text's content hash.
func (s *Store) Save(ctx context.Context, text string, vec []float64, tier prompt.ReasoningTier) error {
	_, err := s.Client.Write(ctx, saveQuery, map[string]any{
		"rows": []map[string]any{{
			"hash":      neostore.HashText(text),
			"tier":      string(tier),
			"embedding": vec,
			"text":      text,
			"source":    "oracle",
		}},
	})
	return err
}
