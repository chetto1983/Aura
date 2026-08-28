//go:build db_integration

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testWorkerPauseJobType mirrors swarm.JobTypeSwarmDelegation as a literal: importing
// internal/swarm from a runner test would drag the whole swarm package into this
// package's test binary for one string.
const testWorkerPauseJobType = "swarm_delegation"

// testWorkerPauseLister / testWorkerPauseQueue mirror cmd/aura's
// workerPauseListerAdapter / workerPauseQueueAdapter VERBATIM (package main cannot be
// imported from here): the SAME production mapping, proven against the real store.
type testWorkerPauseLister struct {
	store *documents.PostgresIngestionJobStore
}

func (a testWorkerPauseLister) ListExpiredWorkerPauses(ctx context.Context, identityID string, cutoff time.Time, limit int) ([]ExpiredWorkerPause, error) {
	rows, err := a.store.ListExpiredAwaitingInput(ctx, identityID, cutoff, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ExpiredWorkerPause, 0, len(rows))
	for _, r := range rows {
		out = append(out, ExpiredWorkerPause{JobID: r.JobID, IdentityID: r.IdentityID, Pause: askuser.Pending{
			Token: r.PauseToken, ConversationID: r.ConversationID, Kind: r.Kind, Question: r.Question,
			ToolCallID: r.ToolCallID, PendingActionID: r.PendingActionID, OwningWorkerID: r.JobID,
		}})
	}
	return out, nil
}

type testWorkerPauseQueue struct {
	store *documents.PostgresIngestionJobStore
	err   error // injected: a failing resolution must roll the pause claim and the trace back too
}

func (a testWorkerPauseQueue) ResolveAwaitingInputTx(ctx context.Context, q *sqlc.Queries, identityID, jobID, errorMessage string) (int64, error) {
	if a.err != nil {
		return 0, a.err
	}
	return a.store.ResolveIngestionJobAwaitingInputTx(ctx, q, documents.ResolveAwaitingInputRequest{
		IdentityID: identityID, JobID: jobID, ErrorMessage: errorMessage,
	})
}

// parkedWorkerPause is one REAL claim -> pause -> park cycle against the store, the
// exact rows 51-06b Task 1 leaves behind (owning_worker_id = job id, pending_action_id
// = the fence, payload.resume.pause_token = the pause), backdated past the TTL.
type parkedWorkerPause struct {
	jobID, token, fence, question string
}

func parkWorkerPause(t *testing.T, pool *pgxpool.Pool, store *documents.PostgresIngestionJobStore, pause *askuser.Store, convID, key, question string, createdAt time.Time) parkedWorkerPause {
	t.Helper()
	ctx := ownerCtx()
	job, err := store.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: localIdentityID, JobType: testWorkerPauseJobType, Status: "queued",
		IdempotencyKey: key, MaxAttempts: 3, Payload: map[string]any{"goal": "g", "conversation_id": convID},
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithIdentityTxRaw(context.Background(), pool, localIdentityID, func(tx pgx.Tx) error {
			_, e := tx.Exec(context.Background(), "DELETE FROM aura.ingestion_jobs WHERE id=$1::uuid", job.ID)
			return e
		})
	})
	claimed, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID: localIdentityID, JobType: testWorkerPauseJobType, WorkerID: "w-" + key,
		LeaseDuration: time.Minute, BatchSize: 10,
	})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v (%d rows)", err, len(claimed))
	}
	row := claimed[0]
	token := uuid.Must(uuid.NewV7()).String()
	fence := uuid.Must(uuid.NewV7()).String()
	owner := job.ID
	if err := pause.Insert(ctx, askuser.InsertParams{
		Token: token, ConversationID: convID, Kind: "clarification", Question: question,
		ToolCallID: "call-" + key, OwningWorkerID: &owner, PendingActionID: &fence,
	}); err != nil {
		t.Fatalf("insert pause: %v", err)
	}
	n, err := store.ParkIngestionJobAwaitingInput(ctx, documents.ParkAwaitingInputRequest{
		IdentityID: localIdentityID, JobID: row.ID, WorkerID: "w-" + key, LeaseGeneration: row.LeaseGeneration,
		Payload: map[string]any{"goal": "g", "conversation_id": convID, "resume": map[string]any{
			"pause_token": token, "pending_action_id": fence, "worker_id": job.ID,
		}},
	})
	if err != nil || n != 1 {
		t.Fatalf("park: %v (rows %d)", err, n)
	}
	if err := db.WithIdentityTxRaw(ctx, pool, localIdentityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE aura.paused_states SET created_at=$1 WHERE token=$2", createdAt, token)
		return err
	}); err != nil {
		t.Fatalf("backdate pause: %v", err)
	}
	return parkedWorkerPause{jobID: job.ID, token: token, fence: fence, question: question}
}

func workerJobState(t *testing.T, pool *pgxpool.Pool, jobID string) (status, errorCode string) {
	t.Helper()
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		var code *string
		if err := tx.QueryRow(ownerCtx(), "SELECT status, error_code FROM aura.ingestion_jobs WHERE id=$1::uuid", jobID).Scan(&status, &code); err != nil {
			return err
		}
		if code != nil {
			errorCode = *code
		}
		return nil
	})
	return status, errorCode
}

func assistantTurnContents(t *testing.T, pool *pgxpool.Pool, convID string) []string {
	t.Helper()
	var out []string
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ownerCtx(), "SELECT content FROM aura.conversation_turns WHERE conversation_id=$1 AND role='assistant' ORDER BY seq", convID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out
}

func resumedAnswer(t *testing.T, pool *pgxpool.Pool, token string) askuser.ResumeAnswer {
	t.Helper()
	var raw []byte
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ownerCtx(), "SELECT resumed_answer FROM aura.paused_states WHERE token=$1", token).Scan(&raw)
	})
	var answer askuser.ResumeAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("decode resumed answer: %v", err)
	}
	return answer
}

// TestExpireWorkerPausesExpiresWithTraceAndResolvesQueueRow is Task 3's named
// acceptance: an unanswered worker pause past TTL is marked expired, its readable
// trace lands in the ORIGIN conversation as an assistant turn naming the worker and
// the question, and its parked queue row reaches the stated terminal state (failed /
// awaiting_input_expired) -- all in one pass; a second pass expires nothing.
func TestExpireWorkerPausesExpiresWithTraceAndResolvesQueueRow(t *testing.T) {
	pool := migratedRunnerPool(t)
	r, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	convID := newIntegrationConversation(t, pool, convStore)
	store := documents.NewPostgresIngestionJobStore(pool)
	ctx := ownerCtx()
	now := time.Now().UTC()
	ttl := time.Hour

	due := parkWorkerPause(t, pool, store, pauseStore, convID, "sweep-due", "Which region should I deploy to?", now.Add(-2*ttl))
	fresh := parkWorkerPause(t, pool, store, pauseStore, convID, "sweep-fresh", "Still thinking?", now)
	deps := WorkerPauseSweepDeps{
		Lister:  testWorkerPauseLister{store: store},
		Expirer: NewPoolWorkerPauseExpirer(pool, convStore, pauseStore, testWorkerPauseQueue{store: store}),
	}

	expired, err := r.ExpireWorkerPauses(ctx, deps, localIdentityID, now, ttl, 10)
	if err != nil || expired != 1 {
		t.Fatalf("ExpireWorkerPauses = %d, %v; want 1, nil (only the pause past TTL)", expired, err)
	}
	if answer := resumedAnswer(t, pool, due.token); answer.Action != askuser.ActionExpired || answer.Content != expiredWorkerPauseContent {
		t.Fatalf("resumed answer = %#v, want the visible worker-pause expiry", answer)
	}
	if status, code := workerJobState(t, pool, due.jobID); status != "failed" || code != "awaiting_input_expired" {
		t.Fatalf("queue row = %s/%s, want failed/awaiting_input_expired (dead_letter would mean retried to exhaustion)", status, code)
	}
	traces := assistantTurnContents(t, pool, convID)
	if len(traces) != 1 || !strings.Contains(traces[0], due.jobID) || !strings.Contains(traces[0], due.question) {
		t.Fatalf("assistant trace turns = %q; want exactly one naming worker %s and question %q", traces, due.jobID, due.question)
	}
	if n := countPersistedToolTurns(t, pool, convID); n != 0 {
		t.Fatalf("persisted tool turns = %d, want 0 -- the trace is an assistant turn, never an orphan RoleTool", n)
	}
	if ts := resumedAt(t, pool, fresh.token); ts != nil {
		t.Fatal("the pause still inside its TTL was expired")
	}
	if status, _ := workerJobState(t, pool, fresh.jobID); status != "awaiting_input" {
		t.Fatalf("fresh pause's row = %s, want awaiting_input (untouched)", status)
	}
	if _, err := r.SubmitAnswer(ctx, due.token, ResponseInput{Action: askuser.ActionAccept, Content: "eu-west"}); !errors.Is(err, askuser.ErrPauseExpired) {
		t.Fatalf("late operator answer error = %v, want ErrPauseExpired", err)
	}

	again, err := r.ExpireWorkerPauses(ctx, deps, localIdentityID, now, ttl, 10)
	if err != nil || again != 0 {
		t.Fatalf("second pass = %d, %v; want 0, nil (idempotent)", again, err)
	}
	if traces := assistantTurnContents(t, pool, convID); len(traces) != 1 {
		t.Fatalf("second pass wrote %d trace turns, want the original 1", len(traces))
	}
}

// TestExpireWorkerPausesRollsBackWhenTheQueueRowCannotBeResolved proves D-08 extended
// to the queue row: the pause claim, the trace and the row resolution are ONE
// transaction, so a failing resolution leaves the pause pending (resumed_at IS NULL),
// writes no trace, and keeps the row parked -- the next pass retries the whole
// outcome rather than leaving an expired pause with a row nobody will ever resolve.
func TestExpireWorkerPausesRollsBackWhenTheQueueRowCannotBeResolved(t *testing.T) {
	pool := migratedRunnerPool(t)
	r, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	convID := newIntegrationConversation(t, pool, convStore)
	store := documents.NewPostgresIngestionJobStore(pool)
	ctx := ownerCtx()
	now := time.Now().UTC()
	ttl := time.Hour
	due := parkWorkerPause(t, pool, store, pauseStore, convID, "sweep-rollback", "Which inbox?", now.Add(-2*ttl))
	injected := errors.New("queue row resolution failed")

	expired, err := r.ExpireWorkerPauses(ctx, WorkerPauseSweepDeps{
		Lister:  testWorkerPauseLister{store: store},
		Expirer: NewPoolWorkerPauseExpirer(pool, convStore, pauseStore, testWorkerPauseQueue{store: store, err: injected}),
	}, localIdentityID, now, ttl, 10)
	if expired != 0 || !errors.Is(err, injected) {
		t.Fatalf("ExpireWorkerPauses = %d, %v; want 0 and the injected error", expired, err)
	}
	if ts := resumedAt(t, pool, due.token); ts != nil {
		t.Fatal("pause was marked expired although its queue row was not resolved (tx did not roll back)")
	}
	if traces := assistantTurnContents(t, pool, convID); len(traces) != 0 {
		t.Fatalf("trace turns = %q, want none after rollback", traces)
	}
	if status, _ := workerJobState(t, pool, due.jobID); status != "awaiting_input" {
		t.Fatalf("queue row = %s, want awaiting_input (still parked, retried next pass)", status)
	}

	// The next pass, with a healthy resolver, completes the SAME outcome.
	expired, err = r.ExpireWorkerPauses(ctx, WorkerPauseSweepDeps{
		Lister:  testWorkerPauseLister{store: store},
		Expirer: NewPoolWorkerPauseExpirer(pool, convStore, pauseStore, testWorkerPauseQueue{store: store}),
	}, localIdentityID, now, ttl, 10)
	if err != nil || expired != 1 {
		t.Fatalf("retry pass = %d, %v; want 1, nil", expired, err)
	}
	if status, _ := workerJobState(t, pool, due.jobID); status != "failed" {
		t.Fatalf("queue row after retry = %s, want failed", status)
	}
}

// TestExpireWorkerPausesSkipsAPauseTheOperatorAnsweredFirst: an answered pause past
// TTL is not the sweep's to close -- the resume observer un-parks it. The fenced claim
// matches zero rows (ErrPauseNotFound), the sweep skips it, and neither the answer nor
// the parked row is touched.
func TestExpireWorkerPausesSkipsAPauseTheOperatorAnsweredFirst(t *testing.T) {
	pool := migratedRunnerPool(t)
	r, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	convID := newIntegrationConversation(t, pool, convStore)
	store := documents.NewPostgresIngestionJobStore(pool)
	ctx := ownerCtx()
	now := time.Now().UTC()
	ttl := time.Hour
	due := parkWorkerPause(t, pool, store, pauseStore, convID, "sweep-answered", "Which inbox?", now.Add(-2*ttl))
	human := askuser.ResumeAnswer{Action: askuser.ActionAccept, Content: "the shared one"}
	if err := pauseStore.MarkResumedFenced(ctx, due.token, human, due.fence); err != nil {
		t.Fatalf("operator answer: %v", err)
	}

	expired, err := r.ExpireWorkerPauses(ctx, WorkerPauseSweepDeps{
		Lister:  testWorkerPauseLister{store: store},
		Expirer: NewPoolWorkerPauseExpirer(pool, convStore, pauseStore, testWorkerPauseQueue{store: store}),
	}, localIdentityID, now, ttl, 10)
	if err != nil || expired != 0 {
		t.Fatalf("ExpireWorkerPauses = %d, %v; want 0, nil (an answered pause is not the sweep's)", expired, err)
	}
	if answer := resumedAnswer(t, pool, due.token); answer != human {
		t.Fatalf("resumed answer = %#v, want the operator's %#v untouched", answer, human)
	}
	if status, _ := workerJobState(t, pool, due.jobID); status != "awaiting_input" {
		t.Fatalf("queue row = %s, want awaiting_input (left for the resume observer)", status)
	}
	if traces := assistantTurnContents(t, pool, convID); len(traces) != 0 {
		t.Fatalf("trace turns = %q, want none", traces)
	}
}
