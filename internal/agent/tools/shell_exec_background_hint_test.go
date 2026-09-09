package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestSlowRunNotice: a command that eats a large share of its cap gets a notice telling
// the model what to do NEXT time, while a quick one gets nothing.
//
// The hint lives with the result rather than in the system prompt for the reason the
// Working rules in shell_exec.go already give: the model reads it exactly when it is
// about to decide, not thousands of tokens earlier. Measured 2026-09-09 across every
// recorded turn: 28 tool-call turns, ZERO uses of background — while an artifact bundle
// took 50.3s of a 120s cap. Nothing was warning her before the cap became a wall.
func TestSlowRunNotice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		took     time.Duration
		cap      time.Duration
		wantHint bool
	}{
		{"quick command says nothing", 900 * time.Millisecond, 120 * time.Second, false},
		{"just under the share stays quiet", 29 * time.Second, 120 * time.Second, false},
		{"the measured bundle warns", 50300 * time.Millisecond, 120 * time.Second, true},
		{"near the cap warns", 119 * time.Second, 120 * time.Second, true},
		{"a short explicit cap scales with it", 3 * time.Second, 10 * time.Second, true},
		{"no cap, no arithmetic, no hint", 5 * time.Minute, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := slowRunNotice(tc.took, tc.cap)
			if tc.wantHint && got == "" {
				t.Fatalf("took %s of %s: expected a background hint, got none", tc.took, tc.cap)
			}
			if !tc.wantHint && got != "" {
				t.Fatalf("took %s of %s: expected silence, got %q", tc.took, tc.cap, got)
			}
			if tc.wantHint && !strings.Contains(got, "background") {
				t.Fatalf("the hint must name the parameter that fixes it: %q", got)
			}
		})
	}
}

// TestShellExecTimeoutIsActionable: hitting the cap must not be a dead end. The old
// message said only "[command timed out]", which tells the model nothing about the one
// parameter that would have avoided it — so the next attempt was the same command again.
func TestShellExecTimeoutIsActionable(t *testing.T) {
	be := &blockingBoxBackend{}
	tool := &ShellExec{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-sh-timeout-hint", "call-sh")

	raw, _ := json.Marshal(shellExecArgs{Command: "sleep 30", TimeoutMs: 50})
	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("a timeout is a normal result, not a Go error: %v", err)
	}
	if !strings.Contains(res.Preview, "[command timed out]") {
		t.Fatalf("the existing marker must survive: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "background") {
		t.Fatalf("a timeout must point at the parameter that avoids it: %q", res.Preview)
	}
}
