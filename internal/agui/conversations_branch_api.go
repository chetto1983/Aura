package agui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

// conversations_branch_api.go is the D-09 / CHAT-05 branch sub-adapter (plan 25-07),
// split out of conversations_api.go to keep both files ≤600 LOC. It rides the SAME
// /api/conversations/{id}/ subtree (NO new bare /api/) and inherits RequireAuth from the
// parent-mux mount; the two MUTATING routes (edit / branch-select re-run) interpose
// RequireCapability like POST /agent/run (cmd/aura/serve_webui.go), because re-running a
// (possibly background) thread is privileged (T-25-25 / V4).
//
// The endpoints:
//
//	GET  /api/conversations/{id}/branches                       — the navigable branch set
//	POST /api/conversations/{id}/edit                           — fork+re-run (edit a turn /
//	                                                              regenerate); SSE stream
//	POST /api/conversations/{id}/branches/{branchSeq}/select    — re-run the selected branch
//	                                                              path; SSE stream
//
// Both mutating routes drive the re-run over the path-aware loader from plan 25-06
// (TurnBranch → LoadManagedHistoryForBranch) so the messages[0] head stays byte-identical
// across a branch switch — the CAP-04 cache invariant (T-25-24). They do NOT re-implement
// the walk; they call the runner primitive. ids are uuid.Parse-guarded to a clean 404
// (T-25-26); errors are redacted with sanitizeErr.

// registerConversationBranchRoutes mounts the D-09 branch routes on the supplied mux.
// Called by registerConversationRoutes (conversations_api.go) so the whole conversation
// subtree registers in one place.
func (s *Server) registerConversationBranchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/conversations/{id}/branches", s.handleListBranches)
	mux.HandleFunc("POST /api/conversations/{id}/edit", s.handleEditBranch)
	mux.HandleFunc("POST /api/conversations/{id}/branches/{branchSeq}/select", s.handleSelectBranch)
}

// branchItem is the JSON projection of one navigable branch (D-09). branchSeq is the
// leaf seq the BranchPicker selects (and the select route's path param); branchId is the
// owning branch uuid; parentSeq is the divergence point (0 = root).
type branchItem struct {
	BranchSeq int    `json:"branch_seq"`
	BranchID  string `json:"branch_id"`
	ParentSeq int    `json:"parent_seq"`
}

// handleListBranches returns the conversation's navigable branch set (ListBranches). A
// non-branched conversation reports exactly one branch (its canonical leaf), so the
// picker stays hidden until an edit/regenerate forks a sibling.
func (s *Server) handleListBranches(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	branches, err := s.conv.ListBranches(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	items := make([]branchItem, 0, len(branches))
	for _, b := range branches {
		items = append(items, branchItem{
			BranchSeq: b.LeafSeq,
			BranchID:  b.BranchID.String(),
			ParentSeq: b.ParentSeq,
		})
	}
	writeJSON(w, items)
}

// editBranchBody is the POST /edit request payload: divergeSeq is the turn being edited
// (a user turn) or regenerated (an assistant turn); content is the new turn text (the
// edited user message, or empty for a regenerate where the agent produces the assistant
// turn). role defaults to "user" (an edit); a regenerate omits it and supplies no content.
type editBranchBody struct {
	DivergeSeq int    `json:"diverge_seq"`
	Role       string `json:"role"`
	Content    string `json:"content"`
}

// handleEditBranch forks a NEW sibling branch off the diverging turn's parent then
// re-runs over that branch (the edit-a-user-turn / regenerate path). The fork is durable
// (ForkBranch) and the old branch stays queryable (full tree, RESEARCH OQ3). The re-run
// streams the agent turn as SSE over the rehydrated SELECTED-branch history. A non-UUID
// id / unknown diverging seq → 404. The fork+re-run is serialized on the per-thread lock
// (409 if a run is already active).
func (s *Server) handleEditBranch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var body editBranchBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.DivergeSeq <= 0 {
		http.Error(w, "diverge_seq must be a positive turn seq", http.StatusBadRequest)
		return
	}
	role := body.Role
	if role == "" {
		role = "user"
	}
	leaf, _, err := s.conv.ForkBranch(r.Context(), id, body.DivergeSeq, role, body.Content)
	if err != nil {
		if errors.Is(err, conversations.ErrTurnNotFound) {
			http.Error(w, "turn not found", http.StatusNotFound)
			return
		}
		writeStoreErr(w, err)
		return
	}
	s.rerunBranch(w, r, id, leaf)
}

// handleSelectBranch re-runs the conversation over an EXISTING selected branch leaf (the
// {branchSeq} path param — the leaf seq ListBranches reported). It drives the path loader
// (LoadManagedHistoryForBranch) so the head is byte-identical; the body diverges per
// branch. An unknown branch leaf yields an empty path → the re-run produces a normal turn
// over whatever the walk returns (an unknown seq is a 404 at the parse boundary only when
// non-numeric; a numeric-but-absent seq walks to empty, which the agent handles).
func (s *Server) handleSelectBranch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	leaf, ok := parseBranchSeq(w, r)
	if !ok {
		return
	}
	s.rerunBranch(w, r, id, leaf)
}

// rerunBranch is the shared SSE re-run tail for the edit + select routes. It resolves the
// thread (404), acquires the per-thread lock (409 on contention), and streams the
// translated TurnBranch(leaf) over the SELECTED-branch rehydrated history. leaf <= 0 falls
// back to the canonical leaf (the same history a linear Turn would load). Mirrors
// handleRun's lock + SSE machinery (server.go) so a branch re-run behaves exactly like a
// normal turn on the wire — only the loaded history differs.
func (s *Server) rerunBranch(w http.ResponseWriter, r *http.Request, convID string, leaf int) {
	ctx := r.Context()
	if _, err := s.conv.Get(ctx, convID); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			http.Error(w, "thread not found", http.StatusNotFound)
			return
		}
		http.Error(w, "thread lookup failed", http.StatusInternalServerError)
		return
	}

	var unlock func()
	if locker, ok := s.run.(threadTryLocker); ok {
		var locked bool
		unlock, locked = locker.TryLockThread(convID)
		if !locked {
			http.Error(w, runner.ErrThreadBusy.Error(), http.StatusConflict)
			return
		}
		defer unlock()
		ctx = runner.WithThreadLockHeld(ctx)
	}

	runID := "run-" + uuid.NewString()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	turn := s.run.TurnBranch(ctx, convID, leaf)
	// Cockpit reasoning-on (D-01), same as handleRun: the only viewer is the
	// authenticated operator (whole-origin private), so the live CoT stream is surfaced.
	s.streamSSE(ctx, w, Translate(convID, runID, s.idgen, turn, true))
}

// parseBranchSeq resolves the {branchSeq} path param to a positive int. A non-numeric or
// non-positive value can never be a valid leaf seq → clean 404 (never a 500), mirroring
// the uuid.Parse-before-store-call guard for the {id}.
func parseBranchSeq(w http.ResponseWriter, r *http.Request) (int, bool) {
	seq, err := strconv.Atoi(r.PathValue("branchSeq"))
	if err != nil || seq <= 0 {
		http.Error(w, "branch not found", http.StatusNotFound)
		return 0, false
	}
	return seq, true
}
