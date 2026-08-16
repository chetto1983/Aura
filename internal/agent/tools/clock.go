package tools

import "time"

// The zone every clock the model reads is rendered in.
//
// It exists because a UTC clock is not a neutral default: it hands the model an arithmetic
// problem, and the model gets it wrong. Measured on the live deployment 2026-08-16 — the
// agent read `<current_time>2026-08-16T15:49:04Z`, reasoned "Europe/Rome (+1 ora)" (it is
// +2 in August, CEST) and then answered with the UTC time unconverted. Two errors, one
// cause: nobody had told it where it lives.
//
// A rendered local time removes the arithmetic entirely, which is the only fix that
// survives a model swap.

// LocationOrUTC resolves an IANA name to a location, falling back to UTC.
//
// An unknown zone must not fail a boot: a typo in AURA_TIMEZONE would take the whole daemon
// down over a display detail, and UTC labelled as UTC is honest — wrong only in the way it
// was already wrong.
func LocationOrUTC(name string) *time.Location {
	if name == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// FormatClock renders an instant the way the model should read it: the offset from RFC-3339
// plus the zone's own name, so "+02:00 CEST" states the daylight-saving fact instead of
// leaving it to be inferred from the month.
func FormatClock(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	local := t.In(loc)
	zone, _ := local.Zone()
	formatted := local.Format(time.RFC3339)
	if zone == "" || zone == "UTC" {
		return formatted
	}
	// A fixed zone names itself after its abbreviation, so "(CEST, CEST)" is what a naive
	// join produces. Say it once.
	if loc.String() == zone {
		return formatted + " (" + zone + ")"
	}
	return formatted + " (" + loc.String() + ", " + zone + ")"
}
