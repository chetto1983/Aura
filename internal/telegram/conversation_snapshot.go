package telegram

import (
	"strings"
	"time"
)

type orchestrationSnapshot struct {
	StoredAt            time.Time
	PromptVersion       string
	PromptModules       []string
	PromptHash          string
	Toolset             string
	ToolsetSelectReason string
	ToolsExposed        []string
	ToolsCalled         []string
	ReadSkills          []string
	LoopSteps           int
	HiddenToolRejected  bool
	SkillsRead          bool
	SwarmUsed           bool
	SandboxUsed         bool
	TerminalTool        string
	DuplicateToolCall   bool
	TokensPrompt        int
	TokensCompletion    int
	TokensTotal         int
	CostUSD             float64
}

func (b *Bot) storeOrchestrationSnapshot(userID string, stats turnStats) {
	if b == nil || strings.TrimSpace(userID) == "" {
		return
	}
	now := time.Now()
	b.orchMap.Store(userID, orchestrationSnapshot{
		StoredAt:            now,
		PromptVersion:       stats.promptVersion,
		PromptModules:       append([]string(nil), stats.promptModules...),
		PromptHash:          stats.promptHash,
		Toolset:             stats.toolset,
		ToolsetSelectReason: stats.toolsetSelectReason,
		ToolsExposed:        append([]string(nil), stats.toolsExposed...),
		ToolsCalled:         append([]string(nil), stats.toolsCalled...),
		ReadSkills:          append([]string(nil), stats.readSkills...),
		LoopSteps:           stats.loopSteps,
		HiddenToolRejected:  stats.hiddenToolRejected,
		SkillsRead:          stats.skillsRead,
		SwarmUsed:           stats.swarmUsed,
		SandboxUsed:         stats.sandboxUsed,
		TerminalTool:        stats.terminalTool,
		DuplicateToolCall:   stats.duplicateToolCall,
		TokensPrompt:        stats.tokensPrompt,
		TokensCompletion:    stats.tokensCompletion,
		TokensTotal:         stats.tokensTotal,
		CostUSD:             stats.costUSD,
	})
	b.pruneOrchestrationSnapshots(now)
}

func (b *Bot) loadOrchestrationSnapshot(userID string) (orchestrationSnapshot, bool) {
	if b == nil {
		return orchestrationSnapshot{}, false
	}
	value, ok := b.orchMap.Load(userID)
	if !ok {
		return orchestrationSnapshot{}, false
	}
	snap, ok := value.(orchestrationSnapshot)
	return snap, ok
}

func (b *Bot) pruneOrchestrationSnapshots(now time.Time) {
	if b == nil || b.cfg == nil || b.cfg.TraceRetentionDays <= 0 {
		return
	}
	cutoff := now.Add(-time.Duration(b.cfg.TraceRetentionDays) * 24 * time.Hour)
	b.orchMap.Range(func(key, value any) bool {
		snap, ok := value.(orchestrationSnapshot)
		if !ok {
			return true
		}
		if !snap.StoredAt.IsZero() && snap.StoredAt.Before(cutoff) {
			b.orchMap.Delete(key)
		}
		return true
	})
}
