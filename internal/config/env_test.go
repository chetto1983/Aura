package config

import (
	"testing"
	"time"
)

// TestGetEnvDurationValid verifies a well-formed duration is parsed and returned.
func TestGetEnvDurationValid(t *testing.T) {
	t.Setenv("AURA_TEST_DURATION", "45s")
	got := getEnvDuration("AURA_TEST_DURATION", 30*time.Second)
	if got != 45*time.Second {
		t.Errorf("got %v, want 45s", got)
	}
}

// TestGetEnvDurationEmptyReturnsFallback verifies that an unset env var falls
// back to the default. (Go test framework's t.Setenv requires a value, so we
// explicitly do not set it here; relying on the test process not having it.)
func TestGetEnvDurationEmptyReturnsFallback(t *testing.T) {
	const key = "AURA_TEST_DURATION_UNSET_LIKELY"
	got := getEnvDuration(key, 30*time.Second)
	if got != 30*time.Second {
		t.Errorf("got %v, want fallback 30s", got)
	}
}

// TestGetEnvDurationGarbageReturnsFallback verifies that an unparseable
// duration string falls back to the default (and logs a warning).
// WR-02: previously this path was silent.
func TestGetEnvDurationGarbageReturnsFallback(t *testing.T) {
	t.Setenv("AURA_TEST_DURATION", "garbage")
	got := getEnvDuration("AURA_TEST_DURATION", 30*time.Second)
	if got != 30*time.Second {
		t.Errorf("got %v, want fallback 30s on unparseable input", got)
	}
}

// TestGetEnvDurationNegativeReturnsFallback verifies that a negative duration
// is rejected and the fallback is used (with a warning logged).
// WR-02: previously this path was silent.
func TestGetEnvDurationNegativeReturnsFallback(t *testing.T) {
	t.Setenv("AURA_TEST_DURATION", "-30s")
	got := getEnvDuration("AURA_TEST_DURATION", 30*time.Second)
	if got != 30*time.Second {
		t.Errorf("got %v, want fallback 30s on negative input", got)
	}
}

// TestGetEnvDurationZero verifies that the explicit zero "0s" is accepted as a
// valid (non-negative) duration. Operators may use this to disable a feature.
func TestGetEnvDurationZero(t *testing.T) {
	t.Setenv("AURA_TEST_DURATION", "0s")
	got := getEnvDuration("AURA_TEST_DURATION", 30*time.Second)
	if got != 0 {
		t.Errorf("got %v, want 0s when env is explicitly zero", got)
	}
}
