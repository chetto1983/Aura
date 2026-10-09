package agui

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/chetto1983/aura/internal/board"
)

// board_api.go serves the work board to the cockpit (prd.md §16, "A work board for the
// identity and its agent", 2026-10-09) in SVAR Kanban's own REST dialect, so the widget's
// RestDataProvider drives it unmodified: GET cards answers a bare array, POST answers the
// stored card whose id replaces the widget's temporary one, and every answer is JSON because
// the provider calls res.json() on all of them. The board is the authenticated principal's own,
// so no route takes an identity.

const boardBase = "/api/board"

// boardBodyMaxBytes bounds a request body: a card's description is at most 4000 characters.
const boardBodyMaxBytes = 64 << 10

// boardStore is the seam over *board.Store, declared at the consumer per D-A2-02.
type boardStore interface {
	Board(ctx context.Context, identityID string) (board.Board, error)
	SetColumns(ctx context.Context, identityID string, columns []board.Column) (board.Board, error)
	Cards(ctx context.Context, identityID string) ([]board.Card, error)
	AddCard(ctx context.Context, identityID string, in board.NewCard, source board.Source, actor board.Actor) (board.Card, error)
	UpdateCard(ctx context.Context, identityID, cardID string, patch board.CardPatch, actor board.Actor) (board.Card, error)
	MoveCard(ctx context.Context, identityID, cardID, column, beforeID string, actor board.Actor) (board.Card, error)
	DuplicateCard(ctx context.Context, identityID, cardID string) (board.Card, error)
	DeleteCard(ctx context.Context, identityID, cardID string) (bool, error)
	Views(ctx context.Context, identityID string) ([]board.View, error)
	SaveView(ctx context.Context, identityID, name string, filters json.RawMessage, pinned bool) (board.View, error)
	DeleteView(ctx context.Context, identityID, viewID string) (bool, error)
}

// SetBoardStore wires the work board. Until set, the board routes answer 503.
func (s *Server) SetBoardStore(store boardStore) { s.board = store }

func (s *Server) registerBoardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+boardBase, s.boardRoute(s.handleBoard))
	mux.HandleFunc("PUT "+boardBase+"/columns", s.boardRoute(s.handleBoardColumns))
	mux.HandleFunc("GET "+boardBase+"/cards", s.boardRoute(s.handleBoardCards))
	mux.HandleFunc("POST "+boardBase+"/cards", s.boardRoute(s.handleBoardAddCard))
	mux.HandleFunc("PUT "+boardBase+"/cards/{id}", s.boardRoute(s.handleBoardUpdateCard))
	mux.HandleFunc("PUT "+boardBase+"/cards/{id}/move", s.boardRoute(s.handleBoardMoveCard))
	mux.HandleFunc("POST "+boardBase+"/cards/{id}/duplicate", s.boardRoute(s.handleBoardDuplicateCard))
	mux.HandleFunc("DELETE "+boardBase+"/cards/{id}", s.boardRoute(s.handleBoardDeleteCard))
	mux.HandleFunc("GET "+boardBase+"/views", s.boardRoute(s.handleBoardViews))
	mux.HandleFunc("PUT "+boardBase+"/views", s.boardRoute(s.handleBoardSaveView))
	mux.HandleFunc("DELETE "+boardBase+"/views/{id}", s.boardRoute(s.handleBoardDeleteView))
}

// boardHandler is a board route with the store checked and the caller's identity resolved.
type boardHandler func(w http.ResponseWriter, r *http.Request, store boardStore, identityID string)

func (s *Server) boardRoute(h boardHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.board == nil {
			writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "board_unavailable"})
			return
		}
		h(w, r, s.board, scopedIdentityID(r.Context()))
	}
}

// boardJSON is the board without its cards: the widget loads cards on their own route.
type boardJSON struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Columns []board.Column `json:"columns"`
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	b, err := store.Board(r.Context(), identityID)
	if err != nil {
		writeBoardError(w, err)
		return
	}
	writeJSON(w, boardJSON{ID: b.ID, Name: b.Name, Columns: b.Columns})
}

// handleBoardColumns replaces the columns: the free widget renames a column and sets its limit
// but cannot add or remove one, so the cockpit's column editor sends the whole array here.
func (s *Server) handleBoardColumns(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	var columns []board.Column
	if !decodeBoardBody(w, r, &columns) {
		return
	}
	b, err := store.SetColumns(r.Context(), identityID, columns)
	if err != nil {
		writeBoardError(w, err)
		return
	}
	slog.InfoContext(r.Context(), "board: columns changed", "identity_id", identityID, "columns", len(b.Columns))
	writeJSON(w, boardJSON{ID: b.ID, Name: b.Name, Columns: b.Columns})
}

// decodeBoardBody is the write boundary every board route crosses, a body-less duplicate too:
// the application/json gate is the CSRF floor a cross-origin form cannot clear. Unknown fields
// pass because the widget echoes its whole card back; an empty body decodes to nothing and the
// store then refuses what is missing.
func decodeBoardBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := strictDecodeJSON(w, r, v, decodeOpts{maxBytes: boardBodyMaxBytes, allowEmpty: true, allowUnknown: true}); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return false
	}
	return true
}

// writeBoardError answers a store error. Another identity's card is ErrNotFound, never a 403:
// RLS makes it invisible, and the answer must not tell the two apart.
func writeBoardError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal"
	switch {
	case errors.Is(err, board.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, board.ErrColumnInUse):
		status, code = http.StatusConflict, "column_in_use"
	case errors.Is(err, board.ErrUnknownColumn):
		status, code = http.StatusBadRequest, "invalid_column"
	case errors.Is(err, board.ErrTooLong):
		status, code = http.StatusBadRequest, "too_long"
	case errors.Is(err, board.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid"
	}
	writeJSONStatus(w, status, map[string]string{"error": code, "message": sanitizeErr(err)})
}
