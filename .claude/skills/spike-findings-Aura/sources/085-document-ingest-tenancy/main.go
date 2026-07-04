// Spike 085 — 2-identity document-ingest tenancy.
//
// Chains 075/077 (image/doc ingest + document_search) through the 083 tenancy
// planes: A ingests a document, B's document_search must MISS it. Closes the
// follow-up 083 flagged.
//
// Finding it proves: today the documents pipeline is IDENTITY-BLIND at the graph
// + search layer (`internal/documents` :Document/:Chunk carry no owner; SearchRequest
// has no identity field), so an UNSCOPED document_search by B leaks A's chunks.
// The fix mirrors YESTERDAY's memory MCP scoping (commit 9a4ca594): a
// (:User {identifier})-[:HAS_DOCUMENT]->(:Document)-[:HAS_CHUNK]->(:Chunk) ownership
// edge + a _SCOPED search variant. Proven live here with the REAL Indexer/Searcher.
//
// Forensic log: ISO-stamped [CATEGORY] lines, [SUMMARY] verdict, exit 0/1.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func ts() string { return time.Now().UTC().Format(time.RFC3339) }
func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", ts(), cat, fmt.Sprintf(format, a...))
}

var failures int

func check(cat, desc string, ok bool, detail string) {
	if ok {
		logf(cat, "PASS %s — %s", desc, detail)
	} else {
		failures++
		logf(cat, "FAIL %s — %s", desc, detail)
	}
}

// neo4jClient adapts the neo4j Go driver to documents.KnowledgeClient (Read/Write).
type neo4jClient struct{ driver neo4j.DriverWithContext }

func (c *neo4jClient) run(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	sess := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer func() { _ = sess.Close(ctx) }()
	res, err := sess.Run(ctx, query, params)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for res.Next(ctx) {
		out = append(out, res.Record().AsMap())
	}
	return out, res.Err()
}
func (c *neo4jClient) Read(ctx context.Context, q string, p map[string]any) ([]map[string]any, error) {
	return c.run(ctx, q, p)
}
func (c *neo4jClient) Write(ctx context.Context, q string, p map[string]any) ([]map[string]any, error) {
	return c.run(ctx, q, p)
}

const (
	idAlice = "spike085-alice"
	idBob   = "spike085-bob"
	// distinct rare terms so a fulltext hit is unambiguous provenance
	aliceTerm = "quetzalcoatl"
	bobTerm   = "borborygmus"
)

func doc(id, identity, term string) documents.ExtractedDocument {
	return documents.ExtractedDocument{
		ID: id, SourceID: "src-" + id, SourceKind: "spike085", FileName: identity + ".txt",
		MIMEType: "text/plain", ContentHash: "hash-" + id, Title: identity + " doc",
		Chunks: []documents.Chunk{{
			ID: id + "-c0", DocumentID: id, SourceID: "src-" + id,
			ContentHash: "hash-" + id, ChunkHash: "ch-" + id, ChunkIndex: 0, ChunkCount: 1,
			Kind: "text", Text: fmt.Sprintf("Confidential note for %s: the %s protocol is private.", identity, term),
		}},
	}
}

// docScopedSearchQuery is the PROPOSED fix, mirroring yesterday's memory _SCOPED
// pattern (9a4ca594): the fulltext hit must be reachable from the caller's :User via
// the HAS_DOCUMENT ownership edge. Identity-blind chunks become identity-scoped.
const docScopedSearchQuery = `
CALL db.index.fulltext.queryNodes('chunk_text', $query, {limit: $candidate_limit})
YIELD node, score
WHERE EXISTS { (:User {identifier: $user_identifier})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }
  AND coalesce(node.active, true) = true AND node.deleted_at IS NULL
RETURN node.document_id AS document_id, node.id AS chunk_id, score AS score
ORDER BY score DESC LIMIT $limit
`

func countHits(rows []map[string]any) int { return len(rows) }

func main() {
	uri := envDefault("AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687")
	user := envDefault("NEO4J_USER", "neo4j")
	pass := os.Getenv("NEO4J_PASSWORD")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logf("START", "spike 085 — 2-identity document-ingest tenancy (real Indexer/Searcher; leak then fix)")

	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, pass, ""))
	if err != nil {
		logf("FATAL", "driver: %v", err)
		os.Exit(1)
	}
	defer func() { _ = driver.Close(ctx) }()
	kc := &neo4jClient{driver: driver}

	// Clean any prior spike085 residue up front. (os.Exit skips defers, so cleanup
	// is also called explicitly via finish() before every exit.)
	cleanup(ctx, kc)

	idx := &documents.Indexer{Client: kc}
	searcher := &documents.Searcher{Client: kc}

	// --- INGEST as A and B (real Indexer.UpsertSparse) ---
	if _, err := idx.UpsertSparse(ctx, doc("spike085-doc-a", idAlice, aliceTerm)); err != nil {
		check("INGEST", "A ingest", false, err.Error())
	} else {
		check("INGEST", "A ingest (alice doc searchable)", true, "1 chunk")
	}
	if _, err := idx.UpsertSparse(ctx, doc("spike085-doc-b", idBob, bobTerm)); err != nil {
		check("INGEST", "B ingest", false, err.Error())
	} else {
		check("INGEST", "B ingest (bob doc searchable)", true, "1 chunk")
	}

	// --- LEAK (today's reality): unscoped Searcher.Search finds A's chunk ---
	// Searcher.Search has NO identity parameter, so "B's agent" running an unscoped
	// document_search over the shared graph gets A's confidential chunk.
	hits, err := searcher.Search(ctx, documents.SearchRequest{Query: aliceTerm, DocumentID: "", Limit: 5})
	leaks := err == nil && len(hits) > 0 && hits[0].DocumentID == "spike085-doc-a"
	check("LEAK", "unscoped document_search surfaces A's chunk (identity-blind pipeline)", leaks,
		fmt.Sprintf("hits=%d err=%v (this is the GAP: SearchRequest has no identity field)", len(hits), err))

	// --- FIX (yesterday's memory pattern applied to docs): add HAS_DOCUMENT ownership edges ---
	for _, e := range []struct{ uid, docID string }{{idAlice, "spike085-doc-a"}, {idBob, "spike085-doc-b"}} {
		if _, err := kc.Write(ctx, `MERGE (u:User {identifier:$uid}) ON CREATE SET u.id=$uid
WITH u MATCH (d:Document {id:$doc}) MERGE (u)-[:HAS_DOCUMENT]->(d)`, map[string]any{"uid": e.uid, "doc": e.docID}); err != nil {
			check("FIX", "add ownership edge "+e.uid, false, err.Error())
		}
	}

	// Scoped search AS BOB for alice's term → must MISS (isolation).
	bobRows, err := kc.Read(ctx, docScopedSearchQuery, map[string]any{
		"query": aliceTerm, "user_identifier": idBob, "candidate_limit": 15, "limit": 5})
	check("FIX", "B scoped document_search MISSES A's doc", err == nil && countHits(bobRows) == 0,
		fmt.Sprintf("bob hits for %q = %d err=%v", aliceTerm, countHits(bobRows), err))

	// Scoped search AS ALICE for alice's term → must HIT (own data reachable).
	aliceRows, err := kc.Read(ctx, docScopedSearchQuery, map[string]any{
		"query": aliceTerm, "user_identifier": idAlice, "candidate_limit": 15, "limit": 5})
	aliceOK := err == nil && countHits(aliceRows) == 1 && stringOf(aliceRows[0]["document_id"]) == "spike085-doc-a"
	check("FIX", "A scoped document_search FINDS own doc", aliceOK,
		fmt.Sprintf("alice hits for %q = %d err=%v", aliceTerm, countHits(aliceRows), err))

	// Symmetry: bob finds his own, alice misses bob's.
	bobOwn, _ := kc.Read(ctx, docScopedSearchQuery, map[string]any{"query": bobTerm, "user_identifier": idBob, "candidate_limit": 15, "limit": 5})
	aliceMissBob, _ := kc.Read(ctx, docScopedSearchQuery, map[string]any{"query": bobTerm, "user_identifier": idAlice, "candidate_limit": 15, "limit": 5})
	check("FIX", "B finds own doc + A misses B's doc (symmetry)", countHits(bobOwn) == 1 && countHits(aliceMissBob) == 0,
		fmt.Sprintf("bobOwn=%d aliceMissBob=%d", countHits(bobOwn), countHits(aliceMissBob)))

	cleanup(ctx, kc) // explicit — os.Exit skips defers
	if failures == 0 {
		logf("SUMMARY", "VALIDATED — leak reproduced with real Searcher; :User HAS_DOCUMENT scope (yesterday's memory pattern) isolates docs")
		os.Exit(0)
	}
	logf("SUMMARY", "FAILED — %d check(s) failed", failures)
	os.Exit(1)
}

func cleanup(ctx context.Context, kc *neo4jClient) {
	_, _ = kc.Write(ctx, `MATCH (d:Document) WHERE d.id STARTS WITH 'spike085-' OPTIONAL MATCH (d)-[:HAS_CHUNK]->(c) DETACH DELETE d, c`, nil)
	_, _ = kc.Write(ctx, `MATCH (u:User) WHERE u.identifier STARTS WITH 'spike085-' DETACH DELETE u`, nil)
}

func envDefault(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}
func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
