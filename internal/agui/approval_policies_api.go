package agui

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/approvalpolicies"
)

// approval_policies_api.go serves the narrowing half of the approval model (prd.md §5, "A
// tool policy per identity", 2026-10-09): the authenticated principal's own ask/deny
// policies. Like the grant routes it is owner-scoped with no identity parameter — a policy
// is a statement by one principal about their own agent. Setting another identity's policy
// is the CLI's job (`aura gateway policy`), run by the root operator.

// approvalPolicyStore is the narrow seam over *approvalpolicies.Store, declared at the
// consumer per D-A2-02.
type approvalPolicyStore interface {
	Set(ctx context.Context, identityID, tool, action string, policy approvalpolicies.Policy, setBy string) error
	List(ctx context.Context, identityID string) ([]approvalpolicies.Row, error)
	Clear(ctx context.Context, identityID, tool, action string) (bool, error)
}

// SetApprovalPolicyStore wires the durable policy store. Until set, the policy routes answer
// 503, the same optional-wiring posture as the grant routes.
func (s *Server) SetApprovalPolicyStore(store approvalPolicyStore) { s.approvalPolicies = store }

func (s *Server) registerApprovalPolicyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/approvals/policies", s.handleListApprovalPolicies)
	mux.HandleFunc("PUT /api/approvals/policies", s.handleSetApprovalPolicy)
	mux.HandleFunc("POST /api/approvals/policies/clear", s.handleClearApprovalPolicy)
}

// approvalPolicyItem is the JSON projection of one policy. `subject` is rendered by the same
// function the grants and the approval prompt use, so a policy reads like what it outranks.
type approvalPolicyItem struct {
	Tool    string `json:"tool"`
	Action  string `json:"action"`
	Subject string `json:"subject"`
	Policy  string `json:"policy"`
	SetAt   string `json:"set_at"`
	SetBy   string `json:"set_by,omitempty"`
}

func (s *Server) handleListApprovalPolicies(w http.ResponseWriter, r *http.Request) {
	if s.approvalPolicies == nil {
		http.Error(w, "tool policies not available", http.StatusServiceUnavailable)
		return
	}
	rows, err := s.approvalPolicies.List(r.Context(), scopedIdentityID(r.Context()))
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	items := make([]approvalPolicyItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, approvalPolicyItem{
			Tool:    row.Tool,
			Action:  row.Action,
			Subject: row.Subject(),
			Policy:  string(row.Policy),
			SetAt:   row.SetAt.UTC().Format(time.RFC3339),
			SetBy:   row.SetBy,
		})
	}
	writeJSON(w, items)
}

// handleSetApprovalPolicy records ask or deny for one subject. Setting ask also revokes a
// standing "always" grant on the same subject, AFTER the policy is written: the policy
// already outranks the grant in the gateway, so a failure between the two writes leaves a
// stale grant that changes nothing, and the revoke keeps the standing-approvals list honest.
func (s *Server) handleSetApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	if s.approvalPolicies == nil {
		http.Error(w, "tool policies not available", http.StatusServiceUnavailable)
		return
	}
	body, ok := decodeApprovalSubject(w, r)
	if !ok {
		return
	}
	policy, err := approvalpolicies.ParsePolicy(body.Policy)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_policy"})
		return
	}
	identityID := scopedIdentityID(r.Context())
	if err := s.approvalPolicies.Set(r.Context(), identityID, body.Tool, body.Action, policy, identityID); err != nil {
		if errors.Is(err, approvalpolicies.ErrInvalidPolicy) {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_policy"})
			return
		}
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	revokedGrant := false
	if policy == approvalpolicies.PolicyAsk && s.approvalGrants != nil {
		revokedGrant, err = s.approvalGrants.Revoke(r.Context(), identityID, body.Tool, body.Action)
		if err != nil {
			http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]bool{"set": true, "revoked_grant": revokedGrant})
}

func (s *Server) handleClearApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	if s.approvalPolicies == nil {
		http.Error(w, "tool policies not available", http.StatusServiceUnavailable)
		return
	}
	removeApprovalSubject(w, r, "cleared", s.approvalPolicies.Clear)
}
