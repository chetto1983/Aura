package agui

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeTaskPurger struct{ recordingPort }

func (f *fakeTaskPurger) PurgeScheduledTasks(_ context.Context, id string) error { return f.record(id) }

func depsWithTaskPurger() (DeprovisionDeps, *deprovFakes, *fakeTaskPurger) {
	deps, f := fullDeprovisionDeps()
	tasks := &fakeTaskPurger{}
	deps.ScheduledTasks = tasks
	return deps, f, tasks
}

func TestDeprovisionPurgeRemovesTheScheduledTasks(t *testing.T) {
	t.Parallel()
	deps, f, tasks := depsWithTaskPurger()

	if err := NewDeprovisioner(deps).Purge(context.Background(), targetFor(testIdentityID)); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(tasks.seen) != 1 || tasks.seen[0] != testIdentityID {
		t.Fatalf("purged tasks of %v, want %s once", tasks.seen, testIdentityID)
	}
	if !f.journal.stepDone(sagaID(sagaKindDeprovision, testIdentityID), sagaStepScheduledTasks) {
		t.Fatal("the step was not journaled done")
	}
}

// A purge that failed after the step skips it on the re-run.
func TestDeprovisionPurgeResumeSkipsTheTasksOnceDone(t *testing.T) {
	t.Parallel()
	deps, f, tasks := depsWithTaskPurger()
	f.memory.err = errors.New("injected: memory database drop crash")
	d := NewDeprovisioner(deps)
	target := targetFor(testIdentityID)

	if err := d.Purge(context.Background(), target); err == nil {
		t.Fatal("want the memory leg's failure")
	}
	f.memory.err = nil
	if err := d.Purge(context.Background(), target); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if tasks.count() != 1 {
		t.Fatalf("tasks purged %d times, want 1: a done step is skipped", tasks.count())
	}
}

// A failure here stops the saga before the box and the data, and the re-run takes the step again.
func TestDeprovisionPurgeRetriesAFailedTasksStep(t *testing.T) {
	t.Parallel()
	deps, f, tasks := depsWithTaskPurger()
	tasks.err = errors.New("injected: scheduler_tasks delete failed")
	d := NewDeprovisioner(deps)
	target := targetFor(testIdentityID)

	if err := d.Purge(context.Background(), target); !errors.Is(err, tasks.err) {
		t.Fatalf("Purge = %v", err)
	}
	if f.conv.count() != 0 {
		t.Fatal("the saga went on past a failed step")
	}
	tasks.err = nil
	if err := d.Purge(context.Background(), target); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if tasks.count() != 1 || f.conv.count() != 1 {
		t.Fatalf("resume: tasks=%d conversations=%d, want 1/1", tasks.count(), f.conv.count())
	}
}

// `aura identity purge --resume-resources` has only the id: its row is gone, and the tasks,
// which name the identity in text, are not.
func TestDeprovisionAnIDOnlyPurgeRemovesTheTasks(t *testing.T) {
	t.Parallel()
	deps, _, tasks := depsWithTaskPurger()

	err := NewDeprovisioner(deps).Purge(context.Background(), DeprovisionTarget{IdentityID: testIdentityID})
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if tasks.count() != 1 {
		t.Fatalf("tasks purged %d times, want 1", tasks.count())
	}
}

type orderedTasks struct{ log *orderLog }

func (o orderedTasks) PurgeScheduledTasks(context.Context, string) error {
	return o.log.add("tasks")
}

// A task firing after the box is gone would wake a new one, with no identity left to own it.
func TestDeprovisionRemovesTheTasksBeforeTheBox(t *testing.T) {
	t.Parallel()
	log := &orderLog{}
	deps := sandboxOrderDeps(log, orderedSandbox{log: log})
	deps.ScheduledTasks = orderedTasks{log: log}

	if err := NewDeprovisioner(deps).Purge(context.Background(), targetFor(testIdentityID)); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	order := log.order()
	if tasks, box := slices.Index(order, "tasks"), slices.Index(order, "sandbox"); tasks < 0 || tasks > box {
		t.Fatalf("order = %v, want tasks before sandbox", order)
	}
}
