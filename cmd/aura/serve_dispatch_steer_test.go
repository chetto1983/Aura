package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/steer"
)

// TestSchedulerSteerOrNilKeepsANilStoreNil: without the guard a nil store reaches cron as a
// non-nil interface and the first agent_job record panics on Push.
func TestSchedulerSteerOrNilKeepsANilStoreNil(t *testing.T) {
	t.Parallel()
	if got := schedulerSteerOrNil(nil, nil); got != nil {
		t.Fatalf("schedulerSteerOrNil(nil, nil) = %#v, want a nil interface", got)
	}
}

type fakeSteerPush struct{ conv, source, text string }

func (f *fakeSteerPush) Push(conv, source, text string) error {
	f.conv, f.source, f.text = conv, source, text
	return nil
}

type fakeLiveTurns map[[2]string]bool

func (f fakeLiveTurns) TurnLive(identityID, convID string) bool {
	return f[[2]string{identityID, convID}]
}

// TestSchedulerSteerPushesOnlyToALiveTurn: a report steered while no turn runs would wait for
// the operator's next, unrelated turn and reach it a second time beside the record already in
// its history, or start an unrequested follow-on turn (final review, 2026-11-28).
func TestSchedulerSteerPushesOnlyToALiveTurn(t *testing.T) {
	t.Parallel()
	push := &fakeSteerPush{}
	s := schedulerSteer{pusher: push, live: fakeLiveTurns{{"id-1", "conv-1"}: true}}

	if err := s.SteerReport("id-1", "conv-2", "idle"); err != nil || push.text != "" {
		t.Fatalf("an idle conversation got %+v, %v; want no push", push, err)
	}
	if err := s.SteerReport("id-1", "conv-1", "report"); err != nil {
		t.Fatalf("SteerReport: %v", err)
	}
	if *push != (fakeSteerPush{conv: "conv-1", source: steer.SourceScheduler, text: "report"}) {
		t.Fatalf("pushed %+v, want the report on conv-1 under source %q", *push, steer.SourceScheduler)
	}
}
