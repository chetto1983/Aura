//go:build neo4j_integration

package documents

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/knowledge"
)

// TestDocumentsBackfill is the live proof of the Phase-36 D-12 owner-edge backfill: it
// attaches the (:User {identifier})-[:HAS_DOCUMENT]->(:Document) edge to EVERY pre-isolation
// document BEFORE the AURA_MUSR_ISOLATION flip, sourcing per-document owners from the
// Postgres map (Op 1) and attaching any still-unowned document to the operator (Op 2). It
// asserts:
//
//	Op 1  — a mapped document gets its mapped owner's edge (idA).
//	Op 2  — an unmapped (CLI/legacy) document is attached to the operator.
//	idem  — a second run creates NO duplicate edges (D-12 re-run safety).
//	behav — with the flag ON, a scoped search as the operator returns the operator's
//	        formerly-orphan document (the flip no longer hides operator docs).
//
// NO-SKIP-AS-GREEN (CLAUDE.md): under $CI an unset NEO4J_PASSWORD is a misconfigured job
// (t.Fatal); locally it skips. It cleans up every node/edge it writes (runID prefix).
func TestDocumentsBackfill(t *testing.T) {
	cfg := config.LoadDB()
	if strings.TrimSpace(cfg.Neo4j.Password) == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("neo4j_integration requires NEO4J_PASSWORD under CI — a skipped tier must never pass as green")
		}
		t.Skip("set NEO4J_PASSWORD (+ stack up) to run the documents backfill live test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mcp.Close() }()

	runID := fmt.Sprintf("bf-%d", time.Now().UnixNano())
	idA := runID + "-idA"
	opID := runID + "-op"
	docMapped := runID + "-doc-mapped"
	docOrphan := runID + "-doc-orphan"
	termMapped := fmt.Sprintf("mappedterm%d", time.Now().UnixNano())
	termOrphan := fmt.Sprintf("orphanterm%d", time.Now().UnixNano())

	cleanup := func() {
		_, _ = mcp.Write(ctx,
			"MATCH (d:Document) WHERE d.id STARTS WITH $p OPTIONAL MATCH (d)-[:HAS_CHUNK]->(c) DETACH DELETE d, c",
			map[string]any{"p": runID})
		_, _ = mcp.Write(ctx,
			"MATCH (u:User) WHERE u.identifier STARTS WITH $p DETACH DELETE u",
			map[string]any{"p": runID})
	}
	cleanup()
	defer cleanup()

	// --- SEED: ingest both documents via the REAL Indexer (real :Document + :Chunk +
	// a temp owner edge), then STRIP every ownership edge to reproduce the pre-36-05 state
	// where documents exist in the graph with NO :User -[:HAS_DOCUMENT]-> edge at all. ---
	indexer := &Indexer{Client: mcp}
	seed := func(docID, term string) {
		doc := ExtractedDocument{
			ID: docID, SourceID: "src-" + docID, SourceKind: "bf-test",
			FileName: docID + ".txt", MIMEType: "text/plain", ContentHash: "hash-" + docID,
			Title: docID, IdentityID: runID + "-seed",
			Chunks: []Chunk{{
				ID: docID + "-c0", DocumentID: docID, SourceID: "src-" + docID,
				ContentHash: "hash-" + docID, ChunkHash: "ch-" + docID,
				ChunkIndex: 0, ChunkCount: 1, Kind: "text",
				Text: fmt.Sprintf("Backfill note: the %s protocol is documented here.", term),
			}},
		}
		if _, err := indexer.UpsertSparse(ctx, doc); err != nil {
			t.Fatalf("seed ingest %s: %v", docID, err)
		}
	}
	seed(docMapped, termMapped)
	seed(docOrphan, termOrphan)
	// Strip the seed owner edges + temp user so both docs are genuinely unowned.
	if _, err := mcp.Write(ctx,
		"MATCH (u:User)-[r:HAS_DOCUMENT]->(d:Document) WHERE d.id STARTS WITH $p DELETE r",
		map[string]any{"p": runID}); err != nil {
		t.Fatalf("strip seed edges: %v", err)
	}
	if _, err := mcp.Write(ctx,
		"MATCH (u:User {identifier: $seed}) DETACH DELETE u", map[string]any{"seed": runID + "-seed"}); err != nil {
		t.Fatalf("drop seed user: %v", err)
	}

	owners := []DocumentOwner{{Identity: idA, DocumentID: docMapped}}
	backfiller := &Backfiller{Graph: mcp, Operator: opID}

	report, err := backfiller.Run(ctx, owners)
	if err != nil {
		t.Fatalf("backfill run 1: %v", err)
	}
	if report.OwnersSourced != 1 {
		t.Fatalf("OwnersSourced = %d, want 1", report.OwnersSourced)
	}

	ownerOf := func(docID string) []string {
		rows, err := mcp.Read(ctx,
			"MATCH (u:User)-[:HAS_DOCUMENT]->(d:Document {id: $id}) RETURN u.identifier AS identifier",
			map[string]any{"id": docID})
		if err != nil {
			t.Fatalf("owner query %s: %v", docID, err)
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, stringValue(r["identifier"]))
		}
		return out
	}

	// Op 1: the mapped document is owned by idA (its Postgres-mapped owner), no one else.
	if got := ownerOf(docMapped); len(got) != 1 || got[0] != idA {
		t.Fatalf("mapped doc owners = %v, want exactly [%s]", got, idA)
	}
	// Op 2: the unmapped document is attached to the operator (the D-12 orphan net).
	if got := ownerOf(docOrphan); len(got) != 1 || got[0] != opID {
		t.Fatalf("orphan doc owners = %v, want exactly [%s] (operator net)", got, opID)
	}

	// Idempotency: a second run creates NO duplicate edges (exactly one owner each).
	if _, err := backfiller.Run(ctx, owners); err != nil {
		t.Fatalf("backfill run 2 (idempotency): %v", err)
	}
	if got := ownerOf(docMapped); len(got) != 1 {
		t.Fatalf("after re-run mapped doc has %d owner edges, want 1 (idempotent MERGE)", len(got))
	}
	if got := ownerOf(docOrphan); len(got) != 1 {
		t.Fatalf("after re-run orphan doc has %d owner edges, want 1 (idempotent MERGE)", len(got))
	}

	// Behavior: with the flag ON, the operator's scoped search now finds the formerly-orphan
	// document (the flip no longer orphans it — D-12), and idA finds its mapped document.
	scoped := &Searcher{Client: mcp, MUSRIsolation: true}
	if hits, err := scoped.Search(ctx, SearchRequest{Query: termOrphan, IdentityID: opID, Limit: 5}); err != nil {
		t.Fatalf("operator scoped search: %v", err)
	} else if len(hits) == 0 || hits[0].DocumentID != docOrphan {
		t.Fatalf("operator scoped search = %d hits (top=%s), want the backfilled orphan %s",
			len(hits), topDocID(hits), docOrphan)
	}
	if hits, err := scoped.Search(ctx, SearchRequest{Query: termMapped, IdentityID: idA, Limit: 5}); err != nil {
		t.Fatalf("idA scoped search: %v", err)
	} else if len(hits) == 0 || hits[0].DocumentID != docMapped {
		t.Fatalf("idA scoped search = %d hits (top=%s), want the mapped doc %s",
			len(hits), topDocID(hits), docMapped)
	}
}
