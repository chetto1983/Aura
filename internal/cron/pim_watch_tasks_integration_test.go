//go:build db_integration

package cron

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/db"
)

// A double click on the watch's switch must not leave two tasks walking one cursor. The database
// keeps one live watch per account and one digest per identity, and a cancelled task frees its slot.
func TestTheDatabaseKeepsOneLiveWatchPerAccountAndOneDigestPerIdentity(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec, err := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	identity := uuid.NewString()
	create := func(kind TaskKind, identityID, accountID string) (Task, error) {
		payload, err := json.Marshal(map[string]string{"account_id": accountID})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		task, err := store.CreateTask(ctx, CreateTaskParams{
			Kind: kind, Spec: spec, Payload: payload, NextRunAt: time.Now().Add(time.Hour),
			NotifyRoute: string(RouteStdout), IdentityID: identityID,
		})
		if err == nil {
			t.Cleanup(func() { cleanupTask(t, pool, task.ID) })
		}
		return task, err
	}

	work, err := create(KindPIMWatch, identity, "work")
	if err != nil {
		t.Fatalf("first watch: %v", err)
	}
	if _, err := create(KindPIMWatch, identity, "work"); !db.IsUniqueViolation(err) {
		t.Fatalf("a second watch of one account: %v", err)
	}
	if _, err := create(KindPIMWatch, identity, "home"); err != nil {
		t.Fatalf("another account: %v", err)
	}
	if _, err := create(KindPIMWatch, uuid.NewString(), "work"); err != nil {
		t.Fatalf("another identity: %v", err)
	}
	if err := store.PauseTask(ctx, work.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err := create(KindPIMWatch, identity, "work"); !db.IsUniqueViolation(err) {
		t.Fatalf("a paused watch must still hold its slot: %v", err)
	}
	if err := store.CancelTask(ctx, work.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := create(KindPIMWatch, identity, "work"); err != nil {
		t.Fatalf("a cancelled watch frees its slot: %v", err)
	}

	if _, err := create(KindPIMDigest, identity, ""); err != nil {
		t.Fatalf("first digest: %v", err)
	}
	if _, err := create(KindPIMDigest, identity, ""); !db.IsUniqueViolation(err) {
		t.Fatalf("a second digest: %v", err)
	}
}

func TestListLiveTasksOfKindFindsThePausedAndNotTheCancelled(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec, err := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	identity := uuid.NewString()
	var ids []string
	for _, account := range []string{"active", "paused", "cancelled"} {
		task, err := store.CreateTask(ctx, CreateTaskParams{
			Kind: KindPIMWatch, Spec: spec, Payload: []byte(`{"account_id":"` + account + `"}`),
			NextRunAt: time.Now().Add(time.Hour), NotifyRoute: string(RouteStdout), IdentityID: identity,
		})
		if err != nil {
			t.Fatalf("create %s: %v", account, err)
		}
		t.Cleanup(func() { cleanupTask(t, pool, task.ID) })
		ids = append(ids, task.ID)
	}
	if err := store.PauseTask(ctx, ids[1]); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := store.CancelTask(ctx, ids[2]); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	live, err := store.ListLiveTasksOfKind(ctx, identity, KindPIMWatch)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("live = %+v", live)
	}
	if others, err := store.ListLiveTasksOfKind(ctx, uuid.NewString(), KindPIMWatch); err != nil || len(others) != 0 {
		t.Fatalf("another identity sees %+v, %v", others, err)
	}
}
