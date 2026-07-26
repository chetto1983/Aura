package adaptive

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

func TestPolicyControllerGateModes(t *testing.T) {
	tests := []struct {
		name    string
		mode    PolicyMode
		observe bool
	}{
		{name: "off is disabled", mode: PolicyOff},
		{name: "rollback is disabled", mode: PolicyRollback},
		{name: "shadow is observe only", mode: PolicyShadow, observe: true},
		{name: "canary is observe only", mode: PolicyCanary, observe: true},
		{name: "active is observe only", mode: PolicyActive, observe: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy := Policy{
				Epoch: 4, Version: "candidate-4", Mode: tt.mode, RolloutBPS: 10_000,
			}
			gate, err := NewPolicyController(&mutablePolicyReader{policy: policy}).Gate(t.Context())
			if err != nil {
				t.Fatalf("Gate(%s): %v", tt.mode, err)
			}
			want := PolicyGate{
				Epoch: 4, PolicyVersion: "candidate-4", Observe: tt.observe,
				Reason: string(tt.mode),
			}
			if gate != want {
				t.Fatalf("Gate(%s) = %+v, want %+v", tt.mode, gate, want)
			}
		})
	}
}

func TestPolicyControllerReadsFreshState(t *testing.T) {
	reader := &mutablePolicyReader{policy: Policy{
		Epoch: 7, Version: "candidate-7", Mode: PolicyActive, RolloutBPS: 10_000,
	}}
	controller := NewPolicyController(reader)

	before, err := controller.Gate(t.Context())
	if err != nil {
		t.Fatalf("Gate(active): %v", err)
	}
	reader.policy = Policy{
		Epoch: 8, Version: "rollback-8", Mode: PolicyRollback, RolloutBPS: 0,
	}
	after, err := controller.Gate(t.Context())
	if err != nil {
		t.Fatalf("Gate(rollback): %v", err)
	}
	if !before.Observe || before.Epoch != 7 || after.Observe || after.Epoch != 8 ||
		after.Reason != string(PolicyRollback) || reader.reads != 2 {
		t.Fatalf("gates before=%+v after=%+v reads=%d, want fresh rollback state", before, after, reader.reads)
	}
}

func TestPolicyControllerFailsClosedWhenAuthoritativeStateIsUnavailable(t *testing.T) {
	readErr := errors.New("postgres partition")
	gate, err := NewPolicyController(&mutablePolicyReader{err: readErr}).Gate(t.Context())
	if !errors.Is(err, readErr) {
		t.Fatalf("Gate error = %v, want %v", err, readErr)
	}
	want := PolicyGate{Reason: "policy_unavailable"}
	if gate != want {
		t.Fatalf("unavailable gate = %+v, want %+v", gate, want)
	}
}

func TestPolicyControllerFailsClosedForUnknownMode(t *testing.T) {
	gate, err := NewPolicyController(&mutablePolicyReader{policy: Policy{
		Epoch: 9, Version: "candidate-9", Mode: PolicyMode("unknown"),
	}}).Gate(t.Context())
	if err == nil || !strings.Contains(err.Error(), "unknown adaptive policy mode") {
		t.Fatalf("Gate error = %v, want unknown-mode error", err)
	}
	want := PolicyGate{Epoch: 9, PolicyVersion: "candidate-9", Reason: "invalid_policy"}
	if gate != want {
		t.Fatalf("unknown-mode gate = %+v, want %+v", gate, want)
	}
}

func TestPolicyStoreRejectsInvalidEvidenceTransitionBeforeDatabase(t *testing.T) {
	store := &PolicyStore{pool: new(pgxpool.Pool)}
	_, err := store.ApplyEvidenceTransition(
		context.Background(), uuid.Nil, -1, uuid.Nil, 10_001, "",
	)
	if err == nil || !strings.Contains(err.Error(), "transition is invalid") {
		t.Fatalf("ApplyEvidenceTransition() error = %v, want invalid request", err)
	}
}
