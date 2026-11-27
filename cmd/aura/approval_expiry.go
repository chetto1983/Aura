package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
)

const approvalExpiryBatchSize = 100

type approvalExpiryRunner interface {
	ExpirePendingApprovals(context.Context, time.Time, int) (int, error)
}

func newApprovalExpirySweeper(
	run approvalExpiryRunner,
	identities runtimeIdentityLister,
	ttl time.Duration,
) *conversations.Sweeper {
	interval := approvalExpiryInterval(ttl)
	return conversations.NewSweeper(conversations.SweeperConfig{
		Interval: interval,
		Sweep: func(ctx context.Context) {
			cutoff := time.Now().UTC().Add(-ttl)
			expired, err := expireApprovalsByIdentity(ctx, identities, run, cutoff, approvalExpiryBatchSize)
			if err != nil {
				slog.Warn("aura serve: approval expiry sweep", "err", err)
				return
			}
			if expired > 0 {
				slog.Info("aura serve: expired unanswered approvals", "count", expired)
			}
		},
	})
}

func approvalExpiryInterval(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 0
	}
	interval := ttl / 2
	if interval <= 0 {
		interval = ttl
	}
	if interval > time.Minute {
		return time.Minute
	}
	return interval
}

func expireApprovalsByIdentity(
	ctx context.Context,
	identities runtimeIdentityLister,
	run approvalExpiryRunner,
	cutoff time.Time,
	limit int,
) (int, error) {
	if identities == nil || run == nil {
		return 0, fmt.Errorf("approval expiry is not configured")
	}
	total := 0
	err := eachActiveIdentity(ctx, identities, func(ctx context.Context, _ string) error {
		expired, err := run.ExpirePendingApprovals(ctx, cutoff, limit)
		total += expired
		return err
	})
	return total, err
}
