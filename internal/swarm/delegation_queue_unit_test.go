package swarm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/documents"
)

// delegation_queue_unit_test.go is the daemon-free half of the delegation queue's
// coverage (no build tag, no Postgres): the config resolvers every claim-loop caller
// depends on, and the payload codec that decides whether a claimed row is workable at
// all. The claim loop's own runtime behaviour (ProcessOnce, deliverSuccess,
// maintainLease, Run, processJob) lives in delegation_queue_lifecycle_test.go, split out
// to stay under CLAUDE.md's 600-LOC ceiling. The claim/lease/retry behaviour against a
// real queue lives in delegation_queue_test.go (db_integration).

// TestDelegationClaimLoopResolversFallBackToBuiltins pins both legs of every
// resolver. A zero field must resolve to the package builtin rather than to a zero
// duration or batch: a zero PollInterval would spin the loop, a zero LeaseDuration
// would make every claim instantly reclaimable by another worker, and a zero
// BatchSize would claim nothing forever.
func TestDelegationClaimLoopResolversFallBackToBuiltins(t *testing.T) {
	zero := &DelegationClaimLoop{}
	if got := zero.workerID(); got != defaultDelegationWorkerID {
		t.Errorf("workerID() = %q, want %q", got, defaultDelegationWorkerID)
	}
	if got, want := zero.leaseDuration(), time.Duration(defaultDelegationLeaseSec)*time.Second; got != want {
		t.Errorf("leaseDuration() = %v, want %v", got, want)
	}
	if got := zero.pollInterval(); got != defaultDelegationPollInterval {
		t.Errorf("pollInterval() = %v, want %v", got, defaultDelegationPollInterval)
	}
	if got := zero.batchSize(); got != defaultDelegationBatchSize {
		t.Errorf("batchSize() = %d, want %d", got, defaultDelegationBatchSize)
	}
	if got := zero.retryBackoff(); got != defaultDelegationRetryBackoff {
		t.Errorf("retryBackoff() = %v, want %v", got, defaultDelegationRetryBackoff)
	}

	set := &DelegationClaimLoop{
		WorkerID:      "explicit-worker",
		LeaseDuration: 7 * time.Second,
		PollInterval:  11 * time.Second,
		BatchSize:     13,
		RetryBackoff:  17 * time.Second,
	}
	if got := set.workerID(); got != "explicit-worker" {
		t.Errorf("workerID() = %q, want the explicit value", got)
	}
	if got := set.leaseDuration(); got != 7*time.Second {
		t.Errorf("leaseDuration() = %v, want the explicit value", got)
	}
	if got := set.pollInterval(); got != 11*time.Second {
		t.Errorf("pollInterval() = %v, want the explicit value", got)
	}
	if got := set.batchSize(); got != 13 {
		t.Errorf("batchSize() = %d, want the explicit value", got)
	}
	if got := set.retryBackoff(); got != 17*time.Second {
		t.Errorf("retryBackoff() = %v, want the explicit value", got)
	}
}

// TestDelegationPayloadFromJobRequiresGoalAndConversation covers the decode that
// stands between a claimed row and a spawned worker. A row missing either field is
// unworkable, and saying so by name here is what keeps the failure a dead-letter with
// a reason rather than a worker started against an empty goal.
func TestDelegationPayloadFromJobRequiresGoalAndConversation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"missing goal", map[string]any{"conversation_id": "conv-1", "fanout_key": "f-test"}, "missing goal"},
		{"empty goal", map[string]any{"goal": "", "conversation_id": "conv-1", "fanout_key": "f-test"}, "missing goal"},
		{"missing conversation", map[string]any{"goal": "do the thing", "fanout_key": "f-test"}, "missing conversation_id"},
		{"missing fanout", map[string]any{"goal": "do the thing", "conversation_id": "conv-1"}, "missing fanout_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := delegationPayloadFromJob(documents.IngestionJob{Payload: tc.payload})
			if err == nil {
				t.Fatalf("delegationPayloadFromJob(%v) = nil error, want %q", tc.payload, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestDelegationPayloadFromJobDecodesAWorkableRow is the positive leg: a row carrying
// both required fields round-trips into the struct the claim loop hands to runChild.
func TestDelegationPayloadFromJobDecodesAWorkableRow(t *testing.T) {
	job := documents.IngestionJob{Payload: map[string]any{
		"goal":            "summarise the inbox",
		"conversation_id": "conv-42",
		"fanout_key":      "f-test",
	}}
	p, err := delegationPayloadFromJob(job)
	if err != nil {
		t.Fatalf("delegationPayloadFromJob: %v", err)
	}
	if p.Goal != "summarise the inbox" {
		t.Errorf("Goal = %q, want the payload goal", p.Goal)
	}
	if p.ConversationID != "conv-42" {
		t.Errorf("ConversationID = %q, want the payload conversation", p.ConversationID)
	}
	if p.FanoutKey != "f-test" {
		t.Errorf("FanoutKey = %q, want f-test", p.FanoutKey)
	}
}

// TestDelegationPayloadFromJobRejectsAMistypedRow covers the decode-failure branch:
// a payload whose field types do not match the struct is a decode error, not a
// silently zeroed Goal that would then fail the missing-goal check for the wrong
// reason.
func TestDelegationPayloadFromJobRejectsAMistypedRow(t *testing.T) {
	job := documents.IngestionJob{Payload: map[string]any{"goal": 42}}
	_, err := delegationPayloadFromJob(job)
	if err == nil {
		t.Fatal("delegationPayloadFromJob(mistyped goal) = nil error, want a decode error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("error = %v, want it to name the decode failure", err)
	}
}

// TestDelegationPayloadMapRoundTrips pins the enqueue-side encoding: the map written
// to the job row must carry the same two fields the claim side requires, or a job
// would be enqueued that can never be decoded.
func TestDelegationPayloadMapRoundTrips(t *testing.T) {
	m, err := delegationPayloadMap(DelegationPayload{Goal: "g", ConversationID: "c", FanoutKey: "f-test"})
	if err != nil {
		t.Fatalf("delegationPayloadMap: %v", err)
	}
	back, err := delegationPayloadFromJob(documents.IngestionJob{Payload: m})
	if err != nil {
		t.Fatalf("round trip failed to decode: %v", err)
	}
	if back.Goal != "g" || back.ConversationID != "c" {
		t.Fatalf("round trip = %+v, want the original goal and conversation", back)
	}
}

// TestMaxDepthFallsBackOnUnsetAndUnparseable covers all three legs of the
// AURA_SWARM_MAX_DEPTH read. An unparseable value must fall back rather than panic or
// yield 0 -- a 0 cap would reject every spawn, turning a typo into a silent outage.
func TestMaxDepthFallsBackOnUnsetAndUnparseable(t *testing.T) {
	t.Setenv("AURA_SWARM_MAX_DEPTH", "")
	if got := maxDepth(); got != defaultMaxDepth {
		t.Errorf("maxDepth() with the var empty = %d, want %d", got, defaultMaxDepth)
	}
	t.Setenv("AURA_SWARM_MAX_DEPTH", "not-a-number")
	if got := maxDepth(); got != defaultMaxDepth {
		t.Errorf("maxDepth() with an unparseable value = %d, want %d", got, defaultMaxDepth)
	}
	t.Setenv("AURA_SWARM_MAX_DEPTH", "5")
	if got := maxDepth(); got != 5 {
		t.Errorf("maxDepth() with an explicit value = %d, want 5", got)
	}
}

// TestDelegationChildID (51-11) pins determinism, per-index distinctness, and
// validatePathSegment acceptance -- including a hostile-looking goal string
// fed through the idempotency key it derives from, which can never leak into
// the id's own fixed alphabet.
func TestDelegationChildID(t *testing.T) {
	key := delegationIdempotencyKey("identity-1", "conv-1", "run-1", 0, "goal text")

	a := delegationChildID(key, 0)
	b := delegationChildID(key, 0)
	if a != b {
		t.Fatalf("delegationChildID is not deterministic over the same (key, index): %q != %q", a, b)
	}

	c := delegationChildID(key, 1)
	if a == c {
		t.Fatalf("delegationChildID did not vary with goal index (same key): both = %q", a)
	}

	hostileGoal := "../../etc/passwd\x00\n<script>alert(1)</script> " + strings.Repeat("x", 500)
	hostileKey := delegationIdempotencyKey("identity-1", "conv-1", "run-1", 2, hostileGoal)
	for i := range 3 {
		id := delegationChildID(hostileKey, i)
		if err := validatePathSegment("child id", id); err != nil {
			t.Fatalf("delegationChildID(%d) over a hostile goal = %q, rejected by validatePathSegment: %v", i, id, err)
		}
	}
}

// TestDelegationFanoutKey (51-11) pins the ONE key every goal of a single
// swarm_spawn call shares, and that the key changes with anything that makes
// it a genuinely different fan-out.
func TestDelegationFanoutKey(t *testing.T) {
	goals := []string{"goal a", "goal b"}

	a := delegationFanoutKey("identity-1", "conv-1", "run-1", goals)
	b := delegationFanoutKey("identity-1", "conv-1", "run-1", goals)
	if a != b {
		t.Fatalf("delegationFanoutKey is not deterministic over identical inputs: %q != %q", a, b)
	}

	diffGoals := delegationFanoutKey("identity-1", "conv-1", "run-1", []string{"goal a", "goal c"})
	if a == diffGoals {
		t.Fatal("delegationFanoutKey did not vary when the goal list changed")
	}

	diffConv := delegationFanoutKey("identity-1", "conv-2", "run-1", goals)
	if a == diffConv {
		t.Fatal("delegationFanoutKey did not vary when the conversation changed -- two swarm_spawn calls in different conversations must be different fan-outs")
	}
}

// TestEnqueueDelegationQueuedResultShape (51-11) decodes EnqueueDelegation's
// return value into the typed struct plan 51-12a's own decoder expects,
// asserting the exact key set and that Workers round-trips into
// []ChildReport (the deliberate json-tag mirror).
func TestEnqueueDelegationQueuedResultShape(t *testing.T) {
	store := &fakeDelegationStore{}
	enq := &DelegationEnqueuer{Store: store}
	goals := []string{"first goal", "second goal"}
	brief := DelegationPayload{ConversationID: "conv-1", ParentRunID: "run-1", Depth: 1}

	msg, err := EnqueueDelegation(context.Background(), enq, "identity-1", goals, brief)
	if err != nil {
		t.Fatalf("EnqueueDelegation: %v", err)
	}

	var decoded struct {
		Queued  int    `json:"queued"`
		Note    string `json:"note"`
		Workers []struct {
			GoalIndex int    `json:"goal_index"`
			ChildID   string `json:"child_id"`
			Status    string `json:"status"`
			Goal      string `json:"goal"`
		} `json:"workers"`
	}
	if err := json.Unmarshal([]byte(msg), &decoded); err != nil {
		t.Fatalf("EnqueueDelegation result did not decode as the queued-result shape: %v\nmsg=%s", err, msg)
	}
	if decoded.Queued != len(goals) {
		t.Fatalf("queued = %d, want %d", decoded.Queued, len(goals))
	}
	if decoded.Note == "" {
		t.Fatal("note must not be empty -- it is the model's only signal not to wait/re-dispatch")
	}
	if len(decoded.Workers) != len(goals) {
		t.Fatalf("workers = %d entries, want %d (one per goal)", len(decoded.Workers), len(goals))
	}
	seen := map[string]bool{}
	for i, w := range decoded.Workers {
		if w.GoalIndex != i {
			t.Fatalf("workers[%d].goal_index = %d, want %d", i, w.GoalIndex, i)
		}
		if w.ChildID == "" || seen[w.ChildID] {
			t.Fatalf("workers[%d].child_id = %q, want a non-empty, unique id", i, w.ChildID)
		}
		seen[w.ChildID] = true
		if w.Status != StatusRunning {
			t.Fatalf("workers[%d].status = %q, want %q", i, w.Status, StatusRunning)
		}
		if w.Goal != goals[i] {
			t.Fatalf("workers[%d].goal = %q, want %q", i, w.Goal, goals[i])
		}
	}

	// The deliberate json-tag mirror with ChildReport: the SAME bytes decode
	// straight into []ChildReport (round-trip through the "workers" array).
	workersJSON, err := json.Marshal(decoded.Workers)
	if err != nil {
		t.Fatalf("marshal decoded workers: %v", err)
	}
	var reports []ChildReport
	if err := json.Unmarshal(workersJSON, &reports); err != nil {
		t.Fatalf("workers array did not round-trip into []ChildReport: %v", err)
	}
	if len(reports) != len(goals) || reports[0].Status != StatusRunning {
		t.Fatalf("round-tripped reports = %+v, want %d entries carrying StatusRunning", reports, len(goals))
	}
}
