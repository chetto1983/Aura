package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/board"
)

const boardOwner = "33333333-3333-4333-8333-333333333333"

// fakeBoardStore records what each route asked of the store and answers with err when set,
// wrapped the way the real store wraps it.
type fakeBoardStore struct {
	err      error
	identity string
	calls    []string
	added    board.NewCard
	source   board.Source
	actor    board.Actor
	patch    board.CardPatch
	moved    [3]string
	columns  []board.Column
	view     [2]string
	pinned   bool
	removed  bool
	cards    []board.Card
}

func (f *fakeBoardStore) note(identity, call string) error {
	f.identity = identity
	f.calls = append(f.calls, call)
	if f.err != nil {
		return fmt.Errorf("%s: %w", call, f.err)
	}
	return nil
}

func (f *fakeBoardStore) Board(_ context.Context, id string) (board.Board, error) {
	return board.Board{ID: "b1", Name: board.DefaultName, Columns: board.DefaultColumns()}, f.note(id, "board")
}

func (f *fakeBoardStore) SetColumns(_ context.Context, id string, cols []board.Column) (board.Board, error) {
	f.columns = cols
	return board.Board{ID: "b1", Name: board.DefaultName, Columns: cols}, f.note(id, "columns")
}

func (f *fakeBoardStore) Cards(_ context.Context, id string) ([]board.Card, error) {
	return f.cards, f.note(id, "cards")
}

func (f *fakeBoardStore) AddCard(_ context.Context, id string, in board.NewCard, s board.Source, a board.Actor) (board.Card, error) {
	f.added, f.source, f.actor = in, s, a
	return board.Card{ID: "c-new", ColumnID: "todo", Label: in.Label, Source: s, UpdatedBy: a}, f.note(id, "add")
}

func (f *fakeBoardStore) UpdateCard(_ context.Context, id, cardID string, p board.CardPatch, a board.Actor) (board.Card, error) {
	f.patch, f.actor = p, a
	return board.Card{ID: cardID}, f.note(id, "update")
}

func (f *fakeBoardStore) MoveCard(_ context.Context, id, cardID, column, before string, a board.Actor) (board.Card, error) {
	f.moved, f.actor = [3]string{cardID, column, before}, a
	return board.Card{ID: cardID, ColumnID: column}, f.note(id, "move")
}

func (f *fakeBoardStore) DuplicateCard(_ context.Context, id, cardID string) (board.Card, error) {
	return board.Card{ID: "c-copy"}, f.note(id, "duplicate "+cardID)
}

func (f *fakeBoardStore) DeleteCard(_ context.Context, id, cardID string) (bool, error) {
	return f.removed, f.note(id, "delete "+cardID)
}

func (f *fakeBoardStore) Views(_ context.Context, id string) ([]board.View, error) {
	return []board.View{{ID: "v1", Name: "urgent", Filters: json.RawMessage(`{"priority":3}`), Pinned: true}}, f.note(id, "views")
}

func (f *fakeBoardStore) SaveView(_ context.Context, id, name string, filters json.RawMessage, pinned bool) (board.View, error) {
	f.view, f.pinned = [2]string{name, string(filters)}, pinned
	return board.View{ID: "v2", Name: name, Filters: filters, Pinned: pinned}, f.note(id, "save view")
}

func (f *fakeBoardStore) DeleteView(_ context.Context, id, viewID string) (bool, error) {
	return f.removed, f.note(id, "delete view "+viewID)
}

func boardServer(store boardStore) *Server {
	s := NewServer(nil, nil, ServerConfig{})
	if store != nil {
		s.SetBoardStore(store)
	}
	return s
}

func boardRequest(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return policyRequest(t, s, method, path, body, boardOwner)
}

func decodeBoard[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

var boardRoutes = []struct{ method, path, body string }{
	{http.MethodGet, "/api/board", ""},
	{http.MethodPut, "/api/board/columns", `[]`},
	{http.MethodGet, "/api/board/cards", ""},
	{http.MethodPost, "/api/board/cards", `{"label":"x"}`},
	{http.MethodPut, "/api/board/cards/c1", `{}`},
	{http.MethodPut, "/api/board/cards/c1/move", `{}`},
	{http.MethodPost, "/api/board/cards/c1/duplicate", ""},
	{http.MethodDelete, "/api/board/cards/c1", ""},
	{http.MethodGet, "/api/board/views", ""},
	{http.MethodPut, "/api/board/views", `{"name":"x"}`},
	{http.MethodDelete, "/api/board/views/v1", ""},
}

// Every route answers JSON, even unwired: the widget's provider calls res.json() on all of them.
func TestBoardRoutesAnswer503JSONUnwired(t *testing.T) {
	s := boardServer(nil)
	for _, c := range boardRoutes {
		rec := boardRequest(t, s, c.method, c.path, c.body)
		if rec.Code != http.StatusServiceUnavailable || decodeBoard[map[string]string](t, rec)["error"] != "board_unavailable" {
			t.Errorf("%s %s = %d %q, want 503 board_unavailable", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

func TestBoardRoutesActForThePrincipal(t *testing.T) {
	store := &fakeBoardStore{removed: true}
	s := boardServer(store)
	for _, c := range boardRoutes {
		store.identity = ""
		rec := boardRequest(t, s, c.method, c.path, c.body)
		if rec.Code != http.StatusOK {
			t.Errorf("%s %s = %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
		if store.identity != boardOwner {
			t.Errorf("%s %s acted for %q, want the principal", c.method, c.path, store.identity)
		}
	}
}

func TestBoardCardsAreTheWidgetsArray(t *testing.T) {
	due := time.Date(2026, 10, 12, 8, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	store := &fakeBoardStore{}
	s := boardServer(store)
	if rec := boardRequest(t, s, http.MethodGet, "/api/board/cards", ""); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("an empty board = %q, want [] for the provider's forEach", rec.Body.String())
	}
	store.cards = []board.Card{
		{ID: "c1", ColumnID: "doing", Label: "call", Priority: 3, Tags: []string{"ops"}, DueAt: &due,
			ConversationID: "conv", Source: board.SourceChat, UpdatedBy: board.ActorAgent},
		{ID: "c2", ColumnID: "todo", Label: "pay", Tags: []string{}, Source: board.SourceCockpit, UpdatedBy: board.ActorOperator},
	}
	rec := boardRequest(t, s, http.MethodGet, "/api/board/cards", "")
	cards := decodeBoard[[]map[string]any](t, rec)
	if len(cards) != 2 || cards[0]["column"] != "doing" || cards[0]["deadline"] != "2026-10-12T06:00:00Z" ||
		cards[0]["source"] != "chat" || cards[0]["conversation_id"] != "conv" || cards[0]["priority"] != float64(3) {
		t.Fatalf("cards = %v", cards)
	}
	if _, has := cards[1]["deadline"]; has {
		t.Errorf("a card with no due date carries a deadline: %v", cards[1])
	}

	b := decodeBoard[boardJSON](t, boardRequest(t, s, http.MethodGet, "/api/board", ""))
	if b.Name != board.DefaultName || len(b.Columns) != 3 || b.Columns[1].CardLimit != 5 {
		t.Fatalf("board = %+v", b)
	}
}

func TestBoardAddCardIsTheOperators(t *testing.T) {
	store := &fakeBoardStore{}
	s := boardServer(store)
	rec := boardRequest(t, s, http.MethodPost, "/api/board/cards",
		`{"id":"temp:123","label":"renew","description":"the domain","column":"doing","priority":1,"tags":["ops"],"deadline":"2026-10-12T08:00:00.000Z","source":"chat"}`)
	got := decodeBoard[boardCardJSON](t, rec)
	if rec.Code != http.StatusOK || got.ID != "c-new" {
		t.Fatalf("add = %d %+v, want the stored card and its server id", rec.Code, got)
	}
	in := store.added
	if in.Label != "renew" || in.Description != "the domain" || in.Column != "doing" || in.Priority != 1 ||
		len(in.Tags) != 1 || in.DueAt == nil || !in.DueAt.Equal(time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("NewCard = %+v", in)
	}
	if store.source != board.SourceCockpit || store.actor != board.ActorOperator {
		t.Fatalf("add stamped %s/%s, want cockpit/operator whatever the body says", store.source, store.actor)
	}
	if rec := boardRequest(t, s, http.MethodPost, "/api/board/cards", `{"label":"x","deadline":"tomorrow"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a bad deadline = %d, want 400", rec.Code)
	}
}

func TestBoardUpdateCardPatchesOnlyWhatIsSent(t *testing.T) {
	store := &fakeBoardStore{}
	s := boardServer(store)
	boardRequest(t, s, http.MethodPut, "/api/board/cards/c1", `{"label":"renamed","column":"done"}`)
	p := store.patch
	if p.Label == nil || *p.Label != "renamed" || p.Description != nil || p.Priority != nil || p.Tags != nil || p.DueAt != nil {
		t.Fatalf("patch = %+v, want only the label", p)
	}
	boardRequest(t, s, http.MethodPut, "/api/board/cards/c1", `{"deadline":null}`)
	if store.patch.DueAt == nil || *store.patch.DueAt != nil {
		t.Fatalf("deadline null = %+v, want a clear", store.patch.DueAt)
	}
	boardRequest(t, s, http.MethodPut, "/api/board/cards/c1", `{"deadline":"2026-10-12T08:00:00Z"}`)
	if store.patch.DueAt == nil || *store.patch.DueAt == nil {
		t.Fatal("a deadline was not applied")
	}
	if store.actor != board.ActorOperator {
		t.Fatalf("update stamped %s", store.actor)
	}
}

func TestBoardMoveDuplicateAndDelete(t *testing.T) {
	store := &fakeBoardStore{}
	s := boardServer(store)
	boardRequest(t, s, http.MethodPut, "/api/board/cards/c1/move", `{"id":"c1","column":"done","before":null}`)
	if store.moved != [3]string{"c1", "done", ""} {
		t.Fatalf("move = %v, want c1 to the bottom of done", store.moved)
	}
	boardRequest(t, s, http.MethodPut, "/api/board/cards/c1/move", `{"id":"c1","before":"c9"}`)
	if store.moved != [3]string{"c1", "", "c9"} {
		t.Fatalf("reorder = %v, want the column kept and c1 before c9", store.moved)
	}
	if got := decodeBoard[boardCardJSON](t, boardRequest(t, s, http.MethodPost, "/api/board/cards/c1/duplicate", "")); got.ID != "c-copy" {
		t.Fatalf("duplicate = %+v", got)
	}
	if rec := boardRequest(t, s, http.MethodDelete, "/api/board/cards/c1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete of nothing = %d, want 404", rec.Code)
	}
	store.removed = true
	if rec := boardRequest(t, s, http.MethodDelete, "/api/board/cards/c1", ""); rec.Code != http.StatusOK || !decodeBoard[map[string]bool](t, rec)["deleted"] {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
	want := []string{"move", "move", "duplicate c1", "delete c1", "delete c1"}
	if strings.Join(store.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", store.calls, want)
	}
}

func TestBoardColumnsAndViews(t *testing.T) {
	store := &fakeBoardStore{}
	s := boardServer(store)
	rec := boardRequest(t, s, http.MethodPut, "/api/board/columns", `[{"id":"todo","label":"To do"},{"id":"done","label":"Done","cardLimit":3}]`)
	if b := decodeBoard[boardJSON](t, rec); len(b.Columns) != 2 || store.columns[1].CardLimit != 3 {
		t.Fatalf("columns = %+v, stored %+v", b, store.columns)
	}
	views := decodeBoard[[]board.View](t, boardRequest(t, s, http.MethodGet, "/api/board/views", ""))
	if len(views) != 1 || !views[0].Pinned || string(views[0].Filters) != `{"priority":3}` {
		t.Fatalf("views = %+v", views)
	}
	boardRequest(t, s, http.MethodPut, "/api/board/views", `{"name":"mine","filters":{"source":"chat"},"pinned":true}`)
	if store.view != [2]string{"mine", `{"source":"chat"}`} || !store.pinned {
		t.Fatalf("saved view = %v pinned=%v", store.view, store.pinned)
	}
	if rec := boardRequest(t, s, http.MethodDelete, "/api/board/views/v1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete of no view = %d, want 404", rec.Code)
	}
}

func TestBoardErrorsAreNamed(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{board.ErrNotFound, http.StatusNotFound, "not_found"},
		{board.ErrColumnInUse, http.StatusConflict, "column_in_use"},
		{fmt.Errorf("%w: column %q", board.ErrUnknownColumn, "later"), http.StatusBadRequest, "invalid_column"},
		{fmt.Errorf("%w: a label", board.ErrTooLong), http.StatusBadRequest, "too_long"},
		{fmt.Errorf("%w: a card needs a label", board.ErrInvalid), http.StatusBadRequest, "invalid"},
		{errors.New("connection refused"), http.StatusInternalServerError, "internal"},
	} {
		s := boardServer(&fakeBoardStore{err: c.err})
		rec := boardRequest(t, s, http.MethodPost, "/api/board/cards", `{"label":"x"}`)
		body := decodeBoard[map[string]string](t, rec)
		if rec.Code != c.status || body["error"] != c.code || body["message"] == "" {
			t.Errorf("%v = %d %v, want %d %s with a message", c.err, rec.Code, body, c.status, c.code)
		}
	}
}

// Every route answers a store failure with its JSON error, never the provider-breaking text/plain.
func TestBoardEveryRouteNamesAStoreFailure(t *testing.T) {
	s := boardServer(&fakeBoardStore{err: board.ErrNotFound})
	for _, c := range boardRoutes {
		rec := boardRequest(t, s, c.method, c.path, c.body)
		if rec.Code != http.StatusNotFound || decodeBoard[map[string]string](t, rec)["error"] != "not_found" {
			t.Errorf("%s %s = %d %s, want 404 not_found", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

func TestBoardRefusesMalformedBodies(t *testing.T) {
	s := boardServer(&fakeBoardStore{})
	for _, body := range []string{`{`, `"text"`, `{"label":"` + strings.Repeat("x", boardBodyMaxBytes) + `"}`} {
		rec := boardRequest(t, s, http.MethodPost, "/api/board/cards", body)
		if rec.Code != http.StatusBadRequest || decodeBoard[map[string]string](t, rec)["error"] != "invalid_json" {
			t.Errorf("body of %d bytes = %d %s, want 400 invalid_json", len(body), rec.Code, rec.Body.String())
		}
	}
	// The CSRF floor: a cross-origin form can post text/plain with no preflight.
	for _, path := range []string{"/api/board/cards", "/api/board/cards/c1/duplicate"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"label":"x"}`))
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, withPrincipal(req, boardOwner))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("text/plain POST %s = %d, want 400", path, rec.Code)
		}
	}
	for _, path := range []string{"/api/board/columns", "/api/board/views", "/api/board/cards/c1/move"} {
		if rec := boardRequest(t, s, http.MethodPut, path, `{`); rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s with a broken body = %d, want 400", path, rec.Code)
		}
	}
}
