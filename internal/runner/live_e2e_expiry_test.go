//go:build live_e2e

package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5"
)

// TestLiveE2E_ExpiredApprovalVisibleToFreshAgent drives the internal expiry action
// through the production Runner, real PostgreSQL, and a genuine fresh model round.
// The expired decision is persisted as the matching tool result, a late public
// decision is rejected, and the agent rehydrates wire-valid history containing the
// explicit refusal instead of receiving an approval-shaped answer.
func TestLiveE2E_ExpiredApprovalVisibleToFreshAgent(t *testing.T) {
	h := newLiveHarness(t)
	ownerCtx := identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity)
	ctx, cancel := context.WithTimeout(ownerCtx, 180*time.Second)
	defer cancel()
	convID := h.newLiveConversation(t, ctx)

	token, err := h.r.MintApprovalPause(
		ctx,
		convID,
		"This operation is gated. If approval expires, do not claim it executed.",
		nil,
		[]string{askuser.ActionAccept, askuser.ActionDecline, askuser.ActionCancel},
	)
	if err != nil {
		t.Fatalf("mint approval pause: %v", err)
	}

	cutoff := time.Now().UTC().Add(-time.Hour)
	if err := db.WithIdentityTxRaw(ctx, h.pool, identityctx.LocalOperatorIdentity, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"UPDATE aura.paused_states SET created_at=$1 WHERE token=$2",
			cutoff.Add(-time.Minute), token)
		return err
	}); err != nil {
		t.Fatalf("backdate approval pause: %v", err)
	}

	expired, err := h.r.ExpirePendingApprovals(ctx, cutoff, 10)
	if err != nil || expired != 1 {
		t.Fatalf("ExpirePendingApprovals = %d, %v; want 1, nil", expired, err)
	}
	answer, resolved := h.resumedAnswerOf(t, ctx, token)
	if !resolved || answer.Action != askuser.ActionExpired || answer.Content != expiredApprovalContent {
		t.Fatalf("expired answer = %#v, resolved=%v; want explicit internal expiry", answer, resolved)
	}
	if _, err := h.r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept, Content: "late"}); !errors.Is(err, askuser.ErrPauseExpired) {
		t.Fatalf("late public accept error = %v, want ErrPauseExpired", err)
	}

	history, err := h.conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("load expired history: %v", err)
	}
	assertWireValid(t, history)
	foundExpiry := false
	for _, msg := range history {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Content, expiredApprovalContent) {
			foundExpiry = true
			break
		}
	}
	if !foundExpiry {
		t.Fatal("rehydrated history omitted the explicit expiry tool result")
	}
	turnsBefore, err := h.conv.CountTurns(ctx, convID)
	if err != nil {
		t.Fatalf("count turns before agent re-drive: %v", err)
	}
	reply, usage, redrove := h.resumePostResume(t, ctx, convID, 3)
	if !redrove {
		t.Fatal("expiry drove no genuine fresh agent round")
	}
	turnsAfter, err := h.conv.CountTurns(ctx, convID)
	if err != nil {
		t.Fatalf("count turns after agent re-drive: %v", err)
	}
	if turnsAfter <= turnsBefore {
		t.Fatalf("agent re-drive persisted no fresh turn (%d -> %d)", turnsBefore, turnsAfter)
	}
	t.Logf("expiry reached a real agent (%d -> %d turns, prompt_tokens=%d, reply=%q)",
		turnsBefore, turnsAfter, usage.PromptTokens, reply)
}
