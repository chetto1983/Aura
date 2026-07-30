// Package envutil holds the silent-fallback env-var parsing leaf shared across the
// config root and the per-subsystem configs (channels, telegram). Each accessor
// absorbs a malformed/unset value to its fallback rather than booting fatal — a typo
// in an ad-hoc env tweak should never block startup; the REQUIRED secrets fail fast in
// their own Validate paths instead. Before this extraction the IntDefault/BoolDefault
// logic was copied across internal/config, internal/channels, and
// internal/channels/telegram (QUAL-03, D-06 leaf extraction).
//
// Scope is deliberately minimal (IntDefault + BoolDefault + FloatDefault): the string/slice
// variants have no cross-package copy to fold, and pulling agent-tool knob reads to
// construction time is QUAL-04 (a later phase), not this leaf.
package envutil

import (
	"os"
	"strconv"
)

// IntDefault returns the integer value of key, falling back to fallback when the
// variable is unset, empty, or fails to parse as an int. Parse failures are silently
// absorbed — the fallback beats a fatal boot on a misformatted ad-hoc env tweak.
func IntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// BoolDefault returns the boolean value of key, falling back to fallback when the
// variable is unset, empty, or fails to parse. Like IntDefault, a malformed value is
// silently absorbed to the fallback rather than booting fatal.
func BoolDefault(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// FloatDefault returns the float64 value of key, falling back to fallback when the
// variable is unset, empty, or fails to parse. It exists for relevance thresholds,
// whose useful range is fractional and whose scale is model-specific — a rerank score
// lives in [0,1] and is not linear, so the value belongs in an env knob measured per
// deployment rather than a constant. Malformed values are absorbed like the others: a
// typo in a threshold must not stop the process from booting.
func FloatDefault(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}
