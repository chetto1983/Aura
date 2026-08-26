package runner

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

const (
	memoryContextHeader = "## Aura long-term memory (your own recalled facts)\n<memory_context>"
	memoryContextFooter = "</memory_context>"
	memoryRecallHeader  = "## Aura recalled for this message (your own knowledge)\n<memory_recall>"
	memoryRecallFooter  = "</memory_recall>"
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

func (r *Runner) contextConfig(ctx context.Context, memory *conversations.TransientContext) conversations.ContextConfig {
	return conversations.ContextConfig{
		ContextWindow:              r.cfg.ContextWindow,
		CompactionTriggerPercent:   r.cfg.CompactionTriggerPercent,
		FixedOverheadTokens:        r.manifestOverheadTokens(),
		MaxOutputTokens:            r.cfg.MaxOutputTokens,
		ToolEvictAfterTurns:        r.evictAfter,
		HistoryHardCapTurns:        r.historyCap,
		AlwaysBlock:                r.renderContextBlock(ctx),
		TransientContext:           memory,
		ProviderErrorReserveTokens: llm.ProviderErrorReserveTokens(r.cfg),
		Summarizer:                 r.compactionSummarizer(),
	}
}

func (r *Runner) compactionSummarizer() conversations.Summarizer {
	if !r.compactionEnabled {
		return nil
	}
	return conversations.NewLLMSummarizer(
		r.client,
		r.compactionModel,
		r.cfg.ContextWindow,
		time.Duration(r.cfg.TotalTimeoutSec)*time.Second,
	)
}

// ValidateCompactionConfig measures the booted registry, not a guessed schema count, so an
// enabled early trigger cannot silently disappear once the manifest is rendered.
func (r *Runner) ValidateCompactionConfig() error {
	return conversations.ValidateCompactionTrigger(
		r.compactionEnabled,
		r.cfg.ContextWindow,
		r.cfg.CompactionTriggerPercent,
		r.manifestOverheadTokens(),
	)
}

// loadMemoryContext builds the pre-user memory block: the always-on query-less digest
// plus, when preload is enabled, a per-message relevance recall over userMsg. Both legs
// are fail-soft (an error/empty/abstention is dropped, never blocks the turn). A nil
// userMsg (resume/branch continuation) skips the recall leg and keeps the digest.
func (r *Runner) loadMemoryContext(ctx context.Context, userMsg *string) *conversations.TransientContext {
	if r.memoryContext == nil {
		return nil
	}
	identityID := identityctx.IdentityID(ctx)
	if identityID == "" {
		slog.Warn("conversation memory context has no identity")
		return nil
	}
	digest := r.memoryDigest(ctx, identityID)
	recall := r.memoryRecall(ctx, identityID, userMsg)

	var b strings.Builder
	if digest != "" {
		b.WriteString(memoryContextHeader + "\n" + digest + "\n" + memoryContextFooter)
	}
	if recall != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(memoryRecallHeader + "\n" + recall + "\n" + memoryRecallFooter)
	}
	if b.Len() == 0 {
		return nil
	}
	return &conversations.TransientContext{
		Content:           b.String(),
		BeforeCurrentUser: userMsg != nil,
	}
}

// memoryDigest fetches the query-less whole-memory digest; fail-soft (error/empty → "").
func (r *Runner) memoryDigest(ctx context.Context, identityID string) string {
	content, err := r.memoryContext.Context(ctx, identityID)
	if err != nil {
		slog.Warn("conversation memory context unavailable", "identity_id", identityID, "err", err)
		return ""
	}
	return strings.TrimSpace(content)
}

// memoryRecall runs the proactive per-message preload (memory_search over the current
// user text) when enabled; fail-soft (disabled/no query/error/abstention → "").
func (r *Runner) memoryRecall(ctx context.Context, identityID string, userMsg *string) string {
	if !r.memoryPreloadEnabled || userMsg == nil {
		return ""
	}
	query := strings.TrimSpace(*userMsg)
	if query == "" {
		return ""
	}
	content, err := r.memoryContext.Search(ctx, identityID, query)
	if err != nil {
		slog.Warn("conversation memory preload unavailable", "identity_id", identityID, "err", err)
		return ""
	}
	return strings.TrimSpace(content)
}

// renderContextBlock builds messages[1]: the always-on skills block plus the operator
// profile for THIS identity.
//
// The profile joins here rather than being retrieved from the memory graph, where it used
// to live: settings competing for rank against real memories is how "role: programmatore"
// ends up outranking what the operator actually asked to be remembered.
func (r *Runner) renderContextBlock(ctx context.Context) string {
	var parts []string
	if r.alwaysBlock != nil {
		if block := strings.TrimSpace(r.alwaysBlock()); block != "" {
			parts = append(parts, block)
		}
	}
	if profile := r.turnProfileBlock(ctx); profile != "" {
		parts = append(parts, profile)
	}
	return strings.Join(parts, "\n\n")
}
