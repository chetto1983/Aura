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
	b.sessionStore().StoreSnapshot(userID, agent.NewSnapshotFromTurnStats(stats, now))
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
