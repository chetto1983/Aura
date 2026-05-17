package db

import (
	"context"
	"math/rand"
	"strings"
	"time"
)

const (
	retryOnBusyMax    = 5
	retryOnBusyBaseMs = 25
)

// RetryOnBusy calls fn up to retryOnBusyMax times, pausing with exponential
// backoff + jitter when fn returns a SQLite SQLITE_BUSY / "database is locked"
// error. It stops immediately on any non-busy error or on context cancellation.
//
// Use this for operations that must succeed even during brief checkpoint or
// write-lock contention that the busy_timeout pragma alone cannot absorb (e.g.
// operations started inside a long-held read transaction on another connection).
func RetryOnBusy(ctx context.Context, fn func() error) error {
	var err error
	for i := range retryOnBusyMax {
		err = fn()
		if err == nil || !isBusyError(err) {
			return err
		}
		wait := time.Duration(retryOnBusyBaseMs*(1<<i))*time.Millisecond +
			time.Duration(rand.Int63n(int64(retryOnBusyBaseMs)))*time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return err
}

// isBusyError reports whether err is a SQLite busy/locked contention error.
func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "DATABASE IS LOCKED") ||
		strings.Contains(msg, "SQLITE_LOCKED")
}
