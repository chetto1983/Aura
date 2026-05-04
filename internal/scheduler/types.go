// Package scheduler runs background tasks on a SQLite-backed schedule.
//
// Task kinds:
//   - reminder         a Telegram message dispatched to a user/chat.
//   - wiki_maintenance the autonomous nightly pass that runs list_wiki +
//     lint_wiki + rebuild_index + append_log so the
//     wiki stays healthy without user intervention.
//   - agent_job        a bounded scheduled agent routine with propose-only
//     memory growth by default.
//
// Three schedule kinds:
//   - at    fires once at an absolute UTC timestamp; status flips to done.
//   - daily fires at HH:MM in the bot's local timezone; optionally limited
//     to selected weekdays (mon,tue,wed,thu,fri,sat,sun).
//   - every fires every N minutes; the first fire is N minutes from
//     creation, then continues at that cadence. Covers hourly,
//     weekly, and custom intervals (every 30/120/10080 minutes).
//
// All tasks survive process restarts because state lives in SQLite. The
// tick loop wakes every TickInterval, picks up rows where status='active'
// AND next_run_at <= now, fires them sequentially, then advances
// next_run_at (daily) or marks done (at). Failures are recorded in the
// row so the LLM can introspect them via list_tasks.
package scheduler

import "time"

// TaskKind enumerates what the dispatcher does when a task fires.
type TaskKind string

const (
	KindReminder        TaskKind = "reminder"
	KindWikiMaintenance TaskKind = "wiki_maintenance"
	KindAgentJob        TaskKind = "agent_job"
	KindAutoImprove     TaskKind = "auto_improve"
)

// ScheduleKind enumerates how the scheduler computes a task's next run.
type ScheduleKind string

const (
	ScheduleAt    ScheduleKind = "at"
	ScheduleDaily ScheduleKind = "daily"
	ScheduleEvery ScheduleKind = "every"
)

// Status enumerates the lifecycle of a task row.
type Status string

const (
	StatusActive    Status = "active"
	StatusDone      Status = "done"
	StatusCancelled Status = "cancelled"
	StatusFailed    Status = "failed"
)

// Task is the on-disk + in-memory representation of a scheduled job.
// Exactly one of ScheduleAt / ScheduleDaily / ScheduleEveryMinutes is the
// active field, matched by ScheduleKind. ScheduleWeekdays optionally narrows
// ScheduleDaily. NextRunAt is what the tick loop actually consults; it's
// derived from the schedule and updated after each fire.
type Task struct {
	ID                   int64
	Name                 string // unique; bootstrap jobs use this for idempotent UPSERT
	Kind                 TaskKind
	Payload              string // task-specific body — for reminder, the message text
	RecipientID          string // Telegram user ID for reminder delivery; empty for system jobs
	ScheduleKind         ScheduleKind
	ScheduleAt           time.Time // populated when ScheduleKind == ScheduleAt (UTC)
	ScheduleDaily        string    // populated when ScheduleKind == ScheduleDaily ("HH:MM" local)
	ScheduleWeekdays     string    // optional daily filter: comma-separated mon,tue,...
	ScheduleEveryMinutes int       // populated when ScheduleKind == ScheduleEvery (minutes between fires)
	NextRunAt            time.Time // UTC
	LastRunAt            time.Time // zero until first fire
	LastError            string    // set when the dispatcher returns an error
	LastOutput           string    // compact last agent_job output, if any
	LastMetricsJSON      string    // compact JSON metrics for the last agent_job run
	WakeSignature        string    // deterministic signature used by wake_if_changed gates
	Status               Status
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// IsRecurring reports whether the task should be rescheduled after firing.
func (t *Task) IsRecurring() bool {
	return t.ScheduleKind == ScheduleDaily || t.ScheduleKind == ScheduleEvery
}
