package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The bug this closes, stated as a test: the model was handed 15:49:04Z, called Rome
// "+1 ora" (August is +2) and answered with the UTC time unchanged. A rendered local clock
// removes the arithmetic instead of hoping the model gets it right.
func TestFormatClockStatesTheOffsetAndTheZone(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata in this environment")
	}
	august := time.Date(2026, 8, 16, 15, 49, 4, 0, time.UTC)

	got := FormatClock(august, rome)

	if !strings.HasPrefix(got, "2026-08-16T17:49:04+02:00") {
		t.Fatalf("FormatClock = %q, want the instant converted to CEST", got)
	}
	for _, want := range []string{"Europe/Rome", "CEST"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatClock = %q, missing %q: the model must not infer the season", got, want)
		}
	}

	// Winter is the same instant in a different offset; the zone name says which.
	january := time.Date(2026, 1, 16, 15, 49, 4, 0, time.UTC)
	if winter := FormatClock(january, rome); !strings.Contains(winter, "+01:00") || !strings.Contains(winter, "CET") {
		t.Fatalf("winter = %q, want +01:00 CET", winter)
	}
}

// UTC keeps the bare RFC-3339 form: appending "(UTC, UTC)" would be noise.
func TestFormatClockLeavesUTCBare(t *testing.T) {
	got := FormatClock(time.Date(2026, 8, 16, 15, 49, 4, 0, time.UTC), time.UTC)
	if got != "2026-08-16T15:49:04Z" {
		t.Fatalf("FormatClock = %q", got)
	}
}

// A typo in AURA_TIMEZONE must not take the daemon down over a display detail.
func TestLocationOrUTCFallsBackInsteadOfFailing(t *testing.T) {
	if loc := LocationOrUTC("Not/AZone"); loc != time.UTC {
		t.Fatalf("LocationOrUTC = %v, want UTC", loc)
	}
	if loc := LocationOrUTC(""); loc != time.Local {
		t.Fatalf("empty zone = %v, want the process zone", loc)
	}
}

// An argument-free call answers in the operator's zone -- that is the whole fix.
func TestCurrentTimeDefaultsToTheOperatorZone(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata in this environment")
	}
	res, err := CurrentTime{Location: rome}.Execute(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Preview, "Europe/Rome") {
		t.Fatalf("preview = %q, want the operator's zone", res.Preview)
	}
}

// Naming a different zone is a real question about elsewhere: an unknown name must be an
// error, not silently answered with the operator's own clock.
func TestCurrentTimeRejectsAnUnknownNamedZone(t *testing.T) {
	if _, err := (CurrentTime{Location: time.UTC}).Execute(
		t.Context(), json.RawMessage(`{"timezone":"Not/AZone"}`)); err == nil {
		t.Fatal("an invalid timezone was accepted")
	}
}
