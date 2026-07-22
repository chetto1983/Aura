package retention

import (
	"testing"
	"time"
)

func TestActivityEvidenceProtectsEveryClass(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	freshness := time.Minute
	for _, tc := range []struct {
		name string
		e    ActivityEvidence
	}{
		{"live turn lease", ActivityEvidence{LiveTurnLeaseUntil: now}},
		{"worker heartbeat", ActivityEvidence{WorkerHeartbeatAt: now.Add(-freshness)}},
		{"pending pause", ActivityEvidence{PendingPause: true}},
		{"pending approval", ActivityEvidence{PendingApproval: true}},
		{"queued scheduler", ActivityEvidence{SchedulerStatus: "queued"}},
		{"running scheduler", ActivityEvidence{SchedulerStatus: "running"}},
		{"background tool", ActivityEvidence{BackgroundToolJobs: 1}},
		{"sandbox", ActivityEvidence{SandboxJobs: 1}},
		{"artifact operation", ActivityEvidence{UnfinishedArtifactOperations: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			protected, reason := tc.e.Protected(now, freshness)
			if !protected || reason == "" {
				t.Fatalf("Protected() = %v, %q", protected, reason)
			}
		})
	}
}

func TestActivityFreshnessBeforeEqualAfter(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	freshness := time.Minute
	for _, tc := range []struct {
		name      string
		heartbeat time.Time
		want      bool
	}{
		{"before", now.Add(-freshness - time.Nanosecond), false},
		{"equal", now.Add(-freshness), true},
		{"after", now.Add(-freshness + time.Nanosecond), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := (ActivityEvidence{WorkerHeartbeatAt: tc.heartbeat}).Protected(now, freshness)
			if got != tc.want {
				t.Fatalf("Protected() = %v, want %v", got, tc.want)
			}
		})
	}
}
