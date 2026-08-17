package agui

import (
	"encoding/json"
	"errors"
	"io"
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

const createTitleMaxRunes = 80

// maxListLimit caps ?limit= for every paginated list/search endpoint routed through
// parseLimit (share list, conversations search, governance). The raw value reaches
// make([]T, 0, limit) pre-allocations (share.Store.ListForIdentity), where an
// authenticated ?limit=2000000000 would OOM the 768MB container in one request
// (CodeQL #40, go/uncontrolled-allocation-size) — so the clamp lives HERE at the
// parse seam, not in each store.
const maxListLimit = 200

// parseLimit reads the ?limit= query value, falling back to defaultSearchLimit when
// it is absent, non-numeric, or non-positive, and clamping to maxListLimit above.
// The clamp is written as an explicit comparison guard, NOT min(n, maxListLimit):
// CodeQL's go/uncontrolled-allocation-size query recognizes a relational guard as a
// sanitizer barrier on the tainted size but does not model the Go builtin min(), so
// the builtin form left alert #40 open despite being equally safe at runtime.
func parseLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultSearchLimit
	}
	if n > maxListLimit {
		return maxListLimit
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
	mux.HandleFunc("POST /api/conversations", s.handleCreateConversation)
	mux.HandleFunc("GET /api/conversations/search", s.handleSearchConversations)
	mux.HandleFunc("GET /api/conversations/{id}", s.handleGetConversation)
	mux.HandleFunc("GET /api/conversations/{id}/rot-events", s.handleConversationRotEvents)
	// WEBSHARE-01 (plan 37F-09): the owner-scoped conversation export (md|json,
	// share_export.go). It rides this already-mounted subtree's whole-origin
	// RequireAuth (serve_webui.go:381, F-1) — no separate route wiring or auth
	// is added there.
	mux.HandleFunc("GET /api/conversations/{id}/export", s.handleConversationExport)
	mux.HandleFunc("GET /api/conversations/{id}/owner-export", s.handleOwnerExport)
	mux.HandleFunc("POST /api/conversations/{id}/export-delete", s.handleOwnerExportDelete)
	mux.HandleFunc("GET /api/owner-exports/{id}", s.handleOwnerExportDownload)
	mux.HandleFunc("POST /api/conversations/{id}/rename", s.handleRenameConversation)
	mux.HandleFunc("POST /api/conversations/{id}/archive", s.handleArchiveConversation)
	mux.HandleFunc("POST /api/conversations/{id}/unarchive", s.handleArchiveConversation)
	mux.HandleFunc("DELETE /api/conversations/{id}", s.handleDeleteConversation)
	// D-09 / CHAT-05 branch tree routes (plan 25-07) — list/edit/select, rides the same
	// /api/conversations/{id}/ subtree (conversations_branch_api.go).
	s.registerConversationBranchRoutes(mux)
	// The composer's `/compact` command + the in-chat marker it leaves
	// (conversations_compaction_api.go), same subtree.
	s.registerCompactionRoutes(mux)
}

// writeJSON encodes v as the JSON body of a 200 response. A late encode failure (the
// client went away mid-write) is a WARN, not a surfaced error — the header is sent.
func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
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

// writeForeignMutateStatus resolves a not-owned mutate (a *ForIdentity mutate that affected
// 0 rows) to 404, whether the id is foreign or absent.
//
// D-06 asked for 403-when-known-foreign, and this used to deliver it by probing existence on
// the UNSCOPED base Get — which worked only because migration 0032's policy was
// permissive-on-unset and let an identity-less pool read observe a foreign row. Migration
// 0089 removed that branch, and with it the ability to distinguish the two cases at all:
// there is no read left in the system that can see a conversation the caller does not own,
// which is the entire point of the migration. Keeping the probe would have been dark code —
// a branch that can no longer be reached — and pretending to distinguish them would mean
// re-opening a cross-tenant read to serve a status code.
//
// So the split collapses toward the SAFER side, matching what internal/share/share_api.go
// already does for share links: "ANY error — foreign id, absent id, store failure — is 404,
// never 403 (SC4 row 6)". A caller now learns nothing about ids they do not own.
func (s *Server) writeForeignMutateStatus(w http.ResponseWriter, _ *http.Request, _ string) {
	http.Error(w, "conversation not found", http.StatusNotFound)
}

type createConversationBody struct {
	Title string `json:"title"`
}

func normalizeCreateTitle(raw string) string {
	title := strings.Join(strings.Fields(raw), " ")
	title = strings.Trim(title, "\"'` \t\r\n")
	title = strings.TrimRight(title, ".?!,:;")
	if title == "" {
		return ""
	}
	runes := []rune(title)
	if len(runes) > createTitleMaxRunes {
		title = strings.TrimSpace(string(runes[:createTitleMaxRunes]))
	}
	return title
}

// handleCreateConversation creates a new active conversation through the runner's
// existing lifecycle helper, then returns the store projection the frontend already
// consumes for lists, footer aggregates, and active-thread state.
func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	if s.run == nil || s.conv == nil {
		http.Error(w, "conversation creator unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var body createConversationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	id, err := s.run.NewConversation(r.Context())
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	if title := normalizeCreateTitle(body.Title); title != "" {
		if err := s.conv.SetTitleIfNull(scopedCtx(r.Context()), id, title); err != nil {
			http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
			return
		}
	}
	conv, err := s.conv.Get(r.Context(), id)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusCreated, conv)
}

// conversationDTO decorates the store projection with the additive-optional
// live_run_id (fix-plan 1.3 Tier B, amendment #90 point 6) — the reload-attach
// discovery key (§4.2): a client reopening a thread with a live detached run
// attaches read-only via GET /agent/runs/{live_run_id}/events. omitempty + the
// nil-registry guard keep the JSON byte-identical when the flag is off or no run is
// live.
type conversationDTO struct {
	conversations.Conversation
	LiveRunID string `json:"live_run_id,omitempty"`
}

// conversationDTOFor stamps the (identity, thread)'s live run id from the in-memory
// registry — a map lookup, never a store round-trip. The identity scoping rides
// LiveForThread's composite key, so a foreign live run is never disclosed.
func (s *Server) conversationDTOFor(r *http.Request, conv conversations.Conversation) conversationDTO {
	dto := conversationDTO{Conversation: conv}
	if s.runs != nil {
		if sess, ok := s.runs.LiveForThread(scopedIdentityID(r.Context()), conv.ID); ok {
			dto.LiveRunID = sess.RunID
		}
	}
	return dto
}

// handleListConversations returns the authenticated principal's conversations
// (ListForIdentity — MUSR-01, owner-scoped through WithIdentityTx). ?archived=true adds
// the archived rows (deleted rows are always excluded by the query). The `local` fallback
// keeps the loopback-dev operator seeing their own threads (no self-lockout).
func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("archived") == "true"
	rows, err := s.conv.ListForIdentity(r.Context(), scopedIdentityID(r.Context()), includeArchived)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	// nil stays nil (JSON `null`) so the empty-list wire is byte-identical to the
	// pre-DTO projection.
	var dtos []conversationDTO
	if rows != nil {
		dtos = make([]conversationDTO, 0, len(rows))
		for _, row := range rows {
			dtos = append(dtos, s.conversationDTOFor(r, row))
		}
	}
	writeJSON(w, dtos)
}

// handleGetConversation returns the single Conversation row including the
// session-cumulative aggregates (Total{Input,Output,Cached}Tokens / TotalCostUSD) —
// the D-10 footer reload seed — plus the optional live_run_id (amendment #90 point 6).
func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	// Owner-scoped read (MUSR-01 / D-06): a foreign or absent id both map to 404 via
	// ErrConversationNotFound — a read hides the existence of another identity's thread.
	conv, err := s.conv.GetForIdentity(r.Context(), id, scopedIdentityID(r.Context()))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, s.conversationDTOFor(r, conv))
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
	// Owner-scoped FTS (MUSR-01): hits are filtered to the principal's conversations so a
	// search never surfaces another identity's turn content.
	hits, err := s.conv.SearchConversationTurnsForIdentity(r.Context(), q, scopedIdentityID(r.Context()), limit)
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
	// Owner gate (MUSR-01 / D-06): a foreign/absent id 404s before the unscoped read.
	id, ok := s.ownedConversation(w, r)
	if !ok {
		return
	}
	events, err := s.conv.ListContextRotEvents(scopedCtx(r.Context()), id)
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
	// Owner-scoped mutate (MUSR-01 / D-06): rename only the caller's own conversation;
	// rows-affected==0 is a foreign (403) or absent (404) id.
	affected, err := s.conv.RenameForIdentity(r.Context(), id, scopedIdentityID(r.Context()), body.Title)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if affected == 0 {
		s.writeForeignMutateStatus(w, r, id)
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
	// Owner-scoped mutate (MUSR-01 / D-06): archive/unarchive only the caller's own thread.
	affected, err := s.conv.UpdateStatusForIdentity(r.Context(), id, scopedIdentityID(r.Context()), status)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if affected == 0 {
		s.writeForeignMutateStatus(w, r, id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteConversation routes the D-07 hard delete through the runner's single
// conversation-delete lifecycle (MUSR-05 / D-22): cancel active work → expire pauses → evict
// session tools → terminate background jobs → THEN the owner-scoped persistence delete. The
// route MUST NOT call a raw store delete (that would skip the teardown and leak live session
// state). rows-affected==0 is a foreign (403) or absent (404) id (D-06).
func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	if s.run == nil {
		http.Error(w, "conversation deleter unavailable", http.StatusServiceUnavailable)
		return
	}
	affected, err := s.run.DeleteConversationLifecycle(r.Context(), scopedIdentityID(r.Context()), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if affected == 0 {
		s.writeForeignMutateStatus(w, r, id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
