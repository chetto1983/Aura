package pimwatch

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/cron"
)

func TestRepairStepsResumeAndRunAWatchPausedForACredentialFailure(t *testing.T) {
	watch := WatchTask{AccountID: "home", Paused: true, PausedReason: cron.PausedByFailures}
	for _, code := range []string{"provider_auth", "provider_transient", "pim_unreachable"} {
		resume, runNow := repairSteps(watch, []Cursor{{AccountID: "home", LastError: code}}, "home")
		if !resume || !runNow {
			t.Fatalf("%s: resume=%v runNow=%v", code, resume, runNow)
		}
	}
}

func TestRepairStepsLeaveAFailurePauseForAnotherCauseAlone(t *testing.T) {
	watch := WatchTask{AccountID: "home", Paused: true, PausedReason: cron.PausedByFailures}
	resume, runNow := repairSteps(watch, []Cursor{{AccountID: "home", LastError: CodePermissionDenied}}, "home")
	if resume || runNow {
		t.Fatalf("resume=%v runNow=%v", resume, runNow)
	}
}

func TestRepairStepsOnlyRunAnActiveWatch(t *testing.T) {
	resume, runNow := repairSteps(WatchTask{AccountID: "home"}, nil, "home")
	if resume || !runNow {
		t.Fatalf("resume=%v runNow=%v", resume, runNow)
	}
}

func TestRepairStepsLeaveAnOperatorPauseAlone(t *testing.T) {
	watch := WatchTask{AccountID: "home", Paused: true, PausedReason: cron.PausedByOperator}
	resume, runNow := repairSteps(watch, []Cursor{{AccountID: "home", LastError: "provider_auth"}}, "home")
	if resume || runNow {
		t.Fatalf("resume=%v runNow=%v", resume, runNow)
	}
}

func TestRepairStepsIgnoreAnotherAccountsFailure(t *testing.T) {
	watch := WatchTask{AccountID: "home", Paused: true, PausedReason: cron.PausedByFailures}
	resume, runNow := repairSteps(watch, []Cursor{{AccountID: "work", LastError: "provider_auth"}}, "home")
	if resume || runNow {
		t.Fatalf("resume=%v runNow=%v", resume, runNow)
	}
}

// repairRig is a Service whose Store is never reachable: an identity that is not a UUID makes a
// cursor read fail before it touches the pool, so a test sees whether Repaired read them.
func repairRig(watches ...WatchTask) (*Service, *fakeTasks) {
	tasks := newFakeTasks()
	for _, w := range watches {
		tasks.watches[repairIdentity+"|"+w.AccountID] = w
	}
	return &Service{Store: NewStore(nil), Tasks: tasks}, tasks
}

const repairIdentity = "identity-not-a-uuid"

func TestRepairedWithNoWatchWakesNothing(t *testing.T) {
	s, tasks := repairRig(WatchTask{ID: "w-work", AccountID: "work"})
	if err := s.Repaired(context.Background(), repairIdentity, "home"); err != nil {
		t.Fatalf("repaired: %v", err)
	}
	if len(tasks.runNow) != 0 {
		t.Fatalf("runNow = %v, want none", tasks.runNow)
	}
}

func TestRepairedRunsAnActiveWatchWithoutReadingCursors(t *testing.T) {
	s, tasks := repairRig(WatchTask{ID: "w-home", AccountID: "home"})
	if err := s.Repaired(context.Background(), repairIdentity, "home"); err != nil {
		t.Fatalf("repaired: %v; an active watch must not read cursors", err)
	}
	if !slices.Equal(tasks.runNow, []string{"w-home"}) {
		t.Fatalf("runNow = %v, want [w-home]", tasks.runNow)
	}
}

func TestRepairedReportsAFailedWatchListing(t *testing.T) {
	s, tasks := repairRig(WatchTask{ID: "w-home", AccountID: "home"})
	tasks.err = errors.New("scheduler down")
	if err := s.Repaired(context.Background(), repairIdentity, "home"); !errors.Is(err, tasks.err) {
		t.Fatalf("repaired = %v, want the listing error", err)
	}
	if len(tasks.runNow) != 0 {
		t.Fatalf("runNow = %v, want none", tasks.runNow)
	}
}

func TestRepairedReportsAFailedRunNow(t *testing.T) {
	s, tasks := repairRig(WatchTask{ID: "w-home", AccountID: "home"})
	tasks.runNowErr = errors.New("queue full")
	if err := s.Repaired(context.Background(), repairIdentity, "home"); !errors.Is(err, tasks.runNowErr) {
		t.Fatalf("repaired = %v, want the RunNow error", err)
	}
}

func TestRepairedReadsCursorsBeforeTouchingAPausedWatch(t *testing.T) {
	paused := WatchTask{ID: "w-home", AccountID: "home", Paused: true, PausedReason: cron.PausedByFailures}
	s, tasks := repairRig(paused)
	if err := s.Repaired(context.Background(), repairIdentity, "home"); err == nil {
		t.Fatal("repaired = nil, want the cursor read's error")
	}
	if !tasks.watches[repairIdentity+"|home"].Paused || len(tasks.runNow) != 0 {
		t.Fatalf("paused=%v runNow=%v; a watch whose cause is unknown stays as it was",
			tasks.watches[repairIdentity+"|home"].Paused, tasks.runNow)
	}
}
