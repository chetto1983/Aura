package usersandbox

import "testing"

// TestStopOptionsCarriesTheShortGrace pins the suspend grace period. It is a daemon-free test
// of daemon-gated code on purpose: Suspend itself only runs under docker_integration, which
// contributes ZERO coverage to the gate, so the value it depends on would otherwise ship
// unasserted (CLAUDE.md).
//
// The number is load-bearing, not cosmetic. Docker's default is 10s and a Suspend performs TWO
// stops (box + egress sidecar), so leaving Timeout nil cost ~20s of pure waiting per suspended
// box — waiting for a SIGTERM nothing in the box answers, because its PID 1 is a plain
// sleep-style command with no handler and PID 1 ignores signals it has not installed one for.
// Measured on a native-Linux daemon: 10.26s at the default against 2.22s here.
func TestStopOptionsCarriesTheShortGrace(t *testing.T) {
	opts := stopOptions()
	if opts.Timeout == nil {
		t.Fatal("Timeout must be set — a nil Timeout means Docker's 10s default, paid twice per Suspend")
	}
	if *opts.Timeout != suspendGraceSec {
		t.Fatalf("grace = %ds, want %ds", *opts.Timeout, suspendGraceSec)
	}
	if suspendGraceSec <= 0 {
		// 0 means "SIGKILL immediately" and a negative value means "wait forever" — both are
		// reachable by a careless edit of the constant and neither is what this path wants.
		t.Fatalf("suspendGraceSec = %d; 0 kills instantly and <0 waits forever", suspendGraceSec)
	}

	// Each call must hand out its OWN pointer: the two stops in a Suspend share this helper,
	// and a shared *int would let a future mutation of one silently retune the other.
	a, b := stopOptions(), stopOptions()
	if a.Timeout == b.Timeout {
		t.Error("stopOptions must not share one *int across calls")
	}
}
