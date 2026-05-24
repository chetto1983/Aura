package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/api/auth"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
)

type recordingChatService struct {
	calls             int
	userID            string
	threadID          string
	actorID           string
	identityActorID   string
	authorizerPresent bool
	message           string
	reply             ChatReply
}

type recordingChatStreamService struct {
	calls    int
	userID   string
	threadID string
	message  string
}

func (s *recordingChatStreamService) ChatStream(_ context.Context, userID, threadID, message string, w io.Writer, flush func()) error {
	s.calls++
	s.userID = userID
	s.threadID = threadID
	s.message = message
	_, _ = io.WriteString(w, "data: {\"type\":\"start\",\"messageId\":\"msg_test\"}\n\n")
	_, _ = io.WriteString(w, "data: {\"type\":\"text-start\",\"id\":\"text_test\"}\n\n")
	_, _ = io.WriteString(w, "data: {\"type\":\"text-delta\",\"id\":\"text_test\",\"textDelta\":\"ok\",\"delta\":\"ok\"}\n\n")
	_, _ = io.WriteString(w, "data: {\"type\":\"text-end\",\"id\":\"text_test\"}\n\n")
	_, _ = io.WriteString(w, "data: {\"type\":\"finish\",\"finishReason\":\"stop\",\"usage\":{\"inputTokens\":1,\"outputTokens\":1}}\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flush != nil {
		flush()
	}
	return nil
}

func (s *recordingChatService) Chat(ctx context.Context, userID, threadID, message string) (ChatReply, error) {
	s.calls++
	s.userID = userID
	s.threadID = threadID
	s.actorID = auth.ActorIDFromContext(ctx)
	s.identityActorID = identity.ActorIDFromContext(ctx)
	_, s.authorizerPresent = identity.AuthorizerFromContext(ctx)
	s.message = message
	if s.reply.Reply == "" {
		s.reply.Reply = "ok"
	}
	return s.reply, nil
}

func TestChatBearerActorContext(t *testing.T) {
	env := newChatAuthEnv(t, "alice")

	rr := env.doChat(t, env.token, `{"message":"hello"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	if env.chat.calls != 1 {
		t.Fatalf("chat calls = %d, want 1", env.chat.calls)
	}
	if env.chat.userID != "alice" {
		t.Fatalf("chat userID = %q, want alice", env.chat.userID)
	}
	if env.chat.actorID != identity.TelegramSessionActorID("alice") {
		t.Fatalf("chat actorID = %q, want %q", env.chat.actorID, identity.TelegramSessionActorID("alice"))
	}
	if env.chat.identityActorID != identity.TelegramSessionActorID("alice") {
		t.Fatalf("chat identityActorID = %q, want %q", env.chat.identityActorID, identity.TelegramSessionActorID("alice"))
	}
	if !env.chat.authorizerPresent {
		t.Fatal("chat context missing identity authorizer")
	}
	if env.chat.message != "hello" {
		t.Fatalf("chat message = %q, want hello", env.chat.message)
	}

	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM principals WHERE id = ?`, 1, identity.TelegramPrincipalID("alice"))
	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM channel_accounts WHERE id = ?`, 1, identity.TelegramChannelAccountID("alice"))
	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM actors WHERE id = ?`, 1, identity.TelegramSessionActorID("alice"))
	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM capability_grants WHERE id = ?`, 1, identity.TelegramPrincipalGrantID("alice", identity.CapabilityAPIChat))
	assertChatScalar(t, env.db, `
SELECT COUNT(*)
FROM authz_decisions
WHERE actor_id = ? AND capability = ? AND resource_type = ? AND resource_id = ? AND decision = ?
`, 1, identity.TelegramSessionActorID("alice"), identity.CapabilityAPIChat, "api", "chat", identity.DecisionAllow)

	rr = env.doChat(t, env.token, `{"message":"again"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("second status %d, body=%s", rr.Code, rr.Body)
	}
	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM principals WHERE id = ?`, 1, identity.TelegramPrincipalID("alice"))
	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM channel_accounts WHERE id = ?`, 1, identity.TelegramChannelAccountID("alice"))
	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM actors WHERE id = ?`, 1, identity.TelegramSessionActorID("alice"))
	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM capability_grants WHERE id = ?`, 1, identity.TelegramPrincipalGrantID("alice", identity.CapabilityAPIChat))
	assertChatScalar(t, env.db, `
SELECT COUNT(*)
FROM authz_decisions
WHERE actor_id = ? AND capability = ? AND resource_type = ? AND resource_id = ? AND decision = ?
`, 2, identity.TelegramSessionActorID("alice"), identity.CapabilityAPIChat, "api", "chat", identity.DecisionAllow)
}

func TestChatReplyIncludesBudgetWarning(t *testing.T) {
	env := newChatAuthEnv(t, "alice")
	env.chat.reply = ChatReply{Reply: "ok", BudgetWarning: "soft budget hit"}

	rr := env.doChat(t, env.token, `{"message":"hello"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	var body ChatReply
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if body.BudgetWarning != "soft budget hit" {
		t.Fatalf("budget_warning = %q", body.BudgetWarning)
	}
}

func TestChatStreamHeadersAndLegacyBody(t *testing.T) {
	stream := &recordingChatStreamService{}
	router := NewRouter(Deps{ChatStream: stream})

	req := httptest.NewRequest(http.MethodPost, "/chat/stream", strings.NewReader(`{"user_id":"alice","thread_id":"live","message":"ciao"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	if stream.calls != 1 || stream.userID != "alice" || stream.threadID != "live" || stream.message != "ciao" {
		t.Fatalf("stream call = calls=%d user=%q thread=%q message=%q", stream.calls, stream.userID, stream.threadID, stream.message)
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-cache, no-transform" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rr.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q", got)
	}
	if got := rr.Header().Get("x-vercel-ai-ui-message-stream"); got != "v1" {
		t.Fatalf("ui message header = %q", got)
	}
	if !strings.Contains(rr.Body.String(), `"type":"text-delta"`) || !strings.HasSuffix(rr.Body.String(), "data: [DONE]\n\n") {
		t.Fatalf("stream body = %q", rr.Body.String())
	}
}

func TestChatStreamExtractsAssistantUIMessage(t *testing.T) {
	stream := &recordingChatStreamService{}
	router := NewRouter(Deps{ChatStream: stream})
	body := `{
		"threadId":"thread-from-ui",
		"messages":[
			{"role":"assistant","content":"old"},
			{"role":"user","content":[{"type":"text","text":"ciao"},{"type":"text","text":"mondo"}]}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/chat/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	if stream.threadID != "thread-from-ui" {
		t.Fatalf("threadID = %q", stream.threadID)
	}
	if stream.message != "ciao\nmondo" {
		t.Fatalf("message = %q", stream.message)
	}
}

func TestChatStreamUnavailable(t *testing.T) {
	router := NewRouter(Deps{})
	req := httptest.NewRequest(http.MethodPost, "/chat/stream", strings.NewReader(`{"message":"hello"}`))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
}

func TestChatBearerRejectsBodyUserOverride(t *testing.T) {
	env := newChatAuthEnv(t, "alice")

	rr := env.doChat(t, env.token, `{"user_id":"bob","message":"hello"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	if env.chat.calls != 0 {
		t.Fatalf("chat calls = %d, want 0", env.chat.calls)
	}
	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM authz_decisions`, 0)
}

func TestChatBearerMissingGrantDenied(t *testing.T) {
	env := newChatAuthEnv(t, "alice")
	if err := env.identity.RevokeGrant(context.Background(), identity.TelegramPrincipalGrantID("alice", identity.CapabilityAPIChat)); err != nil {
		t.Fatalf("revoke api.chat grant: %v", err)
	}

	rr := env.doChat(t, env.token, `{"message":"hello"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	if env.chat.calls != 0 {
		t.Fatalf("chat calls = %d, want 0", env.chat.calls)
	}
	assertChatScalar(t, env.db, `
SELECT COUNT(*)
FROM authz_decisions
WHERE actor_id = ? AND capability = ? AND resource_type = ? AND resource_id = ? AND decision = ? AND reason = ?
`, 1, identity.TelegramSessionActorID("alice"), identity.CapabilityAPIChat, "api", "chat", identity.DecisionDeny, "missing_grant")
}

func TestChatBearerRequiresTokenBeforeGrant(t *testing.T) {
	env := newChatAuthEnv(t, "alice")

	rr := env.doChat(t, "", `{"message":"hello"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, body=%s", rr.Code, rr.Body)
	}
	if env.chat.calls != 0 {
		t.Fatalf("chat calls = %d, want 0", env.chat.calls)
	}
	assertChatScalar(t, env.db, `SELECT COUNT(*) FROM authz_decisions`, 0)
}

type chatAuthEnv struct {
	db       *sql.DB
	identity *identity.Store
	router   http.Handler
	chat     *recordingChatService
	token    string
}

func newChatAuthEnv(t *testing.T, userID string) *chatAuthEnv {
	t.Helper()
	db, err := auradb.Open(filepath.Join(t.TempDir(), "chat-auth.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	authStore, err := auth.NewStoreWithDB(db)
	if err != nil {
		t.Fatalf("auth store: %v", err)
	}
	identityStore, err := identity.NewStore(db)
	if err != nil {
		t.Fatalf("identity store: %v", err)
	}
	if ok, err := authStore.BootstrapUser(context.Background(), userID); err != nil || !ok {
		t.Fatalf("bootstrap user ok=%v err=%v, want true nil", ok, err)
	}
	token, err := authStore.Issue(context.Background(), userID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	chat := &recordingChatService{}
	router := NewRouter(Deps{
		Auth: authStore,
		Allowlist: func(uid string) bool {
			return uid == userID
		},
		Chat: chat,
	})
	return &chatAuthEnv{
		db:       db,
		identity: identityStore,
		router:   router,
		chat:     chat,
		token:    token,
	}
}

func (e *chatAuthEnv) doChat(t *testing.T, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

// TestChatThreadIDScoping verifies that thread_id from the request body is
// forwarded to ChatService as a distinct parameter, producing distinct
// conversation scopes (web:<userID>:<threadID>) for different thread values.
func TestChatThreadIDScoping(t *testing.T) {
	env := newChatAuthEnv(t, "alice")

	rr := env.doChat(t, env.token, `{"message":"hello","thread_id":"a"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("thread_id=a: status %d, body=%s", rr.Code, rr.Body)
	}
	if env.chat.threadID != "a" {
		t.Fatalf("thread_id=a: threadID = %q, want a", env.chat.threadID)
	}

	rr = env.doChat(t, env.token, `{"message":"hello","thread_id":"b"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("thread_id=b: status %d, body=%s", rr.Code, rr.Body)
	}
	if env.chat.threadID != "b" {
		t.Fatalf("thread_id=b: threadID = %q, want b", env.chat.threadID)
	}
	// The two thread IDs are distinct, so they map to distinct conversation_ids
	// (web:alice:a vs web:alice:b) in the agent_note scratchpad.

	rr = env.doChat(t, env.token, `{"message":"hello"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("no thread_id: status %d, body=%s", rr.Code, rr.Body)
	}
	if env.chat.threadID != "default" {
		t.Fatalf("no thread_id: threadID = %q, want default", env.chat.threadID)
	}
}

func assertChatScalar(t *testing.T, db *sql.DB, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatalf("query scalar: %v\nquery: %s", err, query)
	}
	if got != want {
		b, _ := json.Marshal(args)
		t.Fatalf("scalar = %d, want %d\nquery: %s\nargs: %s", got, want, query, b)
	}
}
