package agui

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/google/uuid"
)

// defaultSearchLimit is the FTS hit cap applied when ?limit= is absent or unparseable
// (the store further normalizes a non-positive value).
const defaultSearchLimit = 20

// parseLimit reads the ?limit= query value, falling back to defaultSearchLimit when
// it is absent, non-numeric, or non-positive. The store clamps the upper bound.
func parseLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultSearchLimit
	}
	return n
}

// conversations_api.go is the CHAT-02 thin REST adapter over conversations.Store
// (Phase 25). Every handler is a thin shell: parse the request, uuid.Parse-guard any
// {id} path param to a clean 404 BEFORE the store round-trip (T-25-02), call EXACTLY
// ONE store method, project to JSON. There is NO business logic here — the ladder /
// FTS / aggregation all live in the store (the locked seams). Error responses are
// redacted with sanitizeErr (server.go) so a DSN/token never leaks on the wire.
//
// The routes are registered on the agui Server.Mux under the /api/conversations/
// subtree; the PARENT-mux mount behind RequireAuth is cmd/aura/serve_webui.go's job
// (the whole-origin gate is inherited, no second auth check here).

// registerConversationRoutes mounts the CHAT-02 conversation-management routes on the
// supplied mux using Go 1.22 method-pattern routing. The patterns are method- and
// path-specific so an FTS /search never collides with the {id} catch and a malformed
// verb 404s cleanly. List + search are the two non-{id} GETs; the rest carry an {id}.
func (s *Server) registerConversationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/conversations", s.handleListConversations)
	mux.HandleFunc("GET /api/conversations/search", s.handleSearchConversations)
	mux.HandleFunc("GET /api/conversations/{id}", s.handleGetConversation)
	mux.HandleFunc("GET /api/conversations/{id}/rot-events", s.handleConversationRotEvents)
	mux.HandleFunc("POST /api/conversations/{id}/rename", s.handleRenameConversation)
	mux.HandleFunc("POST /api/conversations/{id}/archive", s.handleArchiveConversation)
	mux.HandleFunc("POST /api/conversations/{id}/unarchive", s.handleArchiveConversation)
	mux.HandleFunc("DELETE /api/conversations/{id}", s.handleDeleteConversation)
}

// writeJSON encodes v as the JSON body of a 200 response. A late encode failure (the
// client went away mid-write) is a WARN, not a surfaced error — the header is sent.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("agui: encode conversations response", "err", err)
	}
}

// parseConvID resolves the {id} path param to a clean 404 BEFORE any store call: a
// non-UUID id can never identify an existing conversation, so it returns false +
// writes the 404 (T-25-02) rather than leaking the store's parse error as a 500.
func parseConvID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return "", false
	}
	return id, true
}

// writeStoreErr maps a store error to the wire: ErrConversationNotFound → 404, any
// other failure → 500 with the message redacted by sanitizeErr.
func writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, conversations.ErrConversationNotFound) {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}
	http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
}

// handleListConversations returns Store.List(ctx, includeArchived). ?archived=true
// adds the archived rows (deleted rows are always excluded by the query).
func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("archived") == "true"
	rows, err := s.conv.List(r.Context(), includeArchived)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

// handleGetConversation returns the single Conversation row including the
// session-cumulative aggregates (Total{Input,Output,Cached}Tokens / TotalCostUSD) —
// the D-10 footer reload seed.
func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	conv, err := s.conv.Get(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, conv)
}

// handleSearchConversations returns SearchConversationTurns(ctx, q, limit) projected
// to JSON (the D-08 snippet + conversationID + title hits). An empty q is a 400. The
// query string binds via the LOCKED sqlc content % $1 contract — never rewritten here
// (T-25-01).
func (s *Server) handleSearchConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing search query", http.StatusBadRequest)
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	hits, err := s.conv.SearchConversationTurns(r.Context(), q, limit)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, hits)
}

// handleConversationRotEvents returns the microcompact ladder events (pairs_dropped)
// for the conversation via the thin ListContextRotEvents wrapper — the D-11
// context-budget gauge markers (plan 25-04 Task 2 is the sole consumer). The
// underlying sqlc query already exists; this adds only the JSON projection.
func (s *Server) handleConversationRotEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	events, err := s.conv.ListContextRotEvents(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, events)
}

// renameBody is the POST /rename request payload.
type renameBody struct {
	Title string `json:"title"`
}

// handleRenameConversation calls Store.Rename(ctx, id, title) and returns 204. An
// empty title is a 400. The body is capped with maxRunBodyBytes (T-12-12 DoS guard).
func (s *Server) handleRenameConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var body renameBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Title == "" {
		http.Error(w, "title must not be empty", http.StatusBadRequest)
		return
	}
	if err := s.conv.Rename(r.Context(), id, body.Title); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleArchiveConversation drives UpdateStatus from the path verb: /archive →
// archived, /unarchive → active (D-07 archive-first). Returns 204.
func (s *Server) handleArchiveConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	status := conversations.StatusArchived
	if strings.HasSuffix(r.URL.Path, "/unarchive") {
		status = conversations.StatusActive
	}
	if err := s.conv.UpdateStatus(r.Context(), id, status); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteConversation calls Store.Delete(ctx, id) — the D-07 hard delete (the
// confirm dialog lives in the UI) — and returns 204.
func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	if err := s.conv.Delete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
