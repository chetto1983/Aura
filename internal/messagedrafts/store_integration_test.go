//go:build db_integration

package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
)

func draftDBEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("message draft integration requires %s", key)
		}
		t.Skipf("message draft integration requires %s", key)
	}
	return value
}

func draftTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	password := draftDBEnv(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, draftDBEnv(t, "AURA_DB_MIGRATE_URL"))
	appURL := draftDBEnv(t, "AURA_DB_URL")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", password, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, password); err != nil {
		t.Fatal("ensure database roles failed")
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("apply migrations failed: %v", err)
	}
	pool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatal("open app pool failed")
	}
	t.Cleanup(pool.Close)
	return pool
}

func draftTestIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(context.Background(), `INSERT INTO aura.identities(id,name,kind) VALUES($1,$2,'user')`, id, "draft-test-"+id[:8]); err != nil {
		t.Fatal("seed identity failed")
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, id); err != nil {
			t.Error("clean up identity failed")
		}
	})
	return id
}

func TestMessageDraftStoreOwnerAndOneClaim(t *testing.T) {
	pool := draftTestPool(t)
	owner, foreign := draftTestIdentity(t, pool), draftTestIdentity(t, pool)
	store := NewStore(pool)
	ctx := context.Background()
	input := DraftInput{
		IdentityID: owner, ConversationID: "draft-conversation", ToolCallID: "call-one",
		Target:             Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"},
		RegisteredToolName: "calendar__calendar",
		OriginalArgs:       json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","body":"Original"}`),
		ExpiresAt:          time.Now().Add(time.Hour),
	}
	draft, err := store.Create(ctx, input)
	if err != nil || draft.ID == "" || draft.Status != StatusPending {
		t.Fatal("create pending draft failed")
	}
	if active, err := store.Reconcile(ctx, owner, draft.ID); err != nil || active.Status != StatusPending {
		t.Fatal("active review was changed during recovery")
	}
	duplicate, err := store.Create(ctx, input)
	if err != nil || duplicate.ID != draft.ID {
		t.Fatal("same call did not resolve to one draft")
	}
	changed := input
	changed.OriginalArgs = json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Different","body":"Original"}`)
	if _, err := store.Create(ctx, changed); !errors.Is(err, ErrDuplicate) {
		t.Fatal("conflicting duplicate call accepted")
	}
	changed = input
	changed.RegisteredToolName = "other__calendar"
	if _, err := store.Create(ctx, changed); !errors.Is(err, ErrDuplicate) {
		t.Fatal("a duplicate call changed its registered server")
	}
	hashed := input
	hashed.ConversationID = "draft-hashed-conversation"
	hashed.ToolCallID = "call-hashed"
	hashed.RegisteredToolName = "namespace_that_is_very_long_and_needs_truncate__c_0123456789ab"
	if _, err := store.Create(ctx, hashed); err != nil {
		t.Fatal("hash-suffixed bridge name was rejected by the migration")
	}
	if _, err := store.Get(ctx, foreign, draft.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("foreign owner read a draft")
	}
	foreignRows, err := store.ListPending(ctx, foreign, input.ConversationID)
	if err != nil || len(foreignRows) != 0 {
		t.Fatal("foreign owner listed pending drafts")
	}
	err = db.WithIdentityTxRaw(ctx, pool, foreign, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM aura.message_drafts WHERE id=$1`, draft.ID).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("foreign row visible under RLS")
		}
		return nil
	})
	if err != nil {
		t.Fatal("RLS owner isolation failed")
	}

	overrides := json.RawMessage(`{"to":["second@example.test"],"body":"Edited"}`)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			<-start
			claimed, claimErr := store.ClaimSend(ctx, owner, draft.ID, overrides)
			if claimErr == nil && (claimed.Status != StatusDispatching || claimed.EffectiveFingerprint == "") {
				claimErr = errors.New("claim lacks effective arguments")
			}
			results <- claimErr
		})
	}
	close(start)
	wg.Wait()
	close(results)
	winners, denied := 0, 0
	for result := range results {
		switch {
		case result == nil:
			winners++
		case errors.Is(result, ErrUnavailable):
			denied++
		default:
			t.Fatal("claim failed unexpectedly")
		}
	}
	if winners != 1 || denied != 1 {
		t.Fatal("claim was not one-shot")
	}
	claimed, err := store.Get(ctx, owner, draft.ID)
	if err != nil {
		t.Fatal("claimed draft missing")
	}
	var effective map[string]any
	if json.Unmarshal(claimed.EffectiveArgs, &effective) != nil || effective["body"] != "Edited" {
		t.Fatal("saved effective arguments differ from approved edit")
	}
	if _, err := store.MarkOutcome(ctx, owner, draft.ID, StatusSent, "sent"); err != nil {
		t.Fatal("sent outcome was not recorded")
	}
	if sent, err := store.Reconcile(ctx, owner, draft.ID); err != nil || sent.Status != StatusSent {
		t.Fatal("terminal send was changed during recovery")
	}
	if _, err := store.ClaimSend(ctx, owner, draft.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("resolved draft was claimed again")
	}
	if _, err := store.MarkOutcome(ctx, owner, draft.ID, StatusSent, "sent"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("outcome was recorded twice")
	}
	if pending, err := store.ListPending(ctx, owner, input.ConversationID); err != nil || len(pending) != 0 {
		t.Fatal("sent draft remained pending")
	}
}

func TestMessageDraftStoreDeclineAndExpiry(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	store := NewStore(pool)
	ctx := context.Background()
	input := DraftInput{
		IdentityID: owner, ConversationID: "draft-conversation", ToolCallID: "call-decline",
		Target:             Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"12345","message":"Hello"}`), ExpiresAt: time.Now().Add(time.Hour),
	}
	draft, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal("create decline draft failed")
	}
	if _, err := store.Decline(ctx, owner, draft.ID); err != nil {
		t.Fatal("decline failed")
	}
	if _, err := store.ClaimSend(ctx, owner, draft.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("declined draft was claimed")
	}
	input.ToolCallID = "call-expire"
	expiring, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal("create expiring draft failed")
	}
	err = db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE aura.message_drafts SET expires_at=now()-interval '1 second' WHERE id=$1`, expiring.ID)
		return err
	})
	if err != nil {
		t.Fatal("expire draft fixture failed")
	}
	if _, err := store.ClaimSend(ctx, owner, expiring.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expired draft was claimed")
	}
	expired, err := store.Reconcile(ctx, owner, expiring.ID)
	if err != nil || expired.Status != StatusExpired {
		t.Fatal("expired draft did not close without sending")
	}

	input.ToolCallID = "call-interrupted"
	interrupted, err := store.Create(ctx, input)
	if err != nil {
		t.Fatal("create interrupted draft failed")
	}
	if _, err := store.ClaimSend(ctx, owner, interrupted.ID, nil); err != nil {
		t.Fatal("claim interrupted draft failed")
	}
	err = db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE aura.message_drafts SET dispatch_started_at=now()-interval '31 minutes' WHERE id=$1`, interrupted.ID)
		return err
	})
	if err != nil {
		t.Fatal("age interrupted dispatch failed")
	}
	uncertain, err := store.Reconcile(ctx, owner, interrupted.ID)
	if err != nil || uncertain.Status != StatusUncertain || uncertain.OutcomeCode != "interrupted" {
		t.Fatal("interrupted dispatch was not made uncertain")
	}
	if _, err := store.ClaimSend(ctx, owner, interrupted.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("interrupted dispatch gained another send authorization")
	}
}
