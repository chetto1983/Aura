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
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: pub, Archiver: successfulReportArchiver()}, IdentityID: "identity-1"}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1"}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7", ChildID: "w1", FanoutKey: "f-test"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{ChildID: "w1", Status: StatusOK, Summary: "done"}); err != nil {
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
		t.Fatalf("stages = %d, want terminal report plus successful archive checkpoint", len(stages))
	}
}

func TestTerminalStageFailureDoesNotStartArchive(t *testing.T) {
	store := &fakeDelegationStore{stageErr: errors.New("stage unavailable")}
	archiveCalls := 0
	archiver := archiverFunc(func(context.Context, string, string, string, string, string) (string, error) {
		archiveCalls++
		return "asset-1", nil
	})
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{
		Recorder: &fakeConversationRecorder{}, Archiver: archiver,
	}}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1"}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7", ChildID: "w1", FanoutKey: "f-test"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{ChildID: "w1", Status: StatusOK}); err == nil {
		t.Fatal("deliverSuccess = nil, want terminal staging failure")
	}
	if archiveCalls != 0 {
		t.Fatalf("archive calls = %d, want zero before durable terminal staging", archiveCalls)
	}
}

func TestArchiveFailureSchedulesDeliveryOnlyRetryBeforeProjection(t *testing.T) {
	store := &fakeDelegationStore{}
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{}
	archiver := archiverFunc(func(context.Context, string, string, string, string, string) (string, error) {
		return "", errors.New("garage unavailable")
	})
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{
		Recorder: recorder, Steer: pub, Archiver: archiver,
	}}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1"}
	payload := DelegationPayload{ConversationID: "conv-7", ChildID: "w1", FanoutKey: "f-test"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{ChildID: "w1", Status: StatusOK}); err != nil {
		t.Fatalf("deliverSuccess = %v, want archive error converted to delivery retry", err)
	}
	if retries := store.retriesSnapshot(); len(retries) != 1 || !strings.Contains(retries[0].ErrorMessage, "archive delegation report") {
		t.Fatalf("retries = %+v, want one archive delivery retry", retries)
	}
	if len(recorder.appended) != 0 || len(pub.pushes) != 0 || len(store.transitionsSnapshot()) != 0 {
		t.Fatal("archive failure must not project or terminally transition the job")
	}
}

func TestArchiveCheckpointFailureReusesOneLogicalArtifact(t *testing.T) {
	store := &fakeDelegationStore{stageErr: errors.New("checkpoint unavailable"), stageErrAt: 2}
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{}
	archiveCalls := 0
	artifacts := map[string]string{}
	archiver := archiverFunc(func(_ context.Context, _, _, deliveryKey, filename, _ string) (string, error) {
		archiveCalls++
		if existing, ok := artifacts[deliveryKey]; ok {
			return existing, nil
		}
		artifacts[deliveryKey] = filename
		return filename, nil
	})
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{
		Recorder: recorder, Steer: pub, Archiver: archiver,
	}}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1", CreatedAt: time.Now()}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7", ChildID: "w1", FanoutKey: "f-test"}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{ChildID: "w1", Status: StatusOK}); err != nil {
		t.Fatalf("deliverSuccess = %v, want checkpoint failure converted to delivery retry", err)
	}
	if len(store.retriesSnapshot()) != 1 || len(artifacts) != 1 || archiveCalls != 1 {
		t.Fatalf("first attempt = retries:%d artifacts:%d archive calls:%d", len(store.retriesSnapshot()), len(artifacts), archiveCalls)
	}
	stages := store.stagesSnapshot()
	if len(stages) < 2 {
		t.Fatalf("stages = %d, want initial stage plus failed archive checkpoint", len(stages))
	}
	stagedPending, err := pendingDeliveryFromJob(documents.IngestionJob{ID: job.ID, Payload: stages[0].Payload})
	if err != nil || stagedPending == nil || stagedPending.ArtifactName != "" {
		t.Fatalf("initial pending delivery = %+v, %v, want no persisted artifact checkpoint", stagedPending, err)
	}

	store.stageErr = nil
	retryJob := job
	retryJob.Payload = stages[0].Payload // the failed checkpoint did not persist ArtifactName
	retryJob.LeaseGeneration++
	if err := l.processJob(context.Background(), retryJob); err != nil {
		t.Fatalf("delivery-only retry: %v", err)
	}
	if len(artifacts) != 1 || archiveCalls != 2 {
		t.Fatalf("retry archive = logical artifacts:%d calls:%d, want one idempotent artifact across two calls", len(artifacts), archiveCalls)
	}
	if len(recorder.appended) != 1 || !strings.Contains(recorder.appended[0].text, "w1.md") || len(pub.pushes) != 1 {
		t.Fatalf("final projections = turns:%+v pushes:%d, want one referenced artifact", recorder.appended, len(pub.pushes))
	}
}

func TestDeliverSuccessRetriesOnlyDeliveryWhenThePushFails(t *testing.T) {
	store := &fakeDelegationStore{}
	pub := &fakeSteerPublisher{err: errors.New("inbox gone")}
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: &fakeConversationRecorder{}, Steer: pub, Archiver: successfulReportArchiver()}, IdentityID: "identity-1"}

	err := l.deliverSuccess(context.Background(), documents.IngestionJob{ID: "j1"},
		DelegationPayload{ConversationID: "conv-7", ChildID: "w1", FanoutKey: "f-test"}, ChildReport{ChildID: "w1", Status: StatusOK})
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
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: pub, Archiver: successfulReportArchiver()}, IdentityID: "identity-1"}
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
		Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Steer: pub, Archiver: successfulReportArchiver()},
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
		Store: store, Delivery: &DelegationDelivery{Recorder: &fakeConversationRecorder{}, Steer: pub, Archiver: successfulReportArchiver()},
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

// A permanent delivery failure must end. Measured live 2026-09-06: two workers finished ok,
// archiving their reports failed with an error no wait could fix, and the rows sat at
// attempt_count 58 against max_attempts 3 — still `queued`, re-claimed every 15 seconds,
// with swarm_status dutifully telling the model that finished workers were still queued.
func TestPermanentDeliveryFailureIsAbandonedRatherThanRetriedForever(t *testing.T) {
	store := &fakeDelegationStore{}
	archiver := archiverFunc(func(context.Context, string, string, string, string, string) (string, error) {
		return "", errors.New("resolve object store for identity: no rows in result set")
	})
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{
		Recorder: &fakeConversationRecorder{}, Steer: &fakeSteerPublisher{}, Archiver: archiver,
	}}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7", ChildID: "w1", FanoutKey: "f-test"}
	job := documents.IngestionJob{
		ID: "j1", IdentityID: "identity-1", CreatedAt: time.Now(),
		MaxAttempts: 3, AttemptCount: 3 + deliveryRetryAllowance,
	}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{ChildID: "w1", Status: StatusOK}); err != nil {
		t.Fatalf("deliverSuccess = %v", err)
	}
	if retries := store.retriesSnapshot(); len(retries) != 0 {
		t.Fatalf("retries = %+v, want none once the delivery allowance is spent", retries)
	}
	tr := store.transitionsSnapshot()
	if len(tr) != 1 {
		t.Fatalf("transitions = %+v, want exactly one terminal transition", tr)
	}
	// The worker succeeded; only its report could not be delivered. Recording it as failed
	// would be a different — and false — fact.
	if tr[0].Status != "succeeded" || tr[0].ErrorCode != "delivery_failed" {
		t.Fatalf("transition = %+v, want succeeded with a delivery_failed reason", tr[0])
	}
	if !strings.Contains(tr[0].ErrorMessage, "could not be delivered") ||
		!strings.Contains(tr[0].ErrorMessage, "no rows in result set") {
		t.Fatalf("message = %q, want the abandonment and its cause", tr[0].ErrorMessage)
	}
}

// One attempt below the allowance is still a retry: the ceiling must not shorten the
// tolerance a transient outage was given.
func TestDeliveryRetriesUpToTheAllowance(t *testing.T) {
	store := &fakeDelegationStore{}
	archiver := archiverFunc(func(context.Context, string, string, string, string, string) (string, error) {
		return "", errors.New("garage unavailable")
	})
	l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{
		Recorder: &fakeConversationRecorder{}, Steer: &fakeSteerPublisher{}, Archiver: archiver,
	}}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7", ChildID: "w1", FanoutKey: "f-test"}
	job := documents.IngestionJob{
		ID: "j1", IdentityID: "identity-1", CreatedAt: time.Now(),
		MaxAttempts: 3, AttemptCount: 3 + deliveryRetryAllowance - 1,
	}

	if err := l.deliverSuccess(context.Background(), job, payload, ChildReport{ChildID: "w1", Status: StatusOK}); err != nil {
		t.Fatalf("deliverSuccess = %v", err)
	}
	if len(store.retriesSnapshot()) != 1 || len(store.transitionsSnapshot()) != 0 {
		t.Fatalf("retries = %d transitions = %d, want one retry and no terminal transition",
			len(store.retriesSnapshot()), len(store.transitionsSnapshot()))
	}
}

// A row that asked for no cap at all keeps the unbounded behavior it asked for.
func TestUncappedJobKeepsRetryingDelivery(t *testing.T) {
	l := &DelegationClaimLoop{}
	uncapped := documents.IngestionJob{MaxAttempts: 0, AttemptCount: 500}
	if l.deliveryAllowanceSpent(uncapped) {
		t.Fatal("a job with no attempt cap must not be abandoned by the delivery ceiling")
	}
}
