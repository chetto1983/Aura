//go:build db_integration

package swarm

import (
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/google/uuid"
)

func TestQueuedWorkerCancellationDeliversWithoutStartingModel(t *testing.T) {
	pool := delegationDisposablePool(t)
	owner, conv := seedDelegationDeliveryConversation(t, pool)
	store := documents.NewPostgresIngestionJobStore(pool)
	job, err := store.Create(t.Context(), documents.CreateIngestionJobRequest{
		IdentityID: owner, JobType: JobTypeSwarmDelegation, Status: "queued", IdempotencyKey: uuid.NewString(), MaxAttempts: 3,
		Payload: map[string]any{"goal": "must never run", "conversation_id": conv, "child_id": "w-queued", "fanout_key": "f-queued", "depth": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := documents.QueuedWorkerCancellationRequest{IdentityID: owner, ConversationID: conv, ChildID: "w-queued", JobID: job.ID}
	for _, mutate := range []func(*documents.QueuedWorkerCancellationRequest){
		func(r *documents.QueuedWorkerCancellationRequest) { r.IdentityID = uuid.NewString() },
		func(r *documents.QueuedWorkerCancellationRequest) { r.ConversationID = uuid.NewString() },
		func(r *documents.QueuedWorkerCancellationRequest) { r.ChildID = "sibling" },
		func(r *documents.QueuedWorkerCancellationRequest) { r.JobID = uuid.NewString() },
		func(r *documents.QueuedWorkerCancellationRequest) { r.AttemptCount++ },
	} {
		wrong := req
		mutate(&wrong)
		if err := store.RequestQueuedWorkerCancellation(t.Context(), wrong); !errors.Is(err, documents.ErrIngestionJobLeaseLost) {
			t.Fatalf("wrong target accepted: %+v: %v", wrong, err)
		}
	}
	for range 2 {
		if err := store.RequestQueuedWorkerCancellation(t.Context(), req); err != nil {
			t.Fatal(err)
		}
	}
	row, found, err := store.FindDelegationJob(t.Context(), owner, conv, req.ChildID)
	if err != nil || !found || !row.OperatorCancelled || row.AttemptCount != 0 || row.Status != "queued" {
		t.Fatalf("durable pre-start acceptance: %+v %v", row, err)
	}
	claimed, err := store.Claim(t.Context(), documents.ClaimIngestionJobsRequest{IdentityID: owner, JobType: JobTypeSwarmDelegation, WorkerID: controlClaimOwner, LeaseDuration: time.Minute, BatchSize: 1})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	heartbeat := documents.HeartbeatIngestionJobRequest{IdentityID: owner, JobID: job.ID, WorkerID: controlClaimOwner, LeaseGeneration: claimed[0].LeaseGeneration, LeaseDuration: time.Minute}
	if _, err := store.Heartbeat(t.Context(), heartbeat); err != nil {
		t.Fatalf("delivery lease heartbeat: %v", err)
	}
	if err := store.RequestQueuedWorkerCancellation(t.Context(), req); !errors.Is(err, documents.ErrIngestionJobLeaseLost) {
		t.Fatalf("claimed target accepted: %v", err)
	}
	recorder := &fakeConversationRecorder{}
	// No client or registry exists: constructing an agent cannot pass this proof.
	loop := &DelegationClaimLoop{Store: store, IdentityID: owner, WorkerID: controlClaimOwner,
		Worker:   RunConfig{Cfg: config.Config{RunDir: t.TempDir()}},
		Delivery: &DelegationDelivery{Recorder: recorder, Steer: &fakeSteerPublisher{}, Archiver: successfulReportArchiver()},
	}
	if err := loop.processJob(t.Context(), claimed[0]); err != nil {
		t.Fatal(err)
	}
	assertCanceledJob(t, pool, job, 1)
	if _, err := store.Heartbeat(t.Context(), heartbeat); !errors.Is(err, documents.ErrIngestionJobLeaseLost) {
		t.Fatalf("heartbeat revived canceled work: %v", err)
	}
	for status, want := range map[string]int64{"queued": 0, "running": 0, "canceled": 1} {
		count, err := store.CountByStatus(t.Context(), owner, status)
		if err != nil || count != want {
			t.Fatalf("%s count = %d, want %d: %v", status, count, want, err)
		}
	}
	if len(recorder.appended) != 1 {
		t.Fatalf("expected one cancellation report, got %d", len(recorder.appended))
	}
}

func TestQueuedWorkerCancellationCannotOverwritePendingDeliveryOrLaterAttempt(t *testing.T) {
	pool := delegationDisposablePool(t)
	owner, conv := seedDelegationDeliveryConversation(t, pool)
	store, job := claimedControlJob(t, pool, owner, conv)
	req := documents.QueuedWorkerCancellationRequest{IdentityID: owner, ConversationID: conv, ChildID: "w-control", JobID: job.ID}
	if err := store.RequestQueuedWorkerCancellation(t.Context(), req); !errors.Is(err, documents.ErrIngestionJobLeaseLost) {
		t.Fatalf("running target accepted: %v", err)
	}
	retried, err := store.Retry(t.Context(), documents.RetryIngestionJobRequest{IdentityID: owner, JobID: job.ID, WorkerID: controlClaimOwner, LeaseGeneration: job.LeaseGeneration, NextAttemptAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RequestQueuedWorkerCancellation(t.Context(), req); !errors.Is(err, documents.ErrIngestionJobLeaseLost) {
		t.Fatalf("old attempt accepted: %v", err)
	}
	req.AttemptCount = retried.AttemptCount
	if err := store.RequestQueuedWorkerCancellation(t.Context(), req); err != nil {
		t.Fatal(err)
	}

	owner, conv = seedDelegationDeliveryConversation(t, pool)
	_, other := claimedControlJob(t, pool, owner, conv)
	req.IdentityID, req.ConversationID = owner, conv
	other.Payload["pending_delivery"] = map[string]any{"target_status": "succeeded"}
	if _, err := store.StageDelegationDelivery(t.Context(), documents.StageDelegationDeliveryRequest{IdentityID: owner, JobID: other.ID, WorkerID: controlClaimOwner, LeaseGeneration: other.LeaseGeneration, Payload: other.Payload}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Retry(t.Context(), documents.RetryIngestionJobRequest{IdentityID: owner, JobID: other.ID, WorkerID: controlClaimOwner, LeaseGeneration: other.LeaseGeneration, NextAttemptAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	req.JobID, req.AttemptCount = other.ID, other.AttemptCount
	if err := store.RequestQueuedWorkerCancellation(t.Context(), req); !errors.Is(err, documents.ErrIngestionJobLeaseLost) {
		t.Fatalf("completed work's report overwritten: %v", err)
	}
}
