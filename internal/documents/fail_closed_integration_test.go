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

// TestDocumentsFailClosed is the live proof of the Phase-36 MUSR-01 documents-plane fix
// (spike 085): with AURA_MUSR_ISOLATION on, the identity-scoped retrieval queries fail
// closed, and with it off the pre-existing unscoped path is retained (D-13 reversibility).
// It ports the spike-085 harness onto the real Indexer/Searcher/Service against Neo4j:
//
//	seed  — identity A ingests a document via the REAL Indexer.UpsertSparse, which now
//	        MERGEs the (:User {A})-[:HAS_DOCUMENT]->(:Document) ownership edge.
//	flag ON  (1) empty identity -> 0 hits (fails closed, guard AND query predicate)
//	         (2) identity B     -> 0 hits (cross-deny; exercises the real EXISTS predicate)
//	         (3) identity A     -> finds own doc
//	         (4) GraphRAG as B  -> 0 hits (graph plane cross-deny); as A -> finds own
//	flag OFF (5) identity B / empty -> DOES return A's doc (unscoped fallback retained, D-13)
//
// NO-SKIP-AS-GREEN (CLAUDE.md): under $CI an unset NEO4J_PASSWORD is a misconfigured job
// (t.Fatal); locally it skips so a developer without the stack is not blocked. It cleans up
// every node it writes. (The documents package has no goleak TestMain — the sibling
// neo4j_integration tiers do not add one either; the live mcp client is Closed via defer.)
func TestDocumentsFailClosed(t *testing.T) {
	cfg := config.LoadDB()
	if strings.TrimSpace(cfg.Neo4j.Password) == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("neo4j_integration requires NEO4J_PASSWORD under CI — a skipped tier must never pass as green")
		}
		t.Skip("set NEO4J_PASSWORD (+ stack up) to run the documents fail-closed live test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mcp.Close() }()

	// Unique run token so the rare fulltext term matches ONLY this run's document,
	// immune to any residual corpus on the shared live graph.
	runID := fmt.Sprintf("fc-%d", time.Now().UnixNano())
	idA := runID + "-alice"
	idB := runID + "-bob"
	docAID := runID + "-doc-a"
	rareTerm := fmt.Sprintf("quetzalcoatl%d", time.Now().UnixNano())

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

	// --- SEED: A ingests a document via the REAL Indexer (creates the ownership edge). ---
	docA := ExtractedDocument{
		ID:          docAID,
		SourceID:    "src-" + docAID,
		SourceKind:  "fc-test",
		FileName:    "alice.txt",
		MIMEType:    "text/plain",
		ContentHash: "hash-" + docAID,
		Title:       "alice confidential",
		IdentityID:  idA,
		Chunks: []Chunk{{
			ID:          docAID + "-c0",
			DocumentID:  docAID,
			SourceID:    "src-" + docAID,
			ContentHash: "hash-" + docAID,
			ChunkHash:   "ch-" + docAID,
			ChunkIndex:  0,
			ChunkCount:  1,
			Kind:        "text",
			Text:        fmt.Sprintf("Confidential note for alice: the %s protocol is private.", rareTerm),
		}},
	}
	if _, err := (&Indexer{Client: mcp}).UpsertSparse(ctx, docA); err != nil {
		t.Fatalf("seed ingest as A: %v", err)
	}

	scoped := &Searcher{Client: mcp, MUSRIsolation: true}
	unscoped := &Searcher{Client: mcp, MUSRIsolation: false}

	search := func(t *testing.T, s *Searcher, identity string) []SearchHit {
		t.Helper()
		hits, err := s.Search(ctx, SearchRequest{Query: rareTerm, IdentityID: identity, Limit: 5})
		if err != nil {
			t.Fatalf("search (identity=%q): %v", identity, err)
		}
		return hits
	}

	// --- FLAG ON: fail closed on empty + foreign, owner sees own. ---
	t.Run("flag_on_empty_identity_fails_closed", func(t *testing.T) {
		if hits := search(t, scoped, ""); len(hits) != 0 {
			t.Fatalf("empty-identity scoped search returned %d hits, want 0 (fail closed)", len(hits))
		}
		// Also prove the QUERY predicate fails closed independently of the Go guard: run the
		// scoped sparse query directly with an empty identity — the `$identity <> ""` clause
		// must yield 0 rows even when the guard is bypassed.
		rows, err := mcp.Read(ctx, sparseSearchQueryScoped, map[string]any{
			"query": rareTerm, "document_id": "", "identity": "", "limit": 5, "candidate_limit": 15,
		})
		if err != nil {
			t.Fatalf("raw scoped query with empty identity: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("raw scoped query with empty identity returned %d rows, want 0 (query-level fail closed)", len(rows))
		}
	})

	t.Run("flag_on_identity_B_cross_deny", func(t *testing.T) {
		if hits := search(t, scoped, idB); len(hits) != 0 {
			t.Fatalf("B scoped search returned %d hits, want 0 (cross-deny — B must not see A's doc)", len(hits))
		}
	})

	t.Run("flag_on_identity_A_finds_own", func(t *testing.T) {
		hits := search(t, scoped, idA)
		if len(hits) == 0 {
			t.Fatal("A scoped search returned 0 hits, want >=1 (owner must find own doc)")
		}
		if hits[0].DocumentID != docAID {
			t.Fatalf("A top hit document_id = %q, want %q", hits[0].DocumentID, docAID)
		}
	})

	// --- FLAG ON: graph plane (GraphRAG) cross-deny, no reranker/embedder needed. ---
	t.Run("flag_on_graphrag_cross_deny", func(t *testing.T) {
		svc := &Service{Searcher: scoped, Knowledge: mcp, MUSRIsolation: true}
		resB, err := svc.GraphRAG(ctx, SearchRequest{Query: rareTerm, IdentityID: idB, Limit: 5})
		if err != nil {
			t.Fatalf("GraphRAG as B: %v", err)
		}
		if len(resB.Hits) != 0 {
			t.Fatalf("GraphRAG as B returned %d hits, want 0 (graph-plane cross-deny)", len(resB.Hits))
		}
		resA, err := svc.GraphRAG(ctx, SearchRequest{Query: rareTerm, IdentityID: idA, Limit: 5})
		if err != nil {
			t.Fatalf("GraphRAG as A: %v", err)
		}
		if len(resA.Hits) == 0 {
			t.Fatal("GraphRAG as A returned 0 hits, want >=1 (owner must find own doc)")
		}
	})

	// --- FLAG OFF: the retained unscoped path returns A's doc for B / empty (D-13). ---
	t.Run("flag_off_unscoped_fallback_retained", func(t *testing.T) {
		hitsB := search(t, unscoped, idB)
		if len(hitsB) == 0 || hitsB[0].DocumentID != docAID {
			t.Fatalf("unscoped search as B = %d hits (top=%v), want A's doc %q (D-13 reversible fallback)",
				len(hitsB), topDocID(hitsB), docAID)
		}
		hitsEmpty := search(t, unscoped, "")
		if len(hitsEmpty) == 0 || hitsEmpty[0].DocumentID != docAID {
			t.Fatalf("unscoped search with empty identity = %d hits (top=%v), want A's doc %q (operator-sees-all)",
				len(hitsEmpty), topDocID(hitsEmpty), docAID)
		}
	})
}

func topDocID(hits []SearchHit) string {
	if len(hits) == 0 {
		return ""
	}
	return hits[0].DocumentID
}
