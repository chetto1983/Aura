package swarm

import (
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
		{"missing goal", map[string]any{"conversation_id": "conv-1"}, "missing goal"},
		{"empty goal", map[string]any{"goal": "", "conversation_id": "conv-1"}, "missing goal"},
		{"missing conversation", map[string]any{"goal": "do the thing"}, "missing conversation_id"},
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
	m, err := delegationPayloadMap(DelegationPayload{Goal: "g", ConversationID: "c"})
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
