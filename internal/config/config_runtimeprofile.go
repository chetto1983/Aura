// config_runtimeprofile.go holds the runtime deployment profile primitives
// (refactor-on-touch, CLAUDE.md ≤600 LOC NO GOD CLASS): the typed RuntimeProfile
// enum, its total ParseProfile, and the Strict() tier helper the per-profile
// validation re-parse pass (config_knobs.go, plan 03) keys on.
package config

import "strings"

// RuntimeProfile is the runtime deployment posture (D-01) read from AURA_PROFILE.
// It selects config-validation strictness and composition-root runtime posture,
// including MCP egress enforcement. Unset → dev (D-03).
type RuntimeProfile string

const (
	// ProfileDev is the default full-host posture (AURA_PROFILE unset → dev, D-03):
	// the loudest, most permissive tier, preserving today's behavior unchanged.
	ProfileDev RuntimeProfile = "dev"
	// ProfileLocalTrusted is a trusted single-machine deployment — lenient like dev.
	ProfileLocalTrusted RuntimeProfile = "local_trusted"
	// ProfileSingleUserHardened is a hardened single-operator deployment — strict.
	ProfileSingleUserHardened RuntimeProfile = "single_user_hardened"
	// ProfileServerProduction is a multi-reachable production deployment — strict.
	ProfileServerProduction RuntimeProfile = "server_production"
)

// ParseProfile maps AURA_PROFILE to a RuntimeProfile. It is total: any unknown or
// empty value resolves to ProfileDev (D-03) — never panics, never errors, and never
// silently selects a stricter tier the operator did not intend. Surrounding
// whitespace is trimmed before the match.
func ParseProfile(s string) RuntimeProfile {
	switch RuntimeProfile(strings.TrimSpace(s)) {
	case ProfileLocalTrusted:
		return ProfileLocalTrusted
	case ProfileSingleUserHardened:
		return ProfileSingleUserHardened
	case ProfileServerProduction:
		return ProfileServerProduction
	default:
		// dev plus any unknown/empty value (D-03).
		return ProfileDev
	}
}

// Strict reports whether the profile enforces the strict validation tier: true for
// single_user_hardened and server_production (a missing/sample credential is Fatal,
// D-07), false for dev and local_trusted (lenient — at most Warn, D-14). This single
// helper is the severity decision the plan-03 re-parse pass keys on.
func (p RuntimeProfile) Strict() bool {
	return p == ProfileSingleUserHardened || p == ProfileServerProduction
}
