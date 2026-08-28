// Tests for swarm_depth.go's depth-cap machinery: checkDepth's boundary (D-10),
// and plan 51-05's workerRegistry/canNest/nestingClosedNotice (SWARM-04/SWARM-05
// depth-bounded nesting). Split out of swarm_test.go on touch (CLAUDE.md's 600-LOC
// cap) so the depth concern lives beside the production file it exercises,
// swarm_depth.go, exactly as swarm_test.go pairs with swarm.go.
package swarm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestSwarmDepthGuard (D-10/SC#2): a synthetic depth >= AURA_SWARM_MAX_DEPTH is
// rejected with the PRD literal and no worker spawns.
func TestSwarmDepthGuard(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().route("x", outcome{kind: "ok", text: "should not run"})
	rc := testRunConfig(t, r, 25)
	rc.Depth = defaultMaxDepth // == cap → rejected

	out, err := Run(context.Background(), rc, []string{"x1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := fmt.Sprintf("MAX_SPAWN_DEPTH=%d exceeded", defaultMaxDepth)
	if !strings.Contains(out, want) {
		t.Errorf("depth-guard output = %q, want it to contain %q", out, want)
	}
	r.mu.Lock()
	total := 0
	for _, c := range r.calls {
		total += c
	}
	r.mu.Unlock()
	if total != 0 {
		t.Errorf("depth guard rejected but %d worker calls fired (want 0)", total)
	}
}

// TestCheckDepth covers the guard helper directly across the boundary.
func TestCheckDepth(t *testing.T) {
	if _, ok := checkDepth(1, 2); !ok {
		t.Error("depth 1 < cap 2 should be allowed")
	}
	if msg, ok := checkDepth(2, 2); ok || !strings.Contains(msg, "MAX_SPAWN_DEPTH=2 exceeded") {
		t.Errorf("depth 2 >= cap 2 should be rejected with the PRD literal, got (%q,%v)", msg, ok)
	}
}

// TestNestedDelegationSynchronous (SWARM-04): a worker-issued (depth>1) delegation
// runs the SAME synchronous runWave path a top-level call does -- it NEVER takes
// the plan-51-01 background branch, even with an Enqueuer configured, and Run does
// not return before every child has actually reported. The scripted worker delay
// is the "fake child runner that records completion times": Run() returning in
// less than that delay would mean it took the background shortcut instead of
// waiting on the real synchronous wave.
func TestNestedDelegationSynchronous(t *testing.T) {
	defer goleak.VerifyNone(t)
	t.Setenv("AURA_SWARM_MAX_DEPTH", "3") // depth 2 must still clear preflight (2 < 3)

	const workDelay = 30 * time.Millisecond
	r := newRouter().
		route("alpha", outcome{kind: "ok", text: "A done", delay: workDelay}).
		route("bravo", outcome{kind: "ok", text: "B done", delay: workDelay})
	rc := testRunConfig(t, r, 25)
	rc.Depth = 2 // a nested, worker-issued spawn (SWARM-04) -- NOT the top-level 1
	store := &fakeDelegationStore{}
	rc.Enqueuer = &DelegationEnqueuer{Store: store} // configured -- must be ignored at this depth

	started := time.Now()
	out, err := Run(context.Background(), rc, []string{"alpha task", "bravo task"})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed < workDelay {
		t.Errorf("Run returned after %v, want it to block for at least the scripted worker delay %v -- "+
			"a nested delegation must complete inside its own turn, not background (SWARM-04)", elapsed, workDelay)
	}
	reports := parseReports(t, out)
	if len(reports) != 2 || reports[0].Status != StatusOK || reports[1].Status != StatusOK {
		t.Fatalf("both nested workers should finish ok: %+v", reports)
	}
	if len(store.created) != 0 {
		t.Errorf("nested (depth>1) delegation enqueued %d durable rows, want 0 -- "+
			"SWARM-04 forbids backgrounding a worker-issued delegation", len(store.created))
	}
}

// TestNestedDelegationFailureIsolation (SWARM-04 edge, explicit): a nested
// (depth>1) delegation whose child errors still returns the failure INSIDE the
// worker's own turn -- Run returns a valid reports JSON carrying a {failed} slot,
// never a Go error, so the parent sees a completed worker report, not a missing
// one.
func TestNestedDelegationFailureIsolation(t *testing.T) {
	defer goleak.VerifyNone(t)
	t.Setenv("AURA_SWARM_MAX_DEPTH", "3")
	r := newRouter().
		route("alpha", outcome{kind: "ok", text: "A done"}).
		route("bravo", outcome{kind: "fail"})
	rc := testRunConfig(t, r, 25)
	rc.Depth = 2 // nested, worker-issued

	out, err := Run(context.Background(), rc, []string{"alpha task", "bravo task"})
	if err != nil {
		t.Fatalf("Run: %v, want the child failure to ride inside the reports JSON, not a Go error", err)
	}
	reports := parseReports(t, out)
	if len(reports) != 2 {
		t.Fatalf("want 2 reports, got %d", len(reports))
	}
	if reports[0].Status != StatusOK {
		t.Errorf("report[0].Status = %q, want ok", reports[0].Status)
	}
	if reports[1].Status != StatusFailed || reports[1].Error == "" {
		t.Errorf("report[1] = %+v, want a failed status carrying the error text", reports[1])
	}
}

// TestSwarmDepthGuardAtCapIsModelReadable (SWARM-05): at the default
// AURA_SWARM_MAX_DEPTH (2), a depth-1 worker's own would-be nested spawn would
// land at depth 2 -- the cap -- so workerRegistry withholds swarm_spawn AND the
// worker reads the nesting-closed notice in its own brief (hermes' degrade-to-leaf
// framing) instead of discovering the limit by calling an absent tool. The router
// only matches the notice text if the worker's REAL outgoing brief carried it (the
// goal text itself never contains it), mirroring
// TestRunnerAdapterThreadsContextToWorkerBrief's marker-in-context proof.
func TestSwarmDepthGuardAtCapIsModelReadable(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().route(nestingClosedNotice, outcome{kind: "ok", text: "ack"})
	rc := testRunConfig(t, r, 25) // Depth: 1, default cap 2 -> at cap for its own children

	out, err := Run(context.Background(), rc, []string{"a goal with no nesting text of its own"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := parseReports(t, out)
	// A goal the router does NOT match also completes {ok, ""} (routerClient's
	// unknown-goal fallback), so the assertion must be the matched outcome's OWN
	// text ("ack") -- Status alone would pass even if the notice never made it
	// into the brief.
	if len(reports) != 1 || reports[0].Status != StatusOK || reports[0].Summary != "ack" {
		t.Fatalf("worker's brief did not carry the nesting-closed notice (the router only replies %q when it matches the notice text): %+v (%s)", "ack", reports, out)
	}
}

// TestWorkerRegistryDepthBoundary (SWARM-05 edge, explicit): with the cap raised
// to 3, a worker built at depth 1 (its own children would land at depth 2 ==
// cap-1) is granted swarm_spawn, but a worker built at depth 2 (its own children
// would land at depth 3 == cap) is not -- depth is counted from the top-level
// call, and the boundary sits exactly at cap-1/cap. checkDepth is asserted at both
// boundaries too (the second line of defence T-51-18 relies on).
func TestWorkerRegistryDepthBoundary(t *testing.T) {
	t.Setenv("AURA_SWARM_MAX_DEPTH", "3")
	rc := testRunConfig(t, newRouter(), 25) // ParentRegistry carries the stub swarm_spawn

	rc.Depth = 1 // this worker's OWN nested call would land at depth 2 == cap-1
	reg, closed := workerRegistry(rc)
	if _, ok := reg.Get(swarmSpawnTool); !ok {
		t.Error("depth-1 worker (child would land at cap-1) should be granted swarm_spawn")
	}
	if closed {
		t.Error("depth-1 worker should not see the nesting-closed notice")
	}

	rc.Depth = 2 // this worker's OWN nested call would land at depth 3 == cap
	reg, closed = workerRegistry(rc)
	if _, ok := reg.Get(swarmSpawnTool); ok {
		t.Error("depth-2 worker (child would land at cap) should NOT be granted swarm_spawn")
	}
	if !closed {
		t.Error("depth-2 worker should see the nesting-closed notice")
	}

	if _, ok := checkDepth(2, 3); !ok {
		t.Error("checkDepth: a call AT depth 2 (cap-1) must still be allowed")
	}
	if _, ok := checkDepth(3, 3); ok {
		t.Error("checkDepth: a call AT depth 3 (cap) must be rejected")
	}
}
