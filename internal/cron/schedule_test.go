package cron

import (
	"errors"
	"testing"
	"time"
)

func mustLoad(t *testing.T, tz string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(tz)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", tz, err)
	}
	return loc
}

// TestNextRunAt covers the three grammar kinds with a frozen reference instant
// (NO real sleep). at returns the stored UTC instant; every adds the interval;
// cron returns the next in-zone fire converted to UTC.
func TestNextRunAt(t *testing.T) {
	rome := mustLoad(t, "Europe/Rome")

	t.Run("at returns the stored instant in UTC", func(t *testing.T) {
		fire := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
		spec, err := ParseSchedule("at", "", 0, fire, "Europe/Rome")
		if err != nil {
			t.Fatalf("ParseSchedule at: %v", err)
		}
		before := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
		got, err := NextRunAt(spec, before)
		if err != nil {
			t.Fatalf("NextRunAt at: %v", err)
		}
		if !got.Equal(fire) {
			t.Errorf("at next = %s, want %s", got, fire)
		}
	})

	t.Run("at already fired returns zero (no further runs)", func(t *testing.T) {
		fire := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
		spec, _ := ParseSchedule("at", "", 0, fire, "Europe/Rome")
		after := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
		got, err := NextRunAt(spec, after)
		if err != nil {
			t.Fatalf("NextRunAt: %v", err)
		}
		if !got.IsZero() {
			t.Errorf("fired one-shot next = %s, want zero", got)
		}
	})

	t.Run("every adds the interval", func(t *testing.T) {
		spec, err := ParseSchedule("every", "", 15, time.Time{}, "Europe/Rome")
		if err != nil {
			t.Fatalf("ParseSchedule every: %v", err)
		}
		ref := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
		got, err := NextRunAt(spec, ref)
		if err != nil {
			t.Fatalf("NextRunAt every: %v", err)
		}
		want := ref.Add(15 * time.Minute)
		if !got.Equal(want) {
			t.Errorf("every next = %s, want %s", got, want)
		}
	})

	t.Run("cron 30 9 returns next 09:30 Rome as UTC", func(t *testing.T) {
		spec, err := ParseSchedule("cron", "30 9 * * *", 0, time.Time{}, "Europe/Rome")
		if err != nil {
			t.Fatalf("ParseSchedule cron: %v", err)
		}
		// Summer (CEST = UTC+2): 09:30 Rome == 07:30 UTC.
		ref := time.Date(2026, 6, 1, 8, 0, 0, 0, rome)
		got, err := NextRunAt(spec, ref)
		if err != nil {
			t.Fatalf("NextRunAt cron: %v", err)
		}
		want := time.Date(2026, 6, 1, 7, 30, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("cron next = %s, want %s (07:30 UTC)", got.UTC(), want)
		}
	})
}

// TestNextRunAtDST is the Open Q1 spring-forward fixture. On 2026-03-29 Europe/Rome
// springs forward 02:00 CET -> 03:00 CEST, so the local wall-clock 02:30 does NOT
// exist that night. A cron "30 2 * * *" with a frozen ref at the start of that day
// must produce a deterministic fire. gronx (via time.Date normalization) SKIPS the
// non-existent slot and fires the NEXT valid 02:30 — i.e. 2026-03-30 02:30 CEST,
// which is 2026-03-30 00:30 UTC. This proves there is no silent ±1h offset drift:
// the engine recomputes in-zone, never storing a fixed UTC offset.
func TestNextRunAtDST(t *testing.T) {
	rome := mustLoad(t, "Europe/Rome")
	spec, err := ParseSchedule("cron", "30 2 * * *", 0, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule cron: %v", err)
	}
	ref := time.Date(2026, 3, 29, 0, 0, 0, 0, rome) // spring-forward day, before the gap
	got, err := NextRunAt(spec, ref)
	if err != nil {
		t.Fatalf("NextRunAt DST: %v", err)
	}
	want := time.Date(2026, 3, 30, 0, 30, 0, 0, time.UTC) // 02:30 CEST next day
	if !got.Equal(want) {
		t.Errorf("DST spring-forward next = %s, want %s (02:30 CEST on 03-30 = 00:30 UTC)", got.UTC(), want)
	}
}

// TestParseScheduleValidation asserts the input-validation gate (T-10-05): an
// invalid cron expr, a sub-floor every, a missing at instant, and a bad tz are all
// rejected at parse time with their sentinel error.
func TestParseScheduleValidation(t *testing.T) {
	t.Run("invalid cron expr rejected", func(t *testing.T) {
		_, err := ParseSchedule("cron", "not a cron", 0, time.Time{}, "Europe/Rome")
		if !errors.Is(err, ErrInvalidCronExpr) {
			t.Errorf("want ErrInvalidCronExpr, got %v", err)
		}
	})
	t.Run("every below floor rejected", func(t *testing.T) {
		_, err := ParseSchedule("every", "", MinScheduleEveryMinutes-1, time.Time{}, "Europe/Rome")
		if !errors.Is(err, ErrEveryTooSmall) {
			t.Errorf("want ErrEveryTooSmall, got %v", err)
		}
	})
	t.Run("at without run_at rejected", func(t *testing.T) {
		_, err := ParseSchedule("at", "", 0, time.Time{}, "Europe/Rome")
		if !errors.Is(err, ErrMissingRunAt) {
			t.Errorf("want ErrMissingRunAt, got %v", err)
		}
	})
	t.Run("invalid tz rejected", func(t *testing.T) {
		_, err := ParseSchedule("cron", "30 9 * * *", 0, time.Time{}, "Mars/Olympus")
		if !errors.Is(err, ErrInvalidTimezone) {
			t.Errorf("want ErrInvalidTimezone, got %v", err)
		}
	})
	t.Run("unknown kind rejected", func(t *testing.T) {
		_, err := ParseSchedule("yearly", "", 0, time.Time{}, "Europe/Rome")
		if !errors.Is(err, ErrInvalidScheduleKind) {
			t.Errorf("want ErrInvalidScheduleKind, got %v", err)
		}
	})
	t.Run("empty tz defaults to Europe/Rome", func(t *testing.T) {
		spec, err := ParseSchedule("cron", "30 9 * * *", 0, time.Time{}, "")
		if err != nil {
			t.Fatalf("ParseSchedule empty tz: %v", err)
		}
		if spec.TZ != "Europe/Rome" {
			t.Errorf("default tz = %q, want Europe/Rome", spec.TZ)
		}
	})
}

// TestFirstFire asserts the schedule-time gate that prevents an unschedulable
// one-shot from being persisted (CR-02): a past `at` is rejected with ErrPastRunAt
// rather than yielding a zero (→ next_run_at=NULL) that DueTasks never selects. A
// future `at` and the recurring kinds return their first fire unchanged.
func TestFirstFire(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	t.Run("past at rejected with ErrPastRunAt", func(t *testing.T) {
		past := now.Add(-1 * time.Hour)
		spec, err := ParseSchedule("at", "", 0, past, "Europe/Rome")
		if err != nil {
			t.Fatalf("ParseSchedule at: %v", err)
		}
		next, err := FirstFire(spec, now)
		if !errors.Is(err, ErrPastRunAt) {
			t.Fatalf("FirstFire past at: err = %v, want ErrPastRunAt", err)
		}
		if !next.IsZero() {
			t.Errorf("FirstFire past at: next = %s, want zero (rejected)", next)
		}
	})

	t.Run("future at returns the stored instant", func(t *testing.T) {
		fire := now.Add(2 * time.Hour)
		spec, _ := ParseSchedule("at", "", 0, fire, "Europe/Rome")
		next, err := FirstFire(spec, now)
		if err != nil {
			t.Fatalf("FirstFire future at: %v", err)
		}
		if !next.Equal(fire) {
			t.Errorf("FirstFire future at: next = %s, want %s", next, fire)
		}
	})

	t.Run("every returns a future fire", func(t *testing.T) {
		spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
		next, err := FirstFire(spec, now)
		if err != nil {
			t.Fatalf("FirstFire every: %v", err)
		}
		if !next.After(now) {
			t.Errorf("FirstFire every: next = %s, want a future instant", next)
		}
	})
}
