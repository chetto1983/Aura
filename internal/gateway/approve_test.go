package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// TestApproveHardenedInteractive proves D-03: single_user_hardened with a
// positively-known responder emits Verdict{Approve} + an ErrAwaitingUserInput{approval}
// carrying a {"type":"gateway_approval",...} ResumeContext, and writes NO row (the
// approve is a pending decision, not a terminal fact).
func TestApproveHardenedInteractive(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	ctx := WithResponder(context.Background())

	v, pause, err := g.Decide(ctx, mutatingRiskySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Approve {
		t.Fatalf("decision = %q, want approve", v.Decision)
	}
	if pause == nil {
		t.Fatal("approve must carry a pause sentinel")
	}
	if pause.Kind != "approval" {
		t.Fatalf("pause kind = %q, want approval", pause.Kind)
	}
	if pause.Priority == 0 {
		t.Fatal("approval priority must be reused from tools.ApprovalPriority, not 0")
	}
	var rc map[string]any
	if err := json.Unmarshal(pause.ResumeContext, &rc); err != nil {
		t.Fatalf("resume context not json: %v", err)
	}
	if rc["type"] != "gateway_approval" {
		t.Fatalf("resume context type = %v, want gateway_approval", rc["type"])
	}
	if got := len(store.calls()); got != 0 {
		t.Fatalf("approve wrote %d rows, want 0", got)
	}
}

// TestApproveProductionDenies proves D-03b: server_production degrades approve to
// deny-with-guidance EVEN WITH a responder present (identity is unverified pre-Phase-36),
// and durably records the degraded_deny terminal fact (D-03 point 1).
func TestApproveProductionDenies(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileServerProduction, store)
	ctx := WithResponder(context.Background()) // even with a responder, production denies

	v, pause, err := g.Decide(ctx, mutatingRiskySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Deny {
		t.Fatalf("decision = %q, want deny", v.Decision)
	}
	if pause != nil {
		t.Fatal("production must not pause in place")
	}
	assertDegradedDenyFact(t, store)
}

// TestApproveHeadlessDenies proves D-03a: a hardened profile with NO positively-known
// responder (a headless swarm/cron run) denies-with-guidance and records the fact.
func TestApproveHeadlessDenies(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	v, pause, err := g.Decide(context.Background(), mutatingRiskySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Deny {
		t.Fatalf("decision = %q, want deny (no responder → default DENY)", v.Decision)
	}
	if pause != nil {
		t.Fatal("headless must not pause in place")
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

	v, pause, err := g.Decide(ctx, mutatingRiskySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Allow {
		t.Fatalf("decision = %q, want allow", v.Decision)
	}
	if v.OperatorID != "op-1" {
		t.Fatalf("operator id = %q, want op-1", v.OperatorID)
	}
	if pause != nil {
		t.Fatal("post-resume approved must not re-pause")
	}
	if got := len(store.calls()); got != 0 {
		t.Fatalf("post-resume approved wrote %d rows, want 0 (marker rides the reservation start)", got)
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
	v, pause, _ := g.Decide(context.Background(), mutatingRiskySpec(), modelArgs, testKey())
	if v.Decision == Approve || v.Decision == Allow {
		t.Fatalf("model self-approval leaked: decision = %q", v.Decision)
	}
	if pause != nil {
		t.Fatal("model-only ctx must not pause for approval")
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
