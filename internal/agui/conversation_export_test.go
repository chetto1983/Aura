//go:build db_integration

// Integration tests for the owner's raw conversation export (conversation_export.go,
// prd.md §7) against a REAL Postgres-backed conversations.Store; the asset side is the
// in-package fakeAssetService (assets_api_test.go). The identity and conversation seed
// helpers here are shared with the share_* tests of this package.
//
// Run against a THROWAWAY database (never the live `aura`):
//
//	go test -tags db_integration -race -p 1 -count=1 -run 'TestConversationExport' ./internal/agui/
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

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
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

// newExportServer builds a Server over the real store whose asset service lists
// threadAssets for every conversation.
func newExportServer(t *testing.T, store *conversations.Store, threadAssets ...assets.Asset) *Server {
	t.Helper()
	s := NewServer(&scriptedRunner{}, store, ServerConfig{})
	s.SetAssetService(&fakeAssetService{listResp: threadAssets})
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

// TestConversationExportMarkdown proves the happy path: the owner downloads their own
// conversation as a .md attachment with the stored-XSS-safe headers and the prose intact.
func TestConversationExportMarkdown(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)
	convID := seedExportConversation(t, store, owner, "Weather Report", []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "what's tomorrow's weather?"},
		{Seq: 2, Role: llm.RoleAssistant, Content: "Sunny with a high of 22C tomorrow."},
	})

	rec := exportRequest(newExportServer(t, store), convID, "", owner)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, `weather-report.md`) {
		t.Errorf("Content-Disposition = %q, want an attachment named weather-report.md", cd)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want the neutral octet-stream type", ct)
	}
	if ns := rec.Header().Get("X-Content-Type-Options"); ns != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", ns)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "# Weather Report\n") || !strings.Contains(body, "Sunny with a high of 22C tomorrow.") {
		t.Errorf("markdown export missing the title heading or the assistant prose: %s", body)
	}
}

// TestConversationExportIsTheRawDump is the 2026-09-24 regression through HTTP: the owner's
// export carries the tool call's arguments, its result and every thread asset, uploads
// included — the very content the redacted snapshot used to drop.
func TestConversationExportIsTheRawDump(t *testing.T) {
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
	convID := seedExportConversation(t, store, owner, "Raw Dump", []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "send me the spreadsheet"},
		{Seq: 2, Role: llm.RoleAssistant, ToolCalls: toolCalls},
		{Seq: 3, Role: llm.RoleTool, Content: "sent /abs/secret.xlsx (5734 bytes)", ToolCallID: "tc1"},
		{Seq: 4, Role: llm.RoleAssistant, Content: "Sent."},
	})
	upload := assets.Asset{ID: "as-web", FileName: "passport.png", MIMEType: "image/png", SizeBytes: 10,
		SourceKind: assets.SourceWeb, Status: assets.StatusComplete}

	rec := exportRequest(newExportServer(t, store, upload), convID, "", owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"**tool call** `send_file` · id `tc1`",
		`"path": "/abs/secret.xlsx"`,
		"· result of `send_file` · id `tc1`",
		"sent /abs/secret.xlsx (5734 bytes)",
		"_(empty content)_",
		"- `passport.png` · image/png · 10 bytes · source web · status complete · id `as-web`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("owner export is missing %q:\n%s", want, body)
		}
	}
}

// TestConversationExportIgnoresTheRetiredFormatParameter: ?format=json once selected the
// share snapshot's JSON. It is gone; any query still yields the Markdown dump.
func TestConversationExportIgnoresTheRetiredFormatParameter(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)
	convID := seedExportConversation(t, store, owner, "Fallback Check", []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "ping"},
	})
	s := newExportServer(t, store)

	for _, query := range []string{"format=json", "format=bogus"} {
		rec := exportRequest(s, convID, query, owner)
		if rec.Code != http.StatusOK {
			t.Fatalf("query %q status = %d, want 200: %s", query, rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); !strings.HasPrefix(body, "# Fallback Check\n") {
			t.Errorf("query %q body is not the Markdown dump: %s", query, body)
		}
	}
}

// TestConversationExportForeignConversation404 is SC4 row 1: a second identity requesting
// the first's conversation gets 404, and the 404 body must not leak the title (a title
// leak would make the 404 an existence oracle).
func TestConversationExportForeignConversation404(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)
	other := seedShareExportIdentity(t, pool)
	const secretTitle = "Owner-Only Secret Plan"
	convID := seedExportConversation(t, store, owner, secretTitle, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "top secret"},
	})

	rec := exportRequest(newExportServer(t, store), convID, "", other)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign export status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secretTitle) {
		t.Errorf("404 body leaked the foreign conversation's title (existence oracle): %s", rec.Body.String())
	}
}

// TestConversationExportUnauthenticated proves a request with no principal at all is
// rejected 401 by the handler's own gate — never 200, never 404 (which would imply the id
// was evaluated without an authenticated caller).
func TestConversationExportUnauthenticated(t *testing.T) {
	pool := migratedPool(t)
	store := newTestStore(t, pool)
	owner := seedShareExportIdentity(t, pool)
	convID := seedExportConversation(t, store, owner, "No Peeking", []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "hello"},
	})

	rec := exportRequest(newExportServer(t, store), convID, "", "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated export status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}
