package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/scoring"
)

func acquiringOperations() *fakeOperationRegistry {
	return &fakeOperationRegistry{decision: idempotency.BeginDecision{Decision: idempotency.DecisionAcquired, ClaimToken: 1}}
}

// A scheduled agent_job has no responder: on a strict profile it must not erase memory, whatever
// the text it read asked for (docs/superpowers/specs/2026-10-30-headless-memory-erase-design.md).
func TestAHeadlessRunCannotEraseMemory(t *testing.T) {
	t.Parallel()
	spec := memoryForgetSpec()
	args := json.RawMessage(`{"entity":"throwaway"}`)
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	g.SetOperationRegistry(acquiringOperations())

	v, err := g.Decide(injectionOperationContext(t, spec, args, "erase"), spec, args, testKey())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if v.Decision != Deny || v.Tier != scoring.Destructive || !strings.Contains(v.Reason, "no interactive approver") {
		t.Fatalf("verdict = %+v, want deny/destructive with no interactive approver", v)
	}
	if len(store.reserves()) != 0 {
		t.Fatal("a denied erasure reached the reservation")
	}
}

func TestAHeadlessRunMayCountWhatAnErasureWouldTake(t *testing.T) {
	t.Parallel()
	spec := memoryForgetSpec()
	args := json.RawMessage(`{"entity":"throwaway","dry_run":true}`)
	g := New(config.ProfileSingleUserHardened, &fakeStore{})
	g.SetOperationRegistry(acquiringOperations())

	v, err := g.Decide(injectionOperationContext(t, spec, args, "count"), spec, args, testKey())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if v.Decision != Allow || v.Tier != scoring.Safe || v.ApprovalRequest != nil {
		t.Fatalf("verdict = %+v, want allow/safe with no approval", v)
	}
}

func TestAnInteractiveErasureAsksTheOperator(t *testing.T) {
	t.Parallel()
	spec := memoryForgetSpec()
	args := json.RawMessage(`{"entity":"throwaway"}`)
	g := New(config.ProfileSingleUserHardened, &fakeStore{})

	v, err := g.Decide(WithResponder(context.Background()), spec, args, testKey())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if v.Decision != Approve || v.ApprovalRequest == nil {
		t.Fatalf("verdict = %+v, want an approval request", v)
	}
}
