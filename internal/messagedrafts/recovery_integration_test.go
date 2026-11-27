//go:build db_integration

package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func recoveryDraft(t *testing.T, store *Store, owner, call string) Draft {
	t.Helper()
	draft, err := store.Create(context.Background(), DraftInput{
		IdentityID: owner, ConversationID: "recovery-conversation", ToolCallID: call,
		Target:             Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"12345","message":"private text"}`),
		ExpiresAt:          time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create %s: %v", call, err)
	}
	return draft
}

// shift says how long ago each of a draft's timestamps is, as a Postgres interval ("46 days");
// an empty field leaves its timestamp alone.
type shift struct{ expires, dispatched, sendAt string }

// interval is a shift field as a query argument: an empty one is NULL, which COALESCE keeps.
func interval(ago string) any {
	if ago == "" {
		return nil
	}
	return ago
}

// age moves one of the owner's drafts back in time, in the owner's RLS transaction as the drafts'
// own tests do. One statement sets all three, so a scheduled draft keeps expires_at > send_at.
func age(t *testing.T, pool *pgxpool.Pool, owner, draftID string, s shift) {
	t.Helper()
	err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE aura.message_drafts SET
  expires_at = COALESCE(now() - $2::interval, expires_at),
  dispatch_started_at = COALESCE(now() - $3::interval, dispatch_started_at),
  send_at = COALESCE(now() - $4::interval, send_at)
WHERE id = $1`, draftID, interval(s.expires), interval(s.dispatched), interval(s.sendAt))
		return err
	})
	if err != nil {
		t.Fatalf("age %s: %v", draftID, err)
	}
}

func statusOf(t *testing.T, store *Store, owner, draftID string) (Status, string) {
	t.Helper()
	draft, err := store.Get(context.Background(), owner, draftID)
	if err != nil {
		t.Fatalf("get %s: %v", draftID, err)
	}
	return draft.Status, draft.OutcomeCode
}

// Recover is the sweep for drafts nobody opens again: on the lab VM a review stayed pending 46 days
// past its expiry, because expiry was applied only when the draft was read.
func TestRecoverClosesTheDraftsNobodyReopens(t *testing.T) {
	pool := draftTestPool(t)
	owner, other := draftTestIdentity(t, pool), draftTestIdentity(t, pool)
	store := NewStore(pool)
	ctx := context.Background()

	stale := recoveryDraft(t, store, owner, "call-stale")
	age(t, pool, owner, stale.ID, shift{expires: "46 days"})
	fresh := recoveryDraft(t, store, owner, "call-fresh")
	abandoned := recoveryDraft(t, store, owner, "call-abandoned")
	if _, err := store.ClaimSend(ctx, owner, abandoned.ID, nil); err != nil {
		t.Fatalf("claim abandoned: %v", err)
	}
	age(t, pool, owner, abandoned.ID, shift{dispatched: "31 minutes"})
	recent := recoveryDraft(t, store, owner, "call-recent")
	if _, err := store.ClaimSend(ctx, owner, recent.ID, nil); err != nil {
		t.Fatalf("claim recent: %v", err)
	}
	declined := recoveryDraft(t, store, owner, "call-declined")
	if _, err := store.Decline(ctx, owner, declined.ID); err != nil {
		t.Fatalf("decline: %v", err)
	}
	age(t, pool, owner, declined.ID, shift{expires: "1 day"})
	scheduled, err := store.Create(ctx, scheduledInput(owner, "call-scheduled", scheduledTask(t, pool, owner), time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("create scheduled: %v", err)
	}
	age(t, pool, owner, scheduled.ID, shift{sendAt: "2 days", expires: "1 day"})
	foreign := recoveryDraft(t, store, other, "call-foreign")
	age(t, pool, other, foreign.ID, shift{expires: "1 day"})

	got, err := store.Recover(ctx, owner)
	if err != nil || got != (Recovered{Expired: 1, Interrupted: 1}) {
		t.Fatalf("Recover = %+v, %v; want one expired and one interrupted", got, err)
	}
	for _, want := range []struct {
		name, owner, id string
		status          Status
		code            string
	}{
		{"a pending draft past its expiry", owner, stale.ID, StatusExpired, ""},
		{"a dispatch older than the cutoff", owner, abandoned.ID, StatusUncertain, "interrupted"},
		{"a pending draft still in time", owner, fresh.ID, StatusPending, ""},
		{"a dispatch still in flight", owner, recent.ID, StatusDispatching, ""},
		{"a terminal draft", owner, declined.ID, StatusDeclined, ""},
		{"a scheduled draft, which its task expires", owner, scheduled.ID, StatusPending, ""},
		{"another identity's draft", other, foreign.ID, StatusPending, ""},
	} {
		if status, code := statusOf(t, store, want.owner, want.id); status != want.status || code != want.code {
			t.Errorf("%s: %s/%q, want %s/%q", want.name, status, code, want.status, want.code)
		}
	}
	if again, err := store.Recover(ctx, owner); err != nil || again != (Recovered{}) {
		t.Fatalf("a second Recover = %+v, %v; want nothing left to change", again, err)
	}
	for _, id := range []string{stale.ID, abandoned.ID} {
		if _, err := store.ClaimSend(ctx, owner, id, nil); !errors.Is(err, ErrUnavailable) {
			t.Errorf("recovered draft %s can still be claimed: %v", id, err)
		}
	}
	if _, err := store.Recover(ctx, "not-a-uuid"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Recover for an invalid owner = %v, want ErrUnavailable", err)
	}
}
