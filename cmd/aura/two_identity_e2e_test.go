//go:build db_integration && neo4j_integration && garage_integration && authula_integration && musr_e2e

// Phase 36 D-29 / MUSR-01 two-identity cross-deny acceptance E2E — the phase keystone.
// It runs with AURA_MUSR_ISOLATION=on (the post-flip enforcement state) against the FULL
// live stack (Postgres + RLS, Neo4j ownership edges, Garage Admin API v2 + S3, embedded
// Authula) and asserts that identity B is denied on EVERY plane while A keeps its data:
//
//	Postgres conversations — B gets 404 on an HTTP read of A's thread; the owner-scoped
//	  store returns not-found (404 source) / rows==0 (403 source); the RLS kernel backstop
//	  hides A's rows from a raw read under B's identity var.
//	Approvals            — B cannot read A's pause (owner-scoped + RLS).
//	Documents            — B's scoped document_search is empty; A finds its own doc.
//	Garage               — A's object is unreadable with B's scoped key; the per-identity
//	  resolver selects B's creds for B and A's for A (request-time selection, not just provisioning).
//	MUSR-02              — a B-created conversation is owned by B and runs.
//
// The provision→login→isolated-run + break-glass happy path is TestProvisionLoginIsolatedRun.
//
// NO-SKIP-AS-GREEN: every env read goes through musrEnvOrSkip (t.Fatal under $CI). This is
// the acceptance gate — it MUST be green on Linux CI (ci.yml musr-e2e job).
package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
)

const musrIdentityHeader = "X-Musr-Test-Identity"

func TestTwoIdentityCrossDeny(t *testing.T) {
	// Guard the flag first: the acceptance gate runs under the post-flip enforcement state.
	if strings.ToLower(musrEnvOrSkip(t, "AURA_MUSR_ISOLATION")) != "true" {
		t.Fatal("TestTwoIdentityCrossDeny must run with AURA_MUSR_ISOLATION=true (post-flip enforcement)")
	}
	pool := musrMigratedPool(t)
	idStore := identity.New(pool)
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})

	idA := musrProvisionIdentity(t, pool, "alice")
	idB := musrProvisionIdentity(t, pool, "bob")

	ctx := context.Background()

	// A seeds a conversation with one turn (owned by A).
	convA := uuid.Must(uuid.NewV7()).String()
	if _, err := convStore.Create(ctx, conversations.CreateParams{ID: convA, IdentityID: idA, Model: "musr-e2e"}); err != nil {
		t.Fatalf("A create conversation: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.conversations WHERE id=$1`, convA) })
	if err := convStore.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convA, Seq: 1, Role: "system", Content: "you are aura",
	}); err != nil {
		t.Fatalf("A append turn: %v", err)
	}

	// ── Postgres conversation plane: real HTTP 404 read cross-deny ──────────────────
	t.Run("http_read_cross_deny", func(t *testing.T) {
		server := agui.NewServer(musrStubRunner{conv: convStore}, convStore, agui.ServerConfig{})
		deps := agui.AuthDeps{
			SecretConfigured: true,
			LocalIdentityID:  musrLocalIdentity,
			Identities:       identityCheckerAdapter{store: idStore},
			SessionValidator: func(r *http.Request) (string, bool) {
				id := r.Header.Get(musrIdentityHeader)
				return id, id != ""
			},
		}
		srv := httptest.NewServer(agui.RequireAuth(server.Mux(), deps))
		t.Cleanup(srv.Close)

		get := func(identityID string) int {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/threads/"+convA+"/messages", nil)
			req.Header.Set(musrIdentityHeader, identityID)
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("GET messages as %s: %v", identityID, err)
			}
			defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
			return resp.StatusCode
		}
		if code := get(idA); code != http.StatusOK {
			t.Errorf("owner A GET A's thread = %d, want 200", code)
		}
		if code := get(idB); code != http.StatusNotFound {
			t.Errorf("foreign B GET A's thread = %d, want 404 (existence hidden)", code)
		}
	})

	// ── Postgres plane: owner-scoped store gate (404 read / 403 mutate / list) + RLS ──
	t.Run("store_owner_gate_and_rls", func(t *testing.T) {
		if _, err := convStore.GetForIdentity(ctx, convA, idB); err == nil {
			t.Error("B GetForIdentity(A's conv) succeeded, want not-found (404 source)")
		}
		if _, err := convStore.GetForIdentity(ctx, convA, idA); err != nil {
			t.Errorf("A GetForIdentity(own conv) = %v, want the conversation", err)
		}
		rows, err := convStore.DeleteForIdentity(ctx, convA, idB)
		if err != nil {
			t.Fatalf("B DeleteForIdentity: %v", err)
		}
		if rows != 0 {
			t.Errorf("B DeleteForIdentity(A's conv) affected %d rows, want 0 (403 source)", rows)
		}
		list, err := convStore.ListForIdentity(ctx, idB, false)
		if err != nil {
			t.Fatalf("B ListForIdentity: %v", err)
		}
		for _, c := range list {
			if c.ID == convA {
				t.Fatal("B ListForIdentity leaked A's conversation")
			}
		}
		// RLS kernel backstop: even a RAW read under B's identity var sees 0 of A's rows,
		// while A's var sees it — proof isolation holds below the app layer (36-04).
		assertRLSCount(t, pool, idB, convA, 0)
		assertRLSCount(t, pool, idA, convA, 1)
	})

	// ── MUSR-02: a B-created conversation is owned by B and runs ─────────────────────
	t.Run("musr02_b_creates_owns_runs", func(t *testing.T) {
		run := musrStubRunner{conv: convStore}
		ctxB := identityctx.WithIdentityID(ctx, idB)
		convB, err := run.NewConversation(ctxB)
		if err != nil {
			t.Fatalf("B NewConversation: %v", err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.conversations WHERE id=$1`, convB) })

		got, err := convStore.GetForIdentity(ctx, convB, idB)
		if err != nil || got.IdentityID != idB {
			t.Fatalf("B-created conversation owner = %q (err %v), want %q", got.IdentityID, err, idB)
		}
		if _, err := convStore.GetForIdentity(ctx, convB, idA); err == nil {
			t.Error("A can read B's conversation, want not-found (cross-deny)")
		}
		// "runs": the turn plumbing drives B's conversation to a terminal event.
		msg := "hello"
		turns := 0
		for _, err := range run.Turn(ctxB, convB, &msg) {
			if err != nil {
				t.Fatalf("B turn error: %v", err)
			}
			turns++
		}
		if turns == 0 {
			t.Error("B-created conversation produced no turn events (did not run)")
		}
	})

	// ── Approvals plane: B cannot read A's pause ─────────────────────────────────────
	t.Run("approvals_cross_deny", func(t *testing.T) {
		aq := askuser.New(pool)
		token := uuid.NewString()
		// The BEFORE INSERT trigger (36-04) stamps identity_id from convA's owner (A).
		if err := aq.Insert(ctx, askuser.InsertParams{
			Token: token, ConversationID: convA, Kind: "approval",
			Question: "approve deploy?", ToolCallID: "call-musr-1",
		}); err != nil {
			t.Fatalf("seed A pause: %v", err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.paused_states WHERE token=$1`, token) })

		if _, err := aq.GetByTokenForIdentity(ctx, token, idB); err == nil {
			t.Error("B read A's pause, want not-found (approvals cross-deny)")
		}
		if _, err := aq.GetByTokenForIdentity(ctx, token, idA); err != nil {
			t.Errorf("A read own pause = %v, want the pause", err)
		}
	})

	// ── Documents plane: flag-on scoped search — B empty, A finds own ────────────────
	t.Run("documents_cross_deny", func(t *testing.T) {
		cfg := config.LoadDB()
		mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
		if err != nil {
			t.Fatalf("knowledge.Open: %v", err)
		}
		docID := "musr-doc-" + uuid.NewString()
		term := "quetzal" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
		// Delete the test's Neo4j nodes, THEN close — a t.Cleanup runs AFTER the deferred
		// Close (cleanup callbacks fire after deferred calls), so it would Write to a closed
		// client (nil stdin) and SIGSEGV. One defer keeps deletes-before-close ordered.
		defer func() {
			c := context.Background()
			_, _ = mcp.Write(c, "MATCH (d:Document {id:$id}) OPTIONAL MATCH (d)-[:HAS_CHUNK]->(c) DETACH DELETE d,c", map[string]any{"id": docID})
			_, _ = mcp.Write(c, "MATCH (u:User {identifier:$id}) DETACH DELETE u", map[string]any{"id": idA})
			_ = mcp.Close()
		}()
		doc := documents.ExtractedDocument{
			ID: docID, SourceID: "src-" + docID, SourceKind: "musr", FileName: "a.txt",
			MIMEType: "text/plain", ContentHash: "h-" + docID, Title: "A private", IdentityID: idA,
			Chunks: []documents.Chunk{{
				ID: docID + "-c0", DocumentID: docID, SourceID: "src-" + docID,
				ContentHash: "h-" + docID, ChunkHash: "ch-" + docID, ChunkIndex: 0, ChunkCount: 1,
				Kind: "text", Text: "Confidential: the " + term + " protocol is A's alone.",
			}},
		}
		if _, err := (&documents.Indexer{Client: mcp}).UpsertSparse(ctx, doc); err != nil {
			t.Fatalf("A ingest doc: %v", err)
		}
		scoped := &documents.Searcher{Client: mcp, MUSRIsolation: true}
		if hits, err := scoped.Search(ctx, documents.SearchRequest{Query: term, IdentityID: idB, Limit: 5}); err != nil {
			t.Fatalf("B document_search: %v", err)
		} else if len(hits) != 0 {
			t.Errorf("B document_search returned %d hits, want 0 (empty for foreign identity)", len(hits))
		}
		if hits, err := scoped.Search(ctx, documents.SearchRequest{Query: term, IdentityID: idA, Limit: 5}); err != nil {
			t.Fatalf("A document_search: %v", err)
		} else if len(hits) == 0 || hits[0].DocumentID != docID {
			t.Errorf("A document_search = %d hits, want its own doc %s", len(hits), docID)
		}
	})

	// ── Garage object plane: per-identity buckets + request-time credential selection ─
	t.Run("garage_cross_deny", func(t *testing.T) {
		endpoint := musrEnvOrSkip(t, "AURA_GARAGE_ADMIN_ENDPOINT")
		adminToken := musrEnvOrSkip(t, "AURA_GARAGE_ADMIN_TOKEN")
		s3Endpoint := musrEnvOrSkip(t, "AURA_OBJECTSTORE_ENDPOINT")
		region := musrEnvOrSkip(t, "AURA_OBJECTSTORE_REGION")
		authulaSecret := musrEnvOrSkip(t, "AURA_AUTHULA_SECRET")

		admin, err := garageadmin.New(endpoint, adminToken)
		if err != nil {
			t.Fatalf("garageadmin.New: %v", err)
		}
		gA := musrProvisionGarage(t, admin, idA)
		gB := musrProvisionGarage(t, admin, idB)

		newS3 := func(g musrGarageIdentity) *objectstore.S3Store {
			s3, err := objectstore.NewS3(ctx, objectstore.S3Config{
				Endpoint: s3Endpoint, Region: region, AccessKey: g.AccessKey, SecretKey: g.SecretKey, PathStyle: true,
			})
			if err != nil {
				t.Fatalf("NewS3: %v", err)
			}
			return s3
		}
		s3A, s3B := newS3(gA), newS3(gB)
		refA := objectstore.ObjectRef{Bucket: gA.Bucket, Key: "a-secret.txt"}
		if _, err := s3A.Put(ctx, refA, strings.NewReader("A confidential payload"), objectstore.PutOptions{MIMEType: "text/plain"}); err != nil {
			t.Fatalf("A put object: %v", err)
		}
		// A reads its own object.
		if rc, _, err := s3A.Get(ctx, refA); err != nil {
			t.Errorf("A get own object = %v, want ok", err)
		} else {
			_, _ = io.Copy(io.Discard, rc)
			_ = rc.Close()
		}
		// B's scoped key cannot read A's bucket/object (Garage denies cross-bucket access).
		if rc, _, err := s3B.Get(ctx, refA); err == nil {
			_ = rc.Close()
			t.Error("B read A's Garage object with B's scoped key, want access denied")
		}

		// Request-time credential selection (carry-forward #4): the per-identity resolver
		// selects B's creds for B and A's for A — never the other identity's.
		shared := objectstore.Credentials{Bucket: gA.Bucket, AccessKey: gA.AccessKey, SecretKey: gA.SecretKey}
		resolver, err := objectstore.NewIdentityStore(pool, authulaSecret, shared, musrLocalIdentity)
		if err != nil {
			t.Fatalf("NewIdentityStore: %v", err)
		}
		if err := resolver.Put(ctx, idA, gA.Bucket, gA.AccessKey, gA.SecretKey); err != nil {
			t.Fatalf("persist A creds: %v", err)
		}
		if err := resolver.Put(ctx, idB, gB.Bucket, gB.AccessKey, gB.SecretKey); err != nil {
			t.Fatalf("persist B creds: %v", err)
		}
		credA, err := resolver.Resolve(identityctx.WithIdentityID(ctx, idA))
		if err != nil || credA.Bucket != gA.Bucket {
			t.Fatalf("resolve A creds = %q (err %v), want %q", credA.Bucket, err, gA.Bucket)
		}
		credB, err := resolver.Resolve(identityctx.WithIdentityID(ctx, idB))
		if err != nil || credB.Bucket != gB.Bucket {
			t.Fatalf("resolve B creds = %q (err %v), want %q", credB.Bucket, err, gB.Bucket)
		}
		if credB.Bucket == credA.Bucket || credB.AccessKey == credA.AccessKey {
			t.Fatal("resolver returned A's creds to B (request-time selection leaked)")
		}
	})
}

// TestProvisionLoginIsolatedRun is the MUSR-06 happy path: a freshly provisioned identity
// gets an ISOLATED run (a conversation it owns, invisible to the operator), and the plan-01
// break-glass CLI mints a WORKING reset token for it. The full Authula credential-login leg
// (PasswordService.Hash + session mint) is separately proven live in
// internal/webauth/authula_multiuser_test.go; the authula_integration tag here gates the
// run to the Authula-configured stack (AURA_AUTHULA_SECRET + the 0019 schema present).
func TestProvisionLoginIsolatedRun(t *testing.T) {
	if strings.ToLower(musrEnvOrSkip(t, "AURA_MUSR_ISOLATION")) != "true" {
		t.Fatal("TestProvisionLoginIsolatedRun must run with AURA_MUSR_ISOLATION=true")
	}
	// Assert the Authula-configured stack is present (the authula_integration precondition).
	musrEnvOrSkip(t, "AURA_AUTHULA_SECRET")

	pool := musrMigratedPool(t)
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	idC := musrProvisionIdentity(t, pool, "carol")
	ctx := context.Background()

	// ── Isolated run: the provisioned identity's conversation is owned by it and hidden
	// from the seeded operator (a fresh identity does not inherit the operator's data). ──
	run := musrStubRunner{conv: convStore}
	convC, err := run.NewConversation(identityctx.WithIdentityID(ctx, idC))
	if err != nil {
		t.Fatalf("provisioned identity NewConversation: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.conversations WHERE id=$1`, convC) })

	got, err := convStore.GetForIdentity(ctx, convC, idC)
	if err != nil || got.IdentityID != idC {
		t.Fatalf("isolated-run conversation owner = %q (err %v), want %q", got.IdentityID, err, idC)
	}
	if _, err := convStore.GetForIdentity(ctx, convC, musrLocalIdentity); err == nil {
		t.Error("the operator can read the provisioned identity's conversation, want isolation")
	}

	// ── Break-glass (MUSR-06 / D-16): the host-only mint produces a WORKING reset token. ──
	token, err := mintBreakGlassToken(ctx, pool, idC, passwordResetTestPepper)
	if err != nil {
		t.Fatalf("mintBreakGlassToken: %v", err)
	}
	if token == "" {
		t.Fatal("mintBreakGlassToken returned an empty token")
	}
	gotID, err := resolveResetTokenHash(ctx, sqlc.New(pool), agui.HashLookupToken(token, passwordResetTestPepper))
	if err != nil {
		t.Fatalf("resolve break-glass token: %v", err)
	}
	if gotID != idC {
		t.Fatalf("break-glass token resolves to %q, want the provisioned identity %q", gotID, idC)
	}
}
