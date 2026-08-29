package swarm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"go.uber.org/goleak"
)

// The claim loop's concurrency contract (finding F, live-check/d03/RESULTS.md):
// a claimed batch runs side by side, Run keeps claiming while workers run, and
// the identity is bound once at the processJob boundary (defect A).

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

// TestRunKeepsClaimingWhileAJobIsInFlight is finding F's second half
// (live-check/d03/RESULTS.md): a row that arrives AFTER a pass has claimed must
// not wait for that pass's workers. The first claim hands out a slow row (400ms
// scripted model delay); the second claim -- which the old blocking loop would
// only have made after the slow worker finished -- hands out a fast row. The
// fast row's report recorded first is the proof that Run claimed while the slow
// worker was still running.
func TestRunKeepsClaimingWhileAJobIsInFlight(t *testing.T) {
	defer goleak.VerifyNone(t)
	const identityID = "11111111-1111-4111-8111-111111111111"
	client := newRouter().
		route("slow goal", outcome{kind: "ok", text: "slow done", delay: 400 * time.Millisecond}).
		route("fast goal", outcome{kind: "ok", text: "fast done"})
	rc := testRunConfig(t, client, 25)
	row := func(id, goal string) documents.IngestionJob {
		return documents.IngestionJob{ID: id, IdentityID: identityID, JobType: JobTypeSwarmDelegation, MaxAttempts: 3,
			Payload: map[string]any{"goal": goal, "conversation_id": "conv-" + id}}
	}
	store := &fakeDelegationStore{claimJobs: []documents.IngestionJob{row("slow", "slow goal")}, claimNext: []documents.IngestionJob{row("fast", "fast goal")}}
	recorder := &fakeConversationRecorder{}
	l := &DelegationClaimLoop{
		Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: &fakeSteerPublisher{}},
		IdentityID: identityID, Worker: rc, LeaseDuration: time.Minute, PollInterval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(withToolCtx(context.Background(), t))
	done := make(chan error, 1)
	go func() { done <- l.Run(ctx) }()

	deadline := time.After(3 * time.Second)
	for {
		recorder.mu.Lock()
		n := len(recorder.appended)
		recorder.mu.Unlock()
		if n == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("recorded %d reports in 3s, want both rows delivered", n)
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v, want nil on context cancellation", err)
	}
	if recorder.appended[0].conversationID != "conv-fast" {
		t.Fatalf("recorded order = %+v, want the fast row (claimed while the slow one ran) FIRST", recorder.appended)
	}
}

// TestProcessOnceRunsAClaimedBatchConcurrently is finding F's proof
// (live-check/d03/RESULTS.md): two rows claimed in one batch must run side by side,
// each renewing its own lease, never one waiting behind the other with its lease
// ticking. Job A's worker is slow (a scripted 400ms delay before its model answers)
// and job B's is immediate; under the former serial loop B's report would land
// only after A's, so the recorder seeing B FIRST is the concurrency proof.
func TestProcessOnceRunsAClaimedBatchConcurrently(t *testing.T) {
	defer goleak.VerifyNone(t)
	const identityID = "11111111-1111-4111-8111-111111111111"
	client := newRouter().
		route("slow goal", outcome{kind: "ok", text: "slow done", delay: 400 * time.Millisecond}).
		route("fast goal", outcome{kind: "ok", text: "fast done"})
	rc := testRunConfig(t, client, 25)
	store := &fakeDelegationStore{claimJobs: []documents.IngestionJob{
		{ID: "slow", IdentityID: identityID, JobType: JobTypeSwarmDelegation, MaxAttempts: 3,
			Payload: map[string]any{"goal": "slow goal", "conversation_id": "conv-slow"}},
		{ID: "fast", IdentityID: identityID, JobType: JobTypeSwarmDelegation, MaxAttempts: 3,
			Payload: map[string]any{"goal": "fast goal", "conversation_id": "conv-fast"}},
	}}
	recorder := &fakeConversationRecorder{}
	l := &DelegationClaimLoop{
		Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: &fakeSteerPublisher{}},
		IdentityID: identityID, Worker: rc, LeaseDuration: time.Minute,
	}

	n, err := l.ProcessOnce(withToolCtx(context.Background(), t))
	l.Wait()
	if err != nil {
		t.Fatalf("ProcessOnce = %v", err)
	}
	if n != 2 {
		t.Fatalf("dispatched = %d, want both claimed rows", n)
	}
	if tr := store.transitionsSnapshot(); len(tr) != 2 {
		t.Fatalf("transitions = %+v, want one succeeded transition per row", tr)
	}
	if len(recorder.appended) != 2 || recorder.appended[0].conversationID != "conv-fast" {
		t.Fatalf("recorded order = %+v, want the fast row's report FIRST -- a serial batch would deliver the slow row's first", recorder.appended)
	}
}

// TestProcessJobBindsTheJobIdentityOnce is the boundary proof for defect A
// (live-check/d03/RESULTS.md): the claim loop's ctx carries NO identityctx, and
// processJob is the ONE place that binds the claimed row's identity before the
// worker, the report delivery and the pause-and-park question all read it. Both
// delivery outcomes are asserted through ProcessOnce on a bare ctx, so a future
// call site that forgets the bind cannot exist -- there is nothing to forget.
func TestProcessJobBindsTheJobIdentityOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  outcome
	}{
		{name: "report delivery", out: outcome{kind: "ok", text: "inbox summarised"}},
		{name: "pause question delivery", out: outcome{kind: "pause", question: "which inbox?"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			const goal = "summarise the inbox"
			const identityID = "11111111-1111-4111-8111-111111111111"
			rc := testRunConfig(t, newRouter().route(goal, tc.out), 25)
			store := &fakeDelegationStore{claimJobs: []documents.IngestionJob{{
				ID: "j1", IdentityID: identityID, JobType: JobTypeSwarmDelegation,
				Payload:     map[string]any{"goal": goal, "conversation_id": "conv-7"},
				MaxAttempts: 3,
			}}}
			recorder := &fakeConversationRecorder{}
			l := &DelegationClaimLoop{
				Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: &fakeSteerPublisher{}},
				IdentityID: identityID, Worker: rc, LeaseDuration: time.Minute, PauseParker: newFakePauseAndPark(),
			}

			if _, err := l.ProcessOnce(withToolCtx(context.Background(), t)); err != nil {
				t.Fatalf("ProcessOnce = %v", err)
			}
			l.Wait()
			if len(recorder.appended) != 1 {
				t.Fatalf("appended = %+v, want exactly one recorded turn", recorder.appended)
			}
			if got := recorder.appended[0].identityID; got != identityID {
				t.Fatalf("recorder saw identity %q on ctx, want the claimed job's %q -- a real "+
					"conversations.Store would have this write hidden by RLS", got, identityID)
			}
		})
	}
}
