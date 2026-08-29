package swarm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/steer"
)

// delegation_queue_lifecycle_test.go is the daemon-free half of DelegationClaimLoop's own
// lifecycle coverage: constructor binding, ProcessOnce's wiring guards and claim/routing
// behaviour, recordFailure's retry/dead-letter split, deliverSuccess's ordering and RLS
// identity contract, the lease heartbeat goroutine, Run's poll loop, and processJob's full
// outcome routing through the real runChild. Split out of delegation_queue_unit_test.go
// (CLAUDE.md's 600-LOC ceiling) -- that file keeps the config resolvers and the payload
// codec, both pure and unrelated to the loop's own runtime behaviour.

// fakeSteerPublisher records the pushes deliverSuccess makes, and can fail one, so
// the ordering contract (push attempted BEFORE the row is transitioned) is
// observable without a queue.
type fakeSteerPublisher struct {
	mu     sync.Mutex // ProcessOnce runs a claimed batch concurrently (finding F)
	pushes []string
	err    error
}

func (f *fakeSteerPublisher) Push(conv, source, text string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes = append(f.pushes, conv+"|"+source+"|"+text)
	return nil
}

func TestNewDelegationClaimLoopBindsItsArguments(t *testing.T) {
	store := &fakeDelegationStore{}
	delivery := &DelegationDelivery{}
	parker := &fakePauseAndPark{}
	l := NewDelegationClaimLoop(store, delivery, parker, "identity-1", RunConfig{ConvID: "seed"}, 9*time.Second, 3*time.Second)
	if l.Store != store || l.Delivery != delivery || l.PauseParker != parker {
		t.Fatal("constructor did not bind the store, delivery and pause parker")
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
			tc.loop.Wait()
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
	l.Wait()
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
			l.Wait()
			l.Wait()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ProcessOnce = %v, want an error naming %q", err, tc.want)
			}
			if n != 0 {
				t.Fatalf("processed = %d, want 0 -- a misrouted row is never run", n)
			}
			if len(store.transitionsSnapshot()) != 0 || len(store.retriesSnapshot()) != 0 {
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
	l.Wait()
	if err != nil {
		t.Fatalf("ProcessOnce = %v, want the undecodable row handled, not propagated", err)
	}
	if n != 1 {
		t.Fatalf("processed = %d, want 1", n)
	}
	if len(store.retriesSnapshot()) != 1 {
		t.Fatalf("retries = %d, want 1 below the attempt cap", len(store.retriesSnapshot()))
	}
	if !strings.Contains(store.retriesSnapshot()[0].ErrorMessage, "missing goal") {
		t.Fatalf("retry message = %q, want the decode reason preserved", store.retriesSnapshot()[0].ErrorMessage)
	}
	if len(store.transitionsSnapshot()) != 0 {
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
	if len(store.retriesSnapshot()) != 0 {
		t.Fatal("a row at the attempt cap must not be retried")
	}
	if tr := store.transitionsSnapshot(); len(tr) != 1 || tr[0].Status != "dead_letter" {
		t.Fatalf("transitions = %+v, want one dead_letter", tr)
	}
	if msg := store.transitionsSnapshot()[0].ErrorMessage; !strings.Contains(msg, "worker exploded") {
		t.Fatalf("dead_letter message = %q, want the cause preserved", msg)
	}
}

// TestDeliverSuccessPushesBeforeTransitioning pins D-04's ordering: the report is
// recorded (SC#1) and pushed under steer.SourceWorker BEFORE the row is marked
// succeeded. A delivery failure must abort before any transition -- otherwise a
// report nobody received would be recorded as delivered.
func TestDeliverSuccessPushesBeforeTransitioning(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{}
	recorder := &fakeConversationRecorder{}
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: pub}, IdentityID: "identity-1"}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1"}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{Status: StatusOK, Summary: "done"}); err != nil {
		t.Fatalf("deliverSuccess = %v", err)
	}
	if len(recorder.appended) != 1 || recorder.appended[0].conversationID != "conv-7" {
		t.Fatalf("appended = %+v, want one SC#1 turn to conv-7", recorder.appended)
	}
	if len(pub.pushes) != 1 {
		t.Fatalf("pushes = %d, want exactly 1", len(pub.pushes))
	}
	if !strings.HasPrefix(pub.pushes[0], "conv-7|"+steer.SourceWorker+"|") {
		t.Fatalf("push = %q, want it addressed to the payload conversation under SourceWorker", pub.pushes[0])
	}
	if tr := store.transitionsSnapshot(); len(tr) != 1 || tr[0].Status != "succeeded" {
		t.Fatalf("transitions = %+v, want one succeeded", tr)
	}
}

func TestDeliverSuccessDoesNotTransitionWhenThePushFails(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{err: errors.New("inbox gone")}
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: &fakeConversationRecorder{}, Steer: pub}, IdentityID: "identity-1"}

	err := l.deliverSuccess(context.Background(), documents.IngestionJob{ID: "j1"},
		DelegationPayload{ConversationID: "conv-7"}, ChildReport{Status: StatusOK})
	if err == nil || !strings.Contains(err.Error(), "push") {
		t.Fatalf("deliverSuccess = %v, want the push failure surfaced", err)
	}
	if len(store.transitionsSnapshot()) != 0 {
		t.Fatal("the row must NOT be marked succeeded when the report was never delivered")
	}
}

func TestDeliverSuccessSurfacesATransitionFailure(t *testing.T) {
	store := &fakeDelegationStore{transitErr: errors.New("row vanished")}
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: &fakeConversationRecorder{}, Steer: &fakeSteerPublisher{}}, IdentityID: "identity-1"}

	err := l.deliverSuccess(context.Background(), documents.IngestionJob{ID: "j1"},
		DelegationPayload{ConversationID: "conv-7"}, ChildReport{Status: StatusOK})
	if err == nil || !strings.Contains(err.Error(), "succeed transition") {
		t.Fatalf("deliverSuccess = %v, want the transition failure named", err)
	}
}

// TestRecordFailureBlocksSucceeded is the plan's own named acceptance test:
// when the SC#1 conversation record fails (a WARN inside Deliver, not a hard
// Go error), deliverSuccess must NOT transition the row to succeeded --
// instead it retries via the shipped attempt_count/next_attempt_at backoff,
// exactly like any other retryable failure. The push still happens (D-04):
// a present operator is not denied the mid-turn rail just because the durable
// copy failed to write.
func TestRecordFailureBlocksSucceeded(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{}
	recorder := &fakeConversationRecorder{err: errors.New("pool exhausted")}
	l := &DelegationClaimLoop{
		Store:      store,
		Delivery:   &DelegationDelivery{Recorder: recorder, Steer: pub},
		IdentityID: "identity-1",
	}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1", MaxAttempts: 3}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{Status: StatusOK, Summary: "done"}); err != nil {
		t.Fatalf("deliverSuccess = %v, want the record failure absorbed into a retry, not surfaced", err)
	}
	if len(store.transitionsSnapshot()) != 0 {
		t.Fatal("the row must NOT be marked succeeded when the SC#1 record failed")
	}
	if len(store.retriesSnapshot()) != 1 {
		t.Fatalf("retries = %d, want 1 -- a record failure must be retried by the shipped backoff", len(store.retriesSnapshot()))
	}
	// D-04: the push still happens even though the record failed.
	if len(pub.pushes) != 1 {
		t.Fatalf("pushes = %d, want 1 -- a record failure must not suppress the present-operator rail", len(pub.pushes))
	}
}

// TestDeliverSuccessBindsTheJobIdentityForRLS is the plan's own named acceptance test for
// defect A (live-check/d03/RESULTS.md): the claim loop's own ctx (ProcessOnce's daemon
// background loop) carries NO identityctx, exactly like the real db_integration proof below.
// deliverSuccess must bind job.IdentityID onto the ctx it hands to Delivery.Deliver, because
// the real ConversationRecorder (conversations.Store.AppendTurn) reads identityctx.IdentityID
// to scope the RLS carrier -- an unbound ctx makes the write invisible to itself and fails
// "conversation not found", which measured live as every delegation attempt failing to
// record, the queue retrying the whole worker from scratch, and dead-lettering at the cap.
func TestDeliverSuccessPassesTheBoundIdentityThrough(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{}
	recorder := &fakeConversationRecorder{}
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: pub}, IdentityID: "identity-1"}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1"}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7"}

	// The bind is processJob's (TestProcessJobBindsTheJobIdentityOnce); deliverSuccess
	// must hand the SAME ctx to the recorder, never strip or re-derive it.
	bound := identityctx.WithIdentityID(context.Background(), job.IdentityID)
	if err := l.deliverSuccess(bound, job, payload, ChildReport{Status: StatusOK, Summary: "done"}); err != nil {
		t.Fatalf("deliverSuccess = %v", err)
	}
	if len(recorder.appended) != 1 {
		t.Fatalf("appended = %+v, want exactly one recorded turn", recorder.appended)
	}
	if got := recorder.appended[0].identityID; got != job.IdentityID {
		t.Fatalf("recorder saw identity %q on ctx, want the job's identity %q -- a real "+
			"conversations.Store would have this write hidden by RLS", got, job.IdentityID)
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

// TestProcessJobRunsTheWorkerAndRoutesEveryOutcome drives a claimed row all the way
// through runWithHeartbeat and the SAME runChild a synchronous swarm invocation uses
// -- no second worker construction -- with a scripted client standing in for the
// model. The three cases are the three terminal shapes processJob must distinguish:
// a delivered report, a failed worker, and (51-06b Task 1) a pause the worker opens
// its OWN attributed pause for and parks its row, rather than dead-lettering the
// question away.
func TestProcessJobRunsTheWorkerAndRoutesEveryOutcome(t *testing.T) {
	for _, tc := range []struct {
		name        string
		out         outcome
		wantPushes  int
		wantStatus  string
		wantRetried bool
		wantPaused  bool
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
			// openPauseAndPark delivers the question through the SAME plan-51-10
			// seam a consolidated report uses -- 1 push, not 0 (D-04, no new
			// delivery policy).
			name: "paused for input", out: outcome{kind: "pause", question: "which inbox?"},
			wantPushes: 1, wantPaused: true, wantInMsg: "which inbox?",
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
			parker := newFakePauseAndPark()
			l := &DelegationClaimLoop{
				Store: store, Delivery: &DelegationDelivery{Recorder: &fakeConversationRecorder{}, Steer: pub}, IdentityID: identityID,
				Worker: rc, LeaseDuration: time.Minute, PauseParker: parker,
			}

			n, err := l.ProcessOnce(withToolCtx(context.Background(), t))
			l.Wait()
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
				if tr := store.transitionsSnapshot(); len(tr) != 1 || tr[0].Status != tc.wantStatus {
					t.Fatalf("transitions = %+v, want one %q", tr, tc.wantStatus)
				}
			}
			if tc.wantRetried && len(store.retriesSnapshot()) != 1 {
				t.Fatalf("retries = %d, want 1", len(store.retriesSnapshot()))
			}
			if msg := store.retriesSnapshot(); tc.wantInMsg != "" && !tc.wantPaused && !strings.Contains(msg[0].ErrorMessage, tc.wantInMsg) {
				t.Fatalf("retry message = %q, want it to preserve %q",
					msg[0].ErrorMessage, tc.wantInMsg)
			}
			if tc.wantPaused {
				calls := parker.callsSnapshot()
				if len(calls) != 1 {
					t.Fatalf("OpenPauseAndPark calls = %d, want 1", len(calls))
				}
				if calls[0].pause.Question != tc.wantInMsg {
					t.Fatalf("pause question = %q, want %q", calls[0].pause.Question, tc.wantInMsg)
				}
				if calls[0].pause.OwningWorkerID == nil || *calls[0].pause.OwningWorkerID != "j1" {
					t.Fatalf("pause OwningWorkerID = %v, want job.ID j1 (D-13)", calls[0].pause.OwningWorkerID)
				}
				if calls[0].pause.PendingActionID == nil || *calls[0].pause.PendingActionID == "" {
					t.Fatal("pause PendingActionID must be minted fresh (D-12)")
				}
				if calls[0].park.JobID != "j1" {
					t.Fatalf("park JobID = %q, want j1", calls[0].park.JobID)
				}
				// The row is neither retried nor dead-lettered/succeeded -- it is
				// parked, a non-terminal state distinct from all three.
				if len(store.retriesSnapshot()) != 0 || len(store.transitionsSnapshot()) != 0 {
					t.Fatalf("a paused row must not be retried/transitioned by the queue lifecycle, got retries=%v transitions=%v",
						store.retriesSnapshot(), store.transitionsSnapshot())
				}
			}
		})
	}
}
