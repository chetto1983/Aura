package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// messageDraftRecoveryInterval is a fraction of the draft lifetime (30 minutes) and of the dispatch
// cutoff (30 minutes), so a draft is closed within a minute of either.
const messageDraftRecoveryInterval = time.Minute

type messageDraftRecoverer interface {
	Recover(context.Context, string) (messagedrafts.Recovered, error)
}

// newMessageDraftRecoverySweeper closes, for every active identity, the drafts nobody opens again:
// reviews past their expiry and dispatches past the cutoff. It logs counts, never message content.
func newMessageDraftRecoverySweeper(drafts messageDraftRecoverer, identities runtimeIdentityLister) *conversations.Sweeper {
	return conversations.NewSweeper(conversations.SweeperConfig{
		Interval: messageDraftRecoveryInterval,
		Sweep: func(ctx context.Context) {
			got, err := recoverMessageDraftsByIdentity(ctx, identities, drafts)
			if err != nil {
				slog.Warn("aura serve: message draft recovery sweep", "err", err)
			}
			if got != (messagedrafts.Recovered{}) {
				slog.Info("aura serve: closed abandoned message drafts", "expired", got.Expired, "interrupted", got.Interrupted)
			}
		},
	})
}

func recoverMessageDraftsByIdentity(ctx context.Context, identities runtimeIdentityLister, drafts messageDraftRecoverer) (messagedrafts.Recovered, error) {
	var total messagedrafts.Recovered
	err := eachActiveIdentity(ctx, identities, func(ctx context.Context, ownerID string) error {
		got, err := drafts.Recover(ctx, ownerID)
		total.Expired += got.Expired
		total.Interrupted += got.Interrupted
		return err
	})
	return total, err
}
