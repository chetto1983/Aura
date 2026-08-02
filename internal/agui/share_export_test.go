//go:build db_integration

// Integration tests for the WEBSHARE-01 identity-scoped conversation export
// endpoint (share_export.go) against a REAL Postgres-backed
// conversations.Store. There is no other dependency here: no object store, no
// embedded UI, no channel bridge — the asset side is a nil-safe in-package
// fake (fakeAssetService, assets_api_test.go) since these tests exercise the
// conversation-history redaction path, not artifact bundling.
//
// Requires a running Postgres with the migrations applied:
//
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race -p 1 -count=1 -run 'TestShareExport' ./internal/agui/
//
// No-skip-as-green: migratedPool's envOrSkip (server_integration_test.go)
// t.Fatals under $CI when the DSN is unset, so a skipped run fails the gate
// rather than passing green.
package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/share"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedShareExportIdentity inserts a fresh, non-wildcard identity (R-13) named after the
// running test — parallel/serial runs never collide on the aura.identities.name UNIQUE
// constraint (migration 0004), and no owner-vs-foreign assertion here can pass vacuously
// the way it would against the seeded `local` operator. Its ON DELETE CASCADE
// (0004/0005) means t.Cleanup deleting the identity also removes every conversation it
// owns, so seedExportConversation registers no separate cleanup of its own.
func seedShareExportIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := ownerCtx()
	name := "share-export-" + t.Name() + "-" + uuid.Must(uuid.NewV7()).String()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES (gen_random_uuid(), $1, 'user') RETURNING id::text`,
		name,
	).Scan(&id); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ownerCtx(), `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

// seedExportConversation creates a conversation owned by identityID, sets its title, and
// appends turns in order — returning its id.
func seedExportConversation(t *testing.T, store *conversations.Store, identityID, title string, turns []conversations.AppendTurnParams) string {
	t.Helper()
	// Scoped to identityID, NOT to ownerCtx's seeded local identity: these fixtures belong to
	// a fresh per-test owner, and since migration 0089 a store call reads and writes as
	// whoever is on the context.
	ctx := identityctx.WithIdentityID(context.Background(), identityID)
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := store.Create(ctx, conversations.CreateParams{ID: id, IdentityID: identityID, Model: "test-model"}); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if title != "" {
		if err := store.SetTitleIfNull(ctx, id, title); err != nil {
			t.Fatalf("set title: %v", err)
		}
	}
	for i := range turns {
		turns[i].ConversationID = id
		if err := store.AppendTurn(ctx, turns[i]); err != nil {
			t.Fatalf("append turn seq %d: %v", turns[i].Seq, err)
		}
	}
	return id
}

// newShareExportServer builds a Server over the real store with a no-op asset service
// wired (so the nil-service guard passes and ListForThread returns an empty candidate
// list) — every test below only cares about the redacted conversation history.
func newShareExportServer(t *testing.T, store *conversations.Store) *Server {
	t.Helper()
	s := NewServer(&scriptedRunner{}, store, ServerConfig{})
	s.SetAssetService(&fakeAssetService{})
	return s
}

// exportRequest drives GET /api/conversations/{id}/export?<query> directly through
// Server.Mux (in-process, no real listener — mirroring asset_download_test.go's shape).
// An empty identityID sends no principal at all (the unauthenticated case).
func exportRequest(s *Server, convID, query, identityID string) *httptest.ResponseRecorder {
	path := "/api/conversations/" + convID + "/export"
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if identityID != "" {
		req = withPrincipal(req, identityID)
	}
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

func newTestStore(t *testing.T, pool *pgxpool.Pool) *conversations.Store {
	t.Helper()
	return conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
}

// TestShareExportMarkdown proves the happy path: the owner downloads their own
// conversation as Markdown with the forced, stored-XSS-safe headers and the assistant's
// prose intact.
func TestShareExportMarkdown(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)
	convID := seedExportConversation(t, store, owner, "Weather Report", []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "what's tomorrow's weather?"},
		{Seq: 2, Role: llm.RoleAssistant, Content: "Sunny with a high of 22C tomorrow."},
	})

	s := newShareExportServer(t, store)
	rec := exportRequest(s, convID, "format=md", owner)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment prefix", cd)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want the neutral octet-stream type", ct)
	}
	if ns := rec.Header().Get("X-Content-Type-Options"); ns != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", ns)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Sunny with a high of 22C tomorrow.") {
		t.Errorf("markdown export body missing the assistant prose: %s", body)
	}
}

// TestShareExportJSON proves ?format=json returns a valid share.Snapshot whose turns
// round-trip byte-for-byte.
func TestShareExportJSON(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)
	convID := seedExportConversation(t, store, owner, "Round Trip", []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "hello"},
		{Seq: 2, Role: llm.RoleAssistant, Content: "hi there"},
	})

	s := newShareExportServer(t, store)
	rec := exportRequest(s, convID, "format=json", owner)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var snap share.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("unmarshal JSON export: %v; body: %s", err, rec.Body.String())
	}
	if snap.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", snap.SchemaVersion)
	}
	if len(snap.Turns) != 2 {
		t.Fatalf("turns = %d, want 2: %+v", len(snap.Turns), snap.Turns)
	}
	if snap.Turns[0].Text != "hello" || snap.Turns[1].Text != "hi there" {
		t.Errorf("turns did not round-trip: %+v", snap.Turns)
	}
}

// TestShareExportForeignConversation404 is SC4 row 1: a second identity requesting the
// first's conversation gets 404, and the 404 body must not leak the title (a title leak
// would make the 404 an existence oracle).
func TestShareExportForeignConversation404(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)
	other := seedShareExportIdentity(t, pool)
	const secretTitle = "Owner-Only Secret Plan"
	convID := seedExportConversation(t, store, owner, secretTitle, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "top secret"},
	})

	s := newShareExportServer(t, store)
	rec := exportRequest(s, convID, "", other)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign export status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secretTitle) {
		t.Errorf("404 body leaked the foreign conversation's title (existence oracle): %s", rec.Body.String())
	}
}

// TestShareExportFormatFallback proves an absent format and a garbage format both
// degrade to the Markdown body — never a 400 or 500 on an optional query value.
func TestShareExportFormatFallback(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)
	convID := seedExportConversation(t, store, owner, "Fallback Check", []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "ping"},
	})
	s := newShareExportServer(t, store)

	for _, query := range []string{"", "format=bogus"} {
		rec := exportRequest(s, convID, query, owner)
		if rec.Code != http.StatusOK {
			t.Fatalf("query %q status = %d, want 200: %s", query, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if strings.HasPrefix(strings.TrimSpace(body), "{") {
			t.Errorf("query %q looks like a JSON body, want the Markdown fallback: %s", query, body)
		}
		if !strings.HasPrefix(body, "# ") {
			t.Errorf("query %q body does not look like Markdown (want a leading '# ' heading): %s", query, body)
		}
	}
}

// TestShareExportRedactsHostPaths is SC3 asserted end-to-end through the HTTP surface:
// a send_file tool call's staged path and a tool-result's shell output both carry host
// paths that must never reach either exported format's bytes.
func TestShareExportRedactsHostPaths(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)

	toolCall := llm.ToolCall{ID: "tc1", Type: "function"}
	toolCall.Function.Name = "send_file"
	toolCall.Function.Arguments = `{"path":"/abs/secret.xlsx"}`
	toolCalls, err := json.Marshal([]llm.ToolCall{toolCall})
	if err != nil {
		t.Fatalf("marshal tool call: %v", err)
	}

	convID := seedExportConversation(t, store, owner, "Redaction Proof", []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "send me the spreadsheet"},
		{Seq: 2, Role: llm.RoleAssistant, Content: "Sending it now.", ToolCalls: toolCalls},
		{Seq: 3, Role: llm.RoleTool, Content: "cat /etc/passwd\nroot:x:0:0:root:/root:/bin/bash", ToolCallID: "tc1"},
	})

	s := newShareExportServer(t, store)

	for _, query := range []string{"format=md", "format=json"} {
		rec := exportRequest(s, convID, query, owner)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200: %s", query, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if strings.Contains(body, "/abs/") {
			t.Errorf("%s export leaked the staged host path under /abs/: %s", query, body)
		}
		if strings.Contains(body, "/etc/") {
			t.Errorf("%s export leaked the tool-result host path under /etc/: %s", query, body)
		}
	}
}

// TestShareExportUnauthenticated proves a request with no principal at all is rejected
// 401 by the handler's own gate — never 200, never 404 (which would imply the id was
// evaluated without an authenticated caller).
func TestShareExportUnauthenticated(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)
	convID := seedExportConversation(t, store, owner, "No Peeking", []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "hello"},
	})

	s := newShareExportServer(t, store)
	rec := exportRequest(s, convID, "", "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated export status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}
