// Package toolselectstore persists oracle-confirmed (query-embedding -> tool)
// tool-selection examples in Neo4j (the existing vector store) and loads them for
// the tool_search ranker to fold into its per-tool centroids — the self-improvement
// substrate for D-06/D-07 (spike 057), the SECOND consumer of the activelearn
// mechanism after the reasoning learner. It rides the existing mcp-neo4j-cypher
// client (knowledge.Client); no new migration: a `:ToolSelectionExample` node is
// created on first MERGE (Neo4j is schemaless, mirroring :ReasoningExample).
package toolselectstore

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/neostore"
)

// LabeledVec is one loaded tool-selection example: the confirmed tool name keyed
// to the query embedding that should route to it. The ranker folds Vec into the
// per-tool centroid keyed by Tool (positives-only Rocchio, D-07). Query is carried
// for provenance/debugging; the centroid math uses only Tool + Vec.
type LabeledVec struct {
	Tool  string
	Vec   []float64
	Query string
}

// Store reads/writes :ToolSelectionExample nodes. The content hash is the MERGE
// key, so re-labeling the same query is idempotent (free dedup). The Cypher seam
// and the hash/column coercers are the canonical neostore leaf (QUAL-03).
type Store struct {
	Client neostore.GraphClient
}

const (
	// The mcp-neo4j-cypher read tool returns NULL for list-valued columns (scalars
	// and indexed elements come back fine, full lists do not), so we serialize the
	// embedding to a JSON string with APOC and parse it in Go. MIRRORED VERBATIM from
	// reasoningstore (RESEARCH risk #4): miss this and embeddings vanish silently, no
	// error.
	loadQuery = `MATCH (e:ToolSelectionExample) WHERE e.embedding IS NOT NULL
RETURN e.tool AS tool, apoc.convert.toJson(e.embedding) AS embedding, e.query AS query`
	// A top-level list param ($embedding) is dropped by the mcp-neo4j-cypher write
	// tool; the proven path (documents.Indexer / reasoningstore) nests the embedding in
	// an UNWIND'd map, so we mirror that exactly with a single-row list. MIRRORED
	// VERBATIM from reasoningstore (RESEARCH risk #4).
	saveQuery = `UNWIND $rows AS row
MERGE (e:ToolSelectionExample {hash: row.hash})
SET e.tool = row.tool, e.embedding = row.embedding, e.query = row.query, e.source = row.source`
)

// LoadExamples returns every stored example as a LabeledVec. A row whose tool is
// empty or whose embedding fails to decode is skipped (the same tolerant discipline
// as reasoningstore.LoadExamples).
func (s *Store) LoadExamples(ctx context.Context) ([]LabeledVec, error) {
	rows, err := s.Client.Read(ctx, loadQuery, nil)
	if err != nil {
		return nil, err
	}
	out := make([]LabeledVec, 0, len(rows))
	for _, row := range rows {
		tool := neostore.AsString(row["tool"])
		if tool == "" {
			continue
		}
		vec := neostore.AsFloats(row["embedding"])
		if len(vec) == 0 {
			continue
		}
		out = append(out, LabeledVec{Tool: tool, Vec: vec, Query: neostore.AsString(row["query"])})
	}
	return out, nil
}

// Save upserts one oracle-confirmed example keyed by the query's content hash.
//
// Hash input (RESEARCH risk #10 — idempotency): the MERGE key is sha256(query), NOT
// sha256(query+tool). Re-labeling the SAME query to a (possibly different) tool is
// therefore an idempotent UPSERT of the single node — the latest oracle label wins
// and we never accumulate two contradictory examples for one query. Keying on
// query+tool would instead let a flip-flopping oracle stack both labels, which the
// anti-amplification guards (min-support, boost cap) would then have to untangle; a
// query-keyed UPSERT keeps the bank honest at the source.
func (s *Store) Save(ctx context.Context, query, tool string, vec []float64) error {
	// Reject an empty tool/embedding at the store boundary (WR-04). LoadExamples skips a
	// len(vec)==0 row, so persisting one creates a permanently un-loadable dead node that
	// still consumes the hash-keyed MERGE slot — and because the caller (activelearn)
	// treats a non-error Save as committed and keeps the content hash in its seen-set, the
	// same query is never retried with a valid embedding. Returning an error keeps the
	// write retryable. This is a public seam, so the guard lives here even though the
	// toolselect Observe path also checks upstream (defense-in-depth).
	if tool == "" || len(vec) == 0 {
		return fmt.Errorf("toolselectstore: refusing to save empty tool/embedding (tool=%q, vec_len=%d)", tool, len(vec))
	}
	_, err := s.Client.Write(ctx, saveQuery, map[string]any{
		"rows": []map[string]any{{
			"hash":      neostore.HashText(query),
			"tool":      tool,
			"embedding": vec,
			"query":     query,
			"source":    "oracle",
		}},
	})
	return err
}
