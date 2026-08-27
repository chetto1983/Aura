package swarm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/steer"
)

// delegation_queue_unit_test.go is the daemon-free half of the delegation queue's
// coverage (no build tag, no Postgres): the config resolvers every claim-loop caller
// depends on, and the payload codec that decides whether a claimed row is workable at
// all. The claim/lease/retry behaviour against a real queue lives in
// delegation_queue_test.go (db_integration).

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

// fakeSteerPublisher records the pushes deliverSuccess makes, and can fail one, so
// the ordering contract (push attempted BEFORE the row is transitioned) is
// observable without a queue.
type fakeSteerPublisher struct {
	pushes []string
	err    error
}

func (f *fakeSteerPublisher) Push(conv, source, text string) error {
	if f.err != nil {
		return f.err
	}
	f.pushes = append(f.pushes, conv+"|"+source+"|"+text)
	return nil
}

func TestNewDelegationClaimLoopBindsItsArguments(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{}
	l := NewDelegationClaimLoop(store, pub, "identity-1", RunConfig{ConvID: "seed"}, 9*time.Second, 3*time.Second)
	if l.Store != store || l.Steer != pub {
		t.Fatal("constructor did not bind the store and publisher")
	}
	if l.IdentityID != "identity-1" || l.LeaseDuration != 9*time.Second || l.PollInterval != 3*time.Second {
		t.Fatalf("constructor bound %+v, want the passed identity and durations", l)
	}
	if l.Worker.ConvID != "seed" {
		t.Fatal("constructor did not carry the worker template")
	}
}

// TestProcessOnceRefusesAnUnconfiguredLoop pins the wiring guards. Both run before
// any claim, so a half-built loop is a named Go error rather than a nil dereference
// inside the queue.
func TestProcessOnceRefusesAnUnconfiguredLoop(t *testing.T) {
	for _, tc := range []struct {
		name string
		loop *DelegationClaimLoop
		want string
	}{
		{"no store", &DelegationClaimLoop{IdentityID: "i"}, "no store"},
		{"no identity", &DelegationClaimLoop{Store: &fakeDelegationStore{}}, "no identity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := tc.loop.ProcessOnce(context.Background())
			if err == nil {
				t.Fatalf("ProcessOnce on a loop with %s = nil error", tc.name)
			}
			if n != 0 {
				t.Fatalf("ProcessOnce processed %d jobs on a refused loop, want 0", n)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestProcessOnceSurfacesAClaimFailure(t *testing.T) {
	store := &fakeDelegationStore{claimErr: errors.New("queue unreachable")}
	l := &DelegationClaimLoop{Store: store, IdentityID: "identity-1"}
	n, err := l.ProcessOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "queue unreachable") {
		t.Fatalf("ProcessOnce = %v, want the claim error surfaced", err)
	}
	if n != 0 {
		t.Fatalf("processed = %d, want 0 when the claim itself failed", n)
	}
}

// TestProcessOnceRejectsAMisroutedRow covers the two post-claim assertions. They are
// defence against the claim query ever widening: a row belonging to another identity,
// or to the document ingestion worker's own job_type, must stop the pass rather than
// be run under this loop's identity.
func TestProcessOnceRejectsAMisroutedRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		job  documents.IngestionJob
		want string
	}{
		{
			"foreign identity",
			documents.IngestionJob{ID: "j1", IdentityID: "someone-else", JobType: JobTypeSwarmDelegation},
			"unexpected identity",
		},
		{
			"foreign job type",
			documents.IngestionJob{ID: "j2", IdentityID: "identity-1", JobType: "document_ingestion"},
			"unexpected job_type",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeDelegationStore{claimJobs: []documents.IngestionJob{tc.job}}
			l := &DelegationClaimLoop{Store: store, IdentityID: "identity-1"}
			n, err := l.ProcessOnce(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ProcessOnce = %v, want an error naming %q", err, tc.want)
			}
			if n != 0 {
				t.Fatalf("processed = %d, want 0 -- a misrouted row is never run", n)
			}
			if len(store.transitions) != 0 || len(store.retries) != 0 {
				t.Fatal("a misrouted row must not be transitioned or retried by this loop")
			}
		})
	}
}

// TestProcessOnceRetriesAnUndecodableRow drives a whole pass without ever starting a
// worker: the claimed row's payload cannot decode, so processJob short-circuits into
// recordFailure, which retries below the attempt cap. The row still counts as
// processed -- the pass handled it to a terminal-for-now state.
func TestProcessOnceRetriesAnUndecodableRow(t *testing.T) {
	store := &fakeDelegationStore{claimJobs: []documents.IngestionJob{{
		ID: "j1", IdentityID: "identity-1", JobType: JobTypeSwarmDelegation,
		Payload:      map[string]any{"conversation_id": "conv-1"}, // no goal
		AttemptCount: 1, MaxAttempts: 3,
	}}}
	l := &DelegationClaimLoop{Store: store, IdentityID: "identity-1"}
	n, err := l.ProcessOnce(context.Background())
	if err != nil {
		t.Fatalf("ProcessOnce = %v, want the undecodable row handled, not propagated", err)
	}
	if n != 1 {
		t.Fatalf("processed = %d, want 1", n)
	}
	if len(store.retries) != 1 {
		t.Fatalf("retries = %d, want 1 below the attempt cap", len(store.retries))
	}
	if !strings.Contains(store.retries[0].ErrorMessage, "missing goal") {
		t.Fatalf("retry message = %q, want the decode reason preserved", store.retries[0].ErrorMessage)
	}
	if len(store.transitions) != 0 {
		t.Fatal("a retryable failure must not dead-letter the row")
	}
}

// TestRecordFailureDeadLettersAtTheAttemptCap is the other leg of the same branch:
// at the cap the row is dead-lettered instead of retried forever, and the reason
// travels with it.
func TestRecordFailureDeadLettersAtTheAttemptCap(t *testing.T) {
	store := &fakeDelegationStore{}
	l := &DelegationClaimLoop{Store: store, IdentityID: "identity-1"}
	job := documents.IngestionJob{ID: "j9", IdentityID: "identity-1", AttemptCount: 3, MaxAttempts: 3}

	if err := l.recordFailure(context.Background(), job, errors.New("worker exploded")); err != nil {
		t.Fatalf("recordFailure = %v, want nil once the row is dead-lettered", err)
	}
	if len(store.retries) != 0 {
		t.Fatal("a row at the attempt cap must not be retried")
	}
	if len(store.transitions) != 1 || store.transitions[0].Status != "dead_letter" {
		t.Fatalf("transitions = %+v, want one dead_letter", store.transitions)
	}
	if !strings.Contains(store.transitions[0].ErrorMessage, "worker exploded") {
		t.Fatalf("dead_letter message = %q, want the cause preserved", store.transitions[0].ErrorMessage)
	}
}

// TestDeliverSuccessPushesBeforeTransitioning pins D-04's ordering: the report is
// pushed under steer.SourceWorker FIRST, and the row is only marked succeeded after.
// A push failure must abort before any transition -- otherwise a report nobody
// received would be recorded as delivered.
func TestDeliverSuccessPushesBeforeTransitioning(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{}
	l := &DelegationClaimLoop{Store: store, Steer: pub, IdentityID: "identity-1"}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1"}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{Status: StatusOK, Summary: "done"}); err != nil {
		t.Fatalf("deliverSuccess = %v", err)
	}
	if len(pub.pushes) != 1 {
		t.Fatalf("pushes = %d, want exactly 1", len(pub.pushes))
	}
	if !strings.HasPrefix(pub.pushes[0], "conv-7|"+steer.SourceWorker+"|") {
		t.Fatalf("push = %q, want it addressed to the payload conversation under SourceWorker", pub.pushes[0])
	}
	if len(store.transitions) != 1 || store.transitions[0].Status != "succeeded" {
		t.Fatalf("transitions = %+v, want one succeeded", store.transitions)
	}
}

func TestDeliverSuccessDoesNotTransitionWhenThePushFails(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{err: errors.New("inbox gone")}
	l := &DelegationClaimLoop{Store: store, Steer: pub, IdentityID: "identity-1"}

	err := l.deliverSuccess(context.Background(), documents.IngestionJob{ID: "j1"},
		DelegationPayload{ConversationID: "conv-7"}, ChildReport{Status: StatusOK})
	if err == nil || !strings.Contains(err.Error(), "push") {
		t.Fatalf("deliverSuccess = %v, want the push failure surfaced", err)
	}
	if len(store.transitions) != 0 {
		t.Fatal("the row must NOT be marked succeeded when the report was never delivered")
	}
}

func TestDeliverSuccessSurfacesATransitionFailure(t *testing.T) {
	store := &fakeDelegationStore{transitErr: errors.New("row vanished")}
	l := &DelegationClaimLoop{Store: store, Steer: &fakeSteerPublisher{}, IdentityID: "identity-1"}

	err := l.deliverSuccess(context.Background(), documents.IngestionJob{ID: "j1"},
		DelegationPayload{ConversationID: "conv-7"}, ChildReport{Status: StatusOK})
	if err == nil || !strings.Contains(err.Error(), "succeed transition") {
		t.Fatalf("deliverSuccess = %v, want the transition failure named", err)
	}
}

// TestMaintainLeaseRenewsUntilTheRunEnds covers the happy tick plus the ctx.Done
// exit: cancelling the run context must end the heartbeat goroutine cleanly, which
// is what keeps runWithHeartbeat from leaking one goroutine per claimed job.
func TestMaintainLeaseRenewsUntilTheRunEnds(t *testing.T) {
	defer goleak.VerifyNone(t)
	store := &fakeDelegationStore{}
	l := &DelegationClaimLoop{Store: store, IdentityID: "identity-1", LeaseDuration: 30 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- l.maintainLease(ctx, cancel, documents.IngestionJob{ID: "j1", IdentityID: "identity-1"})
	}()

	deadline := time.After(2 * time.Second)
	for store.heartbeatCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("no heartbeat within 2s")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("maintainLease = %v, want nil on context cancellation", err)
	}
}

// TestMaintainLeaseCancelsTheRunWhenTheLeaseIsLost is the branch that matters for
// correctness: a heartbeat rejected while the run context is still live means
// another worker owns the row, so the run is cancelled rather than allowed to finish
// and write state over the new owner's.
func TestMaintainLeaseCancelsTheRunWhenTheLeaseIsLost(t *testing.T) {
	defer goleak.VerifyNone(t)
	store := &fakeDelegationStore{heartbeatErr: errors.New("lease generation moved")}
	l := &DelegationClaimLoop{Store: store, IdentityID: "identity-1", LeaseDuration: 30 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := l.maintainLease(ctx, cancel, documents.IngestionJob{ID: "j1", IdentityID: "identity-1"})
	if err == nil || !strings.Contains(err.Error(), "lease generation moved") {
		t.Fatalf("maintainLease = %v, want the heartbeat failure surfaced", err)
	}
	if ctx.Err() == nil {
		t.Fatal("maintainLease must cancel the run context when the lease is lost")
	}
}

// TestRunSwallowsAPassErrorAndStopsOnCancel pins the daemon lifecycle: one bad pass
// is logged and the loop keeps polling (a queue blip must not kill the worker), and
// a cancelled context ends Run without an error.
func TestRunSwallowsAPassErrorAndStopsOnCancel(t *testing.T) {
	defer goleak.VerifyNone(t)
	store := &fakeDelegationStore{claimErr: errors.New("transient")}
	l := &DelegationClaimLoop{Store: store, IdentityID: "identity-1", PollInterval: 5 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- l.Run(ctx) }()

	deadline := time.After(2 * time.Second)
	for store.claimCount() < 2 { // proves the loop survived the first failing pass
		select {
		case <-deadline:
			t.Fatalf("only %d passes ran; Run stopped on the first error", store.claimCount())
		case <-time.After(2 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v, want nil on context cancellation", err)
	}
}

// TestProcessJobRunsTheWorkerAndRoutesEveryOutcome drives a claimed row all the way
// through runWithHeartbeat and the SAME runChild a synchronous swarm invocation uses
// -- no second worker construction -- with a scripted client standing in for the
// model. The three cases are the three terminal shapes processJob must distinguish:
// a delivered report, a failed worker, and a pause that this phase deliberately does
// not yet handle (51-06b) and therefore must fail loudly with the question preserved
// rather than silently succeed.
func TestProcessJobRunsTheWorkerAndRoutesEveryOutcome(t *testing.T) {
	for _, tc := range []struct {
		name        string
		out         outcome
		wantPushes  int
		wantStatus  string
		wantRetried bool
		wantInMsg   string
	}{
		{
			name: "delivered", out: outcome{kind: "ok", text: "inbox summarised"},
			wantPushes: 1, wantStatus: "succeeded",
		},
		{
			name: "worker failed", out: outcome{kind: "fail"},
			wantPushes: 0, wantRetried: true,
		},
		{
			name: "paused for input", out: outcome{kind: "pause", question: "which inbox?"},
			wantPushes: 0, wantRetried: true, wantInMsg: "which inbox?",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			const goal = "summarise the inbox"
			// The operation root this path mints validates the identity as a real
			// uuid (idempotency.WithOperation), so the loop's identity is one here
			// rather than the readable label the store-only tests above can use.
			const identityID = "11111111-1111-4111-8111-111111111111"
			rc := testRunConfig(t, newRouter().route(goal, tc.out), 25)
			store := &fakeDelegationStore{claimJobs: []documents.IngestionJob{{
				ID: "j1", IdentityID: identityID, JobType: JobTypeSwarmDelegation,
				Payload:     map[string]any{"goal": goal, "conversation_id": "conv-7"},
				MaxAttempts: 3,
			}}}
			pub := &fakeSteerPublisher{}
			l := &DelegationClaimLoop{
				Store: store, Steer: pub, IdentityID: identityID,
				Worker: rc, LeaseDuration: time.Minute,
			}

			n, err := l.ProcessOnce(withToolCtx(context.Background(), t))
			if err != nil {
				t.Fatalf("ProcessOnce = %v", err)
			}
			if n != 1 {
				t.Fatalf("processed = %d, want 1", n)
			}
			if len(pub.pushes) != tc.wantPushes {
				t.Fatalf("pushes = %d, want %d", len(pub.pushes), tc.wantPushes)
			}
			if tc.wantStatus != "" {
				if len(store.transitions) != 1 || store.transitions[0].Status != tc.wantStatus {
					t.Fatalf("transitions = %+v, want one %q", store.transitions, tc.wantStatus)
				}
			}
			if tc.wantRetried && len(store.retries) != 1 {
				t.Fatalf("retries = %d, want 1", len(store.retries))
			}
			if tc.wantInMsg != "" && !strings.Contains(store.retries[0].ErrorMessage, tc.wantInMsg) {
				t.Fatalf("retry message = %q, want it to preserve %q",
					store.retries[0].ErrorMessage, tc.wantInMsg)
			}
		})
	}
}
