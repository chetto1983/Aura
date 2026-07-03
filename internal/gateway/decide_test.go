package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// fakeStore captures every decision-fact Insert so the unit tier can assert both the
// SC-4 no-write invariant and the D-01e/D-03 recorded facts without the live PG stack.
type fakeStore struct {
	mu       sync.Mutex
	inserted []toolinvocations.Event
	err      error
}

func (f *fakeStore) Insert(_ context.Context, e toolinvocations.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.inserted = append(f.inserted, e)
	return nil
}

func (f *fakeStore) calls() []toolinvocations.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]toolinvocations.Event, len(f.inserted))
	copy(out, f.inserted)
	return out
}

// readOnlySpec is a non-mutating, non-multiplexed tool → classify returns Safe.
func readOnlySpec() tools.Spec { return tools.Spec{Name: "read_file", Mutating: false} }

// mutatingRiskySpec is a mutating, non-multiplexed tool → classify returns Risky
// (GateRecommended) so it routes to approve.
func mutatingRiskySpec() tools.Spec { return tools.Spec{Name: "shell_exec", Mutating: true} }

func testKey() ReservationKey {
	return ReservationKey{
		ConversationID: "11111111-1111-1111-1111-111111111111",
		RequestID:      "22222222-2222-2222-2222-222222222222",
		ToolCallID:     "call-1",
	}
}

// TestDecideDevNoOp proves SC-4: the dev + local_trusted profiles (and a nil Gateway)
// return Allow as a host-direct no-op and write NO reservation/decision row.
func TestDecideDevNoOp(t *testing.T) {
	for _, profile := range []config.RuntimeProfile{config.ProfileDev, config.ProfileLocalTrusted} {
		store := &fakeStore{}
		g := New(profile, store)
		v, pause, err := g.Decide(context.Background(), mutatingRiskySpec(), nil, testKey())
		if err != nil {
			t.Fatalf("%s: Decide err: %v", profile, err)
		}
		if v.Decision != Allow {
			t.Fatalf("%s: decision = %q, want allow", profile, v.Decision)
		}
		if pause != nil {
			t.Fatalf("%s: unexpected pause", profile)
		}
		if got := len(store.calls()); got != 0 {
			t.Fatalf("%s: wrote %d rows, want 0 (SC-4 no-op)", profile, got)
		}
	}

	// A nil *Gateway is an Allow no-op too (dev-parity for tests/standalone).
	var g *Gateway
	v, _, err := g.Decide(context.Background(), mutatingRiskySpec(), nil, testKey())
	if err != nil || v.Decision != Allow {
		t.Fatalf("nil gateway: got (%v, %v), want allow/nil", v.Decision, err)
	}
}

// TestDecideReadOnlyDecisionFact proves D-01e: a read-only tool under a strict profile
// returns Allow and records a durable decision-fact — as a START row (never an end row,
// so the async observer's real end lands intact), verdict in Meta.
func TestDecideReadOnlyDecisionFact(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	v, pause, err := g.Decide(context.Background(), readOnlySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Allow || v.Tier != scoring.Safe {
		t.Fatalf("verdict = %+v, want allow/safe", v)
	}
	if pause != nil {
		t.Fatal("read-only must not pause")
	}
	rows := store.calls()
	if len(rows) != 1 {
		t.Fatalf("wrote %d rows, want 1 decision-fact", len(rows))
	}
	fact := rows[0]
	if fact.Event != toolinvocations.EventStart {
		t.Fatalf("decision-fact event = %q, want start (an end fact would clobber the observer's real end)", fact.Event)
	}
	if fact.ConversationID != testKey().ConversationID {
		t.Fatalf("decision-fact conv = %q, want originating UUID", fact.ConversationID)
	}
	if fact.Meta["decision_fact"] != true || fact.Meta["gateway_verdict"] != string(Allow) {
		t.Fatalf("decision-fact meta = %v, want allow decision_fact", fact.Meta)
	}
}

// TestDecideMutatingAutoAllowFact proves the mutating-Allow funnel: a mutating but
// NOT-GateRecommended action (skill restore → Normal) auto-allows and records a
// decision-fact (the funnel 35-04 converts to the single Reserve).
func TestDecideMutatingAutoAllowFact(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	args := mustDecideArgs(t, map[string]string{"action": "restore"})
	v, pause, err := g.Decide(context.Background(), skillSpec(), args, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Allow || v.Tier != scoring.Normal {
		t.Fatalf("verdict = %+v, want allow/normal", v)
	}
	if pause != nil {
		t.Fatal("normal mutating must not pause")
	}
	rows := store.calls()
	if len(rows) != 1 || rows[0].Event != toolinvocations.EventStart {
		t.Fatalf("want 1 start decision-fact, got %+v", rows)
	}
}

func mustDecideArgs(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return raw
}
