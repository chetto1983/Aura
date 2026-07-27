package runner

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
)

// ledgerStartOwnershipStore stands in for the durable ledger and reports which event kinds
// this observer tried to write.
type ledgerStartOwnershipStore struct{ kinds []string }

func (s *ledgerStartOwnershipStore) Insert(_ context.Context, e toolinvocations.Event) error {
	s.kinds = append(s.kinds, e.Event)
	return nil
}

func toolEvent(kind string, requestID uuid.UUID) *agent.Event {
	at := time.Now().UTC()
	ti := &agent.ToolInvocation{
		Event:      kind,
		ToolCallID: "call-owned-1",
		ToolName:   "shell_exec",
		Arguments:  `{"command":"echo hi"}`,
		ArgsBytes:  len(`{"command":"echo hi"}`),
		StartedAt:  &at,
	}
	if kind == agent.ToolInvocationEnd {
		ti.EndedAt = &at
		ti.Status = "ok"
		ti.ResultPreview = "hi\n"
	}
	return &agent.Event{RequestID: requestID, Actions: agent.Actions{ToolInvocation: ti}}
}

// TestLedgerStartRowHasExactlyOneWriter pins the ownership rule that a live appliance broke.
//
// The gateway's Reserve INSERTs the `start` row under the UNIQUE (conversation_id,
// request_id, tool_call_id, event_kind) index with ON CONFLICT DO NOTHING and reads the
// rows-affected count AS the idempotency key (GATE-03/04). This observer was writing the same
// row, and winning: the agent yields start events BEFORE executing so the cockpit can show the
// call as running, so its row landed first, and the gateway then read its own conflict as
// "someone already holds this slot" and refused to run the tool. Every mutating tool call
// under a strict profile failed that way — shell_exec, fs_write and skill alike — and before
// the companion gateway fix it failed as a fabricated SUCCESS.
//
// Both directions are asserted because both are load-bearing. Skipping the start under a
// strict gateway is what unbreaks execution; still writing it under dev/local_trusted is what
// keeps SC-4 true, since there the gateway short-circuits before reserving and writes nothing
// at all. That asymmetry is precisely why the defect could only ever appear on a strict
// deployment, and never on the dev stack it was developed against.
func TestLedgerStartRowHasExactlyOneWriter(t *testing.T) {
	for _, tc := range []struct {
		name      string
		profile   config.RuntimeProfile
		wantKinds []string
	}{
		{
			name:      "strict: the gateway reserves, so the observer must not write the start",
			profile:   config.ProfileSingleUserHardened,
			wantKinds: []string{agent.ToolInvocationEnd},
		},
		{
			name:      "dev: the gateway writes nothing, so the observer stays the only writer",
			profile:   config.ProfileDev,
			wantKinds: []string{agent.ToolInvocationStart, agent.ToolInvocationEnd},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
			ledger := &ledgerStartOwnershipStore{}
			r.toolInvocations = ledger
			r.gatewayOwnsToolStarts = gateway.New(tc.profile, ledgerReserveStore{}).OwnsToolStartRows()

			tr := &turnTracker{convID: newConvID(t)}
			requestID := uuid.Must(uuid.NewV7())
			for _, kind := range []string{agent.ToolInvocationStart, agent.ToolInvocationEnd} {
				if err := r.persistEvent(context.Background(), tr, toolEvent(kind, requestID)); err != nil {
					t.Fatalf("persist %s: %v", kind, err)
				}
			}

			if len(ledger.kinds) != len(tc.wantKinds) {
				t.Fatalf("ledger writes = %v, want %v", ledger.kinds, tc.wantKinds)
			}
			for i, want := range tc.wantKinds {
				if ledger.kinds[i] != want {
					t.Fatalf("ledger writes = %v, want %v", ledger.kinds, tc.wantKinds)
				}
			}
		})
	}
}

// ledgerReserveStore satisfies the gateway's ledger seam. OwnsToolStartRows only needs a
// non-nil store plus the profile, so the methods are never called here.
type ledgerReserveStore struct{}

func (ledgerReserveStore) Insert(context.Context, toolinvocations.Event) error { return nil }

func (ledgerReserveStore) Reserve(context.Context, toolinvocations.Event) (bool, *toolinvocations.Event, error) {
	return true, nil, nil
}

func (ledgerReserveStore) GetEnd(context.Context, string, string, string) (*toolinvocations.Event, error) {
	return nil, nil
}
