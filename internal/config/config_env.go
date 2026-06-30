// config_env.go holds the env-var parsing helpers split out of config.go
// (refactor-on-touch, CLAUDE.md ≤600 LOC NO GOD CLASS): the typed envDefault /
// envSliceDefault accessors loadBase composes the Config from (the int/bool defaults
// now live in the shared internal/envutil leaf, QUAL-03). Each absorbs a
// malformed/unset value to its fallback rather than booting
// fatal — a typo in an ad-hoc env tweak should never block startup (the REQUIRED secrets
// are fail-fast in Validate / llm.Load instead).
package config

import (
	"os"
	"strings"
)

// envDefault returns the value of `key` from the environment, falling back to
// `fallback` when the variable is unset or empty.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envSliceDefault returns the comma-separated value of `key` split into a
// trimmed, empty-dropped slice, falling back to `fallback` when the variable is
// unset or empty. A set-but-all-empty value (e.g. ",,") yields an empty slice —
// an operator can deliberately clear the blocklist that way.
func envSliceDefault(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
