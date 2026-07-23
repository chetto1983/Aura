package agui

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// runregistry_test.go pins the RunRegistry contract (fix-plan 1.3 Tier B RS-02,
// design §2.5/§1.3/§7.2 row 2): max-live refusal, byThread removal at terminal
// (linger applies to byRun only), reaper evicting only terminal+expired sessions,
// and Close cancelling sessions + joining the reaper (goleak via the package
// TestMain plus explicit per-test verification).

// testRegistryConfig uses a millisecond linger so the reaper's tick (linger/2)
// fires fast enough for a bounded poll, and a maxLive of 2 so the refusal path is
// reachable with three Starts.
func testRegistryConfig() runRegistryConfig {
	return runRegistryConfig{
		ringCap:      16,
		subBuffer:    4,
		maxLive:      2,
		linger:       30 * time.Millisecond,
		maxWallclock: time.Minute,
	}
}

// TestNewRunRegistryFromServerConfig: the exported constructor resolves the
// ServerConfig ints into registry bounds (seconds → Durations, BufferCap →
// subscriber headroom) — the SSEHeartbeatSec-style resolution seam (RS-03).
func TestNewRunRegistryFromServerConfig(t *testing.T) {
	r := NewRunRegistry(ServerConfig{
		BufferCap:          8,
		RunBufferEvents:    128,
		RunLingerSec:       60,
		RunMaxWallclockSec: 900,
		RunMaxLive:         3,
	})
	defer r.Close()
	if r.cfg.ringCap != 128 || r.cfg.subBuffer != 8 || r.cfg.maxLive != 3 {
		t.Fatalf("resolved caps = %+v, want ringCap 128 / subBuffer 8 / maxLive 3", r.cfg)
	}
	if r.cfg.linger != 60*time.Second || r.cfg.maxWallclock != 900*time.Second {
		t.Fatalf("resolved durations = %+v, want linger 60s / maxWallclock 900s", r.cfg)
	}

	zero := NewRunRegistry(ServerConfig{})
	defer zero.Close()
	if zero.cfg.ringCap != defaultRunBufferEvents || zero.cfg.subBuffer != fanoutBuffer ||
		zero.cfg.maxLive != defaultRunMaxLive || zero.cfg.linger != defaultRunLinger ||
		zero.cfg.maxWallclock != defaultRunMaxWallclock {
		t.Fatalf("zero ServerConfig must resolve to the design defaults, got %+v", zero.cfg)
	}
}

// TestRunRegistryDefaults: every non-positive knob falls back to its design
// default (amendment #90 point 9) rather than disabling a bound.
func TestRunRegistryDefaults(t *testing.T) {
	r := newRunRegistry(runRegistryConfig{})
	defer r.Close()
	if r.cfg.ringCap != defaultRunBufferEvents {
		t.Errorf("ringCap default = %d, want %d", r.cfg.ringCap, defaultRunBufferEvents)
	}
	if r.cfg.subBuffer != fanoutBuffer {
		t.Errorf("subBuffer default = %d, want %d", r.cfg.subBuffer, fanoutBuffer)
	}
	if r.cfg.maxLive != defaultRunMaxLive {
		t.Errorf("maxLive default = %d, want %d", r.cfg.maxLive, defaultRunMaxLive)
	}
	if r.cfg.linger != defaultRunLinger {
		t.Errorf("linger default = %v, want %v", r.cfg.linger, defaultRunLinger)
	}
	if r.cfg.maxWallclock != defaultRunMaxWallclock {
		t.Errorf("maxWallclock default = %v, want %v", r.cfg.maxWallclock, defaultRunMaxWallclock)
	}
}

// TestRunRegistryMaxLiveRefusal: Start past the live cap returns the sentinel
// (RS-04's 503 seam); a terminal session frees its live slot immediately even
// though it keeps lingering in byRun for replay.
func TestRunRegistryMaxLiveRefusal(t *testing.T) {
	defer goleak.VerifyNone(t)
	cfg := testRegistryConfig()
	cfg.linger = time.Hour // keep the reaper out of this test's window
	r := newRunRegistry(cfg)
	defer r.Close()

	if _, err := r.Start(runParams{runID: "run-a", threadID: "t-a", identityID: "id"}); err != nil {
		t.Fatalf("Start a: %v", err)
	}
	b, err := r.Start(runParams{runID: "run-b", threadID: "t-b", identityID: "id"})
	if err != nil {
		t.Fatalf("Start b: %v", err)
	}
	if _, err := r.Start(runParams{runID: "run-c", threadID: "t-c", identityID: "id"}); !errors.Is(err, errRunRegistryFull) {
		t.Fatalf("Start past maxLive: err=%v, want errRunRegistryFull", err)
	}

	b.finish()
	if _, err := r.Start(runParams{runID: "run-c", threadID: "t-c", identityID: "id"}); err != nil {
		t.Fatalf("Start after a slot freed at terminal: %v", err)
	}
	if _, ok := r.Get("run-b"); !ok {
		t.Fatal("terminal run-b evicted from byRun before its linger expired")
	}
}

// TestRunRegistryByThreadRemovedAtTerminal: the live-run discovery index drops the
// session at finish (via the cleanup chain, which also fires the thread unlock
// exactly once), while Get keeps serving the lingering session; the (identity,
// thread) key is composite so identities never share a slot (D-23).
func TestRunRegistryByThreadRemovedAtTerminal(t *testing.T) {
	defer goleak.VerifyNone(t)
	cfg := testRegistryConfig()
	cfg.linger = time.Hour
	r := newRunRegistry(cfg)
	defer r.Close()

	var unlocks atomic.Int32
	sess, err := r.Start(runParams{
		runID:      "run-1",
		threadID:   "t-1",
		identityID: "id-1",
		unlock:     func() { unlocks.Add(1) },
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got, ok := r.LiveForThread("id-1", "t-1"); !ok || got != sess {
		t.Fatal("LiveForThread missed the live session")
	}
	if _, ok := r.LiveForThread("id-2", "t-1"); ok {
		t.Fatal("LiveForThread leaked a session across identities (composite key)")
	}

	sess.finish()
	if got := unlocks.Load(); got != 1 {
		t.Fatalf("unlock ran %d times at terminal, want 1", got)
	}
	if _, ok := r.LiveForThread("id-1", "t-1"); ok {
		t.Fatal("byThread entry survived terminal")
	}
	if _, ok := r.Get("run-1"); !ok {
		t.Fatal("byRun entry gone before linger expiry")
	}
	sess.finish()
	if got := unlocks.Load(); got != 1 {
		t.Fatalf("double finish reran unlock: %d", got)
	}
}

// TestRunRegistryReaperEvictsOnlyTerminalExpired: with a 30ms linger the reaper
// (tick 15ms) evicts the finished session within the poll window while the
// non-terminal one — long past any linger multiple by then — is never reaped.
func TestRunRegistryReaperEvictsOnlyTerminalExpired(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRunRegistry(testRegistryConfig())
	defer r.Close()

	if _, err := r.Start(runParams{runID: "run-live", threadID: "t-live", identityID: "id"}); err != nil {
		t.Fatalf("Start live: %v", err)
	}
	done, err := r.Start(runParams{runID: "run-done", threadID: "t-done", identityID: "id"})
	if err != nil {
		t.Fatalf("Start done: %v", err)
	}
	done.finish()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := r.Get("run-done"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("terminal+expired session not reaped within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, ok := r.Get("run-live"); !ok {
		t.Fatal("non-terminal session was reaped; only terminal+expired may be evicted")
	}
}

// TestRunRegistryCloseJoinsReaperAndCancelsSessions: Close fires every session's
// cancel handle (§1.1 shutdown bound), refuses further Starts, joins the reaper
// (the deferred goleak proves the join), and is idempotent.
func TestRunRegistryCloseJoinsReaperAndCancelsSessions(t *testing.T) {
	defer goleak.VerifyNone(t)
	cfg := testRegistryConfig()
	cfg.linger = time.Hour
	r := newRunRegistry(cfg)

	var cancelled atomic.Bool
	if _, err := r.Start(runParams{
		runID:      "run-1",
		threadID:   "t-1",
		identityID: "id-1",
		cancel:     func() { cancelled.Store(true) },
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	r.Close()
	if !cancelled.Load() {
		t.Fatal("Close did not cancel the registered session")
	}
	if _, err := r.Start(runParams{runID: "run-2", threadID: "t-2", identityID: "id-1"}); !errors.Is(err, errRunRegistryClosed) {
		t.Fatalf("Start after Close: err=%v, want errRunRegistryClosed", err)
	}
	r.Close() // idempotent — re-joins the already-stopped reaper, no panic
}

// TestSetRunRegistrySeam pins the narrow-seam convention (SetApprovalStore parity):
// a fresh Server carries a nil registry (flag off) until the composition root wires
// one.
func TestSetRunRegistrySeam(t *testing.T) {
	srv := NewServer(nil, nil, ServerConfig{})
	if srv.runs != nil {
		t.Fatal("fresh Server must carry a nil RunRegistry (flag off)")
	}
	r := newRunRegistry(testRegistryConfig())
	defer r.Close()
	srv.SetRunRegistry(r)
	if srv.runs != r {
		t.Fatal("SetRunRegistry did not wire the registry")
	}
}
