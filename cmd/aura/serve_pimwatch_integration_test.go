//go:build db_integration

package main

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/pimwatch"
)

func TestAMovedDigestAnswersWhereItsDeliveryWent(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	ctx := context.Background()
	identity := uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE identity_id = $1`, identity)
	})
	store := cron.New(pool)
	tasks := pimWatchTasks{store: store}
	spec := pimwatch.DigestSpec{Hour: 18, Zone: "UTC", Title: "Digest", Delivery: pimwatch.Delivery{Route: string(cron.RouteStdout)}}
	if err := tasks.CreateDigest(ctx, identity, spec); err != nil {
		t.Fatalf("create: %v", err)
	}
	digest := func() cron.Task {
		t.Helper()
		live, err := store.ListLiveTasksOfKind(ctx, identity, cron.KindPIMDigest)
		if err != nil || len(live) != 1 {
			t.Fatalf("digests = %+v, %v", live, err)
		}
		return live[0]
	}

	opened := uuid.NewString()
	if err := store.SetTaskOrigin(ctx, digest().ID, opened); err != nil {
		t.Fatalf("set origin: %v", err)
	}
	spec.Hour = 19
	if err := tasks.RescheduleDigest(ctx, digest().ID, spec); err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if got := digest(); got.OriginConversationID != opened {
		t.Fatalf("a new hour moved the conversation the task opened: %+v", got)
	}

	thread := uuid.NewString()
	spec.Delivery = pimwatch.Delivery{Route: string(cron.RouteTelegram), OriginConversationID: thread, TelegramRecipients: []int64{42}}
	if err := tasks.RescheduleDigest(ctx, digest().ID, spec); err != nil {
		t.Fatalf("move to telegram: %v", err)
	}
	got := digest()
	if got.NotifyRoute != string(cron.RouteTelegram) || got.OriginConversationID != thread || !slices.Equal(got.TelegramRecipients, []int64{42}) {
		t.Fatalf("after the move: %+v", got)
	}
}
