package agui

// server_run_terminal_test.go covers the one guarantee a subscriber cannot live without: every
// run ends with a frame that says so.
//
// Measured twice on a live stack (2026-09-06). POST /agent/runs/{id}/cancel returned 202, the
// agent turn ended — the server logged it at the same instant — and the subscriber saw
// TOOL_CALL_START, TOOL_CALL_ARGS, heartbeats, then nothing. Four minutes later the stream still
// carried zero RUN_FINISHED and zero RUN_ERROR. The cockpit ends a run on one of those two
// frames, so pressing stop stopped the work and left the interface spinning forever.

import (
	"context"
	"iter"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// streamOf replays a fixed slice as the producer's source, then ends — exactly what a cancelled
// turn does: the iterator simply stops, with no error and no terminal frame.
func streamOf(evs ...events.Event) iter.Seq2[events.Event, error] {
	return func(yield func(events.Event, error) bool) {
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func drainTypes(t *testing.T, sess *RunSession) []events.EventType {
	t.Helper()
	ch, cancelSub, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("subscribeFrom(0) on a fresh session must succeed")
	}
	defer cancelSub()
	var got []events.EventType
	for sev := range ch {
		got = append(got, sev.Ev.Type())
	}
	return got
}

// TestCancelledRunStillEndsWithATerminalFrame is the regression: a stream that stops early must
// still leave the subscriber an ending, and it must be RUN_ERROR — a client that rendered a
// cancelled turn as RUN_FINISHED would be claiming the work completed.
func TestCancelledRunStillEndsWithATerminalFrame(t *testing.T) {
	sess := newRunSession("run-c", "thread-c", "id-c", 16, 8, nil)
	srv := &Server{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the run was cancelled while the tool call was in flight

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runProducer(ctx, func() {}, sess, streamOf(
			events.NewToolCallStartEvent("c1", "shell_exec"),
			events.NewToolCallArgsEvent("c1", `{"command":"sleep 999"}`),
		))
	}()
	got := drainTypes(t, sess)
	<-done

	if len(got) == 0 || got[len(got)-1] != events.EventTypeRunError {
		t.Fatalf("a cancelled run must end on RUN_ERROR, got %v", got)
	}
}

// TestFinishedRunIsNotGivenASecondEnding: the guarantee must not become a duplicate. A stream
// that already said RUN_FINISHED is complete, and appending an error after it would turn every
// successful run into a failed one.
func TestFinishedRunIsNotGivenASecondEnding(t *testing.T) {
	sess := newRunSession("run-f", "thread-f", "id-f", 16, 8, nil)
	srv := &Server{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runProducer(context.Background(), func() {}, sess, streamOf(
			events.NewTextMessageContentEvent("m", "ok"),
			events.NewRunFinishedEvent("thread-f", "run-f"),
		))
	}()
	got := drainTypes(t, sess)
	<-done

	if len(got) != 2 || got[1] != events.EventTypeRunFinished {
		t.Fatalf("a completed run must keep exactly its own ending, got %v", got)
	}
}

// TestErroredRunKeepsItsOwnError: the same, for the arm that already ends on RUN_ERROR.
func TestErroredRunKeepsItsOwnError(t *testing.T) {
	sess := newRunSession("run-e", "thread-e", "id-e", 16, 8, nil)
	srv := &Server{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runProducer(context.Background(), func() {}, sess, streamOf(
			events.NewRunErrorEvent("the model refused"),
		))
	}()
	got := drainTypes(t, sess)
	<-done

	if len(got) != 1 || got[0] != events.EventTypeRunError {
		t.Fatalf("an errored run must not be given a second ending, got %v", got)
	}
}
