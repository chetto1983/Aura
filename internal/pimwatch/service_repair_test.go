package pimwatch

import (
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
