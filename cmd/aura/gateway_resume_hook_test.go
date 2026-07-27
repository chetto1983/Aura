package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/runner"
)

// gatewayApprovalPending drives one real gateway.Decide to obtain the authentic
// approval-required result, then projects its resume_context + question onto the
// askuser.Pending the resume hook receives — so the recorded args_sha256 matches the
// fingerprint routeApprove recomputes on the re-drive (fidelity, not a hand-fabricated
// sha). It returns the pending plus the spec/args/key so the caller can re-Decide to
// OBSERVE whether the hook recorded the approval (a re-entry Allow ⇒ recorded).
func gatewayApprovalPending(t *testing.T, gw *gateway.Gateway, convID string) (askuser.Pending, tools.Spec, json.RawMessage, gateway.ReservationKey) {
	t.Helper()
	// skill/delete — Destructive, the only tier that stops the turn — so this seed actually
	// reaches routeApprove instead of being allowed outright.
	spec := tools.Spec{Name: "skill", Mutating: true}
	args := json.RawMessage(`{"action":"delete","name":"obsolete-skill"}`)
	key := gateway.ReservationKey{ConversationID: convID, RequestID: "req-1", ToolCallID: "call-1"}

	v, err := gw.Decide(gateway.WithResponder(context.Background()), spec, args, key)
	if err != nil {
		t.Fatalf("seed Decide err: %v", err)
	}
	if v.Decision != gateway.Approve || v.ApprovalRequest == nil {
		t.Fatalf("seed Decide = %+v, want approve + approval request", v)
	}
	var preview struct {
		Question      string          `json:"question"`
		ResumeContext json.RawMessage `json:"resume_context"`
	}
	if err := json.Unmarshal([]byte(v.ApprovalRequest.Preview), &preview); err != nil {
		t.Fatalf("seed preview not json: %v", err)
	}
	return askuser.Pending{
		ConversationID: convID,
		Kind:           tools.KindApproval,
		Question:       preview.Question,
		ResumeContext:  preview.ResumeContext,
	}, spec, args, key
}

// reDecideAllows reports whether a re-drive of the exact call now returns Allow (i.e. the
// hook recorded the operator's approval and routeApprove Consumed it). A nil store is
// fine: reserve short-circuits to Allow when g.store == nil.
func reDecideAllows(t *testing.T, gw *gateway.Gateway, spec tools.Spec, args json.RawMessage, key gateway.ReservationKey) gateway.Verdict {
	t.Helper()
	v, err := gw.Decide(gateway.WithResponder(context.Background()), spec, args, key)
	if err != nil {
		t.Fatalf("re-Decide err: %v", err)
	}
	return v
}

// TestGatewayResumeHookRecordsAcceptedApproval proves an accept on a gateway_approval
// pending records the operator's ResolvedApproval, so the re-drive re-enters Decide and
// returns Verdict{Allow, OperatorID:"local"} (D-03 point 2, single-principal owner).
func TestGatewayResumeHookRecordsAcceptedApproval(t *testing.T) {
	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	hook := newGatewayResumeHook(gw)
	pending, spec, args, key := gatewayApprovalPending(t, gw, "conv-accept")

	if err := hook(context.Background(), pending, runner.ResponseInput{Action: askuser.ActionAccept}); err != nil {
		t.Fatalf("hook: %v", err)
	}
	v := reDecideAllows(t, gw, spec, args, key)
	if v.Decision != gateway.Allow || v.OperatorID != "local" {
		t.Fatalf("re-drive = %+v, want allow/local (accepted approval recorded)", v)
	}
}

// TestGatewayResumeHookIgnoresNonAccept proves decline/cancel/wrong-type/non-approval-kind
// record NOTHING — the re-drive still returns Approve (fail-closed: the tool stays
// withheld until an accept is recorded).
func TestGatewayResumeHookIgnoresNonAccept(t *testing.T) {
	for _, tc := range []struct {
		name          string
		action        string
		kind          string
		mangleContext bool // replace the real gateway_approval context with a wrong-type one
	}{
		{"decline", askuser.ActionDecline, tools.KindApproval, false},
		{"cancel", askuser.ActionCancel, tools.KindApproval, false},
		{"wrong type", askuser.ActionAccept, tools.KindApproval, true},
		{"not approval kind", askuser.ActionAccept, tools.KindClarification, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := gateway.New(config.ProfileSingleUserHardened, nil)
			hook := newGatewayResumeHook(gw)
			pending, spec, args, key := gatewayApprovalPending(t, gw, "conv-"+strings.ReplaceAll(tc.name, " ", "-"))
			pending.Kind = tc.kind
			if tc.mangleContext {
				pending.ResumeContext = rawJSON(t, map[string]string{"type": "skill_approval", "skill_name": "x"})
			}
			if err := hook(context.Background(), pending, runner.ResponseInput{Action: tc.action}); err != nil {
				t.Fatalf("hook: %v", err)
			}
			if v := reDecideAllows(t, gw, spec, args, key); v.Decision != gateway.Approve {
				t.Fatalf("re-drive = %q, want approve (nothing recorded for %s)", v.Decision, tc.name)
			}
		})
	}
}

// TestGatewayResumeHookRejectsMalformedContext proves a gateway_approval accept missing
// the tool or args_sha256 is a hard error (the hook cannot record an unkeyed approval).
func TestGatewayResumeHookRejectsMalformedContext(t *testing.T) {
	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	hook := newGatewayResumeHook(gw)
	for _, tc := range []struct {
		name string
		rc   map[string]string
	}{
		{"missing tool", map[string]string{"type": "gateway_approval", "args_sha256": "abc"}},
		{"missing args_sha256", map[string]string{"type": "gateway_approval", "tool": "skill"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := hook(context.Background(), askuser.Pending{
				ConversationID: "conv-x",
				Kind:           tools.KindApproval,
				ResumeContext:  rawJSON(t, tc.rc),
			}, runner.ResponseInput{Action: askuser.ActionAccept})
			if err == nil {
				t.Fatalf("%s: hook accepted a malformed gateway_approval context", tc.name)
			}
		})
	}
}

// TestGatewayResumeHookRejectsMismatchedQuestion is the WR-03 adversarial negative that
// machine-checks CR-01: an ask_user relay carrying the REAL resume_context (matching fp)
// but a DIFFERENT, benign question must be REJECTED. The hook returns a question-mismatch
// error, records NOTHING, and the re-drive stays Approve (the tool is withheld — the
// operator could not be tricked into authorizing an action they were never shown). Before
// the CR-01 fix this drove the confused-deputy bypass (the accept was recorded blindly).
func TestGatewayResumeHookRejectsMismatchedQuestion(t *testing.T) {
	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	hook := newGatewayResumeHook(gw)
	// Seed a REAL challenge (the gateway records it on issue) + the authentic resume_context.
	pending, spec, args, key := gatewayApprovalPending(t, gw, "conv-adv")
	// The model relays the REAL resume_context (fp still matches) but a benign, false question.
	pending.Question = "Save your meeting notes?"

	err := hook(context.Background(), pending, runner.ResponseInput{Action: askuser.ActionAccept})
	if err == nil {
		t.Fatal("a mismatched/benign relayed question must be rejected (CR-01)")
	}
	if !strings.Contains(err.Error(), "question mismatch") {
		t.Fatalf("error = %v, want a question-mismatch error", err)
	}
	if v := reDecideAllows(t, gw, spec, args, key); v.Decision != gateway.Approve {
		t.Fatalf("re-drive = %q, want approve (nothing recorded — the tool stays withheld)", v.Decision)
	}
}

// TestGatewayResumeHookNilGateway proves a nil gateway yields a nil hook (chainResumeHooks
// filters it out), mirroring newShellResumeHook.
func TestGatewayResumeHookNilGateway(t *testing.T) {
	if newGatewayResumeHook(nil) != nil {
		t.Fatal("newGatewayResumeHook(nil) must return a nil hook")
	}
}

// TestGatewayResumeHookInChain proves the gateway hook fires alongside the shell/skill
// hooks in the chained composition (the production wiring in chat_boot.go), recording the
// accept so the re-drive Allows.
func TestGatewayResumeHookInChain(t *testing.T) {
	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	var shellFired bool
	chain := chainResumeHooks(
		func(context.Context, askuser.Pending, runner.ResponseInput) error { shellFired = true; return nil },
		newGatewayResumeHook(gw),
	)
	pending, spec, args, key := gatewayApprovalPending(t, gw, "conv-chain")

	if err := chain(context.Background(), pending, runner.ResponseInput{Action: askuser.ActionAccept}); err != nil {
		t.Fatalf("chain: %v", err)
	}
	if !shellFired {
		t.Fatal("chain must run the sibling hook too")
	}
	if v := reDecideAllows(t, gw, spec, args, key); v.Decision != gateway.Allow {
		t.Fatal("chained gateway hook must record the accepted approval")
	}
}
