//go:build db_integration

package pimwatch

import (
	"context"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/cron"
)

func TestRepairedResumesAWatchPausedForACredentialFailure(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	key := r.identity + "|" + rigAccount
	r.tasks.watches[key] = WatchTask{ID: key, AccountID: rigAccount, Paused: true, PausedReason: cron.PausedByFailures}
	if _, err := r.store.MarkFailing(ctx, r.identity, rigAccount, ResourceMail, "provider_auth", r.clock.now()); err != nil {
		t.Fatalf("mark failing: %v", err)
	}
	if err := r.service.Repaired(ctx, r.identity, rigAccount); err != nil {
		t.Fatalf("repaired: %v", err)
	}
	if r.tasks.watches[key].Paused || !slices.Equal(r.tasks.runNow, []string{key}) {
		t.Fatalf("paused=%v runNow=%v", r.tasks.watches[key].Paused, r.tasks.runNow)
	}
}

func TestRepairedLeavesAnOperatorPauseAlone(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	key := r.identity + "|" + rigAccount
	r.tasks.watches[key] = WatchTask{ID: key, AccountID: rigAccount, Paused: true, PausedReason: cron.PausedByOperator}
	if _, err := r.store.MarkFailing(ctx, r.identity, rigAccount, ResourceMail, "provider_auth", r.clock.now()); err != nil {
		t.Fatalf("mark failing: %v", err)
	}
	if err := r.service.Repaired(ctx, r.identity, rigAccount); err != nil {
		t.Fatalf("repaired: %v", err)
	}
	if !r.tasks.watches[key].Paused || len(r.tasks.runNow) != 0 {
		t.Fatalf("paused=%v runNow=%v", r.tasks.watches[key].Paused, r.tasks.runNow)
	}
}

func TestRepairedWithNoWatchDoesNothing(t *testing.T) {
	r := newServiceRig(t)
	if err := r.service.Repaired(context.Background(), r.identity, rigAccount); err != nil {
		t.Fatalf("repaired: %v", err)
	}
	if len(r.tasks.runNow) != 0 || r.tasks.created != 0 {
		t.Fatalf("runNow=%v created=%d", r.tasks.runNow, r.tasks.created)
	}
}
