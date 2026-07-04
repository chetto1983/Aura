package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// TestApproveHardenedInteractive proves D-03: single_user_hardened with a
// positively-known responder and NO ledger hit returns Verdict{Approve} carrying an
// ApprovalRequest tool RESULT (mirroring shellApprovalRequiredResult) — a
// gateway_approval preview with args_sha256, a descriptive question, and the exact
// resume_context the model relays via ask_user. It writes NO row and takes NO
// reservation (the mutating action is WITHHELD until an approval is recorded).
func TestApproveHardenedInteractive(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	ctx := WithResponder(context.Background())

	v, err := g.Decide(ctx, mutatingRiskySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Approve {
		t.Fatalf("decision = %q, want approve", v.Decision)
	}
	if v.ApprovalRequest == nil {
		t.Fatal("approve must carry an ApprovalRequest tool result (mirroring shell_exec)")
	}
	var preview map[string]any
	if err := json.Unmarshal([]byte(v.ApprovalRequest.Preview), &preview); err != nil {
		t.Fatalf("approval-required preview not json: %v", err)
	}
	if preview["error"] != "gateway_approval_required" {
		t.Fatalf("preview error = %v, want gateway_approval_required", preview["error"])
	}
	if s, _ := preview["args_sha256"].(string); s == "" {
		t.Fatal("preview must carry a non-empty args_sha256")
	}
	if q, _ := preview["question"].(string); q == "" {
		t.Fatal("preview must carry a non-empty descriptive question")
	}
	rc, ok := preview["resume_context"].(map[string]any)
	if !ok {
		t.Fatalf("resume_context missing/not an object: %v", preview["resume_context"])
	}
	if rc["type"] != "gateway_approval" {
		t.Fatalf("resume context type = %v, want gateway_approval", rc["type"])
	}
	if rc["args_sha256"] != preview["args_sha256"] {
		t.Fatal("resume_context args_sha256 must match the top-level fingerprint")
	}
	if got := len(store.calls()); got != 0 {
		t.Fatalf("approve wrote %d Insert rows, want 0", got)
	}
	if got := len(store.reserves()); got != 0 {
		t.Fatalf("approve reserved %d rows, want 0 (withheld until an approval is recorded)", got)
	}
}

// TestApproveHardenedLedgerReEntry proves D-03 point 2's PRODUCTION carrier: after the
// resume hook records the operator's approval via RecordResolvedApproval (the cross-turn
// ledger, NOT a hand-set ctx), a re-drive with the SAME args re-enters routeApprove,
// Consumes the one-shot approval keyed on the recomputed fingerprint, and returns
// Verdict{Allow, OperatorID} through the SINGLE reservation — with NO competing Insert.
func TestApproveHardenedLedgerReEntry(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	ctx := WithResponder(context.Background())
	args := mustDecideArgs(t, map[string]any{"goals": []string{"build x"}})

	g.RecordResolvedApproval(testKey().ConversationID, mutatingRiskySpec().Name,
		gatewayArgsFingerprint(args), ResolvedApproval{Approved: true, OperatorID: "op-led"})

	v, err := g.Decide(ctx, mutatingRiskySpec(), args, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Allow || v.OperatorID != "op-led" {
		t.Fatalf("verdict = %+v, want allow/op-led (ledger re-entry)", v)
	}
	if v.ApprovalRequest != nil {
		t.Fatal("a ledger-approved re-emit must not re-request approval")
	}
	reserved := store.reserves()
	if len(reserved) != 1 || reserved[0].Meta["operator_id"] != "op-led" {
		t.Fatalf("want exactly 1 reservation start with operator_id in Meta, got %+v", reserved)
	}
	if got := len(store.calls()); got != 0 {
		t.Fatalf("ledger re-entry wrote %d competing Inserts, want 0", got)
	}
	// The approval is one-shot: a second re-drive with the same args finds no ledger hit
	// and re-issues the approval-required result (fail-closed).
	v2, err := g.Decide(ctx, mutatingRiskySpec(), args, testKey())
	if err != nil {
		t.Fatalf("second Decide err: %v", err)
	}
	if v2.Decision != Approve || v2.ApprovalRequest == nil {
		t.Fatalf("second re-drive = %+v, want approve + approval request (one-shot consumed)", v2)
	}
}

// TestApproveProductionDenies proves D-03b: server_production degrades approve to
// deny-with-guidance EVEN WITH a responder present (identity is unverified pre-Phase-36),
// and durably records the degraded_deny terminal fact (D-03 point 1).
func TestApproveProductionDenies(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileServerProduction, store)
	ctx := WithResponder(context.Background()) // even with a responder, production denies

	v, err := g.Decide(ctx, mutatingRiskySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Deny {
		t.Fatalf("decision = %q, want deny", v.Decision)
	}
	if v.ApprovalRequest != nil {
		t.Fatal("production must not return an approval request")
	}
	assertDegradedDenyFact(t, store)
}

// TestApproveHeadlessDenies proves D-03a: a hardened profile with NO positively-known
// responder (a headless swarm/cron run) denies-with-guidance and records the fact.
func TestApproveHeadlessDenies(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	v, err := g.Decide(context.Background(), mutatingRiskySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Deny {
		t.Fatalf("decision = %q, want deny (no responder → default DENY)", v.Decision)
	}
	if v.ApprovalRequest != nil {
		t.Fatal("headless must not return an approval request")
	}
	assertDegradedDenyFact(t, store)
}

// TestApprovePostResumeAllow proves D-03 point 2: a resume carrying the operator's
// resolution returns Verdict{Allow, OperatorID} and writes ZERO rows of its own — the
// executed marker rides 35-04's single reservation start Meta, never a competing Insert.
func TestApprovePostResumeAllow(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	ctx := WithResolvedApproval(WithResponder(context.Background()),
		ResolvedApproval{Approved: true, OperatorID: "op-1"})

	v, err := g.Decide(ctx, mutatingRiskySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Allow {
		t.Fatalf("decision = %q, want allow", v.Decision)
	}
	if v.OperatorID != "op-1" {
		t.Fatalf("operator id = %q, want op-1", v.OperatorID)
	}
	if v.ApprovalRequest != nil {
		t.Fatal("post-resume approved must not re-request approval")
	}
	if got := len(store.calls()); got != 0 {
		t.Fatalf("post-resume approved wrote %d Insert rows, want 0 (marker rides the reservation start)", got)
	}
}

// TestApproveIsHostSideOnly proves D-03c: a model-only ctx (no host-set responder
// marker) can NEVER obtain Approve or Allow for a gated mutating call — the responder
// signal is host/policy-side (WithResponder), not derivable from model-supplied args.
func TestApproveIsHostSideOnly(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	// A crafted "approve"-shaped arg blob must not flip the verdict: only the host's
	// WithResponder marker (absent here) can reach the interactive branch.
	modelArgs := mustDecideArgs(t, map[string]any{"decision": "approve", "approved": true})
	v, _ := g.Decide(context.Background(), mutatingRiskySpec(), modelArgs, testKey())
	if v.Decision == Approve || v.Decision == Allow {
		t.Fatalf("model self-approval leaked: decision = %q", v.Decision)
	}
	if v.ApprovalRequest != nil {
		t.Fatal("model-only ctx (no responder) must not return an approval request")
	}
}

// assertDegradedDenyFact asserts the fake store captured exactly one degraded_deny
// terminal decision-fact: an END row (event_kind='end', status='error'), reason=no_approver
// in Meta, keyed on the ORIGINATING conversation UUID (D-03 point 1 / GATE-01).
func assertDegradedDenyFact(t *testing.T, store *fakeStore) {
	t.Helper()
	rows := store.calls()
	if len(rows) != 1 {
		t.Fatalf("degraded_deny wrote %d rows, want 1 terminal fact", len(rows))
	}
	f := rows[0]
	if f.Event != toolinvocations.EventEnd {
		t.Fatalf("degraded_deny event = %q, want end (the only legal terminal shape)", f.Event)
	}
	if f.Status != "error" {
		t.Fatalf("degraded_deny status = %q, want error", f.Status)
	}
	if f.ConversationID != testKey().ConversationID {
		t.Fatalf("degraded_deny conv = %q, want originating UUID", f.ConversationID)
	}
	if f.Meta["degraded_deny"] != true || f.Meta["reason"] != "no_approver" {
		t.Fatalf("degraded_deny meta = %v, want degraded_deny/no_approver", f.Meta)
	}
}
