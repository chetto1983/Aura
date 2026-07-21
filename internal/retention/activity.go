package retention

import "time"

// Stable activity reasons recorded when automatic cleanup skips an owner.
const (
	ActivityLiveTurn          = "live_turn"
	ActivityWorkerHeartbeat   = "worker_heartbeat"
	ActivityPendingPause      = "pending_pause"
	ActivityPendingApproval   = "pending_approval"
	ActivitySchedulerWork     = "scheduler_work"
	ActivityBackgroundTool    = "background_tool"
	ActivitySandbox           = "sandbox"
	ActivityArtifactOperation = "artifact_operation"
)

// ActivityEvidence is a content-free projection of every durable active-work class.
type ActivityEvidence struct {
	LiveTurnLeaseUntil           time.Time
	WorkerHeartbeatAt            time.Time
	PendingPause                 bool
	PendingApproval              bool
	SchedulerStatus              string
	BackgroundToolJobs           int
	SandboxJobs                  int
	UnfinishedArtifactOperations int
}

// Protected reports whether any locked activity class is live at now.
func (e ActivityEvidence) Protected(now time.Time, freshness time.Duration) (bool, string) {
	if !e.LiveTurnLeaseUntil.IsZero() && !e.LiveTurnLeaseUntil.Before(now) {
		return true, ActivityLiveTurn
	}
	cutoff := now.Add(-freshness)
	if freshness > 0 && !e.WorkerHeartbeatAt.IsZero() && !e.WorkerHeartbeatAt.Before(cutoff) {
		return true, ActivityWorkerHeartbeat
	}
	if e.PendingPause {
		return true, ActivityPendingPause
	}
	if e.PendingApproval {
		return true, ActivityPendingApproval
	}
	if e.SchedulerStatus == "queued" || e.SchedulerStatus == "running" || e.SchedulerStatus == "pending_approval" {
		return true, ActivitySchedulerWork
	}
	if e.BackgroundToolJobs > 0 {
		return true, ActivityBackgroundTool
	}
	if e.SandboxJobs > 0 {
		return true, ActivitySandbox
	}
	if e.UnfinishedArtifactOperations > 0 {
		return true, ActivityArtifactOperation
	}
	return false, ""
}
