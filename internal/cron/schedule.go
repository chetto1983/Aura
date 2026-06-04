// Package cron is the Phase 10 scheduler: the at|every|cron schedule engine
// (this file), the thin sqlc Store over scheduler_tasks + agent_job_runs
// (store.go), and — landing in downstream waves — the tick loop, held-conn
// advisory-lock claim, heartbeat, and recovery scan.
//
// The schedule engine is DST-safe by construction (D-06/D-07): tasks store an
// (expr, IANA tz) pair and next_run_at is ALWAYS recomputed in-zone then read as
// UTC. A fixed UTC offset is never stored — that would drift ±1h across a DST
// transition (RESEARCH Pitfall 3).
package cron

import (
	"errors"
	"fmt"
	"time"

	"github.com/adhocore/gronx"
)

// MinScheduleEveryMinutes is the floor for the `every` grammar: an interval
// shorter than this is rejected at parse time. Five minutes keeps the tick loop
// (30s default) from thrashing on near-continuous reschedules.
const MinScheduleEveryMinutes = 5

// ScheduleKind is the at|every|cron grammar union (D-06).
type ScheduleKind string

// The three schedule grammars: a one-shot at a fixed instant, a fixed-interval
// recurrence, and a cron expression evaluated in the task's TZ.
const (
	KindAt    ScheduleKind = "at"    // one-shot at a fixed instant
	KindEvery ScheduleKind = "every" // recurring every N minutes
	KindCron  ScheduleKind = "cron"  // recurring per a cron expression in TZ
)

// Sentinel parse errors so callers classify a bad spec without string-matching.
var (
	ErrInvalidScheduleKind = errors.New("invalid schedule kind")
	ErrInvalidCronExpr     = errors.New("invalid cron expression")
	ErrEveryTooSmall       = fmt.Errorf("every interval below the %d-minute floor", MinScheduleEveryMinutes)
	ErrMissingRunAt        = errors.New("at schedule requires a run_at instant")
	ErrInvalidTimezone     = errors.New("invalid IANA timezone")
)

// ScheduleSpec is the parsed, validated grammar union. Exactly one of the
// kind-specific fields is meaningful per Kind: RunAt for at, EveryMinutes for
// every, CronExpr for cron. TZ is the IANA zone the recurring recompute runs in
// (ignored for at, which carries an absolute instant).
type ScheduleSpec struct {
	Kind         ScheduleKind
	CronExpr     string
	EveryMinutes int
	RunAt        time.Time
	TZ           string
}

// ParseSchedule validates the raw grammar inputs and returns a ScheduleSpec.
// Cron expressions are validated via gronx.IsValid BEFORE any persist (T-10-05 /
// V5 input validation); every is floored at MinScheduleEveryMinutes; at requires a
// non-zero run_at. The tz must resolve via time.LoadLocation (the binary embeds
// tzdata via tzdata.go so this never depends on host zoneinfo).
func ParseSchedule(kind, cronExpr string, everyMinutes int, runAt time.Time, tz string) (ScheduleSpec, error) {
	if tz == "" {
		tz = "Europe/Rome"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return ScheduleSpec{}, fmt.Errorf("%w: %q: %v", ErrInvalidTimezone, tz, err)
	}

	spec := ScheduleSpec{Kind: ScheduleKind(kind), TZ: tz}
	switch ScheduleKind(kind) {
	case KindAt:
		if runAt.IsZero() {
			return ScheduleSpec{}, ErrMissingRunAt
		}
		spec.RunAt = runAt.UTC()
	case KindEvery:
		if everyMinutes < MinScheduleEveryMinutes {
			return ScheduleSpec{}, fmt.Errorf("%w: got %d", ErrEveryTooSmall, everyMinutes)
		}
		spec.EveryMinutes = everyMinutes
	case KindCron:
		if !gronx.IsValid(cronExpr) {
			return ScheduleSpec{}, fmt.Errorf("%w: %q", ErrInvalidCronExpr, cronExpr)
		}
		spec.CronExpr = cronExpr
	default:
		return ScheduleSpec{}, fmt.Errorf("%w: %q", ErrInvalidScheduleKind, kind)
	}
	return spec, nil
}

// NextRunAt computes the next fire instant strictly after `after`, returned in
// UTC. For at it returns the stored instant (or a zero time once it has fired).
// For every it adds the interval. For cron it recomputes IN-ZONE: the ref is
// converted into spec.TZ so gronx preserves the location through its bump loop
// (D-07), and the result is converted back to UTC. NEVER returns a fixed offset.
func NextRunAt(spec ScheduleSpec, after time.Time) (time.Time, error) {
	switch spec.Kind {
	case KindAt:
		if !spec.RunAt.After(after) {
			return time.Time{}, nil // one-shot already due/fired — no further runs
		}
		return spec.RunAt.UTC(), nil
	case KindEvery:
		if spec.EveryMinutes < MinScheduleEveryMinutes {
			return time.Time{}, fmt.Errorf("%w: got %d", ErrEveryTooSmall, spec.EveryMinutes)
		}
		return after.Add(time.Duration(spec.EveryMinutes) * time.Minute).UTC(), nil
	case KindCron:
		loc, err := time.LoadLocation(spec.TZ)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: %q: %v", ErrInvalidTimezone, spec.TZ, err)
		}
		refInZone := after.In(loc) // carry tz via the ref (gronx preserves ref.Location())
		next, err := gronx.NextTickAfter(spec.CronExpr, refInZone, false)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: %q: %v", ErrInvalidCronExpr, spec.CronExpr, err)
		}
		return next.UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidScheduleKind, spec.Kind)
	}
}
