package agui

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// server_run_resume.go carries the two run-scoped routes of fix-plan 1.3 Tier B
// (amendment #90 points 3-4): the read-only SSE resume GET /agent/runs/{runID}/events
// and the explicit-Stop mutation POST /agent/runs/{runID}/cancel. Both mount inside
// the agui mux, so they inherit the parent RequireAuth whole-origin gate and withCORS
// exactly like /agent/run — zero bespoke auth here (§3.4). While the registry is nil
// (flag off) both answer 404, hiding the surface entirely (§7.1).

// resolveRunSession walks the §3.2 identity-scoped resolution ladder shared by
// resume and cancel; every rung answers the SAME 404 body so a caller can never
// distinguish malformed / never-existed / reaped / foreign (MUSR-01/D-06 — hide
// existence, never 403):
//  1. nil registry (flag off) → 404 (the surface does not exist);
//  2. runID fails the "run-"+uuid shape → 404 before any lookup (the T-12-11
//     malformed-thread-id chokepoint parity);
//  3. registry miss (never existed, or reaped past linger) → 404;
//  4. owner mismatch against scopedIdentityID → 404.
func (s *Server) resolveRunSession(w http.ResponseWriter, r *http.Request) (*RunSession, bool) {
	if s.runs == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return nil, false
	}
	rest, found := strings.CutPrefix(r.PathValue("runID"), "run-")
	if !found {
		http.Error(w, "run not found", http.StatusNotFound)
		return nil, false
	}
	if _, err := uuid.Parse(rest); err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return nil, false
	}
	sess, ok := s.runs.Get("run-" + rest)
	if !ok || sess.IdentityID != scopedIdentityID(r.Context()) {
		http.Error(w, "run not found", http.StatusNotFound)
		return nil, false
	}
	return sess, true
}

// handleRunEvents is the read-only SSE resume (§3.3, §5.1): it performs ZERO writes —
// no SubmitAnswers, no effort persistence, no lock acquisition, no turn start — it
// only subscribes to an existing session, so a reconnect can never re-trigger a side
// effect no matter how many times it fires (deliberately NOT in httpMutationRoutes;
// the coverage sweep is method-based and GETs are read-only by construction).
// Last-Event-ID: absent → full-buffer replay from seq 1 (the page-reload attach
// path); `n` → replay strictly from n+1 (the server never re-sends an acknowledged
// id); unparsable/negative → 400; replay gap (ring rotated past n+1, or a reaped
// tail) → 410 Gone so the client knows the run may still be live but partial-frame
// recovery is impossible and falls back to MESSAGES_SNAPSHOT (§4.3). A terminal,
// still-lingering run replays the requested tail — necessarily ending in
// RUN_FINISHED/RUN_ERROR — then closes cleanly (the subscription channel arrives
// already closed). The live continuation reuses drainSession: same id: discipline,
// same Tier A heartbeat.
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.resolveRunSession(w, r)
	if !ok {
		return
	}
	fromSeq := int64(0)
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "invalid Last-Event-ID", http.StatusBadRequest)
			return
		}
		fromSeq = n
	}
	ch, cancelSub, ok := sess.subscribeFrom(fromSeq)
	if !ok {
		writeJSONStatus(w, http.StatusGone, map[string]string{"error": "replay window exceeded"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	s.drainSession(r.Context(), w, ch, cancelSub)
}

// handleRunCancel is the sanctioned Stop now that disconnect no longer cancels
// (amendment #90 point 4, §4.4): resolve through the same ladder, fire the session's
// cancel handle — the detached ctx → turn ctx unwinds exactly as a disconnect-cancel
// did pre-detach, and the translator's RUN_ERROR lands in the ring as the terminal
// frame — and answer 202 (the unwind is async). Cancelling an already-terminal run
// is a 202 no-op (the expired ctx's CancelFunc is idempotent by nature). The route
// IS in httpMutationRoutes (agent_run_cancel, Idempotency-Key required).
func (s *Server) handleRunCancel(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.resolveRunSession(w, r)
	if !ok {
		return
	}
	if sess.cancel != nil {
		sess.cancel()
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
}
