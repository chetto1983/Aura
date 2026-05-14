package telegram

import (
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
)

type orchestrationSnapshot = agent.Snapshot

// StoreOrchestrationSnapshot is the exported surface of storeOrchestrationSnapshot.
// Used by internal/channels/telegram.InvocationBuilder.
func (b *Bot) StoreOrchestrationSnapshot(userID string, stats agent.TurnStats) {
	b.storeOrchestrationSnapshot(userID, stats)
}

func (b *Bot) storeOrchestrationSnapshot(userID string, stats agent.TurnStats) {
	if b == nil || strings.TrimSpace(userID) == "" {
		return
	}
	now := time.Now()
	b.sessionStore().StoreSnapshot(userID, orchestrationSnapshot{
		StoredAt:                now,
		PromptVersion:           stats.PromptVersion,
		PromptModules:           append([]string(nil), stats.PromptModules...),
		PromptHash:              stats.PromptHash,
		Toolset:                 stats.Toolset,
		ToolsetSelectReason:     stats.ToolsetSelectReason,
		ToolsExposed:            append([]string(nil), stats.ToolsExposed...),
		ToolsCalled:             append([]string(nil), stats.ToolsCalled...),
		ReadSkills:              append([]string(nil), stats.ReadSkills...),
		RetrievalCapsulePresent: stats.RetrievalCapsulePresent,
		LoopSteps:               stats.LoopSteps,
		LLMCalls:                stats.LLMCalls,
		ToolCalls:               stats.ToolCalls,
		SkillsRead:              stats.SkillsRead,
		SwarmUsed:               stats.SwarmUsed,
		SandboxUsed:             stats.SandboxUsed,
		TerminalTool:            stats.TerminalTool,
		DuplicateToolCall:       stats.DuplicateToolCall,
		TokensPrompt:            stats.TokensPrompt,
		TokensCompletion:        stats.TokensCompletion,
		TokensTotal:             stats.TokensTotal,
		CostUSD:                 stats.CostUSD,
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
