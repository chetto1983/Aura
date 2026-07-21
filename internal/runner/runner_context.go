package runner

// runner_context.go: the per-turn context-assembly concern split out of runner.go
// (refactor-on-touch, 600-LOC cap). It owns how a turn's history is rehydrated and how
// the messages[1] always-block is composed — the L1/L2/L2.5 ladder inputs, the L4
// archival-memory recall block (amendment #21), and the always-on skill block.

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
)

// loadTurnHistory rehydrates the turn history the agent runs over: the full linear
// history (branchLeaf <= 0, the default Turn path, byte-identical to the pre-D-09 case)
// or the selected branch path (branchLeaf > 0, the D-09 re-run). Both feed the SAME
// L1/L2/L2.5 ladder, so the protected messages[0] head is identical either way — only
// body turns differ per branch (the CAP-04 cache invariant). It does NOT re-implement
// the walk; it calls the store's path-aware loader (plan 25-06).
func (r *Runner) loadTurnHistory(ctx context.Context, convID string, cfg conversations.ContextConfig, branchLeaf int) ([]llm.Message, error) {
	if branchLeaf > 0 {
		return r.Conv.LoadManagedHistoryForBranch(ctx, convID, branchLeaf, cfg)
	}
	return r.Conv.LoadManagedHistory(ctx, convID, cfg)
}

// contextConfig builds the L1/L2/L2.5 ladder inputs from the Runner's llm.Config +
// eviction knob.
func (r *Runner) contextConfig(ctx context.Context, convID, recallQuery string) (conversations.ContextConfig, error) {
	block, err := r.renderContextBlock(ctx, convID, recallQuery)
	if err != nil {
		return conversations.ContextConfig{}, err
	}
	return conversations.ContextConfig{
		ContextWindow:              r.cfg.ContextWindow,
		MaxOutputTokens:            r.cfg.MaxOutputTokens,
		ToolEvictAfterTurns:        r.evictAfter,
		AlwaysBlock:                block,
		ProviderErrorReserveTokens: llm.ProviderErrorReserveTokens(r.cfg),
	}, nil
}

// renderContextBlock composes the messages[1] always-block from up to two legs, in
// order: the L4 archival-memory recall block and the always-on skill block. Each leg
// is nil-guarded — nil => the leg contributes nothing (the feature's default-off state
// is produced upstream by the composition root injecting a nil provider).
func (r *Runner) renderContextBlock(ctx context.Context, convID, recallQuery string) (string, error) {
	var parts []string
	if r.archivalRecaller != nil {
		// L4 recall needs the owning identity; it is scoped by owner.ID — the SAME value
		// the memory-MCP write path keys on (mcptools bridge → identityctx.IdentityID).
		// owner.Name would query the wrong/global scope and silently recall nothing.
		// recallQuery (the current user message) keys top-K relevance; "" => identity-only.
		owner, err := r.resolveOwner(ctx, convID)
		if err != nil {
			return "", err
		}
		block, err := r.archivalRecaller(ctx, owner.ID, recallQuery)
		if err != nil {
			return "", fmt.Errorf("archival recall: %w", err)
		}
		if block = strings.TrimSpace(block); block != "" {
			parts = append(parts, block)
		}
	}
	if r.alwaysBlock != nil {
		if block := strings.TrimSpace(r.alwaysBlock()); block != "" {
			parts = append(parts, block)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// resolveOwner loads the conversation's owning identity — used by the archival-recall
// leg of renderContextBlock, which scopes recall by the owner id.
func (r *Runner) resolveOwner(ctx context.Context, convID string) (identity.Identity, error) {
	conv, err := r.Conv.Get(ctx, convID)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("context block: load conversation identity: %w", err)
	}
	owner, err := r.identity.GetIdentityByID(ctx, conv.IdentityID)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("context block: resolve identity %s: %w", conv.IdentityID, err)
	}
	return owner, nil
}
