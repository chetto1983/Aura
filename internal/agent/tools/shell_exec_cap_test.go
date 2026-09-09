package tools

import (
	"testing"
	"time"
)

// TestDefaultShellTimeoutIsReactive: the implicit cap is short.
//
// It can be short only because reaching it no longer kills anything (shell_bg_promote.go):
// the job is handed back as a background shell and its completion wakes the conversation.
// Before promotion a 3s cap would have been destructive — it would have shredded every
// build. After it, the cap is just "stop blocking the operator", and an operator waiting
// two minutes for a prompt to come back is the failure being fixed.
func TestDefaultShellTimeoutIsReactive(t *testing.T) {
	if defaultShellTimeout > 5*time.Second {
		t.Fatalf("defaultShellTimeout = %s: an operator should not wait this long for the turn to breathe", defaultShellTimeout)
	}
}

// TestShellDefaultTimeoutFromEnv: deployments tune the cap without a rebuild, and a
// malformed or absent value falls back to the compiled default rather than to zero.
func TestShellDefaultTimeoutFromEnv(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset uses the compiled default", "", defaultShellTimeout},
		{"explicit milliseconds", "12000", 12 * time.Second},
		{"garbage falls back", "soon", defaultShellTimeout},
		{"zero falls back", "0", defaultShellTimeout},
		{"negative falls back", "-5", defaultShellTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envShellDefaultTimeoutMs, tc.env)
			if got := shellDefaultTimeout(); got != tc.want {
				t.Fatalf("shellDefaultTimeout() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestEffectiveShellTimeoutHonorsExplicitRequest: a caller that knows the job is worth
// waiting for can still block on it — the short cap is a default, not a ceiling.
func TestEffectiveShellTimeoutHonorsExplicitRequest(t *testing.T) {
	if got := effectiveShellTimeout(0, 30_000); got != 30*time.Second {
		t.Fatalf("explicit timeout_ms ignored: got %s", got)
	}
}
