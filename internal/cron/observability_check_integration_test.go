//go:build db_integration

package cron

import (
	"context"
	"testing"
	"time"
)

func TestObservabilityCheckSeedKindIsAdmitted(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	spec, err := ParseSchedule(string(KindEvery), "", MinScheduleEveryMinutes, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	next, err := NextRunAt(spec, time.Now().UTC())
	if err != nil {
		t.Fatalf("NextRunAt: %v", err)
	}
	task, err := store.CreateTask(context.Background(), CreateTaskParams{
		Kind: KindObservabilityCheck, Spec: spec, NextRunAt: next, NotifyRoute: "none",
	})
	if err != nil {
		t.Fatalf("CreateTask(observability_check) rejected; migration 0104 did not widen the kind check: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })
	if task.Kind != KindObservabilityCheck {
		t.Fatalf("kind = %q, want %q", task.Kind, KindObservabilityCheck)
	}
}
