package runner

import (
	"context"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

const (
	memoryContextHeader = "## Aura long-term memory (your own recalled facts)\n<memory_context>"
	memoryContextFooter = "</memory_context>"
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

func (r *Runner) contextConfig(memory *conversations.TransientContext) conversations.ContextConfig {
	var summarizer conversations.Summarizer
	if r.compactionEnabled {
		summarizer = conversations.NewLLMSummarizer(r.client, r.compactionModel)
	}
	return conversations.ContextConfig{
		ContextWindow:              r.cfg.ContextWindow,
		MaxOutputTokens:            r.cfg.MaxOutputTokens,
		ToolEvictAfterTurns:        r.evictAfter,
		HistoryHardCapTurns:        r.historyCap,
		AlwaysBlock:                r.renderContextBlock(),
		TransientContext:           memory,
		ProviderErrorReserveTokens: llm.ProviderErrorReserveTokens(r.cfg),
		Summarizer:                 summarizer,
	}
}

func (r *Runner) loadMemoryContext(ctx context.Context, beforeCurrentUser bool) *conversations.TransientContext {
	if r.memoryContext == nil {
		return nil
	}
	identityID := identityctx.IdentityID(ctx)
	if identityID == "" {
		slog.Warn("conversation memory context has no identity")
		return nil
	}
	content, err := r.memoryContext.Context(ctx, identityID)
	if err != nil {
		slog.Warn("conversation memory context unavailable", "identity_id", identityID, "err", err)
		return nil
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return &conversations.TransientContext{
		Content:           memoryContextHeader + "\n" + content + "\n" + memoryContextFooter,
		BeforeCurrentUser: beforeCurrentUser,
	}
}

func (r *Runner) renderContextBlock() string {
	if r.alwaysBlock == nil {
		return ""
	}
	return strings.TrimSpace(r.alwaysBlock())
}
