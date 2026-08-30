package swarm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/steer"
)

func TestDeliverSuccessStagesAndProjectsBeforeTransitioning(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{}
	recorder := &fakeConversationRecorder{}
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: pub}, IdentityID: "identity-1"}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1"}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7", FanoutKey: "f-test"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{Status: StatusOK, Summary: "done"}); err != nil {
		t.Fatalf("deliverSuccess = %v", err)
	}
	if len(recorder.appended) != 1 || recorder.appended[0].conversationID != "conv-7" {
		t.Fatalf("appended = %+v, want one SC#1 turn to conv-7", recorder.appended)
	}
	if len(pub.pushes) != 1 || !strings.HasPrefix(pub.pushes[0], "conv-7|"+steer.SourceWorker+"|") {
		t.Fatalf("pushes = %+v, want one SourceWorker projection to conv-7", pub.pushes)
	}
	if tr := store.transitionsSnapshot(); len(tr) != 1 || tr[0].Status != "succeeded" {
		t.Fatalf("transitions = %+v, want one succeeded", tr)
	}
	if stages := store.stagesSnapshot(); len(stages) < 2 {
		t.Fatalf("stages = %d, want terminal report plus archive-attempt checkpoint", len(stages))
	}
}

func TestDeliverSuccessRetriesOnlyDeliveryWhenThePushFails(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{err: errors.New("inbox gone")}
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: &fakeConversationRecorder{}, Steer: pub}, IdentityID: "identity-1"}

	err := l.deliverSuccess(context.Background(), documents.IngestionJob{ID: "j1"},
		DelegationPayload{ConversationID: "conv-7", FanoutKey: "f-test"}, ChildReport{Status: StatusOK})
	if err != nil {
		t.Fatalf("deliverSuccess = %v, want the staged delivery failure converted to a queue retry", err)
	}
	if len(store.transitionsSnapshot()) != 0 {
		t.Fatal("the row must NOT be marked succeeded when the report was never delivered")
	}
	if retries := store.retriesSnapshot(); len(retries) != 1 || retries[0].ErrorCode != "delivery_failed" {
		t.Fatalf("retries = %+v, want one delivery-only retry", retries)
	}
}

func TestDeliverSuccessRetriesOnlyDeliveryWhenTransitionFails(t *testing.T) {
	store := &fakeDelegationStore{transitErr: errors.New("row vanished")}
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{}
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: pub}, IdentityID: "identity-1"}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1", JobType: JobTypeSwarmDelegation, CreatedAt: time.Now()}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7", ChildID: "w1", FanoutKey: "f-test"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{ChildID: "w1", Status: StatusOK}); err != nil {
		t.Fatalf("deliverSuccess = %v, want the transition failure converted to a queue retry", err)
	}
	if retries := store.retriesSnapshot(); len(retries) != 1 || !strings.Contains(retries[0].ErrorMessage, "terminal transition") {
		t.Fatalf("retries = %+v, want one delivery retry naming the transition failure", retries)
	}
	stages := store.stagesSnapshot()
	store.transitErr = nil
	retryJob := job
	retryJob.Payload = stages[len(stages)-1].Payload
	retryJob.LeaseGeneration++
	if err := l.processJob(context.Background(), retryJob); err != nil {
		t.Fatalf("transition recovery = %v", err)
	}
	if len(recorder.appended) != 1 || len(pub.pushes) != 1 {
		t.Fatalf("replayed projections = turns:%d pushes:%d, want no duplicates", len(recorder.appended), len(pub.pushes))
	}
}

func TestPendingDeliveryRetrySkipsWorkerAndDeduplicatesProjections(t *testing.T) {
	const (
		identityID = "11111111-1111-4111-8111-111111111111"
		goal       = "summarise the inbox"
	)
	router := newRouter().route(goal, outcome{kind: "fail"})
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{err: errors.New("inbox gone")}
	store := &fakeDelegationStore{}
	l := &DelegationClaimLoop{
		Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: pub},
		IdentityID: identityID, Worker: testRunConfig(t, router, 25),
	}
	job := documents.IngestionJob{
		ID: "j1", IdentityID: identityID, JobType: JobTypeSwarmDelegation,
		AttemptCount: 1, MaxAttempts: 3, CreatedAt: time.Now(),
	}
	payload := DelegationPayload{Goal: goal, ConversationID: "conv-7", ChildID: "w1", FanoutKey: "f-test"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{ChildID: "w1", Status: StatusOK, Summary: "done"}); err != nil {
		t.Fatalf("first deliverSuccess = %v", err)
	}
	if len(recorder.appended) != 1 || len(pub.pushes) != 0 {
		t.Fatalf("first projections = turns:%d pushes:%d, want the conversation-only partial result", len(recorder.appended), len(pub.pushes))
	}
	stages := store.stagesSnapshot()
	if len(stages) == 0 {
		t.Fatal("terminal report was not staged")
	}

	pub.err = nil
	retryJob := job
	retryJob.Payload = stages[len(stages)-1].Payload
	retryJob.LeaseGeneration++
	if err := l.processJob(context.Background(), retryJob); err != nil {
		t.Fatalf("delivery-only retry = %v", err)
	}
	router.mu.Lock()
	modelCalls := router.calls[goal]
	router.mu.Unlock()
	if modelCalls != 0 {
		t.Fatalf("model calls = %d, want 0 after terminal staging", modelCalls)
	}
	if len(recorder.appended) != 1 || len(pub.pushes) != 1 {
		t.Fatalf("final projections = turns:%d pushes:%d, want exactly one of each", len(recorder.appended), len(pub.pushes))
	}
	if tr := store.transitionsSnapshot(); len(tr) != 1 || tr[0].Status != "succeeded" {
		t.Fatalf("transitions = %+v, want one succeeded after delivery recovery", tr)
	}
}

func TestDeadLetterDeliveryFailureRemainsRetryable(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{err: errors.New("inbox gone")}
	l := &DelegationClaimLoop{
		Store: store, Delivery: &DelegationDelivery{Recorder: &fakeConversationRecorder{}, Steer: pub},
		IdentityID: "identity-1",
	}
	job := documents.IngestionJob{
		ID: "j1", IdentityID: "identity-1", JobType: JobTypeSwarmDelegation,
		AttemptCount: 3, MaxAttempts: 3, CreatedAt: time.Now(),
		Payload: map[string]any{
			"goal": "g", "conversation_id": "conv-7", "child_id": "w1", "fanout_key": "f-test",
		},
	}

	if err := l.recordFailure(context.Background(), job, errors.New("worker exhausted")); err != nil {
		t.Fatalf("recordFailure = %v", err)
	}
	if len(store.transitionsSnapshot()) != 0 {
		t.Fatal("dead-letter transition must wait for its projections")
	}
	if retries := store.retriesSnapshot(); len(retries) != 1 || retries[0].ErrorCode != "delivery_failed" {
		t.Fatalf("retries = %+v, want one delivery-only retry beyond the worker attempt cap", retries)
	}
	stages := store.stagesSnapshot()
	if len(stages) == 0 {
		t.Fatal("dead-letter report was not staged before delivery")
	}
	pending, err := pendingDeliveryFromJob(documents.IngestionJob{ID: job.ID, Payload: stages[len(stages)-1].Payload})
	if err != nil || pending == nil || pending.TargetStatus != "dead_letter" {
		t.Fatalf("pending dead-letter = %+v, %v", pending, err)
	}
}

func TestPendingDeliveryRejectsAnotherJobsKey(t *testing.T) {
	job := documents.IngestionJob{ID: "job-a", Payload: map[string]any{
		pendingDeliveryPayloadKey: delegationPendingDelivery{
			DeliveryKey: "job-b:terminal", Report: ChildReport{ChildID: "w1"}, TargetStatus: "succeeded",
		},
	}}
	if _, err := pendingDeliveryFromJob(job); err == nil || !strings.Contains(err.Error(), "does not match job") {
		t.Fatalf("pendingDeliveryFromJob = %v, want a job-key mismatch", err)
	}
}
