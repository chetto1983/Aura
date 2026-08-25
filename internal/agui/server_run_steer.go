package agui

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/chetto1983/aura/internal/steer"
)

// server_run_steer.go carries POST /agent/runs/{runID}/steer (amendment #132
// item 2, D-01/D-02): the cockpit's mid-turn redirect route. It is
// structurally identical to handleRunCancel (server_run_resume.go) — resolve
// through the SAME owner-scoped 404-hiding ladder, then a different side
// effect, then a status response. sess.ThreadID IS the conversation id the
// shared steer.Inbox keys on (confirmed: server_run.go's s.run.Turn(ctx,
// in.ThreadID, ...) and runner.buildAgent's SessionID: convID assignment are
// the same value on this path — FA-4's cockpit half).

// steerRequest carries the raw operator text under one named field. Nothing
// else on this route is caller-supplied: the conversation id comes from the
// resolved RunSession, never the request body.
type steerRequest struct {
	Text string `json:"text"`
}

// handleRunSteer resolves runID through resolveRunSession — zero new
// resolution logic, every rung already answers the identical 404 (T-52-11) —
// then pushes the decoded text into the SHARED inbox both this route and the
// agent's drain points read (T-52-31: the single-inbox wiring lands in the
// SAME commit as this route, cmd/aura/chat_boot.go + serve_agui.go). The
// route renders internal/steer's OWN sentinels; it re-derives no
// classification of its own (T-52-13 — the caps and the empty/oversize
// decision live in internal/steer, 52-02).
func (s *Server) handleRunSteer(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.resolveRunSession(w, r)
	if !ok {
		return
	}
	// AURA_AGUI_RUN_STEER=false (D-12's explicit rollback): the composition
	// root wires s.steer nil, and this route answers exactly like a
	// nil-registry run surface — hide existence entirely — rather than
	// accepting a POST nothing will ever drain (a half-live feature).
	if s.steer == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRunBodyBytes+1))
	if err != nil || len(body) > maxRunBodyBytes {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var req steerRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.steer.Push(sess.ThreadID, "cockpit", req.Text); err != nil {
		writeSteerRefusal(w, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// writeSteerRefusal renders internal/steer's own sentinel errors as the
// ratified refusal ladder — empty/oversize 400, queue-full 429 with
// Retry-After, closed 410 — never re-deriving the classification behind them.
func writeSteerRefusal(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, steer.ErrEmpty), errors.Is(err, steer.ErrTooLarge):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, steer.ErrQueueFull):
		w.Header().Set("Retry-After", "5")
		writeJSONStatus(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
	case errors.Is(err, steer.ErrClosed):
		http.Error(w, err.Error(), http.StatusGone)
	default:
		http.Error(w, "steer refused", http.StatusInternalServerError)
	}
}
