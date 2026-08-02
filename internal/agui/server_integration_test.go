//go:build db_integration

// Integration tests for the AG-UI server (Slice 8b) against a real Postgres-backed
// conversations.Store (SC1 SSE round-trip + SC3 MESSAGES_SNAPSHOT). Requires a running
// Postgres with the Phase-4 migrations applied:
//
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race -p 1 ./internal/agui -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset (a skipped
// integration test must never pass green). The Runner stays a scripted fake — the
// integration value here is the REAL store round-trip (GET snapshot + 404 chokepoint),
// not the LLM stack.
package agui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// localIdentityID (the seeded single-user identity owning new conversations, pre-seeded by
// the identity migration) is defined as a package const in auth.go (Phase 36) — the former
// duplicate declaration here collided with it in the db_integration build, breaking the
// whole tier's compile; removed on touch so the tests share the one production const.

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(ownerCtx(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := envOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)

	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedConversation creates a conversation owned by the seeded local identity, appends
// the supplied turns, and returns its id (cleaned up at test end).
func seedConversation(t *testing.T, s *conversations.Store, pool *pgxpool.Pool, turns []conversations.AppendTurnParams) string {
	t.Helper()
	ctx := ownerCtx()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := s.Create(ctx, conversations.CreateParams{
		ID: id, IdentityID: localIdentityID, Model: "test-model",
	}); err != nil {
		t.Fatalf("Create conversation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ownerCtx(), "DELETE FROM aura.conversations WHERE id = $1", id)
	})
	for i := range turns {
		turns[i].ConversationID = id
		if err := s.AppendTurn(ctx, turns[i]); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", turns[i].Seq, err)
		}
	}
	return id
}

// TestServer_Integration_SSERoundTrip (SC1): the SSE round-trip against a REAL
// Postgres-backed conversations.Store resolving the thread (a scripted fake Runner
// drives the turn — the DB value is the real Get chokepoint).
func TestServer_Integration_SSERoundTrip(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	tid := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{Seq: 2, Role: llm.RoleUser, Content: "ciao"},
	})

	s := NewServer(&scriptedRunner{events: textTurn("Ciao")}, store, ServerConfig{})
	s.idgen = &fixedIDGen{}
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)

	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	got, data := sseFrames(t, string(raw))
	want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("frame sequence = %v, want %v", got, want)
	}
	if !strings.Contains(data, "Ciao") {
		t.Errorf("TEXT_MESSAGE_CONTENT delta missing: %q", data)
	}
}

// TestServer_Integration_MessagesSnapshot (SC3): GET /threads/<id>/messages returns the
// persisted history (seeded into the real store) as a MESSAGES_SNAPSHOT JSON body.
func TestServer_Integration_MessagesSnapshot(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	tid := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "ciao"},
		{Seq: 2, Role: llm.RoleAssistant, Content: "salve, come posso aiutare?"},
	})

	s := NewServer(&scriptedRunner{}, store, ServerConfig{})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/threads/" + tid + "/messages")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)
	for _, want := range []string{"MESSAGES_SNAPSHOT", "ciao", "salve, come posso aiutare?"} {
		if !strings.Contains(body, want) {
			t.Errorf("snapshot missing %q in %q", want, body)
		}
	}
}

// TestServer_Integration_UnknownThread404: an unknown thread id → 404 against the real
// store (the Get chokepoint, T-12-11).
func TestServer_Integration_UnknownThread404(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	s := NewServer(&scriptedRunner{}, store, ServerConfig{})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)

	missing := uuid.Must(uuid.NewV7()).String()
	resp := postRun(t, srv, runPayload(missing))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
