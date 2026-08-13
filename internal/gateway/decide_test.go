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

// fakeStore captures every decision-fact Insert AND every Reserve so the unit tier can
// assert the SC-4 no-write invariant, the D-01e/D-03 recorded facts, and the reservation
// funnel (rows==1 acquire / rows==0 replay / err deny) without the live PG stack.
type fakeStore struct {
	mu       sync.Mutex
	inserted []toolinvocations.Event
	reserved []toolinvocations.Event
	err      error // Insert error

	// Reserve knobs: reserveErr forces the fail-closed deny path; notAcquired forces the
	// rows==0 replay path returning replayEnd (nil ⇒ crash-orphaned in-flight).
	reserveErr  error
	notAcquired bool
	replayEnd   *toolinvocations.Event
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

func (f *fakeStore) Reserve(_ context.Context, start toolinvocations.Event) (bool, *toolinvocations.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reserveErr != nil {
		return false, nil, f.reserveErr
	}
	f.reserved = append(f.reserved, start)
	if f.notAcquired {
		return false, f.replayEnd, nil
	}
	return true, nil, nil
}

func (f *fakeStore) GetEnd(_ context.Context, _, _, _ string) (*toolinvocations.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.replayEnd, nil
}

func (f *fakeStore) calls() []toolinvocations.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]toolinvocations.Event, len(f.inserted))
	copy(out, f.inserted)
	return out
}

func (f *fakeStore) reserves() []toolinvocations.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]toolinvocations.Event, len(f.reserved))
	copy(out, f.reserved)
	return out
}

// readOnlySpec is a non-mutating, non-multiplexed tool → classify returns Safe.
func readOnlySpec() tools.Spec { return tools.Spec{Name: "read_file", Mutating: false} }

// gatedSpec/gatedArgs are a call that actually reaches the approval path, so the tests
// below assert the approval machinery rather than nothing: `skill` with action=delete,
// which classifies Destructive — the ONE tier that stops the turn (see decide.go's gated).
//
// It is deliberately not shell_exec (Normal), not swarm_spawn (Normal), and not a Risky
// call either: Risky no longer gates. If a future change re-widens the gate, these tests
// keep passing and TestDecideRiskyIsNotGated below is what fails.
func gatedSpec() tools.Spec { return tools.Spec{Name: "skill_manage", Mutating: true} }

func gatedArgs() json.RawMessage { return json.RawMessage(`{"action":"delete","name":"x"}`) }

// ordinaryWriteSpec is the other side of that line: a mutating, non-multiplexed tool, which
// is the shape of nearly every write the agent makes (shell_exec, fs_write, an MCP write).
func ordinaryWriteSpec() tools.Spec { return tools.Spec{Name: "shell_exec", Mutating: true} }

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
		v, err := g.Decide(context.Background(), gatedSpec(), gatedArgs(), testKey())
		if err != nil {
			t.Fatalf("%s: Decide err: %v", profile, err)
		}
		if v.Decision != Allow {
			t.Fatalf("%s: decision = %q, want allow", profile, v.Decision)
		}
		if v.ApprovalRequest != nil {
			t.Fatalf("%s: unexpected approval request", profile)
		}
		if got := len(store.calls()); got != 0 {
			t.Fatalf("%s: wrote %d rows, want 0 (SC-4 no-op)", profile, got)
		}
	}

	// A nil *Gateway is an Allow no-op too (dev-parity for tests/standalone).
	var g *Gateway
	v, err := g.Decide(context.Background(), gatedSpec(), gatedArgs(), testKey())
	if err != nil || v.Decision != Allow {
		t.Fatalf("nil gateway: got (%v, %v), want allow/nil", v.Decision, err)
	}
}

// TestDecideOrdinaryWriteIsNotGated pins the classifier's mutating floor: under the STRICTEST
// single-user profile, an ordinary write executes — reserved and audited — without stopping
// the turn for approval.
//
// The regression it guards is a real one that reached a live appliance: the floor was Risky,
// GateRecommended fires from Risky up, so every write the agent made — storing a memory,
// writing a file — required the operator to approve it. A gate that fires on harmless writes
// does not add safety; it teaches an operator to approve without reading. Genuinely dangerous
// calls keep their own escalation and are covered by TestClassifyTable and approve_test.go.
func TestDecideOrdinaryWriteIsNotGated(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	// A responder IS present: the point is that approval is never REQUESTED, not that it
	// could not be delivered.
	ctx := WithResponder(context.Background())

	v, err := g.Decide(ctx, ordinaryWriteSpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Allow {
		t.Fatalf("decision = %q, want allow (an ordinary write must not be gated)", v.Decision)
	}
	if v.Tier != scoring.Normal {
		t.Fatalf("tier = %q, want normal", v.Tier)
	}
	if v.ApprovalRequest != nil {
		t.Fatal("an ordinary write must not mint an approval request")
	}
	// Allowed is not unrecorded: the call still takes its durable reservation, so the
	// difference from the old behaviour is only that the operator is not interrupted.
	reserved := store.reserves()
	if len(reserved) != 1 || reserved[0].Event != toolinvocations.EventStart {
		t.Fatalf("want exactly 1 reservation start for an allowed write, got %+v", reserved)
	}
}

// TestDecideReadOnlyDecisionFact proves D-01e: a read-only tool under a strict profile
// returns Allow and records a durable decision-fact — as a START row (never an end row,
// so the async observer's real end lands intact), verdict in Meta.
func TestDecideReadOnlyDecisionFact(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	v, err := g.Decide(context.Background(), readOnlySpec(), nil, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Allow || v.Tier != scoring.Safe {
		t.Fatalf("verdict = %+v, want allow/safe", v)
	}
	if v.ApprovalRequest != nil {
		t.Fatal("read-only must not request approval")
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

// TestDecideMutatingAutoAllowFact proves the unified reserve funnel: a mutating but
// NOT-GateRecommended action (skill restore → Normal) auto-allows through the SINGLE
// store.Reserve call (rows==1 acquire) — exactly one reservation start row, NO separate
// decision-fact Insert. The reservation start carries the verdict in Meta.
func TestDecideMutatingAutoAllowFact(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	args := mustDecideArgs(t, map[string]string{"action": "restore"})
	v, err := g.Decide(context.Background(), skillSpec(), args, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Decision != Allow || v.Tier != scoring.Normal {
		t.Fatalf("verdict = %+v, want allow/normal", v)
	}
	if v.ApprovalRequest != nil {
		t.Fatal("normal mutating must not request approval")
	}
	if v.Replay != nil {
		t.Fatal("rows==1 acquire must not carry a replay")
	}
	reserved := store.reserves()
	if len(reserved) != 1 || reserved[0].Event != toolinvocations.EventStart {
		t.Fatalf("want 1 reservation start, got %+v", reserved)
	}
	if reserved[0].Meta["reservation"] != true {
		t.Fatalf("reservation start meta = %v, want reservation:true", reserved[0].Meta)
	}
	if got := len(store.calls()); got != 0 {
		t.Fatalf("mutating auto-allow wrote %d decision-fact Inserts, want 0 (reserve is the only durable write)", got)
	}
}

// TestDecideRiskyIsNotGated pins where the approval line now sits: under the STRICTEST
// profile, with a responder present, a RISKY call (skill/create — self-extension) is
// allowed and reserved without stopping the turn. Only Destructive gates.
//
// The regression it guards reached a live appliance: the gate was scoring.GateRecommended
// (Risky OR Destructive), so authoring a skill, running a task the operator had just asked
// for, or any action the classifier could not parse all raised an approval prompt. An
// operator who is asked to approve the expected case learns to answer yes without reading,
// which is precisely what the prompt exists to prevent.
func TestDecideRiskyIsNotGated(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	ctx := WithResponder(context.Background()) // a responder IS available; it is not asked

	args := mustDecideArgs(t, map[string]string{"action": "create", "name": "x"})
	v, err := g.Decide(ctx, gatedSpec(), args, testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Tier != scoring.Risky {
		t.Fatalf("tier = %q, want risky (precondition: this test must exercise the Risky arm)", v.Tier)
	}
	if v.Decision != Allow {
		t.Fatalf("decision = %q, want allow (only Destructive stops the turn)", v.Decision)
	}
	if v.ApprovalRequest != nil {
		t.Fatal("a Risky call must not mint an approval request")
	}
	// Not gated is not unrecorded: it still takes its durable reservation.
	if reserved := store.reserves(); len(reserved) != 1 {
		t.Fatalf("want exactly 1 reservation start for an allowed risky call, got %+v", reserved)
	}
}

// TestDecideDestructiveGates is the other side of that line: skill/delete stops the turn.
func TestDecideDestructiveGates(t *testing.T) {
	g := New(config.ProfileSingleUserHardened, &fakeStore{})
	v, err := g.Decide(WithResponder(context.Background()), gatedSpec(), gatedArgs(), testKey())
	if err != nil {
		t.Fatalf("Decide err: %v", err)
	}
	if v.Tier != scoring.Destructive {
		t.Fatalf("tier = %q, want destructive", v.Tier)
	}
	if v.Decision != Approve || v.ApprovalRequest == nil {
		t.Fatalf("verdict = %+v, want approve + approval request (deletion is the gated case)", v)
	}
}

func TestDecideDestructiveMCPGatesWithoutBlockingOrdinaryMCPWrites(t *testing.T) {
	tests := []struct {
		name         string
		spec         tools.Spec
		wantDecision Decision
		wantTier     scoring.RiskTier
	}{
		{
			name: "external send",
			spec: tools.Spec{
				Name: "whatsapp__send_message", Mutating: true, Destructive: true,
			},
			wantDecision: Approve,
			wantTier:     scoring.Destructive,
		},
		{
			name: "reversible update",
			spec: tools.Spec{
				Name: "calendar__update_event", Mutating: true,
			},
			wantDecision: Allow,
			wantTier:     scoring.Normal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeStore{}
			g := New(config.ProfileSingleUserHardened, store)
			v, err := g.Decide(WithResponder(context.Background()), tt.spec, json.RawMessage(`{}`), testKey())
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if v.Decision != tt.wantDecision || v.Tier != tt.wantTier {
				t.Fatalf("verdict = %+v, want %s/%s", v, tt.wantDecision, tt.wantTier)
			}
			if tt.wantDecision == Allow && len(store.reserves()) != 1 {
				t.Fatalf("ordinary mutation reservations = %d, want 1", len(store.reserves()))
			}
		})
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
