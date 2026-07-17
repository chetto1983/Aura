// expiry.go implements the D-04 pure expiry math for a new share link: a
// symbolic ExpiryOption resolved against an injected clock and operator cap.
// This file is deliberately I/O-free and clock-free — it takes now and
// capDays as parameters rather than reading the wall clock or the
// AURA_SHARE_MAX_EXPIRY_DAYS environment knob itself, because a function
// that reaches for ambient global state internally is not deterministically
// testable, and the caller (the share service) already owns the transaction
// clock and the config surface (internal/config/config_share.go's
// ShareConfig.MaxExpiryDays).
//
// This file does NOT add the later expiry-sweep/reaper seam — that belongs
// to a downstream plan that has the store dependency this one deliberately
// lacks. Note also that the value computed here is only what gets WRITTEN
// at mint time: the lazy-resolve predicate that later decides whether a
// link is still live compares against the DB clock, not this value, so a
// skewed application clock can never resurrect an expired link (OQ3).
package share

import (
	"errors"
	"time"
)

// ExpiryOption is the D-04 symbolic expiry choice an owner selects when
// creating a share link: a fixed preset (1/7/30 days) or a custom day
// count. It is a string so it round-trips cleanly through JSON/form input
// with no custom (un)marshaler.
type ExpiryOption string

const (
	// ExpiryDefault requests the D-04 default of 7 days. It is also the
	// zero value "" — an absent/empty option must resolve to the default,
	// never to an error and never (silently) to ExpiryCustom.
	ExpiryDefault ExpiryOption = ""
	// Expiry1Day requests a 1-day expiry.
	Expiry1Day ExpiryOption = "1d"
	// Expiry7Days requests a 7-day expiry (the same value as ExpiryDefault).
	Expiry7Days ExpiryOption = "7d"
	// Expiry30Days requests a 30-day expiry.
	Expiry30Days ExpiryOption = "30d"
	// ExpiryCustom requests an operator-supplied day count, subject to the
	// capDays clamp and the non-positive rejection below.
	ExpiryCustom ExpiryOption = "custom"
)

// ErrNonPositiveCustomExpiry is returned when ExpiryCustom is requested with
// a non-positive day count. A zero/negative expiry is not an operator
// intent — it would mint an already-dead link — so it is rejected outright
// rather than clamped the way an over-cap value is (T-37F-27).
var ErrNonPositiveCustomExpiry = errors.New("share: custom expiry days must be positive")

// ResolveExpiry computes the mint-time expires_at for a new share link from
// a symbolic ExpiryOption, given the caller-owned clock (now) and the
// operator's AURA_SHARE_MAX_EXPIRY_DAYS cap (capDays). A custom value above
// capDays clamps to capDays (friendlier than a hard reject, and still
// fail-closed: the link can never outlive the cap). A custom value <= 0 is
// rejected: it is not an operator intent, it would mint an already-dead
// link. An unrecognized ExpiryOption fails closed with an error rather than
// silently defaulting.
//
// RED-phase stub (Task 3): replaced with the real switch below in this
// task's GREEN commit.
func ResolveExpiry(_ ExpiryOption, _ int, _ time.Time, _ int) (time.Time, error) {
	return time.Time{}, nil
}
