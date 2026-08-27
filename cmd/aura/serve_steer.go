// serve_steer.go wires the D-07/D-08 steer/delegation-result queue TTL sweep into the
// daemon composition root. Mirrors approval_expiry.go's shape exactly: one
// conversations.Sweeper, started on the resident work ctx alongside every other
// background loop in runServe (including plan 51-01's delegation claim loop), stopped
// in drainShutdown. No second scheduler — the interval-driven tick primitive is the
// SAME one every other periodic worker in this daemon already uses.
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/steer"
)

// steerQueueSweepBatchSize bounds one sweep tick's ExpireDue call, mirroring
// approvalExpiryBatchSize's own reasoning: a batch cap keeps one tick bounded even if a
// long daemon outage let due rows pile up, deferring the remainder to the next tick
// rather than blocking the sweep loop on an unbounded pass.
const steerQueueSweepBatchSize = 100

// newSteerQueueSweeper builds the resident TTL sweep over aura.steer_queue. sweeper is
// nil-safe (steer.Sweeper's own ExpireDue guards against a nil/unconfigured receiver),
// matching newRuntimeDelegationWorker's own "no pool configured degrades to a no-op"
// posture rather than the caller needing its own nil check.
//
// The interval is derived from the SHORTER of the two configured TTLs (steerTTL,
// delegationTTL): a row of the shorter-TTL kind must not sit expired-but-unswept for
// longer than the interval, mirroring approvalExpiryInterval's ttl/2-capped-at-1-minute
// reasoning. A TTL of <=0 (that kind's expiry disabled) is excluded from the minimum; if
// BOTH are <=0 the sweep interval is 0 (conversations.Sweeper.Start's own "no worker
// launched" disabled state — a queue where nothing ever expires needs no sweep).
func newSteerQueueSweeper(sweeper *steer.Sweeper, steerTTL, delegationTTL time.Duration) *conversations.Sweeper {
	interval := steerQueueSweepInterval(steerTTL, delegationTTL)
	return conversations.NewSweeper(conversations.SweeperConfig{
		Interval: interval,
		Sweep: func(ctx context.Context) {
			expired, err := sweeper.ExpireDue(ctx, time.Now(), steerQueueSweepBatchSize)
			if err != nil {
				slog.Warn("aura serve: steer queue TTL sweep", "err", err)
				return
			}
			if expired > 0 {
				slog.Info("aura serve: expired steer queue rows", "count", expired)
			}
		},
	})
}

// steerQueueSweepInterval picks the tick period from whichever of the two TTLs is both
// positive (expiry enabled for that kind) and smaller, halved (so a row is never more
// than roughly half its own TTL late to being swept) and capped at one minute — the SAME
// two rules approvalExpiryInterval applies to its own single TTL.
func steerQueueSweepInterval(steerTTL, delegationTTL time.Duration) time.Duration {
	shortest := time.Duration(0)
	for _, ttl := range []time.Duration{steerTTL, delegationTTL} {
		if ttl <= 0 {
			continue
		}
		if shortest <= 0 || ttl < shortest {
			shortest = ttl
		}
	}
	if shortest <= 0 {
		return 0
	}
	interval := shortest / 2
	if interval <= 0 {
		interval = shortest
	}
	if interval > time.Minute {
		return time.Minute
	}
	return interval
}
