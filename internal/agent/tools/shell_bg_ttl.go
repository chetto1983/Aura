package tools

import (
	"context"
	"os"
	"strings"
	"time"
)

// Background-job TTL (MUSR-04 / D-17). A background shell with no time bound is a DoS
// footgun (a runaway build/server never reaped). Every job carries a default 1h TTL
// (env-overridable via AURA_SHELL_BG_TTL); on expiry the reaper terminates the whole
// process group and records status "expired". The sweep runs both opportunistically
// (on every start) and from a bounded background ticker (StartReaper), so an idle
// daemon still bounds a runaway. The ticker is goleak-safe: it is started only by the
// composition root and joined by Shutdown (once-guarded stop mirroring the drain).

const (
	envShellBackgroundTTL     = "AURA_SHELL_BG_TTL"
	defaultShellBackgroundTTL = time.Hour
	// reaper sweep cadence is derived from the TTL and clamped to this window: a long
	// TTL does not need frequent checks, a short override is still swept promptly.
	maxReaperInterval     = time.Minute
	minReaperInterval     = time.Second
	reaperStopJoinTimeout = 5 * time.Second
)

// shellBackgroundTTL resolves the default per-job TTL: AURA_SHELL_BG_TTL parsed as a
// Go duration ("1h", "30m", "90s"), else the 1h default (D-17). A missing, non-positive,
// or unparseable value falls back to the default — the TTL is an always-on safety bound
// (MUSR-04), not disable-able via env (mirrors the buf/max env helpers).
func shellBackgroundTTL() time.Duration {
	v := strings.TrimSpace(os.Getenv(envShellBackgroundTTL))
	if v == "" {
		return defaultShellBackgroundTTL
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return defaultShellBackgroundTTL
	}
	return d
}

// reaperIntervalFor derives the background sweep cadence from the TTL (ttl/4, clamped
// to [minReaperInterval, maxReaperInterval]). A non-positive TTL disables the reaper.
func reaperIntervalFor(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 0
	}
	return max(min(ttl/4, maxReaperInterval), minReaperInterval)
}

// age is the wall-clock time since the job started — the shell_poll age metric (MUSR-04).
// startedAt is immutable after start, so this is lock-free.
func (s *bgShell) age(now time.Time) time.Duration {
	return now.Sub(s.startedAt)
}

// sweepExpired terminates every job past its TTL and returns the number reaped. It is
// the deterministic core the ticker and the opportunistic start-path both call.
func (b *BackgroundShells) sweepExpired(now time.Time) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sweepExpiredLocked(now)
}

// sweepExpiredLocked marks each live job past startedAt+ttl as expired (status
// "expired", no exit code) and cancels its process ctx — which fires
// cmd.Cancel = killProcessGroup, terminating the whole group (D-17). The per-job
// reaper goroutine then observes the exit; finish() preserves the "expired" status.
// Caller holds b.mu. cancel() is non-blocking (ctx cancel), so calling it here is safe.
func (b *BackgroundShells) sweepExpiredLocked(now time.Time) int {
	reaped := 0
	for _, sh := range b.shells {
		var cancel context.CancelFunc
		sh.mu.Lock()
		if !sh.done && !sh.expired && sh.ttl > 0 && now.Sub(sh.startedAt) >= sh.ttl {
			sh.expired = true
			sh.exitCode = nil
			cancel = sh.cancel
		}
		sh.mu.Unlock()
		if cancel != nil {
			cancel()
			reaped++
		}
	}
	return reaped
}

// StartReaper launches the background TTL sweep on ctx. The composition root wires it
// once at daemon boot (on the long-lived work ctx); the goroutine exits on ctx
// cancellation OR Shutdown/stopReaper, whichever comes first. A non-positive interval
// (disabled TTL) launches nothing. It is deliberately NOT called by NewBackgroundShells
// so any code path that never wires it stays goleak-clean.
func (b *BackgroundShells) StartReaper(ctx context.Context) {
	if b == nil || b.reaperInterval <= 0 || b.reaperStop == nil {
		return
	}
	b.reaperWG.Go(func() {
		ticker := time.NewTicker(b.reaperInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-b.reaperStop:
				return
			case <-ticker.C:
				b.sweepExpired(time.Now())
			}
		}
	})
}

// stopReaper signals the sweep goroutine to exit and joins it under a bounded wait so a
// hung tick cannot wedge shutdown. Once-guarded (safe to call repeatedly) and safe when
// StartReaper was never invoked (disabled interval, or a directly-constructed registry
// with a nil stop channel). Folded into Shutdown so the existing teardown hook stops it.
func (b *BackgroundShells) stopReaper() {
	if b == nil {
		return
	}
	b.reaperOnce.Do(func() {
		if b.reaperStop != nil {
			close(b.reaperStop)
		}
	})
	done := make(chan struct{})
	go func() {
		b.reaperWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(reaperStopJoinTimeout):
	}
}
