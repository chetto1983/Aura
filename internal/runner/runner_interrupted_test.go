package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
)

// ctxRecordingConv proves the write outlives the context that killed the round. The shared
// fake ignores ctx entirely, which is exactly the property under test here.
type ctxRecordingConv struct {
	*fakeConvStore
	sawCtxErr error
	appended  int
}

func (c *ctxRecordingConv) AppendTurn(ctx context.Context, p conversations.AppendTurnParams) error {
	c.sawCtxErr = ctx.Err()
	c.appended++
	return c.fakeConvStore.AppendTurn(ctx, p)
}

func interruptedFixture(t *testing.T) (*Runner, *ctxRecordingConv, *turnTracker) {
	t.Helper()
	conv := &ctxRecordingConv{fakeConvStore: newFakeConvStore()}
	return &Runner{Conv: conv}, conv, &turnTracker{convID: "conv-1"}
}

// The measured failure: the process goes away mid-round, so the assistant turn that would
// have been written never is, and the conversation keeps a question with nothing after it.
func TestRecordInterruptedRoundWritesTheTurnACancelledRoundNeverWrote(t *testing.T) {
	runner, conv, tracker := interruptedFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runner.recordInterruptedRound(ctx, tracker, nil); err != nil {
		t.Fatal(err)
	}
	if conv.appended != 1 {
		t.Fatalf("appended %d turns, want the interruption recorded once", conv.appended)
	}
	turns := conv.turns["conv-1"]
	if len(turns) != 1 || !strings.Contains(turns[0].Content, "interrupted") {
		t.Fatalf("turn = %#v, want a marker naming the interruption", turns)
	}
	// Written BECAUSE the context died, so inheriting its cancellation would drop the only
	// record of the failure — the same reason flushPause uses WithoutCancel.
	if conv.sawCtxErr != nil {
		t.Fatalf("the write inherited the dead context (%v); it must outlive it", conv.sawCtxErr)
	}
}

func TestRecordInterruptedRoundRecordsAFailureThatIsNotACancellation(t *testing.T) {
	runner, conv, tracker := interruptedFixture(t)

	if err := runner.recordInterruptedRound(
		context.Background(), tracker, errors.New("upstream refused")); err != nil {
		t.Fatal(err)
	}
	if conv.appended != 1 {
		t.Fatalf("appended %d turns, want the failure recorded", conv.appended)
	}
}

// A round that answered, and a round that paused to ask the person something, both ended
// the way they were supposed to. Neither may grow a failure turn.
func TestRecordInterruptedRoundStaysOutOfTheWayOfAFinishedRound(t *testing.T) {
	for name, mutate := range map[string]func(*turnTracker){
		"answered": func(tr *turnTracker) { tr.answered = true },
		"paused":   func(tr *turnTracker) { tr.paused = true },
	} {
		t.Run(name, func(t *testing.T) {
			runner, conv, tracker := interruptedFixture(t)
			mutate(tracker)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			if err := runner.recordInterruptedRound(ctx, tracker, nil); err != nil {
				t.Fatal(err)
			}
			if conv.appended != 0 {
				t.Fatalf("a %s round grew a failure turn", name)
			}
		})
	}
}

// No answer, no pause, no cause. That is not a shape this runner produces, and inventing a
// failure for it would put an error into a conversation that never had one.
func TestRecordInterruptedRoundInventsNothingWithoutACause(t *testing.T) {
	runner, conv, tracker := interruptedFixture(t)

	if err := runner.recordInterruptedRound(context.Background(), tracker, nil); err != nil {
		t.Fatal(err)
	}
	if conv.appended != 0 {
		t.Fatalf("a failure turn was invented with no failure to report")
	}
}

func TestRecordInterruptedRoundIsInertWithoutATracker(t *testing.T) {
	runner, conv, _ := interruptedFixture(t)

	if err := runner.recordInterruptedRound(context.Background(), nil, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	if conv.appended != 0 {
		t.Fatalf("appended %d turns without a round to attribute them to", conv.appended)
	}
}
