package runner

import (
	"context"
	"strings"

	"github.com/chetto1983/aura/internal/conversations"
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

func (r *Runner) contextConfig() conversations.ContextConfig {
	return conversations.ContextConfig{
		ContextWindow:              r.cfg.ContextWindow,
		MaxOutputTokens:            r.cfg.MaxOutputTokens,
		ToolEvictAfterTurns:        r.evictAfter,
		HistoryHardCapTurns:        r.historyCap,
		AlwaysBlock:                r.renderContextBlock(),
		ProviderErrorReserveTokens: llm.ProviderErrorReserveTokens(r.cfg),
	}
}

func (r *Runner) renderContextBlock() string {
	if r.alwaysBlock == nil {
		return ""
	}
	return strings.TrimSpace(r.alwaysBlock())
}
