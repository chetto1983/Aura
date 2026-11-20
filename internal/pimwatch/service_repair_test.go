package pimwatch

import (
	"context"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/cron"
)

type fakeCursors []Cursor

func (f fakeCursors) Cursors(context.Context, string) ([]Cursor, error) { return f, nil }

func repairRig(task *WatchTask, lastError string) (*Service, *fakeTasks) {
	tasks := newFakeTasks()
	if task != nil {
		tasks.watches["id|home"] = *task
	}
	return &Service{Tasks: tasks, cursors: fakeCursors{{AccountID: "home", Resource: ResourceMail, LastError: lastError}}}, tasks
}

func TestRepairedResumesAWatchPausedForACredentialFailure(t *testing.T) {
	for _, code := range []string{"provider_auth", "provider_transient", "pim_unreachable"} {
		svc, tasks := repairRig(&WatchTask{ID: "id|home", AccountID: "home", Paused: true, PausedReason: cron.PausedByFailures}, code)
		if err := svc.Repaired(context.Background(), "id", "home"); err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if tasks.watches["id|home"].Paused || !slices.Equal(tasks.runNow, []string{"id|home"}) {
			t.Fatalf("%s: paused=%v runNow=%v", code, tasks.watches["id|home"].Paused, tasks.runNow)
		}
	}
}

func TestRepairedLeavesAFailurePauseForAnotherCauseAlone(t *testing.T) {
	svc, tasks := repairRig(&WatchTask{ID: "id|home", AccountID: "home", Paused: true, PausedReason: cron.PausedByFailures}, CodePermissionDenied)
	if err := svc.Repaired(context.Background(), "id", "home"); err != nil {
		t.Fatal(err)
	}
	if !tasks.watches["id|home"].Paused || len(tasks.runNow) != 0 {
		t.Fatalf("paused=%v runNow=%v", tasks.watches["id|home"].Paused, tasks.runNow)
	}
}

func TestRepairedRunsAnActiveWatchNow(t *testing.T) {
	svc, tasks := repairRig(&WatchTask{ID: "id|home", AccountID: "home"}, "")
	if err := svc.Repaired(context.Background(), "id", "home"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tasks.runNow, []string{"id|home"}) {
		t.Fatalf("runNow = %v", tasks.runNow)
	}
}

func TestRepairedLeavesAnOperatorPauseAlone(t *testing.T) {
	svc, tasks := repairRig(&WatchTask{ID: "id|home", AccountID: "home", Paused: true, PausedReason: cron.PausedByOperator}, "provider_auth")
	if err := svc.Repaired(context.Background(), "id", "home"); err != nil {
		t.Fatal(err)
	}
	if !tasks.watches["id|home"].Paused || len(tasks.runNow) != 0 {
		t.Fatalf("paused=%v runNow=%v", tasks.watches["id|home"].Paused, tasks.runNow)
	}
}

func TestRepairedWithNoWatchDoesNothing(t *testing.T) {
	svc, tasks := repairRig(nil, "provider_auth")
	if err := svc.Repaired(context.Background(), "id", "home"); err != nil {
		t.Fatal(err)
	}
	if len(tasks.runNow) != 0 || tasks.created != 0 {
		t.Fatalf("runNow=%v created=%d", tasks.runNow, tasks.created)
	}
}
