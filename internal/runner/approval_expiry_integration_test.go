//go:build db_integration

package runner

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5"
)

func TestExpirePendingApprovalPersistsVisibleRefusal(t *testing.T) {
	pool := migratedRunnerPool(t)
	r, convStore, _ := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()
	token, err := r.MintApprovalPause(ctx, convID, "Approve this operation?", nil,
		[]string{askuser.ActionAccept, askuser.ActionDecline, askuser.ActionCancel})
	if err != nil {
		t.Fatalf("MintApprovalPause: %v", err)
	}
	cutoff := time.Now().UTC().Add(-time.Hour)
	if err := db.WithIdentityTxRaw(ctx, pool, localIdentityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE aura.paused_states SET created_at=$1 WHERE token=$2", cutoff.Add(-time.Minute), token)
		return err
	}); err != nil {
		t.Fatalf("backdate pause: %v", err)
	}

	expired, err := r.ExpirePendingApprovals(ctx, cutoff, 10)
	if err != nil || expired != 1 {
		t.Fatalf("ExpirePendingApprovals = %d, %v; want 1, nil", expired, err)
	}
	var raw []byte
	if err := db.WithIdentityTxRaw(ctx, pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT resumed_answer FROM aura.paused_states WHERE token=$1", token).Scan(&raw)
	}); err != nil {
		t.Fatalf("read resumed answer: %v", err)
	}
	var answer askuser.ResumeAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("decode resumed answer: %v", err)
	}
	if answer.Action != askuser.ActionExpired || answer.Content != expiredApprovalContent {
		t.Fatalf("resumed answer = %#v, want visible expiry", answer)
	}
	if n := countPersistedToolTurns(t, pool, convID); n != 1 {
		t.Fatalf("persisted tool answers = %d, want 1", n)
	}
	if _, err := r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionDecline}); !errors.Is(err, askuser.ErrPauseExpired) {
		t.Fatalf("late human decision error = %v, want ErrPauseExpired", err)
	}
	history, err := convStore.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	assertWireValid(t, history)
	if !historyHasToolContentMsgs(history, expiredApprovalContent) {
		t.Fatal("wire-valid history omitted the explicit expiry answer")
	}
}

func TestExpiryAndHumanResumeRaceHasOneAtomicWinner(t *testing.T) {
	pool := migratedRunnerPool(t)
	r, convStore, _ := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()
	token, err := r.MintApprovalPause(ctx, convID, "Approve this operation?", nil,
		[]string{askuser.ActionAccept, askuser.ActionDecline, askuser.ActionCancel})
	if err != nil {
		t.Fatalf("MintApprovalPause: %v", err)
	}
	cutoff := time.Now().UTC().Add(-time.Hour)
	if err := db.WithIdentityTxRaw(ctx, pool, localIdentityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE aura.paused_states SET created_at=$1 WHERE token=$2", cutoff.Add(-time.Minute), token)
		return err
	}); err != nil {
		t.Fatalf("backdate pause: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var expired int
	var expireErr, resumeErr error
	go func() {
		defer wg.Done()
		<-start
		expired, expireErr = r.ExpirePendingApprovals(ctx, cutoff, 10)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, resumeErr = r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionDecline})
	}()
	close(start)
	wg.Wait()

	if expireErr != nil {
		t.Fatalf("expiry race error: %v", expireErr)
	}
	resumeWon := resumeErr == nil
	expiryWon := expired == 1
	if resumeWon == expiryWon {
		t.Fatalf("race winners: expired=%d resumeErr=%v, want exactly one winner", expired, resumeErr)
	}
	if resumeErr != nil && !errors.Is(resumeErr, askuser.ErrPauseNotFound) && !errors.Is(resumeErr, askuser.ErrPauseExpired) {
		t.Fatalf("resume loser error = %v", resumeErr)
	}
	if n := countPersistedToolTurns(t, pool, convID); n != 1 {
		t.Fatalf("persisted tool answers = %d, want exactly 1", n)
	}
	history, err := convStore.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	assertWireValid(t, history)
}
