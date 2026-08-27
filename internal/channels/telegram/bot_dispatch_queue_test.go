package telegram

import (
	"context"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	assetspkg "github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/steer/steertest"
)

// gatedCall records one turnDriver invocation: the composed userMsg it was
// driven with, and a per-call gate the test closes to let THAT specific
// invocation return. A single shared channel cannot do this because the media
// queue's delivered turn re-invokes the driver from WITHIN the same startTurn
// goroutine — the test needs to control each hop independently to prove the
// chain cap (TestQueueChainIsBounded).
type gatedCall struct {
	gate    chan struct{}
	userMsg string
}

// gatedTurnDriver returns a turnDriver whose every invocation blocks on its OWN
// gate (or ctx.Done(), so a real /cancel still unblocks it) and publishes a
// gatedCall the test reads to control that invocation precisely.
func gatedTurnDriver(calls *atomic.Int32) (turnDriver, chan gatedCall) {
	ch := make(chan gatedCall, 8)
	driver := func(ctx context.Context, _ string, userMsg *string) iter.Seq2[*agent.Event, error] {
		calls.Add(1)
		call := gatedCall{gate: make(chan struct{})}
		if userMsg != nil {
			call.userMsg = *userMsg
		}
		ch <- call
		return func(_ func(*agent.Event, error) bool) {
			select {
			case <-call.gate:
			case <-ctx.Done():
			}
		}
	}
	return driver, ch
}

// TestNonTextDuringLiveTurnIsQueuedNotDropped proves D-05 by fixing today's
// defect: a media message arriving on a busy chat DOES eventually drive its own
// turn — proven by COUNTING turn invocations, never merely by observing a reply
// (a reply-only assertion would pass against today's silent drop too).
func TestNonTextDuringLiveTurnIsQueuedNotDropped(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	rt := &recordingTurn{}
	driver, gates := gatedTurnDriver(&calls)
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Steer = steertest.New(steer.Config{})
		d.Turn = driver
	})

	bot := &dispatchBot{}
	handle := tg.onText(context.Background())

	first := chatMsg(21)
	first.Text = "richiesta lunga"
	if err := handle(msgContext(bot, first)); err != nil {
		t.Fatalf("onText(first): %v", err)
	}
	hop1 := <-gates

	media := chatMsg(21)
	tg.runTurnWithAssets(context.Background(), msgContext(bot, media), 21,
		"Analizza l'allegato Telegram.", []assetspkg.Asset{{ID: "a1"}}, false)

	close(hop1.gate) // the live turn ends normally -> the queued media is delivered
	hop2 := <-gates
	close(hop2.gate)
	tg.wg.Wait()

	if got := calls.Load(); got != 2 {
		t.Fatalf("a queued media message must eventually drive its OWN turn (today's defect drops it); calls=%d, want 2", got)
	}
	var gotQueued bool
	for _, s := range bot.sentTexts() {
		if s == turnBusyMessage {
			t.Fatalf("a wired steer inbox must never drop a media message to turnBusyMessage; got %v", bot.sentTexts())
		}
		if s == turnQueuedForNextTurnMessage {
			gotQueued = true
		}
	}
	if !gotQueued {
		t.Fatalf("a queued media message must be told it will run after the current request; got %v", bot.sentTexts())
	}
}

// TestQueuedTurnDeliveredAfterLiveTurnEnds proves the delivered turn carries the
// EXACT already-composed text that was queued, under the runner's own
// handleTurn call — not a re-composed or truncated variant.
func TestQueuedTurnDeliveredAfterLiveTurnEnds(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	rt := &recordingTurn{}
	driver, gates := gatedTurnDriver(&calls)
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Steer = steertest.New(steer.Config{})
		d.Turn = driver
	})

	bot := &dispatchBot{}
	handle := tg.onText(context.Background())

	first := chatMsg(22)
	first.Text = "richiesta lunga"
	if err := handle(msgContext(bot, first)); err != nil {
		t.Fatalf("onText(first): %v", err)
	}
	hop1 := <-gates

	const attachmentText = "Analizza l'allegato Telegram."
	media := chatMsg(22)
	tg.runTurnWithAssets(context.Background(), msgContext(bot, media), 22,
		attachmentText, []assetspkg.Asset{{ID: "a1"}}, false)

	close(hop1.gate)
	hop2 := <-gates
	close(hop2.gate)
	tg.wg.Wait()

	if got := calls.Load(); got != 2 {
		t.Fatalf("the queued message must drive exactly one more turn, got %d calls", got)
	}
	if hop2.userMsg != attachmentText {
		t.Fatalf("delivered turn userMsg = %q, want the queued composed text %q", hop2.userMsg, attachmentText)
	}
}

// TestQueuedTurnNotDeliveredOnCancelIsAnnounced proves T-52-35: if the live turn
// is cancelled (/cancel) before it could deliver, the queued message is NOT
// silently swallowed — the operator is told it did not run, and no second turn
// is driven for it.
func TestQueuedTurnNotDeliveredOnCancelIsAnnounced(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	rt := &recordingTurn{}
	driver, gates := gatedTurnDriver(&calls)
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Steer = steertest.New(steer.Config{})
		d.Turn = driver
	})

	bot := &dispatchBot{}
	handle := tg.onText(context.Background())

	first := chatMsg(23)
	first.Text = "richiesta lunga"
	if err := handle(msgContext(bot, first)); err != nil {
		t.Fatalf("onText(first): %v", err)
	}
	<-gates // hop 1 started; discard its gate — this turn is CANCELLED, not released

	media := chatMsg(23)
	tg.runTurnWithAssets(context.Background(), msgContext(bot, media), 23,
		"Analizza l'allegato Telegram.", []assetspkg.Asset{{ID: "a1"}}, false)

	cancelMsg := chatMsg(23)
	cancelMsg.Text = "/cancel"
	if err := handle(msgContext(bot, cancelMsg)); err != nil {
		t.Fatalf("onText(/cancel): %v", err)
	}
	tg.wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("a cancelled live turn must NOT deliver the queued message, got %d calls", got)
	}
	var gotNotDelivered bool
	for _, s := range bot.sentTexts() {
		if s == turnQueuedNotDeliveredMessage {
			gotNotDelivered = true
		}
	}
	if !gotNotDelivered {
		t.Fatalf("a cancelled live turn must announce the queued message did NOT run; got %v", bot.sentTexts())
	}
	if pending, ok := tg.takePendingTurns(23); ok {
		t.Fatalf("the pending slot must be cleared on cancel, got %v", pending)
	}
}

// TestQueueChainIsBounded proves T-52-36: a media message queued DURING the
// delivered (second) turn is queued again for after it, but the delivery chain
// within one startTurn goroutine is capped at ONE hop — no unbounded recursion.
func TestQueueChainIsBounded(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	rt := &recordingTurn{}
	driver, gates := gatedTurnDriver(&calls)
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Steer = steertest.New(steer.Config{})
		d.Turn = driver
	})

	bot := &dispatchBot{}
	handle := tg.onText(context.Background())

	first := chatMsg(24)
	first.Text = "richiesta lunga"
	if err := handle(msgContext(bot, first)); err != nil {
		t.Fatalf("onText(first): %v", err)
	}
	hop1 := <-gates

	media1 := chatMsg(24)
	tg.runTurnWithAssets(context.Background(), msgContext(bot, media1), 24, "primo allegato", []assetspkg.Asset{{ID: "a1"}}, false)

	close(hop1.gate) // hop 1 ends normally -> delivers "primo allegato" as hop 2
	hop2 := <-gates

	// a SECOND media message arrives while hop 2 (the delivered turn) is live —
	// the SAME registration is still held, so this queues again rather than
	// being accepted as a fresh turn.
	media2 := chatMsg(24)
	tg.runTurnWithAssets(context.Background(), msgContext(bot, media2), 24, "secondo allegato", []assetspkg.Asset{{ID: "a2"}}, false)

	close(hop2.gate) // hop 2 ends normally too
	tg.wg.Wait()

	if got := calls.Load(); got != 2 {
		t.Fatalf("the chain must be capped at ONE auto-delivery hop, got %d calls (a third would be unbounded recursion)", got)
	}
	pending, ok := tg.takePendingTurns(24)
	if !ok || len(pending) != 1 || pending[0].text != "secondo allegato" {
		t.Fatalf("the message queued during the delivered turn must remain queued for the NEXT turn, got %v ok=%v", pending, ok)
	}
}

// TestMediaQueueNilSteerDegradesToBusy proves the rollback: with Steer unwired,
// a media message on a busy chat gets today's turnBusyMessage and is never
// enqueued — no half-live queue.
func TestMediaQueueNilSteerDegradesToBusy(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	rt := &recordingTurn{}
	driver, gates := gatedTurnDriver(&calls)
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Turn = driver // d.Steer left nil (the rollback)
	})

	bot := &dispatchBot{}
	handle := tg.onText(context.Background())

	first := chatMsg(25)
	first.Text = "richiesta lunga"
	if err := handle(msgContext(bot, first)); err != nil {
		t.Fatalf("onText(first): %v", err)
	}
	hop1 := <-gates

	media := chatMsg(25)
	tg.runTurnWithAssets(context.Background(), msgContext(bot, media), 25,
		"Analizza l'allegato Telegram.", []assetspkg.Asset{{ID: "a1"}}, false)

	close(hop1.gate)
	tg.wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("a nil Steer must never enqueue a follow-on turn, got %d calls", got)
	}
	var gotBusy bool
	for _, s := range bot.sentTexts() {
		if s == turnQueuedForNextTurnMessage {
			t.Fatalf("a nil Steer must never enqueue; got the queued-notice among %v", bot.sentTexts())
		}
		if s == turnBusyMessage {
			gotBusy = true
		}
	}
	if !gotBusy {
		t.Fatalf("a nil Steer must degrade a busy media message to turnBusyMessage; got %v", bot.sentTexts())
	}
	if pending, ok := tg.takePendingTurns(25); ok {
		t.Fatalf("a nil Steer must never populate the pending slot, got %v", pending)
	}
}

// TestStopIsGoleakCleanWithOutstandingPendingSlot proves Stop drains cleanly
// (no leaked goroutine, no panic) even when a chat's pending slot still holds an
// undelivered media message — the package goleak TestMain fails the binary if
// anything leaked.
func TestStopIsGoleakCleanWithOutstandingPendingSlot(t *testing.T) {
	tg := NewChannel(Deps{Token: "123:offline", Offline: true})
	if err := tg.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	tg.pendingMu.Lock()
	tg.pendingTurns = map[int64][]pendingTurn{123: {{text: "allegato mai consegnato"}}}
	tg.pendingMu.Unlock()

	if err := tg.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if tg.IsHealthy() {
		t.Fatal("channel still healthy after Stop")
	}
}
