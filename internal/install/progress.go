// Package install handles first-run provisioning of large external assets
// (embedding model GGUFs today; future OCR weights, LLM checkpoints, etc.).
//
// The package is dependency-free beyond stdlib so it can be compiled into the
// tiny `cmd/aura-init-models` init container (distroless, no Aura runtime).
// Logging is injected via a slog.Logger; progress is reported through a
// ProgressFn so callers (init binary today, dashboard SSE endpoint tomorrow)
// pick their own delivery channel.
package install

import (
	"log/slog"
	"sync"
	"time"
)

// ProgressFn receives byte-level transfer updates. bytesDone is the count
// already written to disk; bytesTotal is the expected final size (0 when
// the server didn't advertise a Content-Length). Implementations must be
// safe to call from the download goroutine; rate-limiting is the caller's
// responsibility.
type ProgressFn func(bytesDone, bytesTotal int64)

// NopProgressFn is a no-op suitable for tests that don't care about progress.
func NopProgressFn(_, _ int64) {}

// LogProgressFn returns a ProgressFn that emits one slog line every interval.
// The first update fires immediately so the user sees something in the logs
// without waiting for the first tick. Final 100% is always reported.
//
// Use case: `cmd/aura-init-models` runs unattended in a container; flooding
// the logs with one line per 256 KiB chunk would be unreadable. Five-second
// granularity gives the operator enough signal to know the fetch is alive.
func LogProgressFn(logger *slog.Logger, interval time.Duration) ProgressFn {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	var (
		mu       sync.Mutex
		lastEmit time.Time
		lastDone int64
	)
	return func(bytesDone, bytesTotal int64) {
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		isFirst := lastEmit.IsZero()
		isFinal := bytesTotal > 0 && bytesDone >= bytesTotal
		if !isFirst && !isFinal && now.Sub(lastEmit) < interval {
			return
		}
		percent := 0.0
		if bytesTotal > 0 {
			percent = float64(bytesDone) / float64(bytesTotal) * 100
		}
		// MB/s since last emission — useful when fetching a 265 MB file
		// over flaky bandwidth so the operator sees whether the link
		// stalled.
		var speedMBps float64
		if !isFirst {
			elapsed := now.Sub(lastEmit).Seconds()
			if elapsed > 0 {
				speedMBps = float64(bytesDone-lastDone) / 1024 / 1024 / elapsed
			}
		}
		logger.Info("download progress",
			"bytes_done", bytesDone,
			"bytes_total", bytesTotal,
			"percent", round1(percent),
			"speed_mbps", round1(speedMBps),
		)
		lastEmit = now
		lastDone = bytesDone
	}
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
