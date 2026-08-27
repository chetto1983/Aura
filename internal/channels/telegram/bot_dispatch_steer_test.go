package telegram

import (
	"context"
	"errors"
	"iter"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	assetspkg "github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/steer/steertest"
	tele "gopkg.in/telebot.v4"
)

// blockingTurnDriver returns a turnDriver that closes started (once, across every
// call) and then blocks on ctx.Done() — the shared "a turn is live for this chat"
// fixture the busy/steer/queue tests key off (mirrors the inline shape
// TestOnTextCancelInterruptsRunningTurn already established; extracted here so
// bot_dispatch_queue_test.go reuses it instead of re-deriving it). calls counts
// every invocation so a test can assert a steer never starts a SECOND turn.
func blockingTurnDriver(started chan struct{}, calls *atomic.Int32) turnDriver {
	var once sync.Once
	return func(ctx context.Context, _ string, _ *string) iter.Seq2[*agent.Event, error] {
		calls.Add(1)
		return func(_ func(*agent.Event, error) bool) {
			once.Do(func() { close(started) })
			<-ctx.Done()
		}
	}
}

// composedTextAssets is a minimal assetIngress double whose BuildTurnContext
// always prepends a fixed catalog marker, independent of attachments — proving a
// steer captures the RAW text BEFORE this composition, never after (D-07/T-52-33).
type composedTextAssets struct{}

func (composedTextAssets) IngestTelegramFile(context.Context, assetspkg.TelegramIngestRequest) (assetspkg.Asset, error) {
	return assetspkg.Asset{}, errors.New("not used")
}

func (composedTextAssets) GetForIdentity(context.Context, string, string) (assetspkg.Asset, error) {
	return assetspkg.Asset{}, errors.New("not used")
}

func (composedTextAssets) BuildTurnContext(_ context.Context, _, _ string, _ []assetspkg.Asset, userText string) string {
	return "[CATALOG]\n" + userText
}

// driveBusyScenario sends first on chatID, waits for the turn to be live, sends
// second, then /cancel to unwind the blocked first turn — the shared shape every
// steer/queue busy-path test drives.
func driveBusyScenario(t *testing.T, tg *Telegram, bot *dispatchBot, started chan struct{}, chatID int64, first, second *tele.Message) {
	t.Helper()
	handle := tg.onText(context.Background())
	if err := handle(msgContext(bot, first)); err != nil {
		t.Fatalf("onText(first): %v", err)
	}
	<-started
	if err := handle(msgContext(bot, second)); err != nil {
		t.Fatalf("onText(second): %v", err)
	}
	cancelMsg := chatMsg(chatID)
	cancelMsg.Text = "/cancel"
	if err := handle(msgContext(bot, cancelMsg)); err != nil {
		t.Fatalf("onText(/cancel): %v", err)
	}
	tg.wg.Wait()
}

// TestPlainTextDuringLiveTurnSteers proves D-03: a plain-text message arriving
// while a turn is live for the chat pushes onto the steer inbox and echoes the
// redirect (D-04) — it never falls back to turnBusyMessage, and it never starts a
// second turn.
func TestPlainTextDuringLiveTurnSteers(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	var calls atomic.Int32
	inbox := steertest.New(steer.Config{})
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Steer = inbox
		d.Turn = blockingTurnDriver(started, &calls)
	})

	bot := &dispatchBot{}
	first := chatMsg(7)
	first.Text = "scrivi un report lungo"
	second := chatMsg(7)
	second.Text = "anzi usa i dati di ieri"
	driveBusyScenario(t, tg, bot, started, 7, first, second)

	texts := bot.sentTexts()
	var gotSteered bool
	for _, s := range texts {
		if s == turnBusyMessage {
			t.Fatalf("a wired steer inbox must never fall back to turnBusyMessage; got %v", texts)
		}
		if s == turnSteeredMessage {
			gotSteered = true
		}
	}
	if !gotSteered {
		t.Fatalf("plain text during a live turn must echo turnSteeredMessage; got %v", texts)
	}

	msgs := inbox.Drain(convID(7))
	if len(msgs) != 1 || msgs[0].Text != "anzi usa i dati di ieri" {
		t.Fatalf("steer inbox must hold exactly the redirected text, got %v", msgs)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("a steer must not start a second turn, got %d turn(s)", got)
	}
}

// TestSteerCarriesRawTextNotComposedContext closes T-52-33: the pushed steer text
// is the operator's raw message, never composeTurnContext's attachment
// block/knowledge catalog.
func TestSteerCarriesRawTextNotComposedContext(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	var calls atomic.Int32
	inbox := steertest.New(steer.Config{})
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Steer = inbox
		d.Assets = composedTextAssets{}
		d.Turn = blockingTurnDriver(started, &calls)
	})

	bot := &dispatchBot{}
	first := chatMsg(7)
	first.Text = "prima richiesta"
	const raw = "correggi cosi"
	second := chatMsg(7)
	second.Text = raw
	driveBusyScenario(t, tg, bot, started, 7, first, second)

	msgs := inbox.Drain(convID(7))
	if len(msgs) != 1 {
		t.Fatalf("expected exactly one steered message, got %v", msgs)
	}
	if msgs[0].Text != raw {
		t.Fatalf("steer must carry the RAW text, got %q want %q", msgs[0].Text, raw)
	}
	if strings.Contains(msgs[0].Text, "[CATALOG]") {
		t.Fatalf("steer text must not carry composeTurnContext's catalog block, got %q", msgs[0].Text)
	}
}

// TestHitlResumeIsNotASteer proves #132 item 7: an ask_user-paused run's
// continuation is terminal, not steerable — the HITL resume path keeps sendBusy's
// byte-identical turnBusyMessage even with a wired steer inbox, and never pushes.
func TestHitlResumeIsNotASteer(t *testing.T) {
	t.Parallel()
	inbox := steertest.New(steer.Config{})
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Steer = inbox
	})

	bot := &dispatchBot{}
	c := msgContext(bot, chatMsg(7))
	if !tg.cmds.registerTurn(7, func() {}) {
		t.Fatal("registerTurn must succeed for a fresh chat")
	}
	defer tg.cmds.unregisterTurn(7)

	h := tg.hitlFor(c, 7)
	h.resume(context.Background(), convID(7))
	tg.wg.Wait()

	texts := bot.sentTexts()
	if len(texts) != 1 || texts[0] != turnBusyMessage {
		t.Fatalf("HITL resume onto a busy chat must send turnBusyMessage byte-identically, got %v", texts)
	}
	if msgs := inbox.Drain(convID(7)); len(msgs) != 0 {
		t.Fatalf("HITL resume must never push to the steer inbox, got %v", msgs)
	}
}

// TestNilSteerInboxDegradesToBusy proves the rollback: with Steer unwired, a
// plain-text message on a busy chat gets today's turnBusyMessage, never the
// steer echo — no panic, no half-live branch.
func TestNilSteerInboxDegradesToBusy(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	var calls atomic.Int32
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Turn = blockingTurnDriver(started, &calls) // d.Steer left nil (the rollback)
	})

	bot := &dispatchBot{}
	first := chatMsg(7)
	first.Text = "lunga richiesta"
	second := chatMsg(7)
	second.Text = "correzione"
	driveBusyScenario(t, tg, bot, started, 7, first, second)

	var gotBusy bool
	for _, s := range bot.sentTexts() {
		if s == turnSteeredMessage {
			t.Fatalf("a nil Steer must never redirect; got the steer echo among %v", bot.sentTexts())
		}
		if s == turnBusyMessage {
			gotBusy = true
		}
	}
	if !gotBusy {
		t.Fatalf("a nil Steer must degrade to turnBusyMessage; got %v", bot.sentTexts())
	}
}

// TestTelegramConvIDIsTheInboxKey closes FA-4: the key steerBusyTurn pushes under
// is derived from the SAME convID(chatID) call the runner is actually driven
// with — captured from the real turnDriver invocation, never hardcoded or
// re-derived, so a future divergence between the two call sites fails this test.
func TestTelegramConvIDIsTheInboxKey(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var drivenConvID string
	inbox := steertest.New(steer.Config{})
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Steer = inbox
		d.Turn = func(ctx context.Context, conv string, _ *string) iter.Seq2[*agent.Event, error] {
			mu.Lock()
			drivenConvID = conv
			mu.Unlock()
			return func(_ func(*agent.Event, error) bool) {
				once.Do(func() { close(started) })
				<-ctx.Done()
			}
		}
	})

	bot := &dispatchBot{}
	first := chatMsg(9)
	first.Text = "turno live"
	second := chatMsg(9)
	second.Text = "steer questo"
	driveBusyScenario(t, tg, bot, started, 9, first, second)

	mu.Lock()
	key := drivenConvID
	mu.Unlock()
	if key == "" {
		t.Fatal("the turn driver was never invoked")
	}
	msgs := inbox.Drain(key)
	if len(msgs) != 1 || msgs[0].Text != "steer questo" {
		t.Fatalf("the steer must land under the SAME conv id the runner drives the turn with; drained=%v", msgs)
	}
}
