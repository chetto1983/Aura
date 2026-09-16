package telegram

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"
)

// paneActionBot is the status pane's double when the pulse matters: it sends/edits
// like fakeBot AND answers Notify like the live *tele.Bot, which is exactly the
// pairing the pane resolves out of its botSender in production.
type paneActionBot struct {
	*fakeBot
	*recordingNotifier
}

func newPaneActionBot() *paneActionBot {
	return &paneActionBot{fakeBot: newFakeBot(), recordingNotifier: &recordingNotifier{}}
}

// startedController builds a controller and opens its pulse, the pairing the status
// consumer performs at the top of every turn.
func startedController(ctx context.Context, n botNotifier, to tele.Recipient) *mediaActionController {
	c := newMediaActionController(n, to)
	c.start(ctx)
	return c
}

// assertActions compares the recorded chat-action sequence with the expected one.
func assertActions(t *testing.T, rn *recordingNotifier, want ...tele.ChatAction) {
	t.Helper()
	got := rn.chatActions()
	if len(got) != len(want) {
		t.Fatalf("chat actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chat actions = %v, want %v", got, want)
		}
	}
}

// TestMediaActionControllerStartsTyping: the controller opens the turn on "typing"
// (RUN_STARTED → typing) with a single immediate notify, before any tick.
func TestMediaActionControllerStartsTyping(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		defer ctrl.Stop()
		synctest.Wait()
		assertActions(t, rn, tele.Typing)
	})
}

// TestMediaActionControllerSwitchesImmediately: a media tool start flips the action
// at once — the user must not wait out the refresh window to see "sending photo…".
func TestMediaActionControllerSwitchesImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		defer ctrl.Stop()
		synctest.Wait()

		ctrl.Start("call-1", "image_generate")
		synctest.Wait() // no clock advance: the switch cannot have come from a tick
		assertActions(t, rn, tele.Typing, tele.UploadingPhoto)
	})
}

// TestMediaActionControllerRefreshesSelectedAction: the ONE existing four-second
// pulse re-sends whatever the controller currently selects — Telegram expires a
// chat action after ~5s, so an upload longer than that must be refreshed as an
// upload, never silently degraded back to typing.
func TestMediaActionControllerRefreshesSelectedAction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		defer ctrl.Stop()
		ctrl.Start("call-1", "video_generate")
		synctest.Wait()

		time.Sleep(typingPulse + time.Millisecond)
		synctest.Wait()
		time.Sleep(typingPulse)
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingVideo, tele.UploadingVideo, tele.UploadingVideo)
	})
}

// TestMediaActionControllerRestoresTypingOnFinish: when the last media call ends the
// turn goes back to typing (the agent is still composing its answer).
func TestMediaActionControllerRestoresTypingOnFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		defer ctrl.Stop()
		ctrl.Start("call-1", "image_generate")
		ctrl.Finish("call-1")
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingPhoto, tele.Typing)

		time.Sleep(typingPulse + time.Millisecond)
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingPhoto, tele.Typing, tele.Typing)
	})
}

// TestMediaActionControllerVideoWinsOverImage: with an image and a video generating
// at once the chat shows the video action — the longer, heavier upload is the one
// worth describing; the image action returns when the video call ends.
func TestMediaActionControllerVideoWinsOverImage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		defer ctrl.Stop()
		ctrl.Start("img", "image_generate")
		ctrl.Start("vid", "video_generate")
		ctrl.Finish("vid")
		ctrl.Finish("img")
		synctest.Wait()
		assertActions(t, rn,
			tele.Typing, tele.UploadingPhoto, tele.UploadingVideo, tele.UploadingPhoto, tele.Typing)
	})
}

// TestMediaActionControllerTracksCallsIndependently: two concurrent image calls hold
// the action until BOTH finish, and finishing the first notifies nothing (the
// selection did not change — a redundant Notify is a wasted Bot API call).
func TestMediaActionControllerTracksCallsIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		defer ctrl.Stop()
		ctrl.Start("one", "image_generate")
		ctrl.Start("two", "image_generate")
		ctrl.Finish("one")
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingPhoto)

		ctrl.Finish("two")
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingPhoto, tele.Typing)
	})
}

// TestMediaActionControllerIgnoresNonMediaTools: a normal tool call never claims an
// upload action — only image_generate / video_generate do.
func TestMediaActionControllerIgnoresNonMediaTools(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		defer ctrl.Stop()
		ctrl.Start("call-1", "document_search")
		ctrl.Finish("call-1")
		synctest.Wait()
		assertActions(t, rn, tele.Typing)
	})
}

// TestMediaActionControllerNoUploadAfterStop: once the turn is over nothing may
// still advertise an upload — a late Start is refused and the pulse is gone, so no
// tick can resurrect the action.
func TestMediaActionControllerNoUploadAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		ctrl.Start("call-1", "video_generate")
		synctest.Wait()
		ctrl.Stop()

		ctrl.Start("late", "video_generate")
		time.Sleep(3 * typingPulse)
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingVideo)
	})
}

// TestMediaActionControllerStopJoinsIdempotently: Stop joins the pulse goroutine and
// tolerates a second call (the pane stops on RUN_FINISHED and again when the event
// channel closes). The package goleak TestMain fails the binary if the join is fake.
func TestMediaActionControllerStopJoinsIdempotently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		ctrl.Stop()
		ctrl.Stop()
	})
}

// TestMediaActionControllerNilSafe: a pane that never entered consume (unit tests
// drive handle directly) has no controller — the calls must be no-ops, not panics.
func TestMediaActionControllerNilSafe(t *testing.T) {
	t.Parallel()
	var ctrl *mediaActionController
	ctrl.Start("call-1", "video_generate")
	ctrl.Finish("call-1")
	ctrl.Stop()
}

// TestMediaActionControllerWithoutNotifier: a botSender that cannot Notify (the
// render-only doubles, and any future non-telebot sender) degrades to a no-op
// controller instead of a nil dereference.
func TestMediaActionControllerWithoutNotifier(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := startedController(context.Background(), nil, tele.ChatID(7))
		ctrl.Start("call-1", "video_generate")
		ctrl.Finish("call-1")
		ctrl.Stop()
	})
}

// TestStatusPaneOwnsTheTurnChatAction is the wiring proof: the status consumer is the
// single action owner for the turn. It opens on typing, switches to upload_video for
// a video_generate call, keeps refreshing THAT action on the existing four-second
// pulse, and returns to typing on the tool result.
func TestStatusPaneOwnsTheTurnChatAction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bot := newPaneActionBot()
		p := newStatusPane(bot, tele.ChatID(7), 0, false, 0)
		ch := make(chan events.Event)
		done := make(chan struct{})
		go func() {
			defer close(done)
			p.consume(context.Background(), ch)
		}()

		ch <- events.NewRunStartedEvent("thread", "run")
		synctest.Wait()
		assertActions(t, bot.recordingNotifier, tele.Typing)

		ch <- events.NewToolCallStartEvent("call-1", "video_generate")
		synctest.Wait()
		time.Sleep(typingPulse + time.Millisecond)
		synctest.Wait()
		assertActions(t, bot.recordingNotifier, tele.Typing, tele.UploadingVideo, tele.UploadingVideo)

		ch <- events.NewToolCallResultEvent("res-1", "call-1", `{"asset_id":"a"}`)
		synctest.Wait()
		assertActions(t, bot.recordingNotifier,
			tele.Typing, tele.UploadingVideo, tele.UploadingVideo, tele.Typing)

		close(ch)
		<-done
	})
}

// TestStatusPaneToolCallEndReleasesTheAction: AG-UI emits TOOL_CALL_END after the
// tool has EXECUTED (translator emitToolInvocation, agent.ToolInvocationEnd), never
// when the streamed arguments finish — so END alone, with no RESULT, is a valid
// terminal and must release the upload action.
func TestStatusPaneToolCallEndReleasesTheAction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bot := newPaneActionBot()
		p := newStatusPane(bot, tele.ChatID(7), 0, false, 0)
		ch := make(chan events.Event)
		done := make(chan struct{})
		go func() {
			defer close(done)
			p.consume(context.Background(), ch)
		}()

		ch <- events.NewToolCallStartEvent("call-1", "image_generate")
		ch <- events.NewToolCallEndEvent("call-1")
		synctest.Wait()
		assertActions(t, bot.recordingNotifier, tele.Typing, tele.UploadingPhoto, tele.Typing)

		close(ch)
		<-done
	})
}

// TestStatusPaneStopsTheActionOnRunFinished: the pulse ends with the turn — after
// RUN_FINISHED no further tick reaches Telegram even before the channel closes.
func TestStatusPaneStopsTheActionOnRunFinished(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bot := newPaneActionBot()
		p := newStatusPane(bot, tele.ChatID(7), 0, false, 0)
		ch := make(chan events.Event)
		done := make(chan struct{})
		go func() {
			defer close(done)
			p.consume(context.Background(), ch)
		}()

		ch <- events.NewToolCallStartEvent("call-1", "image_generate")
		ch <- events.NewRunFinishedEvent("thread", "run")
		synctest.Wait()
		time.Sleep(3 * typingPulse)
		synctest.Wait()
		assertActions(t, bot.recordingNotifier, tele.Typing, tele.UploadingPhoto)

		close(ch)
		<-done
	})
}

// TestMediaActionControllerHoldSurvivesTheToolTerminal: the pane finishes a call twice
// (TOOL_CALL_END then TOOL_CALL_RESULT). Neither may drop an upload that is under way.
func TestMediaActionControllerHoldSurvivesTheToolTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		defer ctrl.Stop()
		ctrl.Start("call-1", "video_generate")
		ctrl.Hold("call-1", tele.UploadingVideo)
		ctrl.Finish("call-1")
		ctrl.Finish("call-1")
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingVideo)

		time.Sleep(typingPulse + time.Millisecond)
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingVideo, tele.UploadingVideo)

		ctrl.Release("call-1")
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingVideo, tele.UploadingVideo, tele.Typing)
	})
}

// TestMediaActionControllerHoldAfterStopIsNoOp: the turn is over, so a late upload may
// not speak — and must never bring the pulse back.
func TestMediaActionControllerHoldAfterStopIsNoOp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		ctrl.Stop()

		ctrl.Hold("late", tele.UploadingVideo)
		ctrl.start(context.Background()) // a restart attempt must be refused too
		time.Sleep(3 * typingPulse)
		synctest.Wait()
		assertActions(t, rn, tele.Typing)
	})
}

// TestMediaActionControllerStopDuringAHoldJoins: an upload that never returns must not
// pin the action or leak the pulse — Stop is the backstop (goleak TestMain proves the
// join).
func TestMediaActionControllerStopDuringAHoldJoins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rn := &recordingNotifier{}
		ctrl := startedController(context.Background(), rn, tele.ChatID(7))
		ctrl.Hold("stuck", tele.UploadingPhoto)
		synctest.Wait()
		ctrl.Stop()

		time.Sleep(3 * typingPulse)
		synctest.Wait()
		assertActions(t, rn, tele.Typing, tele.UploadingPhoto)
	})
}

// TestStatusPaneSharesItsControllerWithTheArtifactConsumer is the wiring proof for the
// hold: the two consumers the per-turn fanout builds must reach the SAME controller, or
// the artifact consumer would hold an action nothing is pulsing.
func TestStatusPaneSharesItsControllerWithTheArtifactConsumer(t *testing.T) {
	t.Parallel()
	tg := NewChannel(Deps{Offline: true})
	status, _, art := tg.consumers(newPaneActionBot(), tele.ChatID(7))
	pane, isPane := status.(*statusPane)
	if !isPane {
		t.Fatalf("status consumer = %T, want *statusPane", status)
	}
	consumer, isArtifact := art.(*artifact)
	if !isArtifact {
		t.Fatalf("artifact consumer = %T, want *artifact", art)
	}
	if pane.actions == nil || pane.actions != consumer.actions {
		t.Fatal("the pane and the artifact consumer must share ONE chat-action controller")
	}
}
