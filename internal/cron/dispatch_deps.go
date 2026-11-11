package cron

import (
	"time"

	"github.com/chetto1983/aura/internal/scoring"
)

// DispatchDeps carries the run lifecycle collaborators: the store (run completion),
// the Notifier (delivery), the alert threshold, and the quiet-hours predicate.
type DispatchDeps struct {
	Store             RunCompleter
	NotificationStore PendingNotificationStore
	Notifier          Notifier
	AlertThreshold    scoring.RiskTier
	// QuietHours reports whether the current wall time is inside the deferral window
	// (the scheduler's DuringQuietHours predicate, D-23). Nil = never quiet.
	QuietHours func(tz string) bool
	// QuietHoursEnd returns the wall-clock instant when the current quiet-hours
	// window ends. It is paired with QuietHours so deferred notifications get a
	// durable notify_after instead of being dropped.
	QuietHoursEnd func(tz string) (time.Time, bool)
	// ChannelDeliverer projects stdout to its exact origin conversation and explicit
	// telegram to the linked identity. The composition root adapts the channel registry.
	ChannelDeliverer ChannelDeliverer
	// ApprovalReminderStore backs the per-tick pending-approval reminder sweep (fix-plan
	// 1.7 / Amendment #92). Nil → no sweep (defaulted from Store in NewDispatch when the
	// concrete *Store is supplied, mirroring NotificationStore).
	ApprovalReminderStore ApprovalReminderStore
	// ApprovalPauseEnsurer + ApprovalChannel back the on-channel HITL re-surface of a
	// pending_approval task (Amendment #92 revised): the sweep ensures a real ask_user pause
	// exists on the task's origin conversation, then pushes the actionable prompt to the origin
	// channel. Nil (either) → the approval sweep no-ops. The composition root wires them; cron
	// imports neither askuser nor channels (import-cycle constraint).
	ApprovalPauseEnsurer ApprovalPauseEnsurer
	ApprovalChannel      ApprovalChannel
	// ConversationRecorder writes a finished run's outcome back into the conversation
	// the task was scheduled from. It is the cron-LOCAL consumer-declared interface the
	// composition root adapts *conversations.Store onto, mirroring ChannelDeliverer —
	// cron imports neither conversations nor config. Nil → no-op.
	//
	// It exists because a push is not a record. Measured 2026-08-26: an operator
	// scheduled a reminder from the COCKPIT, the run completed, and the result went to
	// Telegram — the only channel in the tree implementing Deliverer — while the cockpit
	// conversation ended at "scheduled ✅" and never learned the outcome. The origin was
	// on the task all along (origin_conversation_id) and only the approval-pause path
	// read it. LibreChat's rule (D-00): the conversation IS the channel.
	ConversationRecorder ConversationRecorder
	// TaskConversations and TaskOrigins move a task whose conversation was deleted, or an
	// agent_job that never had one, into one of its own (task_home.go). TaskOrigins defaults
	// from Store; either nil → the task keeps its origin, deleted or absent.
	TaskConversations TaskConversations
	TaskOrigins       TaskOriginSetter
	// OutcomeRecorder counts failed runs and pauses a task after
	// AURA_SCHEDULER_PAUSE_AFTER_FAILURES of them. Nil → defaulted from Store in NewDispatch
	// when the concrete *Store is supplied; still nil means no counting and no auto-pause.
	OutcomeRecorder RunOutcomeRecorder
	// PauseAfterFailures overrides AURA_SCHEDULER_PAUSE_AFTER_FAILURES when non-nil (tests).
	PauseAfterFailures *int
	// RetryScheduler re-arms a job that failed on a transient model error before any tool ran.
	// Nil → defaulted from Store in NewDispatch; still nil means such a job is never retried.
	RetryScheduler TransientRetryScheduler
	// LapsedApprovals, LapsedPauseCloser and LapsedMessages back the expiry of a one-shot whose
	// time passed unapproved (deliver_lapsed.go). LapsedApprovals defaults from Store; the other
	// two may be nil: no pause to close, no message to settle.
	LapsedApprovals   LapsedApprovalStore
	LapsedPauseCloser LapsedPauseCloser
	LapsedMessages    LapsedMessages
}
