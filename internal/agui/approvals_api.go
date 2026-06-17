package agui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

// approvals_api.go is the APRV-01/02/03 thin adapter for the cross-thread approval
// center (Phase 25 plan 25-02). It closes the two backend gaps the center needs:
//
//  1. GET /api/approvals — the cross-thread pending read. askuser.Store.ListPending
//     is per-conversation; ListPendingAll (Task 1) aggregates every still-pending
//     ask_user pause across ALL conversations so the operator sees a single queue.
//     The surfaced question/answer strings are run through SanitizeString (Security
//     V7 / T-25-06) so a pending question echoing a secret never leaks on the wire.
//
//  2. POST /api/approvals/{token}/resolve — the three-action resume bridge. The
//     AG-UI Resume[] path (server.go:resumeAnswers) maps only resolved→accept and
//     cancelled→cancel; there is NO path to askuser.ActionDecline over POST
//     /agent/run (Pitfall 4). This adapter maps the verb (accept|decline|cancel) to
//     askuser.Action* and calls Runner.SubmitAnswers directly with a single-entry
//     map — reaching the FULL three-action model the Runner already implements
//     end-to-end (RESEARCH Open Question 1, Option A). The Runner owns decline →
//     declinedContent ("user declined to answer", so the agent continues INFORMED of
//     the refusal, NOT killed) and cancel → AutoResolveForConversation; this handler
//     re-implements none of that.
//
// Every handler is a thin shell: parse, uuid.Parse-guard the token to a clean 404
// BEFORE the store/runner round-trip (T-25-09), map the verb, call exactly one
// store/runner method, project to JSON / status. Errors are redacted with sanitizeErr.
// Routes are registered on the agui Server.Mux; the PARENT-mux mount behind RequireAuth
// (+ RequireCapability on resolve) is cmd/aura/serve_webui.go's job.

// defaultApprovalsLimit caps the cross-thread pending read when ?limit= is absent or
// unparseable (the store further clamps a non-positive value to 100).
const defaultApprovalsLimit = 100

// registerApprovalRoutes mounts the approval-center routes using Go 1.22 method-pattern
// routing. The read is a non-{token} GET; the resolve is a method- and path-specific
// POST so a malformed verb 404s cleanly and the read never collides with it.
func (s *Server) registerApprovalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/approvals", s.handleListApprovals)
	mux.HandleFunc("POST /api/approvals/{token}/resolve", s.handleResolveApproval)
}

// approvalItem is the JSON projection of one cross-thread pending pause (APRV-01). It
// carries everything the operator needs to triage and jump to the source thread:
// the question (sanitized), the option set, priority, kind, the owning conversation,
// and the proxied source-thread id (the flat swarm-worker id, "" for a direct call).
type approvalItem struct {
	Token          string          `json:"token"`
	ConversationID string          `json:"conversation_id"`
	Kind           string          `json:"kind"`
	Question       string          `json:"question"`
	Options        json.RawMessage `json:"options,omitempty"`
	Priority       int             `json:"priority"`
	Source         string          `json:"source,omitempty"`
}

// handleListApprovals returns ListPendingAll(ctx, limit) projected to JSON — the
// cross-thread pending queue (APRV-01). question is run through SanitizeString (V7).
// A Server with no ApprovalStore wired answers 503 (the read is optional wiring).
func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals read not available", http.StatusServiceUnavailable)
		return
	}
	limit := parseApprovalsLimit(r.URL.Query().Get("limit"))
	pendings, err := s.approvals.ListPendingAll(r.Context(), limit)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	items := make([]approvalItem, 0, len(pendings))
	for _, p := range pendings {
		items = append(items, approvalItem{
			Token:          p.Token,
			ConversationID: p.ConversationID,
			Kind:           p.Kind,
			Question:       SanitizeString(p.Question), // V7: a question may echo a secret
			Options:        p.Options,
			Priority:       p.Priority,
			Source:         p.ProxiedFromChildID,
		})
	}
	writeJSON(w, items)
}

// resolveBody is the POST /resolve request payload. action is accept|decline|cancel;
// content is the operator's answer text, used ONLY for accept (the Runner overrides it
// with declinedContent for decline and the auto-terminated marker for cancel).
type resolveBody struct {
	Action  string `json:"action"`
	Content string `json:"content"`
}

// handleResolveApproval bridges the HTTP verb to the Runner's three-action resume
// (APRV-02 / D-05). It uuid.Parse-guards the token (→ 404), parses {action, content},
// maps the verb to askuser.Action* (rejecting any other value with 400), builds a
// single-entry map[string]runner.ResponseInput{token: {Action, Content}}, and calls
// Runner.SubmitAnswers. The Runner handles decline → declinedContent (agent continues
// informed) and cancel → AutoResolveForConversation internally. An unknown /
// already-resolved token surfaces as ErrPauseNotFound → 404 (APRV-03: a terminated
// pending is never silently lost — the operator gets a clear "gone" rather than a
// false success). After resolve the run continues over the rehydrated history when the
// React layer re-drives the turn with a no-Resume POST /agent/run (continue-after-resume
// contract for plan 25-06). Returns 204.
func (s *Server) handleResolveApproval(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if _, err := uuid.Parse(token); err != nil {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var body resolveBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	action, ok := resolveAction(body.Action)
	if !ok {
		http.Error(w, "action must be accept, decline, or cancel", http.StatusBadRequest)
		return
	}
	answers := map[string]runner.ResponseInput{
		token: {Action: action, Content: body.Content},
	}
	if _, err := s.run.SubmitAnswers(r.Context(), answers); err != nil {
		if errors.Is(err, askuser.ErrPauseNotFound) {
			http.Error(w, "approval not found or already resolved", http.StatusNotFound)
			return
		}
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveAction maps the HTTP verb to the askuser three-action model, returning false
// for any unrecognized value (Tampering guard: deny ≠ accept, T-25-08 — decline is its
// own distinct action the Runner answers with declinedContent, never the operator text).
func resolveAction(verb string) (string, bool) {
	switch verb {
	case askuser.ActionAccept, askuser.ActionDecline, askuser.ActionCancel:
		return verb, true
	default:
		return "", false
	}
}

// parseApprovalsLimit reads ?limit=, falling back to defaultApprovalsLimit when absent,
// non-numeric, or non-positive. The store further clamps a non-positive value to 100.
func parseApprovalsLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultApprovalsLimit
	}
	return n
}
