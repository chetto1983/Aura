package telegram

import (
	"strings"
	"time"

	"github.com/aura/aura/internal/agentruntime"
)

type orchestrationSnapshot = agentruntime.Snapshot

func (b *Bot) storeOrchestrationSnapshot(userID string, stats turnStats) {
	if b == nil || strings.TrimSpace(userID) == "" {
		return
	}
	now := time.Now()
	b.sessionStore().StoreSnapshot(userID, orchestrationSnapshot{
		StoredAt:                now,
		PromptVersion:           stats.promptVersion,
		PromptModules:           append([]string(nil), stats.promptModules...),
		PromptHash:              stats.promptHash,
		Toolset:                 stats.toolset,
		ToolsetSelectReason:     stats.toolsetSelectReason,
		ToolsExposed:            append([]string(nil), stats.toolsExposed...),
		ToolsCalled:             append([]string(nil), stats.toolsCalled...),
		ReadSkills:              append([]string(nil), stats.readSkills...),
		RetrievalCapsulePresent: stats.retrievalCapsulePresent,
		LoopSteps:               stats.loopSteps,
		LLMCalls:                stats.llmCalls,
		ToolCalls:               stats.toolCalls,
		HiddenToolRejected:      stats.hiddenToolRejected,
		SkillsRead:              stats.skillsRead,
		SwarmUsed:               stats.swarmUsed,
		SandboxUsed:             stats.sandboxUsed,
		TerminalTool:            stats.terminalTool,
		DuplicateToolCall:       stats.duplicateToolCall,
		TokensPrompt:            stats.tokensPrompt,
		TokensCompletion:        stats.tokensCompletion,
		TokensTotal:             stats.tokensTotal,
		CostUSD:                 stats.costUSD,
		RetryNudgesSent:         stats.retryNudgesSent,
		SpiralBreakerFired:      stats.spiralBreakerFired,
		TieredBudgetTier:        stats.tieredBudgetTier,
	})
	b.pruneOrchestrationSnapshots(now)
}

func (b *Bot) loadOrchestrationSnapshot(userID string) (orchestrationSnapshot, bool) {
	if b == nil {
		return orchestrationSnapshot{}, false
	}
	return b.sessionStore().Snapshot(userID)
}

func (b *Bot) pruneOrchestrationSnapshots(now time.Time) {
	if b == nil || b.cfg == nil || b.cfg.TraceRetentionDays <= 0 {
		return
	}
	b.sessionStore().PruneSnapshots(now, b.cfg.TraceRetentionDays)
}
