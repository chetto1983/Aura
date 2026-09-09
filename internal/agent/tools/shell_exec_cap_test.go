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
	if defaultShellTimeout > 30*time.Second {
		t.Fatalf("defaultShellTimeout = %s: an operator should not wait this long for the turn to breathe", defaultShellTimeout)
	}
}

// TestEffectiveShellTimeoutHonorsExplicitRequest: a caller that knows the job is worth
// waiting for can still block on it — the short cap is a default, not a ceiling.
func TestEffectiveShellTimeoutHonorsExplicitRequest(t *testing.T) {
	if got := effectiveShellTimeout(0, 30_000); got != 30*time.Second {
		t.Fatalf("explicit timeout_ms ignored: got %s", got)
	}
}
