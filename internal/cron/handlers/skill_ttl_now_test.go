package handlers

import (
	"context"
	"testing"
	"time"
)

// nowRecordingSweeper captures the `now` timestamp the handler passes through, so a
// test can assert the injectable clock (h.now) is honoured rather than time.Now().
type nowRecordingSweeper struct {
	gotNow time.Time
}

func (s *nowRecordingSweeper) SweepExpiredSnippets(_ context.Context, _ time.Duration, now time.Time, _ string) ([]string, []string, error) {
	s.gotNow = now
	return nil, nil, nil
}

// TestSkillTTLSweepUsesInjectedClock covers the `h.now != nil` branch of Run: when a
// test clock is wired the sweep must use it verbatim (not the wall clock), so the sweep
// is deterministic under test.
func TestSkillTTLSweepUsesInjectedClock(t *testing.T) {
	t.Parallel()
	fixed := time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)
	sw := &nowRecordingSweeper{}
	h := SkillTTLSweepHandler{Sweeper: sw, TTL: time.Hour, now: func() time.Time { return fixed }}

	if _, err := h.Run(context.Background(), Job{}); err != nil {
		t.Fatalf("Run with injected clock: %v", err)
	}
	if !sw.gotNow.Equal(fixed) {
		t.Fatalf("sweep now = %s, want the injected clock %s", sw.gotNow, fixed)
	}
}
