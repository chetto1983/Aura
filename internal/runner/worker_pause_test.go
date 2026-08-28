//go:build db_integration

// worker_pause_test.go proves 51-06a's D-12 fencing / D-13 level-identity guard rails
// (SWARM-06) against LIVE Postgres, run as aura_app (migratedRunnerPool composes
// AURA_DB_URL/AURA_DB_MIGRATE_URL for aura_app/aura_migrate, never the superuser `aura`
// — a superuser+BYPASSRLS run gives a FALSE GREEN on exactly the identity scoping this
// plan is about). It exercises askuser.Store + PoolResumeCommitter directly: this plan
// makes a pause fenceable/attributable/lazily-expiring, it does NOT wire the ask_user
// tool to a background per-worker pause (that observe/continue leg is plan 51-06b), so
// the fixtures here insert pauses directly via askuser.InsertParams the way a future
// worker-pause writer will.
//
//	go test -tags db_integration -race ./internal/runner/ -run 'TestPerWorkerPauseFencing|TestWorkerPauseLazyExpiry|TestUnfencedPauseStillResumes' -v
package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// strp returns a pointer to s, for askuser.InsertParams' *string fencing/attribution
// fields (nil means SQL NULL; a set field must be addressable).
func strp(s string) *string { return &s }

// insertWorkerPause inserts one pause carrying D-12's fencing id and D-13's
// host-written level identity (OwningWorkerID) directly — the shape a future
// background-worker writer (51-06b) will produce. ownerWorker/actionID empty means
// "leave that column NULL" (an ordinary/unfenced pause).
func insertWorkerPause(t *testing.T, pause *askuser.Store, convID, toolCallID, ownerWorker, actionID string) string {
	t.Helper()
	token := uuid.Must(uuid.NewV7()).String()
	p := askuser.InsertParams{
		Token: token, ConversationID: convID, Kind: "clarification",
		Question: "need input?", ToolCallID: toolCallID,
	}
	if ownerWorker != "" {
		p.OwningWorkerID = strp(ownerWorker)
	}
	if actionID != "" {
		p.PendingActionID = strp(actionID)
	}
	if err := pause.Insert(ownerCtx(), p); err != nil {
		t.Fatalf("insert worker pause: %v", err)
	}
	return token
}

// fencedClaim builds a ResumeClaim carrying expectActionID (D-12) alongside its answer.
func fencedClaim(token, convID, toolCallID, content, expectActionID string) ResumeClaim {
	return ResumeClaim{
		Token:  token,
		Answer: askuser.ResumeAnswer{Action: askuser.ActionAccept, Content: content},
		Turn: conversations.AppendTurnParams{
			ConversationID: convID, Role: llm.RoleTool, ToolCallID: toolCallID, Content: content,
		},
		ExpectActionID: expectActionID,
	}
}

// TestPerWorkerPauseFencing proves D-12/D-13's core guarantee: N pauses for N workers on
// one conversation resolve INDEPENDENTLY, a stale/mismatched fencing id cannot resume a
// pause that has since moved on (and appends NO conversation turn), and the checkpoint's
// chosen authority rule (WorkerID() reads the host-written OwningWorkerID) is exercised
// end to end through the real PoolResumeCommitter transaction.
func TestPerWorkerPauseFencing(t *testing.T) {
	pool := migratedRunnerPool(t)
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	pauseStore := askuser.New(pool)
	committer := NewPoolResumeCommitter(pool, convStore, pauseStore)
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()

	tokW1 := insertWorkerPause(t, pauseStore, convID, "call-w1", "w1", "action-w1")
	tokW2 := insertWorkerPause(t, pauseStore, convID, "call-w2", "w2", "action-w2")

	// The authority rule under test: WorkerID() answers "which worker" from the
	// host-written column, not the (here empty) model-relayed proxied pair.
	pendingW1, err := pauseStore.GetByToken(ctx, tokW1)
	if err != nil {
		t.Fatalf("GetByToken w1: %v", err)
	}
	if got := pendingW1.WorkerID(); got != "w1" {
		t.Fatalf("WorkerID() = %q, want w1", got)
	}

	// RLS scoping (run as aura_app, never the superuser aura — a superuser+BYPASSRLS run
	// would give a false green on exactly this): a foreign identity's context must not
	// see w1's pause at all, fenced or not.
	foreignCtx := identityctx.WithIdentityID(context.Background(), "22222222-2222-2222-2222-222222222222")
	if _, err := pauseStore.GetByToken(foreignCtx, tokW1); !errors.Is(err, askuser.ErrPauseNotFound) {
		t.Fatalf("foreign identity read of w1: want ErrPauseNotFound (invisible), got %v", err)
	}

	// Worker 1 resolves with the CORRECT fencing id: succeeds exactly once.
	if err := committer.CommitResume(ctx, fencedClaim(tokW1, convID, "call-w1", "answer-1", "action-w1")); err != nil {
		t.Fatalf("resolve w1: %v", err)
	}
	if resumedAt(t, pool, tokW1) == nil {
		t.Fatal("w1 must be resumed after a correctly-fenced claim")
	}
	// Resolving worker 1's pause must leave worker 2's pending (independent resolution).
	if resumedAt(t, pool, tokW2) != nil {
		t.Fatal("resolving w1 must NOT resolve w2 — pauses resolve independently")
	}

	// Resolving the SAME correct id again fails: the resumed_at IS NULL half of the
	// fence still holds (idempotency, "resolves exactly once").
	if err := committer.CommitResume(ctx, fencedClaim(tokW1, convID, "call-w1", "answer-1-again", "action-w1")); !errors.Is(err, askuser.ErrPauseNotFound) {
		t.Fatalf("re-resolving w1: want ErrPauseNotFound, got %v", err)
	}

	turnsBefore := countPersistedToolTurns(t, pool, convID)

	// Worker 2 resolves with a MISMATCHED (stale) fencing id: matches zero rows, no
	// conversation turn is appended, and w2 stays pending — a stale decision cannot
	// resume a pause that has since paused for a different action.
	staleErr := committer.CommitResume(ctx, fencedClaim(tokW2, convID, "call-w2", "hijacked", "action-w2-STALE"))
	if !errors.Is(staleErr, askuser.ErrPauseNotFound) {
		t.Fatalf("mismatched fence: want ErrPauseNotFound, got %v", staleErr)
	}
	if got := countPersistedToolTurns(t, pool, convID); got != turnsBefore {
		t.Fatalf("mismatched fence must append NO conversation turn: before=%d after=%d", turnsBefore, got)
	}
	if resumedAt(t, pool, tokW2) != nil {
		t.Fatal("w2 must remain pending after a mismatched-fence resume attempt")
	}

	// Worker 2 resolves with the CORRECT fencing id: succeeds — proves the mismatch
	// above did not corrupt or consume the pause.
	if err := committer.CommitResume(ctx, fencedClaim(tokW2, convID, "call-w2", "answer-2", "action-w2")); err != nil {
		t.Fatalf("resolve w2: %v", err)
	}
	if resumedAt(t, pool, tokW2) == nil {
		t.Fatal("w2 must be resumed after a correctly-fenced claim")
	}
}

// TestUnfencedPauseStillResumes proves the fence is ADDITIVE, never a new precondition
// on the shipped resume path: a pause whose pending_action_id is NULL (every row that
// predates migration 0106, and every ordinary operator pause) resumes exactly as it did
// before this plan — with no fence supplied, AND even if the caller happens to supply
// one (a NULL pending_action_id matches regardless).
func TestUnfencedPauseStillResumes(t *testing.T) {
	pool := migratedRunnerPool(t)
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	pauseStore := askuser.New(pool)
	committer := NewPoolResumeCommitter(pool, convStore, pauseStore)
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()

	// No OwningWorkerID, no PendingActionID: an ordinary operator pause.
	tokNoFence := insertWorkerPause(t, pauseStore, convID, "call-plain", "", "")
	if err := committer.CommitResume(ctx, fencedClaim(tokNoFence, convID, "call-plain", "ok", "")); err != nil {
		t.Fatalf("unfenced resume with no ExpectActionID: %v", err)
	}
	if resumedAt(t, pool, tokNoFence) == nil {
		t.Fatal("unfenced pause must resume with no fence supplied")
	}

	// Same shape, but the caller happens to pass a non-empty ExpectActionID anyway — a
	// NULL pending_action_id matches regardless of what the caller supplies.
	tokStraySupplied := insertWorkerPause(t, pauseStore, convID, "call-plain-2", "", "")
	if err := committer.CommitResume(ctx, fencedClaim(tokStraySupplied, convID, "call-plain-2", "ok", "some-action-nobody-set")); err != nil {
		t.Fatalf("unfenced resume with a stray ExpectActionID: %v", err)
	}
	if resumedAt(t, pool, tokStraySupplied) == nil {
		t.Fatal("unfenced pause must resume even when the caller supplies an unrelated fence")
	}
}

// TestWorkerPauseLazyExpiry proves D-12/D-13's lazy expiry (mirrors LibreChat
// ApprovalLifecycle.peek): a still-pending row whose created_at is older than the
// configured TTL reads as ErrPauseNotFound via GetByToken, even before the sweeper
// (plan 51-06b) ever claims it. TTL is opt-in per Store (NewWithPauseTTL) — the
// TTL-disabled default (plain New(pool), every production call site today) reads the
// same row unaffected, proving the check is additive, not a global behavior change.
func TestWorkerPauseLazyExpiry(t *testing.T) {
	pool := migratedRunnerPool(t)
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	plainStore := askuser.New(pool)
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()

	tok := insertWorkerPause(t, plainStore, convID, "call-expiry", "w3", "action-w3")

	// Backdate created_at well past a 1-second TTL, directly on the owner-scoped tx.
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE aura.paused_states SET created_at = now() - interval '10 seconds' WHERE token=$1", tok)
		return err
	})

	ttlStore := askuser.NewWithPauseTTL(pool, 1)
	if _, err := ttlStore.GetByToken(ctx, tok); !errors.Is(err, askuser.ErrPauseNotFound) {
		t.Fatalf("TTL-enabled read of a stale pause: want ErrPauseNotFound, got %v", err)
	}

	// Control: the SAME backdated row, read through a TTL-disabled Store (matching
	// every production call site today), must still read cleanly — expiry is additive.
	if _, err := plainStore.GetByToken(ctx, tok); err != nil {
		t.Fatalf("TTL-disabled read of the same backdated row must NOT expire it: %v", err)
	}

	// Control: a FRESH pause under the SAME TTL-enabled store is not (yet) expired.
	freshTok := insertWorkerPause(t, plainStore, convID, "call-fresh", "w4", "action-w4")
	if _, err := ttlStore.GetByToken(ctx, freshTok); err != nil {
		t.Fatalf("TTL-enabled read of a fresh pause must NOT expire it: %v", err)
	}
}

// TestPausedStateWorkerAttributionExclusive proves the checkpoint's chosen authority
// rule is ENFORCED, not merely documented (T-51-47): a pause cannot carry both a
// host-written OwningWorkerID and a (here simulated) model-relayed ProxiedFromChildID —
// the migration 0106 CHECK constraint fails the INSERT closed.
func TestPausedStateWorkerAttributionExclusive(t *testing.T) {
	pool := migratedRunnerPool(t)
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	pauseStore := askuser.New(pool)
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()

	token := uuid.Must(uuid.NewV7()).String()
	err := pauseStore.Insert(ctx, askuser.InsertParams{
		Token: token, ConversationID: convID, Kind: "clarification",
		Question: "dual-attribution?", ToolCallID: "call-dual",
		OwningWorkerID:     strp("w5"),
		ProxiedFromChildID: strp("w6"),
	})
	if err == nil {
		t.Fatal("insert with BOTH owning_worker_id and proxied_from_child_id set must fail closed (paused_states_worker_attribution_exclusive)")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("want a Postgres error wrapping the CHECK violation, got %v", err)
	}
	if pgErr.Code != "23514" {
		t.Fatalf("want SQLSTATE 23514 (check_violation), got %q (%v)", pgErr.Code, err)
	}
	if pgErr.ConstraintName != "paused_states_worker_attribution_exclusive" {
		t.Fatalf("want constraint paused_states_worker_attribution_exclusive, got %q", pgErr.ConstraintName)
	}
}
