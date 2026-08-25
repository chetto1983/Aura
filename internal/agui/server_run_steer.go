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
	// The 410 half of D-09's boundary (52-05, complementing 52-04's route): a
	// run whose session is ALREADY terminal when this POST arrives is refused
	// before anything is queued — the auto-delivery wrap (runner_steer.go)
	// only ever covers a steer accepted against a run that was still LIVE.
	// Consults the session's OWN terminal accessor (the SAME notion the
	// reaper and the resume route already use) rather than computing
	// terminality here.
	if terminal, _ := sess.terminalState(); terminal {
		writeSteerGone(w, steerTerminalRunMessage)
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

// steerTerminalRunMessage is the terminal-run 410 body (D-09's complement to
// 52-04's route, 52-05): distinguishable BOTH from the resume route's
// replay-window 410 ("replay window exceeded", server_run_resume.go) and
// from the inbox-closed 410 (steer.ErrClosed's own message) by content, so
// neither an operator nor a test has to infer which of the three causes
// fired from the status code alone. It says plainly the message was NOT
// queued — the operator's next move is to send it as an ordinary turn.
const steerTerminalRunMessage = "run has ended: message was not queued; send it as a normal turn"

// writeSteerGone is the SOLE 410-Gone call site in this file, shared by the
// terminal-run refusal above and the inbox-closed sentinel below — two
// structurally distinct causes (a dead RUN vs a closed INBOX), one status
// code, each with its own distinguishing body.
func writeSteerGone(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusGone)
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
		writeSteerGone(w, err.Error())
	default:
		http.Error(w, "steer refused", http.StatusInternalServerError)
	}
}
