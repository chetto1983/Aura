package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
)

const approvalExpiryBatchSize = 100

type approvalExpiryIdentityLister interface {
	ListIdentities(context.Context) ([]identity.Identity, error)
}

type approvalExpiryRunner interface {
	ExpirePendingApprovals(context.Context, time.Time, int) (int, error)
}

func newApprovalExpirySweeper(
	run approvalExpiryRunner,
	identities approvalExpiryIdentityLister,
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
	identities approvalExpiryIdentityLister,
	run approvalExpiryRunner,
	cutoff time.Time,
	limit int,
) (int, error) {
	if identities == nil || run == nil {
		return 0, fmt.Errorf("approval expiry is not configured")
	}
	owners, err := identities.ListIdentities(ctx)
	if err != nil {
		return 0, fmt.Errorf("list identities: %w", err)
	}
	total := 0
	var errs []error
	for _, owner := range owners {
		if owner.ID == "" || owner.Deactivated {
			continue
		}
		expired, err := run.ExpirePendingApprovals(identityctx.WithIdentityID(ctx, owner.ID), cutoff, limit)
		total += expired
		if err != nil {
			errs = append(errs, fmt.Errorf("identity %s: %w", owner.ID, err))
		}
	}
	return total, errors.Join(errs...)
}
