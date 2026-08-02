package swarm

import (
	"strings"
	"testing"
)

func TestPanicChildReportPreservesWorkerIdentity(t *testing.T) {
	report := panicChildReport(2, "boom")
	if report.GoalIndex != 2 ||
		report.ChildID != "w3" ||
		report.Status != StatusFailed ||
		report.Error != "panic: boom" {
		t.Fatalf("panic report = %+v", report)
	}
}

func TestPreflightRejectsInvalidGoalSets(t *testing.T) {
	runConfig := testRunConfig(t, newRouter(), 25)
	runConfig.Cfg.MaxSwarmGoals = 1
	tests := []struct {
		name    string
		goals   []string
		message string
	}{
		{name: "empty", message: "no goals provided"},
		{name: "over cap", goals: []string{"one", "two"}, message: "too many goals"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, ok := preflight(runConfig, test.goals)
			if ok || !strings.Contains(message, test.message) {
				t.Fatalf("preflight = (%q, %t), want rejection containing %q", message, ok, test.message)
			}
		})
	}
}
