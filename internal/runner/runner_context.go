package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
)

func (r *Runner) loadTurnHistory(
	ctx context.Context,
	conversationID string,
	cfg conversations.ContextConfig,
	branchLeaf int,
) ([]llm.Message, error) {
	if branchLeaf > 0 {
		return r.Conv.LoadManagedHistoryForBranch(
			ctx, conversationID, branchLeaf, cfg,
		)
	}
	return r.Conv.LoadManagedHistory(ctx, conversationID, cfg)
}

func (r *Runner) contextConfig(
	dynamicTail *conversations.DynamicTail,
) conversations.ContextConfig {
	return conversations.ContextConfig{
		ContextWindow:              r.cfg.ContextWindow,
		MaxOutputTokens:            r.cfg.MaxOutputTokens,
		ToolEvictAfterTurns:        r.evictAfter,
		HistoryHardCapTurns:        r.historyCap,
		AlwaysBlock:                r.renderContextBlock(),
		ProviderErrorReserveTokens: llm.ProviderErrorReserveTokens(r.cfg),
		DynamicTail:                dynamicTail,
	}
}

func (r *Runner) renderContextBlock() string {
	if r.alwaysBlock == nil {
		return ""
	}
	return strings.TrimSpace(r.alwaysBlock())
}

func (r *Runner) resolveOwner(
	ctx context.Context,
	conversationID string,
) (identity.Identity, error) {
	conversation, err := r.Conv.Get(ctx, conversationID)
	if err != nil {
		return identity.Identity{},
			fmt.Errorf("context block: load conversation identity: %w", err)
	}
	owner, err := r.identity.GetIdentityByID(ctx, conversation.IdentityID)
	if err != nil {
		return identity.Identity{}, fmt.Errorf(
			"context block: resolve identity %s: %w",
			conversation.IdentityID,
			err,
		)
	}
	return owner, nil
}
