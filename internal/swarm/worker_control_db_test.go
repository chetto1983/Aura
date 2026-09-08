//go:build db_integration

package swarm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const controlClaimOwner = "worker-control-test"

func claimedControlJob(t *testing.T, pool *pgxpool.Pool, owner, conv string) (*documents.PostgresIngestionJobStore, documents.IngestionJob) {
	t.Helper()
	store := documents.NewPostgresIngestionJobStore(pool)
	_, err := store.Create(t.Context(), documents.CreateIngestionJobRequest{
		IdentityID: owner, JobType: JobTypeSwarmDelegation, Status: "queued", IdempotencyKey: uuid.NewString(), MaxAttempts: 3,
		Payload: map[string]any{"goal": "controlled work", "conversation_id": conv, "child_id": "w-control", "fanout_key": "f-control", "depth": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := store.Claim(t.Context(), documents.ClaimIngestionJobsRequest{IdentityID: owner, JobType: JobTypeSwarmDelegation, WorkerID: controlClaimOwner, LeaseDuration: time.Minute, BatchSize: 1})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %+v %v", jobs, err)
	}
	return store, jobs[0]
}

func controlCancellation(job documents.IngestionJob) documents.WorkerCancellationRequest {
	return documents.WorkerCancellationRequest{IdentityID: job.IdentityID, JobID: job.ID, WorkerID: controlClaimOwner, LeaseGeneration: job.LeaseGeneration, ChildID: "w-control"}
}

func assertCanceledJob(t *testing.T, pool *pgxpool.Pool, job documents.IngestionJob, attempts int) {
	t.Helper()
	var status string
	var count int
	var cancelled bool
	err := db.WithIdentityTxRaw(t.Context(), pool, job.IdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT status,attempt_count,COALESCE((payload->>'operator_cancelled')::boolean,false) FROM aura.ingestion_jobs WHERE id=$1::uuid", job.ID).Scan(&status, &count, &cancelled)
	})
	if err != nil || status != "canceled" || count != attempts || !cancelled {
		t.Fatalf("cancellation: status=%s attempts=%d intent=%v err=%v", status, count, cancelled, err)
	}
}

func TestWorkerCancellationIsFencedAndSurvivesStaleDeliverySnapshot(t *testing.T) {
	pool := delegationDisposablePool(t)
	owner, conv := seedDelegationDeliveryConversation(t, pool)
	store, job := claimedControlJob(t, pool, owner, conv)
	req := controlCancellation(job)
	wrong := req
	wrong.LeaseGeneration++
	if err := store.RequestWorkerCancellation(t.Context(), wrong); !errors.Is(err, documents.ErrIngestionJobLeaseLost) {
		t.Fatalf("stale lease: %v", err)
	}
	wrong = req
	wrong.IdentityID = uuid.NewString()
	if err := store.RequestWorkerCancellation(t.Context(), wrong); !errors.Is(err, documents.ErrIngestionJobLeaseLost) {
		t.Fatalf("foreign owner: %v", err)
	}
	if err := store.RequestWorkerCancellation(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	staged, err := store.StageDelegationDelivery(t.Context(), documents.StageDelegationDeliveryRequest{
		IdentityID: owner, JobID: job.ID, WorkerID: controlClaimOwner, LeaseGeneration: job.LeaseGeneration, Payload: job.Payload,
	})
	if err != nil || staged.Payload["operator_cancelled"] != true {
		t.Fatalf("stale snapshot erased cancellation: %+v %v", staged.Payload, err)
	}
}

func TestWorkerCancellationRecoveryDoesNotConstructAnotherAgent(t *testing.T) {
	pool := delegationDisposablePool(t)
	owner, conv := seedDelegationDeliveryConversation(t, pool)
	store, job := claimedControlJob(t, pool, owner, conv)
	if err := store.RequestWorkerCancellation(t.Context(), controlCancellation(job)); err != nil {
		t.Fatal(err)
	}
	// Model a dead owner's expired lease on this disposable database. The live
	// SIGKILL probe in spike103 separately verifies the real 300-second timer.
	if err := db.WithIdentityTxRaw(t.Context(), pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE aura.ingestion_jobs SET locked_until=now()-interval '1 second' WHERE id=$1::uuid", job.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := store.Claim(t.Context(), documents.ClaimIngestionJobsRequest{IdentityID: owner, JobType: JobTypeSwarmDelegation, WorkerID: controlClaimOwner, LeaseDuration: time.Minute, BatchSize: 1})
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("reclaim: %+v %v", reclaimed, err)
	}
	model := newRouter().route("controlled work", outcome{kind: "fail"})
	recorder := &fakeConversationRecorder{}
	loop := &DelegationClaimLoop{Store: store, IdentityID: owner, WorkerID: controlClaimOwner, Worker: testRunConfig(t, model, 25), Delivery: &DelegationDelivery{Recorder: recorder, Steer: &fakeSteerPublisher{}, Archiver: successfulReportArchiver()}}
	if err := loop.processJob(t.Context(), reclaimed[0]); err != nil {
		t.Fatal(err)
	}
	if model.calls["controlled work"] != 0 {
		t.Fatal("recovery ran a canceled agent")
	}
	assertCanceledJob(t, pool, job, 2)
	if len(recorder.appended) != 1 {
		t.Fatalf("want one cancellation report, got %d", len(recorder.appended))
	}
}

type controlRuntimeProbe struct {
	started chan agent.WorkerControlParams
}

func (p *controlRuntimeProbe) StartWorker(_ context.Context, params agent.WorkerControlParams) (agent.WorkerControlSession, error) {
	p.started <- params
	var once sync.Once
	var err error
	return agent.WorkerControlSession{RunID: "run-" + uuid.NewString(), Finish: func() error { once.Do(func() { err = params.OnClose() }); return err }}, nil
}

type controlBlockingClient struct{ started chan struct{} }

func (c *controlBlockingClient) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	close(c.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestWorkerStopPersistsCancellationWithoutRetry(t *testing.T) {
	pool := delegationDisposablePool(t)
	owner, conv := seedDelegationDeliveryConversation(t, pool)
	store, job := claimedControlJob(t, pool, owner, conv)
	ctx, cancel := context.WithCancel(identityctx.WithIdentityID(t.Context(), owner))
	defer cancel()
	runtime := &controlRuntimeProbe{started: make(chan agent.WorkerControlParams, 1)}
	model := &controlBlockingClient{started: make(chan struct{})}
	worker := testRunConfig(t, model, 25)
	worker.Controls = runtime
	worker.Steer = steer.NewPostgresStore(pool, steer.Config{Max: 8, MaxBytes: 4096})
	loop := &DelegationClaimLoop{Store: store, IdentityID: owner, WorkerID: controlClaimOwner, LeaseDuration: time.Minute, Worker: worker, Delivery: &DelegationDelivery{Recorder: &fakeConversationRecorder{}, Steer: &fakeSteerPublisher{}, Archiver: successfulReportArchiver()}}
	done := make(chan error, 1)
	go func() { done <- loop.processJob(ctx, job); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	var params agent.WorkerControlParams
	select {
	case params = <-runtime.started:
	case err := <-done:
		t.Fatalf("worker ended before registration: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not register")
	}
	select {
	case <-model.started:
	case err := <-done:
		t.Fatalf("worker ended before model start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("model did not start")
	}
	if err := params.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop promptly")
	}
	assertCanceledJob(t, pool, job, 1)
}
