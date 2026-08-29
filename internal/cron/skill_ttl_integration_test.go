//go:build db_integration

// Integration test for the skill_ttl_sweep TaskKind seed (D-16 / A2 landmine): a
// scheduler_tasks row of kind 'skill_ttl_sweep' INSERTs successfully against the
// 0010-widened kind CHECK (the 0009 CHECK admitted only reminder/agent_job/backup_*;
// 0010 ALTERed it to add skill_ttl_sweep). A failing INSERT would mean the migration
// did not widen the constraint — the exact A2 landmine the planner flagged.
//
// Run via:
//
//	go test -tags db_integration -race -run TestSkillTTLSeed ./internal/cron -count=1
package cron

import (
	"context"
	"testing"
	"time"
)

// TestSkillTTLSeed proves a skill_ttl_sweep task INSERTs against the 0010-widened
// scheduler_tasks.kind CHECK and reads back with its kind intact (A2 closed).
func TestSkillTTLSeed(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	spec, err := ParseSchedule(string(KindCron), "0 3 * * *", 0, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	next, err := NextRunAt(spec, time.Now().UTC())
	if err != nil {
		t.Fatalf("NextRunAt: %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		Kind:        KindSkillTTLSweep,
		Spec:        spec,
		NextRunAt:   next,
		NotifyRoute: "none",
	})
	if err != nil {
		// A 23514 check_violation here is the A2 landmine: the 0010 migration did not
		// widen the kind CHECK. Surface it loudly.
		t.Fatalf("CreateTask(skill_ttl_sweep) rejected — 0010 kind CHECK not widened (A2): %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })

	if task.Kind != KindSkillTTLSweep {
		t.Fatalf("seeded task kind = %q, want %q", task.Kind, KindSkillTTLSweep)
	}

	// Read it back via the active list to confirm it persisted (the idempotent seed's
	// existence check reads this same list).
	tasks, err := store.ListActiveTasks(ctx)
	if err != nil {
		t.Fatalf("ListActiveTasks: %v", err)
	}
	found := false
	for _, tk := range tasks {
		if tk.ID == task.ID && tk.Kind == KindSkillTTLSweep {
			found = true
		}
	}
	if !found {
		t.Fatal("seeded skill_ttl_sweep task not found in active tasks")
	}
}
