//go:build arcadedb_integration

// Does ArcadeDB run the Cypher Aura already has?
//
// The queries below are copied from internal/documents/indexer.go and
// retrieve.go, not paraphrased. If they run here, the document path ports by
// swapping the client, not by being rewritten -- which is the whole question.
//
// Run: go test -tags arcadedb_integration -run AuraCypher ./internal/arcadedb/
package arcadedb

import (
	"context"
	"strings"
	"testing"
)

// Copied verbatim from internal/documents/indexer.go, minus the properties that
// only exist to carry embeddings.
const auraChunkUpsert = `
MATCH (d:Document {id: $document_id})
UNWIND $chunks AS chunk
MERGE (c:Chunk {id: chunk.id})
SET
  c.document_id = chunk.document_id,
  c.file_name = chunk.file_name,
  c.chunk_index = chunk.chunk_index,
  c.text = chunk.text,
  c.active = chunk.active
MERGE (d)-[:HAS_CHUNK]->(c)
WITH count(c) AS chunks
RETURN chunks
`

const auraNextChunkUpsert = `
UNWIND $pairs AS pair
MATCH (a:Chunk {id: pair.prev})
MATCH (b:Chunk {id: pair.next})
MERGE (a)-[:NEXT_CHUNK]->(b)
WITH count(*) AS chunks
RETURN chunks
`

// Copied from internal/documents/retrieve.go's neighborExpandQuery.
const auraNeighborExpand = `
MATCH (c:Chunk) WHERE c.id IN $winner_ids
MATCH (c)-[:NEXT_CHUNK]-(n:Chunk)
WHERE NOT n.id IN $winner_ids
  AND coalesce(n.active, true) = true
WITH DISTINCT n
RETURN
  n.document_id AS document_id,
  n.id AS chunk_id,
  n.text AS text
`

func auraFixture(t *testing.T) (*Client, string) {
	t.Helper()
	client := integrationClient(t)
	ctx := context.Background()
	// ArcadeDB will not index a property that does not exist yet; Neo4j has no
	// such requirement, and it is the one schema difference this port hits.
	for _, ddl := range []string{
		"CREATE VERTEX TYPE Document IF NOT EXISTS",
		"CREATE PROPERTY Document.id IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON Document (id) UNIQUE",
		"CREATE VERTEX TYPE Chunk IF NOT EXISTS",
		"CREATE PROPERTY Chunk.id IF NOT EXISTS STRING",
		"CREATE PROPERTY Chunk.text IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON Chunk (id) UNIQUE",
		"CREATE EDGE TYPE HAS_CHUNK IF NOT EXISTS",
		"CREATE EDGE TYPE NEXT_CHUNK IF NOT EXISTS",
		"CREATE INDEX IF NOT EXISTS ON Chunk (text) FULL_TEXT",
	} {
		if _, err := client.Command(ctx, ddl, nil); err != nil {
			t.Fatalf("ddl %q: %v", ddl, err)
		}
	}
	docID := "doc_" + strings.ReplaceAll(t.Name(), "/", "_")
	cleanup := func() {
		_, _ = client.Command(ctx, "DELETE FROM Chunk WHERE document_id = :d",
			map[string]any{"d": docID})
		_, _ = client.Command(ctx, "DELETE FROM Document WHERE id = :d",
			map[string]any{"d": docID})
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := client.Write(ctx, "MERGE (d:Document {id: $id}) RETURN d.id AS id",
		map[string]any{"id": docID}); err != nil {
		t.Fatalf("create document: %v", err)
	}
	return client, docID
}

func TestAuraCypherWritesChunksInOneStatement(t *testing.T) {
	client, docID := auraFixture(t)
	chunks := make([]any, 0, 5)
	for i := range 5 {
		chunks = append(chunks, map[string]any{
			"id":          docID + "#" + string(rune('a'+i)),
			"document_id": docID,
			"file_name":   "probe.md",
			"chunk_index": i,
			"text":        []string{"alpha", "bravo", "charlie", "delta", "echo"}[i],
			"active":      true,
		})
	}
	rows, err := client.Write(context.Background(), auraChunkUpsert, map[string]any{
		"document_id": docID, "chunks": chunks,
	})
	if err != nil {
		t.Fatalf("chunkUpsertQuery: %v", err)
	}
	if len(rows) != 1 || rowInt(rows[0], "chunks") != 5 {
		t.Fatalf("rows = %+v, want one row reporting 5 chunks", rows)
	}
}

// Re-ingesting the same document must not duplicate: MERGE is what makes that
// true, and it has to behave the same here as it does on Neo4j.
func TestAuraCypherUpsertIsIdempotent(t *testing.T) {
	client, docID := auraFixture(t)
	chunk := []any{map[string]any{
		"id": docID + "#0", "document_id": docID, "file_name": "probe.md",
		"chunk_index": 0, "text": "only once", "active": true,
	}}
	ctx := context.Background()
	for range 2 {
		if _, err := client.Write(ctx, auraChunkUpsert,
			map[string]any{"document_id": docID, "chunks": chunk}); err != nil {
			t.Fatalf("chunkUpsertQuery: %v", err)
		}
	}
	rows, err := client.Query(ctx,
		"SELECT count(*) AS n FROM Chunk WHERE document_id = :d", map[string]any{"d": docID})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if rowInt(rows[0], "n") != 1 {
		t.Fatalf("chunks = %d after two ingests, want 1", rowInt(rows[0], "n"))
	}
}

func TestAuraCypherChainsAndExpandsNeighbours(t *testing.T) {
	client, docID := auraFixture(t)
	ctx := context.Background()
	ids := []string{docID + "#0", docID + "#1", docID + "#2"}
	chunks := []any{}
	for i, id := range ids {
		chunks = append(chunks, map[string]any{
			"id": id, "document_id": docID, "file_name": "probe.md",
			"chunk_index": i, "text": []string{"before", "winner", "after"}[i], "active": true,
		})
	}
	if _, err := client.Write(ctx, auraChunkUpsert,
		map[string]any{"document_id": docID, "chunks": chunks}); err != nil {
		t.Fatalf("chunkUpsertQuery: %v", err)
	}
	pairs := []any{
		map[string]any{"prev": ids[0], "next": ids[1]},
		map[string]any{"prev": ids[1], "next": ids[2]},
	}
	rows, err := client.Write(ctx, auraNextChunkUpsert, map[string]any{"pairs": pairs})
	if err != nil {
		t.Fatalf("nextChunkUpsertQuery: %v", err)
	}
	if rowInt(rows[0], "chunks") != 2 {
		t.Fatalf("chained = %v, want 2", rows[0])
	}

	// The expansion is undirected on purpose: a hit needs the passage before it
	// as much as the one after.
	got, err := client.Read(ctx, auraNeighborExpand, map[string]any{"winner_ids": []string{ids[1]}})
	if err != nil {
		t.Fatalf("neighborExpandQuery: %v", err)
	}
	texts := map[string]bool{}
	for _, row := range got {
		texts[rowString(row, "text")] = true
	}
	if !texts["before"] || !texts["after"] {
		t.Fatalf("neighbours = %+v, want both sides", got)
	}
	if texts["winner"] {
		t.Fatal("the winner must be excluded from its own expansion")
	}
}

// SEARCH_INDEX is the one substitution the port needs: Neo4j's
// db.index.fulltext.queryNodes has no ArcadeDB equivalent by that name.
func TestArcadeDBFullTextReplacesTheNeo4jFulltextCall(t *testing.T) {
	client, docID := auraFixture(t)
	ctx := context.Background()
	chunks := []any{map[string]any{
		"id": docID + "#0", "document_id": docID, "file_name": "probe.md",
		"chunk_index": 0, "text": "the tightening torque is 2 Nm", "active": true,
	}}
	if _, err := client.Write(ctx, auraChunkUpsert,
		map[string]any{"document_id": docID, "chunks": chunks}); err != nil {
		t.Fatalf("chunkUpsertQuery: %v", err)
	}
	rows, err := client.Query(ctx,
		"SELECT id, text FROM Chunk WHERE SEARCH_INDEX('Chunk[text]', :q) AND document_id = :d",
		map[string]any{"q": "torque", "d": docID})
	if err != nil {
		t.Fatalf("SEARCH_INDEX: %v", err)
	}
	if len(rows) != 1 || !strings.Contains(rowString(rows[0], "text"), "torque") {
		t.Fatalf("rows = %+v", rows)
	}
}
