//go:build db_integration

package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestFlushPause_HappyExposesPauseAndTurnAtomically_Integration proves the D-05/LOOP-04
// happy path: after flushPause, a pause is visible (PendingFor) AND its assistant ask_user
// tool_call turn is durable; a 2-pause round writes ONE assistant turn carrying BOTH calls
// + exactly 2 paused_states rows.
func TestFlushPause_HappyExposesPauseAndTurnAtomically_Integration(t *testing.T) {
	pool := migratedRunnerPool(t)
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
	)
	r, convStore, _ := newIntegrationRunner(t, pool, client)
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()

	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, err := r.PendingFor(ctx, convID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("want 2 visible pauses after flush, got %d (err=%v)", len(pending), err)
	}
	hist, err := convStore.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	turns, maxCalls := countAssistantPauseTurns(hist)
	if turns != 1 || maxCalls != 2 {
		t.Fatalf("want exactly 1 assistant pause turn carrying 2 calls (CR-02), got turns=%d maxCalls=%d", turns, maxCalls)
	}
	if got := countPauseRows(t, pool, convID); got != 2 {
		t.Fatalf("want 2 paused_states rows, got %d", got)
	}
}

// errInjectedFlushFailure forces the CommitPause tx to roll back after the assistant turn
// is appended, proving the pause-exposure commit is all-or-nothing.
var errInjectedFlushFailure = errors.New("injected pause-flush failure")

// faultyPauseCommitter wraps the real pool-owning committer but makes CommitPause append
// the assistant turn then fail INSIDE one tx, so the whole pause exposure rolls back
// (LOOP-04 fault injection). CommitResume/CommitResumeBatch delegate to the embedded real
// committer.
type faultyPauseCommitter struct {
	*PoolResumeCommitter
	pool *pgxpool.Pool
	conv *conversations.Store
}

func (f *faultyPauseCommitter) CommitPause(ctx context.Context, assistantTurn conversations.AppendTurnParams, _ []askuser.InsertParams) error {
	return db.WithTx(ctx, f.pool, func(q *sqlc.Queries) error {
		seq, err := allocateResumeTurnSeq(ctx, q, assistantTurn.ConversationID)
		if err != nil {
			return err
		}
		assistantTurn.Seq = seq
		// Append the assistant turn (real), then force a rollback WITHOUT inserting the
		// pauses — proving neither the turn nor the pauses land when the commit fails.
		if err := f.conv.AppendTurnTx(ctx, q, assistantTurn); err != nil {
			return err
		}
		return errInjectedFlushFailure
	})
}

// TestFlushPause_FailureHidesPauseAndTurn_Integration proves D-05/LOOP-04/F-030: when the
// pause-exposure commit fails, NEITHER the pause NOR the assistant tool_call turn is
// visible — so resume never sees an orphan tool answer (the infinite-pause loop this
// closes). Only the user turn (committed before the agent ran) survives.
func TestFlushPause_FailureHidesPauseAndTurn_Integration(t *testing.T) {
	pool := migratedRunnerPool(t)
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "A?", "clarification")),
	)
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	pauseStore := askuser.New(pool)
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	committer := &faultyPauseCommitter{
		PoolResumeCommitter: NewPoolResumeCommitter(pool, convStore, pauseStore),
		pool:                pool,
		conv:                convStore,
	}
	r := New(Deps{
		Conv:            convStore,
		Pause:           pauseStore,
		Identity:        newFakeIdentityStore(),
		CacheMetrics:    newFakeCacheMetricStore(),
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          client,
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		TitleTimeout:    2 * time.Second,
		StopTimeout:     2 * time.Second,
		ResumeCommitter: committer,
	})
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()

	// The Turn pauses; flushPause → faulty CommitPause → tx rolls back → the flush error
	// surfaces on the iterator.
	if _, err := drain(r.Turn(ctx, convID, new("go"))); err == nil {
		t.Fatal("want the injected flush failure to surface on the turn iterator")
	}

	// NEITHER the pause NOR the assistant tool_call turn is visible.
	pendingList, perr := r.PendingFor(ctx, convID)
	if perr != nil {
		t.Fatalf("PendingFor: %v", perr)
	}
	if len(pendingList) != 0 {
		t.Fatalf("a failed flush must expose NO pause, got %d", len(pendingList))
	}
	if got := countPauseRows(t, pool, convID); got != 0 {
		t.Fatalf("a failed flush must leave NO paused_states rows, got %d", got)
	}
	hist, herr := convStore.LoadHistory(ctx, convID)
	if herr != nil {
		t.Fatalf("LoadHistory: %v", herr)
	}
	for _, m := range hist {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
			t.Fatal("a failed flush must persist NO assistant tool_call turn")
		}
	}
}
