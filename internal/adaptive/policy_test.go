package adaptive

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type mutablePolicyReader struct {
	policy Policy
	err    error
	reads  int
}

func (r *mutablePolicyReader) CurrentPolicy(context.Context) (Policy, error) {
	r.reads++
	return r.policy, r.err
}

func TestPolicyControllerReadsFreshStateAndDisablesStaleWorkersOnRollback(t *testing.T) {
	reader := &mutablePolicyReader{policy: Policy{
		Epoch: 7, Version: "candidate-7", Mode: PolicyActive, RolloutBPS: 10_000,
	}}
	firstProcess := NewPolicyController(reader)
	secondProcess := NewPolicyController(reader)
	decisionID := uuid.Must(uuid.NewV7())

	before, err := firstProcess.Gate(context.Background(), decisionID)
	if err != nil {
		t.Fatalf("Gate(active): %v", err)
	}
	if !before.Observe || !before.Adapt || before.Epoch != 7 {
		t.Fatalf("active gate = %+v, want observe+adapt at epoch 7", before)
	}

	// A different process performs the distributed kill switch. The first process must
	// not serve a cached active policy on its next decision.
	reader.policy = Policy{
		Epoch: 8, Version: "rollback-8", Mode: PolicyRollback, RolloutBPS: 0,
	}
	after, err := firstProcess.Gate(context.Background(), uuid.Must(uuid.NewV7()))
	if err != nil {
		t.Fatalf("Gate(rollback): %v", err)
	}
	if after.Observe || after.Adapt || after.Epoch != 8 || after.Reason != "rollback" {
		t.Fatalf("rollback gate = %+v, want completely disabled at epoch 8", after)
	}

	other, err := secondProcess.Gate(context.Background(), uuid.Must(uuid.NewV7()))
	if err != nil {
		t.Fatalf("second Gate(rollback): %v", err)
	}
	if other.Adapt || reader.reads != 3 {
		t.Fatalf("second process gate=%+v reads=%d, want fresh read and no adaptation", other, reader.reads)
	}
}

func TestPolicyControllerFailsClosedWhenAuthoritativeStateIsUnavailable(t *testing.T) {
	readErr := errors.New("postgres partition")
	controller := NewPolicyController(&mutablePolicyReader{err: readErr})

	gate, err := controller.Gate(context.Background(), uuid.Must(uuid.NewV7()))
	if !errors.Is(err, readErr) {
		t.Fatalf("Gate error = %v, want %v", err, readErr)
	}
	if gate.Observe || gate.Adapt || gate.Reason != "policy_unavailable" {
		t.Fatalf("unavailable gate = %+v, want baseline-only fail closed", gate)
	}
}

func TestPolicyControllerCanaryAssignmentIsStableAndVersionBound(t *testing.T) {
	reader := &mutablePolicyReader{policy: Policy{
		Epoch: 2, Version: "candidate-a", Mode: PolicyCanary, RolloutBPS: 2500,
	}}
	controller := NewPolicyController(reader)
	decisionID := uuid.MustParse("018f1d21-89ab-7abc-8def-0123456789ab")

	first, err := controller.Gate(context.Background(), decisionID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := controller.Gate(context.Background(), decisionID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Adapt != second.Adapt || !first.Observe || !second.Observe {
		t.Fatalf("same decision assignment changed: first=%+v second=%+v", first, second)
	}

	reader.policy.Version = "candidate-b"
	changed, err := controller.Gate(context.Background(), decisionID)
	if err != nil {
		t.Fatal(err)
	}
	want := canaryAssigned(decisionID, reader.policy.Version, reader.policy.RolloutBPS)
	if changed.Adapt != want {
		t.Fatalf("version-bound assignment = %v, want %v", changed.Adapt, want)
	}
}
