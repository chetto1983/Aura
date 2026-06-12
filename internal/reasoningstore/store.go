// Package reasoningstore persists oracle-labeled reasoning-tier examples in
// Neo4j (the existing vector store) and loads them for the embedding classifier
// to fold into its centroids — the self-improvement substrate validated in
// spike 053. It rides the existing mcp-neo4j-cypher client (knowledge.Client);
// no new migration: a `:ReasoningExample` node is created on first MERGE.
package reasoningstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/chetto1983/aura/internal/agent/prompt"
)

// GraphClient is the narrow Cypher seam Store needs. *knowledge.Client satisfies
// it (Read/Write decode rows to []map[string]any).
type GraphClient interface {
	Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
	Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}

// Store reads/writes :ReasoningExample nodes. The content hash is the MERGE key,
// so re-labeling the same text is idempotent (free dedup).
type Store struct {
	Client GraphClient
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
		tier := prompt.ReasoningTier(asString(row["tier"]))
		if !tier.Valid() {
			continue
		}
		vec := asFloats(row["embedding"])
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
			"hash":      hashText(text),
			"tier":      string(tier),
			"embedding": vec,
			"text":      text,
			"source":    "oracle",
		}},
	})
	return err
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// asFloats coerces a Cypher embedding column to []float64. The load query
// returns it as an APOC JSON string ("[-0.02,...]"); a raw []any list (other
// transports / tests) is also accepted.
func asFloats(v any) []float64 {
	switch t := v.(type) {
	case string:
		var out []float64
		if json.Unmarshal([]byte(t), &out) != nil {
			return nil
		}
		return out
	case []any:
		out := make([]float64, 0, len(t))
		for _, x := range t {
			switch n := x.(type) {
			case float64:
				out = append(out, n)
			case int64:
				out = append(out, float64(n))
			case int:
				out = append(out, float64(n))
			default:
				return nil
			}
		}
		return out
	default:
		return nil
	}
}
