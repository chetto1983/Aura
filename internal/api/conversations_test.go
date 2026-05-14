package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/cron"
)

type fakeConversationArchive struct {
	turns   []conversation.Turn
	deleted int64
}

func (f *fakeConversationArchive) Append(context.Context, conversation.Turn) error { return nil }
func (f *fakeConversationArchive) ListByChat(_ context.Context, chatID int64, limit int) ([]conversation.Turn, error) {
	out := make([]conversation.Turn, 0, len(f.turns))
	for _, turn := range f.turns {
		if turn.ChatID == chatID {
			out = append(out, turn)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeConversationArchive) ListAll(_ context.Context, limit int) ([]conversation.Turn, error) {
	out := append([]conversation.Turn(nil), f.turns...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeConversationArchive) Get(_ context.Context, id int64) (conversation.Turn, error) {
	for _, turn := range f.turns {
		if turn.ID == id {
			return turn, nil
		}
	}
	return conversation.Turn{}, sql.ErrNoRows
}
func (f *fakeConversationArchive) MaxTurnIndex(context.Context, int64) (int64, error) { return -1, nil }
func (f *fakeConversationArchive) Stats(context.Context) (conversation.ArchiveStats, error) {
	return conversation.ArchiveStats{TotalRows: int64(len(f.turns)), DistinctChats: 1}, nil
}
func (f *fakeConversationArchive) DeleteByChat(context.Context, int64) (int64, error) {
	f.deleted++
	return 1, nil
}
func (f *fakeConversationArchive) DeleteOlderThan(context.Context, time.Time) (int64, error) {
	f.deleted++
	return 1, nil
}
func (f *fakeConversationArchive) DeleteAll(context.Context) (int64, error) {
	f.deleted++
	return 1, nil
}

func TestConversationHandlersAcceptArchiveRepositoryInterface(t *testing.T) {
	archive := &fakeConversationArchive{turns: []conversation.Turn{
		{ID: 7, ChatID: 42, UserID: 1, TurnIndex: 1, Role: "user", Content: "boundary turn", CreatedAt: time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)},
	}}
	router := NewRouter(Deps{Archive: archive})

	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{"GET", "/conversations?chat_id=42", http.StatusOK},
		{"GET", "/conversations/7", http.StatusOK},
		{"GET", "/conversations/stats", http.StatusOK},
		{"POST", "/conversations/cleanup?chat_id=42", http.StatusOK},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s %s got %d want %d: %s", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
}

// newConvTestEnv extends testEnv with an ArchiveStore seeded in a temp DB.
func newConvTestEnv(t *testing.T) (*testEnv, *conversation.ArchiveStore) {
	t.Helper()
	env := newTestEnv(t)
	db := cron.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	env.router = NewRouter(Deps{
		Wiki:      env.wiki,
		Sources:   env.sources,
		Scheduler: env.sched,
		Archive:   store,
	})
	return env, store
}

func seedTurn(t *testing.T, store *conversation.ArchiveStore, turn conversation.Turn) {
	t.Helper()
	if err := store.Append(context.Background(), turn); err != nil {
		t.Fatalf("seed turn: %v", err)
	}
}

func TestHandleConversationList_HappyPath(t *testing.T) {
	_, store := newConvTestEnv(t)
	seedTurn(t, store, conversation.Turn{ChatID: 42, UserID: 1, TurnIndex: 0, Role: "user", Content: "hello"})
	seedTurn(t, store, conversation.Turn{ChatID: 42, UserID: 1, TurnIndex: 1, Role: "assistant", Content: "hi"})

	router := NewRouter(Deps{Archive: store})
	req := httptest.NewRequest("GET", "/conversations?chat_id=42&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body []ConversationTurn
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("want 2 turns, got %d", len(body))
	}
}

func TestHandleConversationList_NoChatID_ListsAll(t *testing.T) {
	env, store := newConvTestEnv(t)
	seedTurn(t, store, conversation.Turn{ChatID: 7, UserID: 1, TurnIndex: 0, Role: "user", Content: "from-7"})
	seedTurn(t, store, conversation.Turn{ChatID: 9, UserID: 2, TurnIndex: 0, Role: "user", Content: "from-9"})

	req := httptest.NewRequest("GET", "/conversations", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body []ConversationTurn
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("want 2 turns across all chats, got %d", len(body))
	}
}

func TestHandleConversationList_MalformedChatID(t *testing.T) {
	router := NewRouter(Deps{Archive: nil})
	req := httptest.NewRequest("GET", "/conversations?chat_id=notanint", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for malformed chat_id, got %d", w.Code)
	}
}

func TestHandleConversationList_NilArchive(t *testing.T) {
	router := NewRouter(Deps{Archive: nil})
	req := httptest.NewRequest("GET", "/conversations?chat_id=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with nil archive, got %d", w.Code)
	}
	var body []ConversationTurn
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("want empty array, got %d", len(body))
	}
}

func TestHandleConversationList_Pagination(t *testing.T) {
	db := cron.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		seedTurn(t, store, conversation.Turn{ChatID: 10, UserID: 1, TurnIndex: int64(i), Role: "user", Content: fmt.Sprintf("msg%d", i)})
	}

	router := NewRouter(Deps{Archive: store})
	req := httptest.NewRequest("GET", "/conversations?chat_id=10&limit=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body []ConversationTurn
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("want 2 (limit), got %d", len(body))
	}
}

func TestHandleConversationDetail_HappyPath(t *testing.T) {
	db := cron.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatal(err)
	}
	toolCallsJSON := `[{"id":"tc1","type":"function","function":{"name":"search_files","arguments":"{}"}}]`
	seedTurn(t, store, conversation.Turn{
		ChatID: 5, UserID: 2, TurnIndex: 0,
		Role: "assistant", Content: "", ToolCalls: toolCallsJSON,
	})
	listed, err := store.ListByChat(context.Background(), 5, 1)
	if err != nil || len(listed) == 0 {
		t.Fatalf("setup list: %v", err)
	}
	id := listed[0].ID

	router := NewRouter(Deps{Archive: store})
	req := httptest.NewRequest("GET", fmt.Sprintf("/conversations/%d", id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body ConversationDetail
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ToolCalls != toolCallsJSON {
		t.Fatalf("tool_calls mismatch: %q", body.ToolCalls)
	}
}

func TestHandleConversationDetail_NotFound(t *testing.T) {
	db := cron.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatal(err)
	}

	router := NewRouter(Deps{Archive: store})
	req := httptest.NewRequest("GET", "/conversations/9999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}
