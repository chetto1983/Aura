package agui

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

// conversations_branch_api.go is the D-09 / CHAT-05 branch sub-adapter (plan 25-07),
// split out of conversations_api.go to keep both files ≤600 LOC. It rides the SAME
// /api/conversations/{id}/ subtree (NO new bare /api/) and inherits RequireAuth from the
// parent-mux mount; the mutating route interposes RequireCapability like POST /agent/run
// (cmd/aura/serve_webui.go), because re-running a (possibly background) thread is
// privileged (T-25-25 / V4).
//
//	POST /api/conversations/{id}/edit — fork + re-run (edit a user turn, or regenerate an
//	                                    answer); SSE stream
//
// Moving between branches needs no route: the history snapshot carries the whole tree, and
// the cockpit switches branches in its own runtime (spec 2026-10-27, D1). The list and
// select routes had no caller, and select re-ran the model, so a click would have answered
// again. Both were removed. A send on another branch names it with aura.parent_seq on
// /agent/run.

// registerConversationBranchRoutes mounts the D-09 branch route on the supplied mux.
// Called by registerConversationRoutes (conversations_api.go) so the whole conversation
// subtree registers in one place.
func (s *Server) registerConversationBranchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/conversations/{id}/edit", s.handleEditBranch)
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

// handleEditBranch forks a NEW sibling branch off the diverging turn's parent, then
// re-runs over that branch (the edit-a-user-turn / regenerate path). The fork is durable
// (ForkBranch), and the old branch stays queryable (full tree, RESEARCH OQ3). A non-UUID id
// or an unknown diverging seq is a 404; a run already active on the thread is a 409.
func (s *Server) handleEditBranch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok {
		return
	}
	// Owner gate BEFORE the durable ForkBranch: a foreign identity must not fork+re-run
	// another's thread (MUSR-01 / D-06 — 404 hides existence).
	if _, err := s.conv.GetForIdentity(r.Context(), id, scopedIdentityID(r.Context())); err != nil {
		writeStoreErr(w, err)
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
	s.forkAndRerun(w, r, id, body.DivergeSeq, role, body.Content)
}

// forkAndRerun takes the thread lock, forks, and re-runs over the fork, in that order. The
// re-run chains every turn it writes to the turn before it, so the fork must be the turn
// written last when it starts. Forked before the lock, a run that appended in between
// became the fork's child (spec 2026-10-27, D10). The re-run streams exactly as handleRun
// does; only the loaded history differs.
func (s *Server) forkAndRerun(w http.ResponseWriter, r *http.Request, convID string, divergeSeq int, role, content string) {
	ctx := r.Context()
	var unlock func()
	if locker, ok := s.run.(threadTryLocker); ok {
		var locked bool
		unlock, locked = locker.TryLockThread(ctx, convID)
		if !locked {
			http.Error(w, runner.ErrThreadBusy.Error(), http.StatusConflict)
			return
		}
		defer unlock()
		ctx = runner.WithThreadLockHeld(ctx)
	}
	leaf, err := s.conv.ForkBranch(scopedCtx(ctx), convID, divergeSeq, role, content)
	if err != nil {
		if errors.Is(err, conversations.ErrTurnNotFound) {
			http.Error(w, "turn not found", http.StatusNotFound)
			return
		}
		writeStoreErr(w, err)
		return
	}

	runID := "run-" + uuid.NewString()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	// Shutdown cancels the re-run, not its stream, as in handleRun.
	turnCtx, endTurn := s.untilShutdown(ctx)
	defer endTurn()
	head := &runner.TurnHead{}
	turnCtx = runner.WithTurnHead(turnCtx, head)
	turn := s.reportTurnSeqs(turnCtx, convID, head, s.run.TurnBranch(turnCtx, convID, leaf))
	// Cockpit reasoning-on (D-01), same as handleRun: the only viewer is the
	// authenticated operator (whole-origin private), so the live CoT stream is surfaced.
	s.streamSSE(ctx, w, Translate(convID, runID, s.idgen, turn, true))
}
