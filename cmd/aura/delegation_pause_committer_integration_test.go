//go:build db_integration

package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/swarm"
	"github.com/google/uuid"
)

// Exercise the production pause writer AND SubmitAnswer: calling MarkResumed
// directly skips policy, fence propagation and parent/worker history ownership.
func TestDelegationPauseResumesThroughRunner(t *testing.T) {
	pool := mcpAuditMigratedPool(t)
	owner, convID := uuid.NewString(), uuid.NewString()
	ctx := identityctx.WithIdentityID(t.Context(), owner)
	if _, err := pool.Exec(ctx, "INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')", owner, "multi103-"+owner); err != nil {
		t.Fatal(err)
	}
	conv := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	if _, err := conv.Create(ctx, conversations.CreateParams{ID: convID, IdentityID: owner, Model: "test", Metadata: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	pauses := askuser.New(pool)
	jobs := documents.NewPostgresIngestionJobStore(pool)
	job, err := jobs.Create(ctx, documents.CreateIngestionJobRequest{IdentityID: owner, JobType: swarm.JobTypeSwarmDelegation,
		Status: "queued", IdempotencyKey: uuid.NewString(), MaxAttempts: 3, Payload: map[string]any{"goal": "multiply by eleven", "fanout_key": "f-multi103"}})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := jobs.Claim(ctx, documents.ClaimIngestionJobsRequest{IdentityID: owner, JobType: swarm.JobTypeSwarmDelegation,
		WorkerID: "multi103", LeaseDuration: time.Minute, BatchSize: 1})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v, %v", claimed, err)
	}
	token, fence := uuid.NewString(), uuid.NewString()
	state := swarm.DelegationResumeState{WorkerID: job.ID, Goal: "multiply by eleven", Depth: 1,
		ConversationID: convID, PendingToolCallID: "ask-worker", PendingActionID: fence, PauseToken: token, AgentIdentity: owner}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var resume map[string]any
	if err := json.Unmarshal(raw, &resume); err != nil {
		t.Fatal(err)
	}
	parked, err := newDelegationPauseCommitter(pool, pauses, jobs).OpenPauseAndPark(ctx,
		askuser.InsertParams{Token: token, ConversationID: convID, Kind: "clarification", Question: "7 or 9?",
			ToolCallID: "ask-worker", OwningWorkerID: &job.ID, PendingActionID: &fence},
		documents.ParkAwaitingInputRequest{IdentityID: owner, JobID: job.ID, WorkerID: "multi103", LeaseGeneration: claimed[0].LeaseGeneration,
			Payload: map[string]any{"goal": state.Goal, "conversation_id": convID, "fanout_key": "f-multi103", "resume": resume}})
	if err != nil || !parked {
		t.Fatalf("pause/park = %v, %v", parked, err)
	}
	r := runner.New(runner.Deps{Conv: conv, Pause: pauses, ResumeCommitter: runner.NewPoolResumeCommitter(pool, conv, pauses)})
	result, err := r.SubmitAnswer(ctx, token, runner.ResponseInput{Action: askuser.ActionAccept, Content: "9"})
	if err != nil {
		t.Fatalf("ordinary cockpit answer rejected: %v", err)
	}
	if result.Outcome != runner.OutcomeApproved {
		t.Fatalf("worker answer must not restart parent: %+v", result)
	}
	if count, err := conv.CountTurns(ctx, convID); err != nil || count != 0 {
		t.Fatalf("worker answer contaminated parent transcript: count=%d err=%v", count, err)
	}
	if _, err := r.SubmitAnswer(ctx, token, runner.ResponseInput{Action: askuser.ActionAccept, Content: "7"}); !errors.Is(err, askuser.ErrPauseNotFound) {
		t.Fatalf("duplicate answer = %v, want already resolved", err)
	}
	observer := swarm.NewDelegationResumeObserver(jobs)
	if n, err := observer.ProcessOnce(ctx, owner, 10); err != nil || n != 1 {
		t.Fatalf("unpark = %d, %v", n, err)
	}
	if n, err := observer.ProcessOnce(ctx, owner, 10); err != nil || n != 0 {
		t.Fatalf("duplicate unpark = %d, %v", n, err)
	}
	resumed, err := jobs.Claim(ctx, documents.ClaimIngestionJobsRequest{IdentityID: owner, JobType: swarm.JobTypeSwarmDelegation,
		WorkerID: "multi103-resume", LeaseDuration: time.Minute, BatchSize: 1})
	if err != nil || len(resumed) != 1 {
		t.Fatalf("reclaim: %v, %v", resumed, err)
	}
	answer := resumed[0].Payload["resume"].(map[string]any)["answer_content"]
	if answer != "9" {
		t.Fatalf("worker answer = %v, want 9", answer)
	}
}
