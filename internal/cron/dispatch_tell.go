package cron

import (
	"errors"
	"fmt"
)

// dispatch_tell.go is the seam that lets one run say different things to different readers
// (docs/superpowers/specs/2026-10-31-mail-calendar-watch-design.md §5.2): the notification, the
// conversation record and the ledger summary. A mail's sender and subject are allowed in the
// first two and never in agent_job_runs.summary.

// toldText is what a handler asked to say through Job.Tell. The last call wins.
type toldText struct {
	set            bool
	notice, record string
}

func (t *toldText) tell(notice, record string) {
	t.set, t.notice, t.record = true, notice, record
}

// texts picks the notification's and the record's text. A handler that never told gets the
// summary in both, as before the seam existed. On stdout the notice is dropped: that route prints
// to the process journal, and there the conversation record is the delivery.
func (t *toldText) texts(task Task, summary string) (notice, record string) {
	if !t.set {
		return summary, summary
	}
	if task.NotifyRoute == string(RouteStdout) {
		return "", t.record
	}
	return t.notice, t.record
}

// suppressed reports a failure that asked to stay out of the notification and the record alike.
func suppressed(runErr error) bool {
	var suppressor interface{ SuppressSchedulerNotification() bool }
	return errors.As(runErr, &suppressor) && suppressor.SuppressSchedulerNotification()
}

// failureText is what a failed run reports. A pause its handler wrote words for reads as those
// alone, because a person reads them: the kind and the cause stay on the ledger.
func failureText(task Task, runErr error) string {
	if paused, ok := errors.AsType[pausedRunError](runErr); ok && paused.report != "" {
		return paused.report
	}
	return fmt.Sprintf("%s failed: %s", task.Kind, runErr.Error())
}

// pausedRunError reports the failure that paused a task. It always answers false to suppressed,
// and errors.As meets it first, so the run that pauses is reported even when its own error asked
// for silence (spec §5.6). report is the handler's own words for this, when its failure carries
// them: whether a run pauses is known only after it ends, so they travel on the error.
type pausedRunError struct {
	cause    error
	failures int
	report   string
}

func newPausedRunError(cause error, failures int) pausedRunError {
	paused := pausedRunError{cause: cause, failures: failures}
	var reporter interface{ PauseReport() string }
	if errors.As(cause, &reporter) {
		paused.report = reporter.PauseReport()
	}
	return paused
}

func (e pausedRunError) Error() string {
	return fmt.Sprintf("%v; paused after %d failed runs in a row, resume it to run again", e.cause, e.failures)
}

func (e pausedRunError) Unwrap() error { return e.cause }

// SuppressSchedulerNotification is false whatever the cause says.
func (e pausedRunError) SuppressSchedulerNotification() bool { return false }
