package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestBackgroundJobTTLExpiry: a job past startedAt+ttl is reaped — the process ctx is
// cancelled (terminating the group) and the status is recorded "expired" (MUSR-04/D-17).
func TestBackgroundJobTTLExpiry(t *testing.T) {
	canceled := false
	bg := &BackgroundShells{shells: map[string]*bgShell{
		"job-ttl": {
			ownerID:   localOwnerID,
			sessionID: "sess-ttl",
			startedAt: time.Now().Add(-2 * time.Hour),
			ttl:       time.Hour,
			cancel:    func() { canceled = true },
			bufCap:    1024,
		},
	}}

	if reaped := bg.sweepExpired(time.Now()); reaped != 1 {
		t.Fatalf("sweepExpired reaped %d, want 1", reaped)
	}
	if !canceled {
		t.Fatal("TTL expiry must cancel the process ctx (terminate the process group)")
	}
	sh, _ := bg.get("job-ttl")
	if _, status := sh.snapshot(nil); status != "expired" {
		t.Fatalf("status = %q, want expired", status)
	}
	// finish() (real process exit after our cancel) must PRESERVE the expired status,
	// never overwrite it with an exit code.
	sh.finish(nil)
	if _, status := sh.snapshot(nil); status != "expired" {
		t.Fatalf("post-finish status = %q, want expired preserved", status)
	}
	// Idempotent: a job already expired is not re-reaped.
	if again := bg.sweepExpired(time.Now()); again != 0 {
		t.Fatalf("second sweep reaped %d, want 0 (idempotent)", again)
	}

	// A within-TTL job is left running.
	bg2 := &BackgroundShells{shells: map[string]*bgShell{
		"fresh": {startedAt: time.Now(), ttl: time.Hour, cancel: func() {}, bufCap: 1024},
	}}
	if reaped := bg2.sweepExpired(time.Now()); reaped != 0 {
		t.Fatalf("a within-TTL job was reaped (%d), want 0", reaped)
	}
}

// TestBackgroundJobAge: a poll on a running job exposes an age metric (now - startedAt)
// on both the Meta and the rendered footer (MUSR-04).
func TestBackgroundJobAge(t *testing.T) {
	started := time.Now().Add(-750 * time.Millisecond)
	bg := &BackgroundShells{shells: map[string]*bgShell{
		"job-age": {ownerID: localOwnerID, sessionID: "sess-age", startedAt: started, cancel: func() {}, bufCap: 1024},
	}}
	res, err := (&ShellPoll{Shells: bg}).Execute(ctxWith(t, "sess-age", "call-age"), mustJSON(t, map[string]string{"shell_id": "job-age"}))
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if res.Meta == nil {
		t.Fatal("poll Meta is nil")
	}
	age, ok := (*res.Meta)["age_ms"].(int64)
	if !ok {
		t.Fatalf("age_ms Meta = %v (%T), want int64", (*res.Meta)["age_ms"], (*res.Meta)["age_ms"])
	}
	if age < 700 {
		t.Fatalf("age_ms = %d, want >= ~750 (job started 750ms ago)", age)
	}
	if !strings.Contains(res.Preview, "age_ms") {
		t.Fatalf("poll footer missing the age_ms metric: %q", res.Preview)
	}

	// The bgShell.age helper is monotone in elapsed time.
	sh, _ := bg.get("job-age")
	if got := sh.age(started.Add(1250 * time.Millisecond)); got != 1250*time.Millisecond {
		t.Fatalf("age = %v, want 1.25s", got)
	}
}

// TestBackgroundJobTTLDefaultAndOverride: default TTL is 1h; AURA_SHELL_BG_TTL overrides
// it; an unparseable value falls back to the 1h safety bound (D-17).
func TestBackgroundJobTTLDefaultAndOverride(t *testing.T) {
	t.Setenv("AURA_SHELL_BG_TTL", "")
	if got := shellBackgroundTTL(); got != time.Hour {
		t.Fatalf("default TTL = %v, want 1h", got)
	}
	t.Setenv("AURA_SHELL_BG_TTL", "30m")
	if got := shellBackgroundTTL(); got != 30*time.Minute {
		t.Fatalf("override TTL = %v, want 30m", got)
	}
	t.Setenv("AURA_SHELL_BG_TTL", "not-a-duration")
	if got := shellBackgroundTTL(); got != time.Hour {
		t.Fatalf("invalid TTL should fall back to 1h, got %v", got)
	}
	t.Setenv("AURA_SHELL_BG_TTL", "0s")
	if got := shellBackgroundTTL(); got != time.Hour {
		t.Fatalf("non-positive TTL should fall back to 1h, got %v", got)
	}
}

// TestBackgroundJobReaperTicksAndReaps: the background reaper goroutine sweeps on its
// ticker and terminates an expired job; stopReaper joins it (goleak, via TestMain).
func TestBackgroundJobReaperTicksAndReaps(t *testing.T) {
	t.Setenv("AURA_SHELL_BG_TTL", "4s") // reaperInterval = 1s (ttl/4, clamped)
	bg := NewBackgroundShells(nil)
	reaped := make(chan struct{}, 1)
	bg.mu.Lock()
	bg.shells["job-runaway"] = &bgShell{
		ownerID:   localOwnerID,
		sessionID: "sess-runaway",
		startedAt: time.Now().Add(-2 * time.Hour),
		ttl:       time.Hour,
		cancel: func() {
			select {
			case reaped <- struct{}{}:
			default:
			}
		},
		bufCap: 1024,
	}
	bg.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bg.StartReaper(ctx)
	// stopReaper joins the ticker goroutine (goleak) without Shutdown's shell-drain,
	// which would otherwise wait out the timeout on this process-less fixture shell.
	defer bg.stopReaper()

	select {
	case <-reaped:
	case <-time.After(3 * time.Second):
		t.Fatal("reaper ticker did not reap the expired job within 3s")
	}
}
