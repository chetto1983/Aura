//go:build db_integration

// Real Go coverage for 51-06b Task 1/2 against a disposable Postgres: the
// pause-and-park atomicity PauseAndPark declares, the parked-row-invisible-to-claim
// property the awaiting_input status vocabulary (migration 0107) exists for, and the
// full park -> answer -> observe -> un-park-exactly-once -> claim -> resume-through-
// runChild round trip. Runs as aura_app (never aura, BYPASSRLS) via
// delegationDisposablePool, the SAME throwaway-database helper delegation_queue_test.go
// already established for this package.
package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPauseAndPark mirrors cmd/aura's delegationPauseCommitter VERBATIM (that type
// lives in package main and cannot be imported from here): one db.WithIdentityTx
// composing askuser.Store.InsertTx + documents.PostgresIngestionJobStore.
// ParkIngestionJobAwaitingInputTx, rolling the whole tx back (including the pause
// insert) when the park's conditional UPDATE matches zero rows. Re-implemented here
// rather than exported from cmd/aura because cmd/aura already imports internal/swarm
// (the reverse import would cycle) -- this test proves the SAME production pattern,
// not a different one.
type testPauseAndPark struct {
	pool  *pgxpool.Pool
	pause *askuser.Store
	jobs  *documents.PostgresIngestionJobStore
}

type testAnsweredRejector struct {
	store *documents.PostgresIngestionJobStore
}

func (r testAnsweredRejector) RejectAnsweredDelegation(ctx context.Context, req RejectAnsweredDelegationRequest) (bool, error) {
	return r.store.RejectAnsweredDelegation(ctx, documents.RejectAnsweredDelegationRequest{
		IdentityID: req.IdentityID, JobID: req.JobID, PendingActionID: req.PendingActionID,
		ErrorCode: req.ErrorCode, ErrorMessage: req.ErrorMessage,
	})
}

var errTestParkLost = errors.New("test pause and park: park lost lease")

func (c *testPauseAndPark) OpenPauseAndPark(ctx context.Context, pause askuser.InsertParams, park documents.ParkAwaitingInputRequest) (bool, error) {
	err := db.WithIdentityTx(ctx, c.pool, park.IdentityID, func(q *sqlc.Queries) error {
		if err := c.pause.InsertTx(ctx, q, pause); err != nil {
			return err
		}
		n, err := c.jobs.ParkIngestionJobAwaitingInputTx(ctx, q, park)
		if err != nil {
			return err
		}
		if n == 0 {
			return errTestParkLost
		}
		return nil
	})
	if errors.Is(err, errTestParkLost) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// seedSwarmConversation inserts a throwaway conversation (FK parent for
// aura.paused_states — the 0032 trigger fills identity_id by SELECTing this row).
func seedSwarmConversation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO aura.conversations (id, identity_id, model) VALUES ($1, $2, 'test-model')", id, identityID)
		return e
	})
	if err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
			_, e := tx.Exec(context.Background(), "DELETE FROM aura.conversations WHERE id = $1", id)
			return e
		})
	})
	return id
}

func countPausesForToken(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, convID, token string) int {
	t.Helper()
	var n int
	err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM aura.paused_states WHERE conversation_id = $1 AND token = $2",
			convID, token).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count paused_states: %v", err)
	}
	return n
}

func jobStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, jobID string) string {
	t.Helper()
	var status string
	err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT status FROM aura.ingestion_jobs WHERE id = $1::uuid", jobID).Scan(&status)
	})
	if err != nil {
		t.Fatalf("read job status: %v", err)
	}
	return status
}

// TestOpenPauseAndParkAtomicity is Task 1's own named acceptance test: writing the
// pause AND parking the row happen in ONE transaction. The happy path proves both
// land together; the forced-failure path (a park whose conditional UPDATE matches
// zero rows, because the row was never claimed/running) proves the pause insert rolls
// back too -- a pause with no parked row would be answered into nothing.
func TestOpenPauseAndParkAtomicity(t *testing.T) {
	pool := delegationDisposablePool(t)
	ctx := context.Background()
	identityID := seedSwarmTestIdentity(t, ctx, pool)
	convID := seedSwarmConversation(t, ctx, pool, identityID)
	store := documents.NewPostgresIngestionJobStore(pool)
	parker := &testPauseAndPark{pool: pool, pause: askuser.New(pool), jobs: store}

	t.Run("happy path", func(t *testing.T) {
		job, err := store.Create(ctx, documents.CreateIngestionJobRequest{
			IdentityID: identityID, JobType: JobTypeSwarmDelegation, Status: "queued",
			IdempotencyKey: "pause-atomic-ok", MaxAttempts: 3,
			Payload: map[string]any{"goal": "g", "conversation_id": convID, "fanout_key": "f-test"},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		claimed, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
			IdentityID: identityID, JobType: JobTypeSwarmDelegation, WorkerID: "w",
			LeaseDuration: time.Minute, BatchSize: 10,
		})
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claim: %v (%d rows)", err, len(claimed))
		}
		row := claimed[0]

		token := uuid.Must(uuid.NewV7()).String()
		fence := uuid.Must(uuid.NewV7()).String()
		owner := job.ID
		parked, err := parker.OpenPauseAndPark(ctx,
			askuser.InsertParams{
				Token: token, ConversationID: convID, Kind: "clarification",
				Question: "which inbox?", ToolCallID: "call-1",
				OwningWorkerID: &owner, PendingActionID: &fence,
			},
			documents.ParkAwaitingInputRequest{
				IdentityID: identityID, JobID: row.ID, WorkerID: "w",
				LeaseGeneration: row.LeaseGeneration,
				Payload:         map[string]any{"goal": "g", "conversation_id": convID, "fanout_key": "f-test"},
			})
		if err != nil {
			t.Fatalf("OpenPauseAndPark: %v", err)
		}
		if !parked {
			t.Fatal("OpenPauseAndPark reported not parked on the happy path")
		}
		if n := countPausesForToken(t, ctx, pool, identityID, convID, token); n != 1 {
			t.Fatalf("paused_states rows = %d, want 1 (the pause must be written)", n)
		}
		if status := jobStatus(t, ctx, pool, identityID, row.ID); status != "awaiting_input" {
			t.Fatalf("job status = %q, want awaiting_input", status)
		}

		// Parked-row-invisible-to-claim: the SAME identity's next claim pass must not
		// re-claim an awaiting_input row.
		again, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
			IdentityID: identityID, JobType: JobTypeSwarmDelegation, WorkerID: "w2",
			LeaseDuration: time.Minute, BatchSize: 10,
		})
		if err != nil {
			t.Fatalf("claim after park: %v", err)
		}
		if len(again) != 0 {
			t.Fatalf("claimed %d rows while parked awaiting_input, want 0", len(again))
		}
	})

	t.Run("forced park failure rolls back the pause too", func(t *testing.T) {
		// A queued (never claimed) row: ParkIngestionJobAwaitingInputTx's conditional
		// UPDATE requires status='running' + a matching lease, so this park's
		// RowsAffected is 0 -- OpenPauseAndPark must report (false, nil) and the pause
		// row must NOT exist afterward.
		job, err := store.Create(ctx, documents.CreateIngestionJobRequest{
			IdentityID: identityID, JobType: JobTypeSwarmDelegation, Status: "queued",
			IdempotencyKey: "pause-atomic-rollback", MaxAttempts: 3,
			Payload: map[string]any{"goal": "g", "conversation_id": convID, "fanout_key": "f-test"},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		token := uuid.Must(uuid.NewV7()).String()
		fence := uuid.Must(uuid.NewV7()).String()
		owner := job.ID
		parked, err := parker.OpenPauseAndPark(ctx,
			askuser.InsertParams{
				Token: token, ConversationID: convID, Kind: "clarification",
				Question: "which inbox?", ToolCallID: "call-1",
				OwningWorkerID: &owner, PendingActionID: &fence,
			},
			documents.ParkAwaitingInputRequest{
				IdentityID: identityID, JobID: job.ID, WorkerID: "w",
				LeaseGeneration: 0, // never claimed -- the row's real generation is not 0
				Payload:         map[string]any{"goal": "g", "conversation_id": convID, "fanout_key": "f-test"},
			})
		if err != nil {
			t.Fatalf("OpenPauseAndPark: %v", err)
		}
		if parked {
			t.Fatal("OpenPauseAndPark reported parked against an unclaimed row")
		}
		if n := countPausesForToken(t, ctx, pool, identityID, convID, token); n != 0 {
			t.Fatalf("paused_states rows = %d, want 0 -- the pause must roll back with the failed park", n)
		}
		if status := jobStatus(t, ctx, pool, identityID, job.ID); status != "queued" {
			t.Fatalf("job status = %q, want queued (untouched by the rolled-back park)", status)
		}
	})
}

func TestInvalidAnsweredDelegationIsQuarantinedInPostgres(t *testing.T) {
	pool := delegationDisposablePool(t)
	ctx := context.Background()
	identityID := seedSwarmTestIdentity(t, ctx, pool)
	convID := seedSwarmConversation(t, ctx, pool, identityID)
	store := documents.NewPostgresIngestionJobStore(pool)
	pauseStore := askuser.New(pool)
	parker := &testPauseAndPark{pool: pool, pause: pauseStore, jobs: store}

	job, err := store.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, Status: "queued",
		IdempotencyKey: "invalid-resume-quarantine", MaxAttempts: 3,
		Payload: map[string]any{"goal": "g", "conversation_id": convID, "child_id": "w1", "fanout_key": "f-test"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	claimed, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, WorkerID: "w",
		LeaseDuration: time.Minute, BatchSize: 1,
	})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %d, %v", len(claimed), err)
	}
	token := uuid.Must(uuid.NewV7()).String()
	fence := uuid.Must(uuid.NewV7()).String()
	resume := &DelegationResumeState{
		WorkerID: job.ID, Goal: "g", ConversationID: convID,
		PendingToolCallID: "call-1", PendingActionID: fence, PauseToken: token,
		AgentIdentity: identityID,
	}
	payloadMap, err := delegationPayloadMap(DelegationPayload{
		Goal: "g", ConversationID: convID, ChildID: "w1", FanoutKey: "f-test", Resume: resume,
	})
	if err != nil {
		t.Fatalf("encode parked payload: %v", err)
	}
	owner := job.ID
	parked, err := parker.OpenPauseAndPark(ctx, askuser.InsertParams{
		Token: token, ConversationID: convID, Kind: "clarification", Question: "which inbox?",
		ToolCallID: "call-1", OwningWorkerID: &owner, PendingActionID: &fence,
	}, documents.ParkAwaitingInputRequest{
		IdentityID: identityID, JobID: job.ID, WorkerID: "w",
		LeaseGeneration: claimed[0].LeaseGeneration, Payload: payloadMap,
	})
	if err != nil || !parked {
		t.Fatalf("park = %v, %v", parked, err)
	}
	if err := pauseStore.MarkResumed(withIdentity(ctx, identityID), token,
		askuser.ResumeAnswer{Action: askuser.ActionAccept, Content: "answer"}); err != nil {
		t.Fatalf("answer pause: %v", err)
	}

	resumeMap := payloadMap["resume"].(map[string]any)
	resumeMap["pending_action_id"] = uuid.Must(uuid.NewV7()).String()
	encoded, err := json.Marshal(payloadMap)
	if err != nil {
		t.Fatalf("encode poisoned payload: %v", err)
	}
	if err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		_, updateErr := tx.Exec(ctx, "UPDATE aura.ingestion_jobs SET payload = $2::jsonb WHERE id = $1", job.ID, string(encoded))
		return updateErr
	}); err != nil {
		t.Fatalf("poison parked payload: %v", err)
	}

	observer := NewDelegationResumeObserver(store)
	observer.Rejector = testAnsweredRejector{store: store}
	if n, err := observer.ProcessOnce(ctx, identityID, 10); n != 0 || err == nil || !strings.Contains(err.Error(), "fence mismatch") {
		t.Fatalf("poison observer = %d, %v; want quarantine with the structural error reported", n, err)
	}
	if status := jobStatus(t, ctx, pool, identityID, job.ID); status != "dead_letter" {
		t.Fatalf("poison job status = %q, want dead_letter", status)
	}
	if n, err := observer.ProcessOnce(ctx, identityID, 10); n != 0 || err != nil {
		t.Fatalf("second observer = %d, %v; quarantined row must not recur", n, err)
	}
}

// TestDelegationPauseResumeFullLifecycle drives the plan's own named end-to-end
// scenario against a REAL queue and REAL pause store: claim -> pause+park
// (openPauseAndPark) -> answer (the SAME generic askuser.Store.MarkResumed the
// /api/approvals bridge uses) -> observe (DelegationResumeObserver un-parks exactly
// once) -> claim again -> processJob rebuilds through runChild with the answer
// substituted in, and completes. Two sibling jobs pause independently to prove
// sibling-independence: answering job A's pause must never touch job B's.
func TestDelegationPauseResumeFullLifecycle(t *testing.T) {
	pool := delegationDisposablePool(t)
	ctx := context.Background()
	identityID := seedSwarmTestIdentity(t, ctx, pool)
	convA := seedSwarmConversation(t, ctx, pool, identityID)
	convB := seedSwarmConversation(t, ctx, pool, identityID)
	store := documents.NewPostgresIngestionJobStore(pool)
	pauseStore := askuser.New(pool)
	parker := &testPauseAndPark{pool: pool, pause: pauseStore, jobs: store}

	const goalA = "summarise inbox A"
	const goalB = "summarise inbox B"
	router := newRouter().
		route(goalA, outcome{kind: "pause_then_ok", question: "which inbox A?", text: "A done"}).
		route(goalB, outcome{kind: "pause_then_ok", question: "which inbox B?", text: "B done"})
	worker := testRunConfig(t, router, 25)

	l := &DelegationClaimLoop{
		Store: store, IdentityID: identityID, WorkerID: "w",
		Worker: worker, LeaseDuration: time.Minute,
		Delivery:    &DelegationDelivery{Recorder: &fakeConversationRecorder{}},
		PauseParker: parker,
	}

	jobA, err := store.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, Status: "queued",
		IdempotencyKey: "lifecycle-a", MaxAttempts: 3,
		Payload: map[string]any{"goal": goalA, "conversation_id": convA, "fanout_key": "f-a"},
	})
	if err != nil {
		t.Fatalf("create job A: %v", err)
	}
	jobB, err := store.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, Status: "queued",
		IdempotencyKey: "lifecycle-b", MaxAttempts: 3,
		Payload: map[string]any{"goal": goalB, "conversation_id": convB, "fanout_key": "f-b"},
	})
	if err != nil {
		t.Fatalf("create job B: %v", err)
	}

	// First pass: both jobs claim, run, pause, and park.
	if n, err := l.ProcessOnce(withToolCtx(ctx, t)); err != nil || n != 2 {
		t.Fatalf("first ProcessOnce = (%d, %v), want (2, nil)", n, err)
	}
	l.Wait()
	if status := jobStatus(t, ctx, pool, identityID, jobA.ID); status != "awaiting_input" {
		t.Fatalf("job A status = %q, want awaiting_input", status)
	}
	if status := jobStatus(t, ctx, pool, identityID, jobB.ID); status != "awaiting_input" {
		t.Fatalf("job B status = %q, want awaiting_input", status)
	}

	// Answer ONLY job A's pause, through the SAME generic path /api/approvals uses.
	tokenA := findPauseToken(t, ctx, pool, identityID, convA)
	if err := pauseStore.MarkResumed(withIdentity(ctx, identityID), tokenA,
		askuser.ResumeAnswer{Action: askuser.ActionAccept, Content: "inbox-a@example.com"}); err != nil {
		t.Fatalf("MarkResumed job A: %v", err)
	}

	observer := NewDelegationResumeObserver(store)
	n, err := observer.ProcessOnce(ctx, identityID, 10)
	if err != nil {
		t.Fatalf("observer ProcessOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("observer unparked %d jobs, want 1 (only A was answered)", n)
	}
	// Sibling independence: B must still be parked, untouched.
	if status := jobStatus(t, ctx, pool, identityID, jobB.ID); status != "awaiting_input" {
		t.Fatalf("job B status = %q after answering only A, want it to remain awaiting_input", status)
	}
	if status := jobStatus(t, ctx, pool, identityID, jobA.ID); status != "queued" {
		t.Fatalf("job A status = %q after un-park, want queued (claimable again)", status)
	}

	// A second observer pass over the same (already un-parked) answer must not
	// double-unpark -- ListAnsweredAwaitingInputJobs only surfaces status='awaiting_input'
	// rows, so job A (now 'queued') is no longer returned.
	if n2, err := observer.ProcessOnce(ctx, identityID, 10); err != nil || n2 != 0 {
		t.Fatalf("second observer pass = (%d, %v), want (0, nil) -- un-park exactly once", n2, err)
	}

	// Claim again: only A (queued) is claimable; B (awaiting_input) is not.
	if n, err := l.ProcessOnce(withToolCtx(ctx, t)); err != nil || n != 1 {
		t.Fatalf("resume ProcessOnce = (%d, %v), want (1, nil) -- only the answered job resumes", n, err)
	}
	l.Wait()
	if status := jobStatus(t, ctx, pool, identityID, jobA.ID); status != "succeeded" {
		t.Fatalf("job A status = %q after resume, want succeeded (the worker completed post-answer)", status)
	}
	if status := jobStatus(t, ctx, pool, identityID, jobB.ID); status != "awaiting_input" {
		t.Fatalf("job B status = %q, want it still parked and untouched by A's resume", status)
	}
}

// findPauseToken reads back the single pending pause token for a conversation --
// openPauseAndPark's own Insert wrote it, so a test that answered the pause needs it
// to call MarkResumed.
func findPauseToken(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, convID string) string {
	t.Helper()
	var token string
	err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT token::text FROM aura.paused_states WHERE conversation_id = $1 AND resumed_at IS NULL", convID,
		).Scan(&token)
	})
	if err != nil {
		t.Fatalf("find pause token: %v", err)
	}
	return token
}

// withIdentity is the minimal identityctx carrier this file's MarkResumed call needs
// (askuser.Store.MarkResumed reads the caller identity off ctx via
// db.WithCallerIdentityTx, which reads identityctx.IdentityID).
func withIdentity(ctx context.Context, identityID string) context.Context {
	return identityctx.WithIdentityID(ctx, identityID)
}
