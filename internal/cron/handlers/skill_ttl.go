package handlers

import (
	"context"
	"fmt"
	"time"
)

// skillTTLMaxDuration bounds the snippet TTL sweep (D-16 / RESEARCH Pattern 4): the
// sweep scans a small N of per-skill usage sidecars + does a handful of FS moves, so a
// 2-minute budget is generous.
const skillTTLMaxDuration = 2 * time.Minute

// SnippetSweeper is the consumer-declared seam the TTL handler drives (the 10-04
// taskStore pattern): the live *skills.Writer satisfies it via SweepExpiredSnippets,
// so this package does not need a concrete skills dependency to unit-test the handler.
// It archives + de-materializes every active snippet older than ttl and writes the
// D-29 `auto` audit row per archive; it returns the archived + kept skill names.
type SnippetSweeper interface {
	SweepExpiredSnippets(ctx context.Context, ttl time.Duration, now time.Time, actorID string) (archived, kept []string, err error)
}

// SkillTTLSweepHandler is the system-seeded snippet TTL sweep TaskKind (D-16). It is
// NOT model-schedulable — serve.go seeds it daily, the 0010-widened kind CHECK admits
// the row. It scans the usage sidecars (D-19) and archives snippets unused longer than
// TTL, de-materializing them from the ro /skills mount + auditing each auto-archive
// (D-29 `auto`: gate_recommended=false, gate_taken=true). A missed sweep does NOT
// reschedule (RESEARCH Pattern 4): the next daily tick re-evaluates the same TTL.
type SkillTTLSweepHandler struct {
	Sweeper SnippetSweeper
	TTL     time.Duration
	// now is injectable for tests; nil → time.Now().UTC().
	now func() time.Time
}

// Meta declares the sweep contract (RESEARCH Pattern 4): a 2-minute budget, no
// reschedule-on-recovery (it is idempotent — re-running the same TTL is safe).
func (SkillTTLSweepHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindSkillTTLSweep, MaxDuration: skillTTLMaxDuration, ReschedulesOnRecovery: false}
}

// Run sweeps expired snippets and returns a count summary. A nil sweeper or a
// non-positive TTL is a no-op success (the sweep is harmlessly disabled, not an
// error). A sweep error is terminal (the dispatcher records + notifies it, D-21).
func (h SkillTTLSweepHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.Sweeper == nil || h.TTL <= 0 {
		return "skill TTL sweep: disabled (no sweeper or non-positive TTL)", nil
	}
	now := time.Now().UTC()
	if h.now != nil {
		now = h.now()
	}
	archived, kept, err := h.Sweeper.SweepExpiredSnippets(ctx, h.TTL, now, "auto")
	if err != nil {
		return "", fmt.Errorf("skill TTL sweep: %w", err)
	}
	return fmt.Sprintf("skill TTL sweep ok: archived %d, kept %d (ttl=%s)", len(archived), len(kept), h.TTL), nil
}
