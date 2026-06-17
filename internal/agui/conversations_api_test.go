//go:build db_integration

// Integration tests for the CHAT-02 conversation REST adapter
// (conversations_api.go) against a REAL Postgres-backed conversations.Store. Each
// route is asserted to call EXACTLY its matching Store method (route→method mapping)
// plus the malformed-id (clean 404, never 500) and not-found edge cases. The thin
// adapter has no business logic, so the integration value is the real store
// round-trip — list/get/search/rename/archive/unarchive/delete + the rot-events read.
//
// Run via (the live stack must be up; derive the DSNs from POSTGRES_PASSWORD):
//
//	go test -tags db_integration ./internal/agui/ -run TestConversationsAPI -race
//
// No-skip-as-green: migratedPool's envOrSkip t.Fatals under $CI when the DSN is
// unset, so a skipped run fails the gate rather than passing green.
package agui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// newConvAPIServer builds an AG-UI server over the real store and returns an
// httptest server serving its Mux (which carries the /api/conversations/ subtree).
func newConvAPIServer(t *testing.T, store ConversationStore) *httptest.Server {
	t.Helper()
	s := NewServer(&scriptedRunner{}, store, ServerConfig{})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

func getJSON(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func doReq(t *testing.T, srv *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// TestConversationsAPI_List asserts GET /api/conversations returns List(ctx,false)
// and ?archived=true returns List(ctx,true) — an archived conversation is absent by
// default and present when requested.
func TestConversationsAPI_List(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	active := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "live one"},
	})
	archived := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "old one"},
	})
	if err := store.UpdateStatus(context.Background(), archived, conversations.StatusArchived); err != nil {
		t.Fatalf("archive seed: %v", err)
	}

	srv := newConvAPIServer(t, store)

	code, body := getJSON(t, srv, "/api/conversations")
	if code != http.StatusOK {
		t.Fatalf("GET /api/conversations status = %d, want 200: %s", code, body)
	}
	if !strings.Contains(body, active) {
		t.Errorf("active conversation %s missing from default list: %s", active, body)
	}
	if strings.Contains(body, archived) {
		t.Errorf("archived conversation %s leaked into the default (active-only) list", archived)
	}

	code, body = getJSON(t, srv, "/api/conversations?archived=true")
	if code != http.StatusOK {
		t.Fatalf("GET ?archived=true status = %d, want 200: %s", code, body)
	}
	if !strings.Contains(body, archived) {
		t.Errorf("archived conversation %s missing from ?archived=true list: %s", archived, body)
	}
}

// TestConversationsAPI_GetAggregates asserts GET /api/conversations/{id} returns the
// single Conversation row including the session-cumulative aggregate fields — the
// D-10 footer reload seed.
func TestConversationsAPI_GetAggregates(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "ciao"},
		{Seq: 2, Role: llm.RoleAssistant, Content: "salve", InputTokens: 11, OutputTokens: 7, CachedTokens: 3, CostUSD: 0.0021},
	})

	srv := newConvAPIServer(t, store)
	code, body := getJSON(t, srv, "/api/conversations/"+id)
	if code != http.StatusOK {
		t.Fatalf("GET /api/conversations/{id} status = %d, want 200: %s", code, body)
	}
	var got conversations.Conversation
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode conversation: %v (%s)", err, body)
	}
	if got.ID != id {
		t.Fatalf("returned id = %q, want %q", got.ID, id)
	}
	if got.TotalInputTokens != 11 || got.TotalOutputTokens != 7 || got.TotalCachedTokens != 3 {
		t.Errorf("aggregates = in:%d out:%d cached:%d, want 11/7/3 (D-10 footer seed)",
			got.TotalInputTokens, got.TotalOutputTokens, got.TotalCachedTokens)
	}
	if got.TotalCostUSD <= 0 {
		t.Errorf("TotalCostUSD = %v, want > 0 (the session-cumulative cost aggregate)", got.TotalCostUSD)
	}
}

// TestConversationsAPI_Search asserts GET /api/conversations/search?q= returns the
// SearchConversationTurns projection (snippet + conversationID), and an empty q is a 400.
func TestConversationsAPI_Search(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	needle := "zephyrine-" + uuid.Must(uuid.NewV7()).String()[:8]
	id := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "tell me about " + needle + " please"},
	})

	srv := newConvAPIServer(t, store)
	code, body := getJSON(t, srv, "/api/conversations/search?q="+needle)
	if code != http.StatusOK {
		t.Fatalf("GET search status = %d, want 200: %s", code, body)
	}
	if !strings.Contains(body, id) {
		t.Errorf("search did not project the matching conversation id %s: %s", id, body)
	}
	if !strings.Contains(body, needle) {
		t.Errorf("search snippet missing the needle %q: %s", needle, body)
	}

	code, body = getJSON(t, srv, "/api/conversations/search?q=")
	if code != http.StatusBadRequest {
		t.Errorf("empty q status = %d, want 400: %s", code, body)
	}
}

// TestConversationsAPI_Rename asserts POST /api/conversations/{id}/rename calls
// Store.Rename and returns 204; an empty title is a 400.
func TestConversationsAPI_Rename(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "x"},
	})

	srv := newConvAPIServer(t, store)
	code, body := doReq(t, srv, http.MethodPost, "/api/conversations/"+id+"/rename", `{"title":"Renamed Thread"}`)
	if code != http.StatusNoContent {
		t.Fatalf("rename status = %d, want 204: %s", code, body)
	}
	got, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get after rename: %v", err)
	}
	if got.Title != "Renamed Thread" {
		t.Errorf("title after rename = %q, want %q (Store.Rename called)", got.Title, "Renamed Thread")
	}

	code, body = doReq(t, srv, http.MethodPost, "/api/conversations/"+id+"/rename", `{"title":""}`)
	if code != http.StatusBadRequest {
		t.Errorf("empty title status = %d, want 400: %s", code, body)
	}
}

// TestConversationsAPI_ArchiveUnarchive asserts the path verb drives UpdateStatus to
// archived / active respectively.
func TestConversationsAPI_ArchiveUnarchive(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "x"},
	})
	srv := newConvAPIServer(t, store)

	code, body := doReq(t, srv, http.MethodPost, "/api/conversations/"+id+"/archive", "")
	if code != http.StatusNoContent {
		t.Fatalf("archive status = %d, want 204: %s", code, body)
	}
	if got, _ := store.Get(context.Background(), id); got.Status != conversations.StatusArchived {
		t.Errorf("status after archive = %q, want archived", got.Status)
	}

	code, body = doReq(t, srv, http.MethodPost, "/api/conversations/"+id+"/unarchive", "")
	if code != http.StatusNoContent {
		t.Fatalf("unarchive status = %d, want 204: %s", code, body)
	}
	if got, _ := store.Get(context.Background(), id); got.Status != conversations.StatusActive {
		t.Errorf("status after unarchive = %q, want active", got.Status)
	}
}

// TestConversationsAPI_Delete asserts DELETE /api/conversations/{id} calls
// Store.Delete and returns 204 (the row is gone afterward).
func TestConversationsAPI_Delete(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "x"},
	})
	srv := newConvAPIServer(t, store)

	code, body := doReq(t, srv, http.MethodDelete, "/api/conversations/"+id, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204: %s", code, body)
	}
	if _, err := store.Get(context.Background(), id); err == nil {
		t.Errorf("conversation %s still exists after DELETE (Store.Delete not called)", id)
	}
}

// TestConversationsAPI_DeleteWithAuditedPause proves real HITL/skill threads can be
// hard-deleted: paused_states cascade away, while skill_audit keeps its historical
// paused_state_token and must not force DELETE /api/conversations/{id} to 500.
func TestConversationsAPI_DeleteWithAuditedPause(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "x"},
	})
	ctx := context.Background()
	token := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.paused_states (token, conversation_id, kind, question, priority, tool_call_id)
		 VALUES ($1, $2, 'approval', 'approve skill?', 0, 'tc')`, token, id); err != nil {
		t.Fatalf("seed pause: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.skill_audit (
			actor_id, skill_name, action, content_hash, approval_source,
			paused_state_token, gate_recommended, gate_taken
		) VALUES (
			'user', 'api-delete-cascade-regression', 'activate', 'sha256:test',
			'ask_user', $1, true, true
		)`, token); err != nil {
		t.Fatalf("seed skill audit: %v", err)
	}

	srv := newConvAPIServer(t, store)
	code, body := doReq(t, srv, http.MethodDelete, "/api/conversations/"+id, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete audited pause status = %d, want 204: %s", code, body)
	}
	var auditCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM aura.skill_audit WHERE paused_state_token=$1", token).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("skill_audit must retain the historical token, got %d rows", auditCount)
	}
}

// TestConversationsAPI_RotEvents asserts GET /api/conversations/{id}/rot-events
// returns the microcompact ladder rows (pairs_dropped) via the thin
// ListContextRotEvents wrapper (D-11). A fresh conversation has no events (empty
// list, 200), and the read uses the existing sqlc query (no new migration).
func TestConversationsAPI_RotEvents(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedConversation(t, store, pool, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "x"},
	})
	// Seed one rot-event row directly so the projection has a row to return.
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO aura.context_rot_events (conversation_id, action, pairs_dropped, tokens_before, tokens_after)
		 VALUES ($1, 'hard_drop_pairs', 4, 9000, 5000)`, id); err != nil {
		t.Fatalf("seed rot event: %v", err)
	}

	srv := newConvAPIServer(t, store)
	code, body := getJSON(t, srv, "/api/conversations/"+id+"/rot-events")
	if code != http.StatusOK {
		t.Fatalf("rot-events status = %d, want 200: %s", code, body)
	}
	var events []conversations.RotEvent
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("decode rot events: %v (%s)", err, body)
	}
	if len(events) != 1 {
		t.Fatalf("rot events len = %d, want 1: %s", len(events), body)
	}
	if events[0].PairsDropped != 4 || events[0].TokensBefore != 9000 || events[0].TokensAfter != 5000 {
		t.Errorf("rot event = %+v, want pairs_dropped=4 before=9000 after=5000", events[0])
	}
}

// TestConversationsAPI_MalformedID asserts every id route returns a clean 404 (never
// a 500) for a non-UUID id — the T-25-02 uuid.Parse-before-store guard.
func TestConversationsAPI_MalformedID(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	srv := newConvAPIServer(t, store)

	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/conversations/not-a-uuid", ""},
		{http.MethodGet, "/api/conversations/not-a-uuid/rot-events", ""},
		{http.MethodPost, "/api/conversations/not-a-uuid/rename", `{"title":"x"}`},
		{http.MethodPost, "/api/conversations/not-a-uuid/archive", ""},
		{http.MethodPost, "/api/conversations/not-a-uuid/unarchive", ""},
		{http.MethodDelete, "/api/conversations/not-a-uuid", ""},
	}
	for _, c := range cases {
		code, body := doReq(t, srv, c.method, c.path, c.body)
		if code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 (clean, never 500): %s", c.method, c.path, code, body)
		}
	}
}

// TestConversationsAPI_NotFound asserts a syntactically-valid but missing id maps
// conversations.ErrConversationNotFound → 404 on the single-conversation GET.
func TestConversationsAPI_NotFound(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	srv := newConvAPIServer(t, store)

	missing := uuid.Must(uuid.NewV7()).String()
	code, body := getJSON(t, srv, "/api/conversations/"+missing)
	if code != http.StatusNotFound {
		t.Errorf("GET missing conversation status = %d, want 404: %s", code, body)
	}
}
