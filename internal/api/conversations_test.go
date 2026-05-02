package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/scheduler"

	_ "modernc.org/sqlite"
)

// newConvTestEnv extends testEnv with an ArchiveStore seeded in a temp DB.
func newConvTestEnv(t *testing.T) (*testEnv, *conversation.ArchiveStore) {
	t.Helper()
	env := newTestEnv(t)
	db := scheduler.NewTestDB(t)
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

func TestHandleConversationList_MissingChatID(t *testing.T) {
	router := NewRouter(Deps{Archive: nil})
	req := httptest.NewRequest("GET", "/conversations", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
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
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
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
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatal(err)
	}
	toolCallsJSON := `[{"id":"tc1","type":"function","function":{"name":"search_wiki","arguments":"{}"}}]`
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
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
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
