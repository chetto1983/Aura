package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/approvalgrants"
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
//     askuser.Action* and calls Runner.SubmitAnswer, which also hands back the
//     ResolveDirective the cockpit renders — reaching the FULL three-action model the Runner
//     already implements
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
	mux.HandleFunc("GET /api/approvals/grants", s.handleListApprovalGrants)
	mux.HandleFunc("POST /api/approvals/grants/revoke", s.handleRevokeApprovalGrant)
}

// approvalGrantStore is the narrow seam over the durable "always approve" rows
// (*approvalgrants.Store). It is declared here, at the consumer, per D-A2-02.
type approvalGrantStore interface {
	List(ctx context.Context, identityID string) ([]approvalgrants.Grant, error)
	Revoke(ctx context.Context, identityID, tool, action string) (bool, error)
}

// SetApprovalGrantStore wires the durable grant store. Until set, the grant routes answer
// 503 — the same optional-wiring posture as the pending read.
func (s *Server) SetApprovalGrantStore(store approvalGrantStore) { s.approvalGrants = store }

// approvalGrantItem is the JSON projection of one standing grant. `subject` is the same
// string the approval option named when the operator granted it, so what they revoke reads
// exactly like what they approved.
type approvalGrantItem struct {
	Tool      string `json:"tool"`
	Action    string `json:"action"`
	Subject   string `json:"subject"`
	GrantedAt string `json:"granted_at"`
	GrantedBy string `json:"granted_by,omitempty"`
}

// handleListApprovalGrants returns the AUTHENTICATED principal's standing grants. It is
// owner-scoped like the pending read: a grant is a statement by one principal about their
// own agent, and no request parameter selects whose grants are meant.
func (s *Server) handleListApprovalGrants(w http.ResponseWriter, r *http.Request) {
	if s.approvalGrants == nil {
		http.Error(w, "approval grants not available", http.StatusServiceUnavailable)
		return
	}
	grants, err := s.approvalGrants.List(r.Context(), scopedIdentityID(r.Context()))
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	items := make([]approvalGrantItem, 0, len(grants))
	for _, g := range grants {
		items = append(items, approvalGrantItem{
			Tool:      g.Tool,
			Action:    g.Action,
			Subject:   g.Subject(),
			GrantedAt: g.GrantedAt.UTC().Format(time.RFC3339),
			GrantedBy: g.GrantedBy,
		})
	}
	writeJSON(w, items)
}

// revokeGrantBody is the POST /grants/revoke payload. The subject is sent as its two
// coordinates rather than the rendered string, so the server never has to parse a label
// back into a key.
type revokeGrantBody struct {
	Tool   string `json:"tool"`
	Action string `json:"action"`
}

// handleRevokeApprovalGrant drops one standing grant of the authenticated principal and
// reports whether a row was actually removed, so the cockpit can distinguish "revoked" from
// "it was already gone" instead of showing success over a stale list.
func (s *Server) handleRevokeApprovalGrant(w http.ResponseWriter, r *http.Request) {
	if s.approvalGrants == nil {
		http.Error(w, "approval grants not available", http.StatusServiceUnavailable)
		return
	}
	var body revokeGrantBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if body.Tool == "" {
		http.Error(w, "tool is required", http.StatusBadRequest)
		return
	}
	revoked, err := s.approvalGrants.Revoke(r.Context(), scopedIdentityID(r.Context()), body.Tool, body.Action)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"revoked": revoked})
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
	// Owner-scoped cross-thread pending read (MUSR-01 / APRV-01): only the principal's own
	// pending approvals. The `local` fallback keeps the loopback-dev operator's queue intact.
	pendings, err := s.approvals.ListPendingAllForIdentity(r.Context(), scopedIdentityID(r.Context()), limit)
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
// maps the verb to askuser.Action* (rejecting any other value with 400), and calls
// Runner.SubmitAnswer. The Runner handles decline → declinedContent (agent continues
// informed) and cancel → AutoResolveForConversation internally. An unknown /
// already-resolved token surfaces as ErrPauseNotFound → 404 (APRV-03: a terminated
// pending is never silently lost — the operator gets a clear "gone" rather than a
// false success). Returns 200 with the ResolveDirective projection: the React layer
// re-drives the turn with a no-Resume POST /agent/run only for outcome "continue"
// (continue-after-resume, plan 25-06) — a scheduled approval is already complete when its
// ResumeHook fires, so re-driving it would resume a turn that no longer exists.
func (s *Server) handleResolveApproval(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if _, err := uuid.Parse(token); err != nil {
		http.Error(w, "approval not found", http.StatusNotFound)
		return
	}
	body, err := decodeResolveBody(w, r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	action, ok := resolveAction(body.Action)
	if !ok {
		http.Error(w, "action must be accept, decline, or cancel", http.StatusBadRequest)
		return
	}
	// Owner gate (MUSR-01 / D-06): resolve only the caller's own approval. The atomic
	// resume itself stays the Runner's job (SubmitAnswers → MarkResumedTx, never forked);
	// this gate authorizes it. A miss on the scoped read means the token is not the
	// caller's, and that is 404 whether it is foreign, unknown, or already resolved.
	//
	// It used to be 403-when-foreign, split by an unscoped existence probe. Migration 0089
	// made aura.paused_states fail closed, so no read in the system can see another
	// identity's pause any more — the probe could only ever miss, and keeping it would have
	// been a branch that never fires. The collapse is toward the safer side: a caller learns
	// nothing about tokens they do not own. Skipped only when the approval store is unwired
	// (tests); the daemon always wires it via SetApprovalStore.
	if s.approvals != nil {
		identity := scopedIdentityID(r.Context())
		if _, err := s.approvals.GetByTokenForIdentity(r.Context(), token, identity); err != nil {
			if errors.Is(err, askuser.ErrPauseNotFound) {
				http.Error(w, "approval not found or already resolved", http.StatusNotFound)
				return
			}
			http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
			return
		}
	}
	directive, err := s.run.SubmitAnswer(r.Context(), token, runner.ResponseInput{Action: action, Content: body.Content})
	if err != nil {
		if errors.Is(err, askuser.ErrPauseNotFound) {
			http.Error(w, "approval not found or already resolved", http.StatusNotFound)
			return
		}
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, resolveResponse{Outcome: outcomeString(directive.Outcome), Remaining: directive.Remaining})
}

// resolveResponse is the projection of runner.ResolveDirective the cockpit renders: a semantic
// outcome code (never user prose — the runner is not locale-aware) plus how many pauses remain.
type resolveResponse struct {
	Outcome   string `json:"outcome"`
	Remaining int    `json:"remaining"`
}

// outcomeString names a ResolveOutcome for the wire. The default is "continue" so an outcome
// added runner-side degrades to "the turn goes on" — the behavior the cockpit had before this
// endpoint returned anything at all.
func outcomeString(o runner.ResolveOutcome) string {
	switch o {
	case runner.OutcomeApproved:
		return "approved"
	case runner.OutcomeRejected:
		return "rejected"
	case runner.OutcomePending:
		return "pending"
	case runner.OutcomeTerminated:
		return "terminated"
	case runner.OutcomeContinue:
		return "continue"
	default:
		return "continue"
	}
}

func decodeResolveBody(w http.ResponseWriter, r *http.Request) (resolveBody, error) {
	var body resolveBody
	err := strictDecodeJSON(w, r, &body, decodeOpts{})
	return body, err
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
