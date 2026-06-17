// config_env.go holds the env-var parsing helpers split out of config.go
// (refactor-on-touch, CLAUDE.md ≤600 LOC NO GOD CLASS): the typed envDefault /
// envIntDefault / envBoolDefault / envSliceDefault accessors loadBase composes the
// Config from. Each absorbs a malformed/unset value to its fallback rather than booting
// fatal — a typo in an ad-hoc env tweak should never block startup (the REQUIRED secrets
// are fail-fast in Validate / llm.Load instead).
package config

import (
	"os"
	"strconv"
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

// envIntDefault returns the integer value of `key`, falling back to `fallback`
// when the variable is unset, empty, or fails to parse as an int. Parsing
// failures are silently absorbed — the fallback is preferable to a fatal boot
// error on a misformatted ad-hoc env tweak.
func envIntDefault(key string, fallback int) int {
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

// envBoolDefault returns the boolean value of `key`, falling back to `fallback`
// when the variable is unset, empty, or fails to parse. Like envIntDefault, a
// malformed value is silently absorbed to the fallback rather than booting fatal
// — a typo in an opt-in toggle should not block startup.
func envBoolDefault(key string, fallback bool) bool {
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
