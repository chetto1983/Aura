package telegram

import (
	"context"
	"io"
	"iter"
	"strings"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
)

// dispatchBot is a tele.API double for the inbound-dispatch tests. It embeds the
// API interface (so the methods the handlers never touch stay nil) and overrides
// only the four the dispatch path exercises: Send (replies + the render edits),
// File (media download), React (the STT hard-fail 😵), and Respond (the callback
// ack). It records the sent texts + the React so a test asserts on them.
type dispatchBot struct {
	tele.API

	ogg []byte // canned bytes returned by File (voice/photo/document download)

	mu        sync.Mutex
	sends     []string
	reactions []string
	edits     []editCall
	responses []string
	responds  int
	notifies  int
}

type editCall struct {
	what   any
	markup *tele.ReplyMarkup
}

func (b *dispatchBot) Send(_ tele.Recipient, what any, _ ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := what.(string); ok {
		b.sends = append(b.sends, s)
	}
	return &tele.Message{ID: len(b.sends)}, nil
}

func (b *dispatchBot) Edit(_ tele.Editable, what any, opts ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.edits = append(b.edits, editCall{what: what, markup: markupOf(opts)})
	return &tele.Message{}, nil
}

func (b *dispatchBot) EditReplyMarkup(_ tele.Editable, markup *tele.ReplyMarkup) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.edits = append(b.edits, editCall{markup: markup})
	return &tele.Message{}, nil
}

func (b *dispatchBot) File(_ *tele.File) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b.ogg))), nil
}

func (b *dispatchBot) React(_ tele.Recipient, _ tele.Editable, r tele.Reactions) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, re := range r.Reactions {
		b.reactions = append(b.reactions, re.Emoji)
	}
	return nil
}

func (b *dispatchBot) Respond(_ *tele.Callback, resp ...*tele.CallbackResponse) error {
	b.mu.Lock()
	b.responds++
	if len(resp) > 0 && resp[0] != nil {
		b.responses = append(b.responses, resp[0].Text)
	} else {
		b.responses = append(b.responses, "")
	}
	b.mu.Unlock()
	return nil
}

// Notify records the "Aura is working" chat action (keepWorking). Overriding it
// keeps the embedded nil tele.API from panicking when a handler shows the indicator.
func (b *dispatchBot) Notify(_ tele.Recipient, _ tele.ChatAction, _ ...int) error {
	b.mu.Lock()
	b.notifies++
	b.mu.Unlock()
	return nil
}

func (b *dispatchBot) sentTexts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.sends))
	copy(out, b.sends)
	return out
}

func (b *dispatchBot) recordedReactions() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.reactions))
	copy(out, b.reactions)
	return out
}

func (b *dispatchBot) recordedEdits() []editCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]editCall, len(b.edits))
	copy(out, b.edits)
	return out
}

func (b *dispatchBot) responseTexts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.responses))
	copy(out, b.responses)
	return out
}

// recordingTurn is a turnDriver that records every userMsg it was driven with so a
// media test can assert the transcript/description/markdown reached the turn. It
// yields the lifecycle frames the fanout guarantees (an empty event stream still
// produces RUN_STARTED/RUN_FINISHED) so handleTurn terminates cleanly.
type recordingTurn struct {
	mu    sync.Mutex
	msgs  []string
	calls int
}

func (r *recordingTurn) driver() turnDriver {
	return func(_ context.Context, _ string, userMsg *string) iter.Seq2[*agent.Event, error] {
		r.mu.Lock()
		r.calls++
		if userMsg != nil {
			r.msgs = append(r.msgs, *userMsg)
		} else {
			r.msgs = append(r.msgs, "<resume>")
		}
		r.mu.Unlock()
		return func(yield func(*agent.Event, error) bool) {
			yield(textEvent("ok"), nil)
		}
	}
}

func (r *recordingTurn) snapshot() (calls int, msgs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.msgs))
	copy(out, r.msgs)
	return r.calls, out
}

// recordingFactory returns a consumerFactory whose consumers drain-and-discard, so
// handleTurn's render path runs goleak-clean without the real status_pane/renderer
// (the dispatch tests assert on routing, not on the rendered bytes).
func recordingFactory() consumerFactory {
	return func(_ botSender, _ tele.Recipient) (eventConsumer, eventConsumer, eventConsumer) {
		return newRecordingConsumer(), newRecordingConsumer(), newRecordingConsumer()
	}
}

// dispatchChannel builds a Telegram channel wired with a recording turn driver +
// drain consumers + the supplied Deps overlay, then builds the dispatch instances
// (as Start would) WITHOUT touching the network.
func dispatchChannel(t *testing.T, rt *recordingTurn, overlay func(*Deps)) *Telegram {
	t.Helper()
	d := Deps{
		Turn:            rt.driver(),
		Offline:         true,
		consumerFactory: recordingFactory(),
	}
	if overlay != nil {
		overlay(&d)
	}
	tg := NewChannel(d)
	tg.buildDispatch()
	return tg
}

// msgContext builds a tele.Context for an inbound message update over the dispatch
// bot, so the handler sees c.Message()/c.Text()/c.Bot() exactly as telebot would.
func msgContext(bot tele.API, msg *tele.Message) tele.Context {
	return tele.NewContext(bot, tele.Update{Message: msg})
}

func chatMsg(chatID int64) *tele.Message {
	return &tele.Message{Chat: &tele.Chat{ID: chatID}}
}

// TestOnTextCommandInterceptNoTurn proves a /command is intercepted BEFORE the LLM:
// dispatch sends the reply and the turn driver is NEVER called (T-13-10-CmdToLLM).
func TestOnTextCommandInterceptNoTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{prompt: 1_000_000, completion: 0}
		d.Prices = map[string]llm.Price{"m": {InputPer1M: 0.1, OutputPer1M: 0.2}}
		d.Model = "m"
		d.Search = &fakeSearch{}
	})

	bot := &dispatchBot{}
	msg := chatMsg(42)
	msg.Text = "/cost"

	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(/cost): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a /command must NOT drive a turn, got %d turn calls", calls)
	}
	if len(bot.sentTexts()) == 0 {
		t.Error("/cost must send a user-facing reply")
	}
}

// TestOnTextStartPayloadRoutesToOnboarding proves a Telegram deep link
// (/start <token>) is routed to the onboarding handler before the generic command
// dispatcher. Without this, the setup deep link silently falls through to the
// ordinary /start greeting and the token is never consumed.
func TestOnTextStartPayloadRoutesToOnboarding(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
	})

	bot := &dispatchBot{}
	msg := chatMsg(42)
	msg.Sender = &tele.User{ID: 555, Username: "dav", FirstName: "Davide"}
	msg.Text = "/start tok-good"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(/start token): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a /start onboarding payload must not drive a turn, got %d calls", calls)
	}
	texts := bot.sentTexts()
	if len(texts) != 1 {
		t.Fatalf("expected one onboarding reply, got %d: %v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Onboarding non disponibile") {
		t.Fatalf("/start token was not routed to onboarding, reply: %q", texts[0])
	}
}

// TestOnTextPlainMessageDrivesTurn proves a non-command message is NOT intercepted
// and drives a turn with the message text.
func TestOnTextPlainMessageDrivesTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
	})

	bot := &dispatchBot{}
	msg := chatMsg(7)
	msg.Text = "ciao Aura"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(plain): %v", err)
	}
	tg.wg.Wait() // the turn now runs async off the poller — join it before asserting

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("a plain message must drive exactly 1 turn, got %d", calls)
	}
	if len(msgs) != 1 || msgs[0] != "ciao Aura" {
		t.Errorf("turn userMsg = %v, want [ciao Aura]", msgs)
	}
}

// TestOnTextCancelInterruptsRunningTurn proves the live poller-blocking fix: a turn
// runs OFF the handler goroutine, so while one is in-flight (a) a second message is
// rejected with the busy copy (no concurrent turn on the same conversation) and (b)
// /cancel reaches its handler and aborts the running turn. If the turn ran
// synchronously the first onText would block forever and this test would time out —
// its completion is itself proof the dispatch is async.
func TestOnTextCancelInterruptsRunningTurn(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	var once sync.Once
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Turn = func(ctx context.Context, _ string, _ *string) iter.Seq2[*agent.Event, error] {
			return func(_ func(*agent.Event, error) bool) {
				once.Do(func() { close(started) })
				<-ctx.Done() // block until /cancel fires the turn ctx
			}
		}
	})

	bot := &dispatchBot{}
	handle := tg.onText(context.Background())

	first := chatMsg(7)
	first.Text = "scrivimi una storia lunga"
	if err := handle(msgContext(bot, first)); err != nil { // returns immediately if async
		t.Fatalf("onText(first): %v", err)
	}
	<-started // turn 1 is in-flight + registered

	second := chatMsg(7)
	second.Text = "un'altra cosa"
	if err := handle(msgContext(bot, second)); err != nil { // busy → no 2nd turn
		t.Fatalf("onText(second): %v", err)
	}

	cancelMsg := chatMsg(7)
	cancelMsg.Text = "/cancel"
	if err := handle(msgContext(bot, cancelMsg)); err != nil { // reaches handler → aborts turn 1
		t.Fatalf("onText(/cancel): %v", err)
	}
	tg.wg.Wait() // turn 1 unblocks once its ctx is cancelled

	var busy, cancelled bool
	for _, s := range bot.sentTexts() {
		busy = busy || strings.Contains(s, "ancora elaborando")
		cancelled = cancelled || strings.Contains(s, "annullato")
	}
	if !busy {
		t.Errorf("a 2nd message while a turn runs must get the busy copy; texts=%v", bot.sentTexts())
	}
	if !cancelled {
		t.Errorf("/cancel must abort the running turn and confirm; texts=%v", bot.sentTexts())
	}
}

// TestOnCallbackRoutesToHITL proves an inline-button callback resolves the pause
// through the Runner seam (SubmitAnswer) and acks the callback. The fakeResume
// records the submit; the resume render is exercised by the handleTurn path.
func TestOnCallbackRoutesToHITL(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 0}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Resume = rs
	})

	bot := &dispatchBot{}
	cb := &tele.Callback{
		Message: chatMsg(41),
		Data:    callbackData("tok-1", askuser.ActionAccept, "cuneo"),
	}
	ctxFn := tg.onCallback(context.Background())
	if err := ctxFn(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onCallback: %v", err)
	}
	tg.wg.Wait()

	calls := rs.calls()
	if len(calls) != 1 {
		t.Fatalf("a callback must submit exactly one answer, got %d", len(calls))
	}
	if calls[0].token != "tok-1" || calls[0].action != askuser.ActionAccept || calls[0].content != "cuneo" {
		t.Errorf("submit = %+v, want {tok-1 accept cuneo}", calls[0])
	}
	if bot.responds != 1 {
		t.Errorf("a callback must be acked exactly once, got %d", bot.responds)
	}
	// remaining==0 → the resume drives a continuation turn (nil userMsg) through the
	// per-turn fanout, so the resumed answer renders to the chat.
	if calls2, msgs := rt.snapshot(); calls2 != 1 || msgs[0] != "<resume>" {
		t.Errorf("resume must drive 1 continuation turn (nil userMsg), got calls=%d msgs=%v", calls2, msgs)
	}
}

// TestOnReplyForceReplyAnswerResumes proves a free-text ForceReply answer (with a
// pending pause) routes to HITL and submits the answer.
func TestOnReplyForceReplyAnswerResumes(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{
		remaining: 0,
		pending:   []askuser.Pending{{Token: "tok-c", Kind: "clarification", Question: "Nome?"}},
	}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Resume = rs
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
	})

	bot := &dispatchBot{}
	msg := chatMsg(51)
	msg.Text = "Davide"
	msg.ReplyTo = &tele.Message{ID: 1}
	if err := tg.onReply(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onReply: %v", err)
	}
	tg.wg.Wait()

	calls := rs.calls()
	if len(calls) != 1 || calls[0].content != "Davide" || calls[0].action != askuser.ActionAccept {
		t.Errorf("ForceReply submit = %+v, want {tok-c accept Davide}", calls)
	}
}

func TestOnReplyThenOnTextDoesNotDoubleDispatchForceReply(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{
		remaining:            0,
		pending:              []askuser.Pending{{Token: "tok-c", Kind: "clarification", Question: "Nome?"}},
		clearPendingOnSubmit: true,
	}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Resume = rs
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
	})

	bot := &dispatchBot{}
	msg := chatMsg(51)
	msg.ID = 99
	msg.Text = "Davide"
	msg.ReplyTo = &tele.Message{ID: 1}
	if err := tg.onReply(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onReply: %v", err)
	}
	tg.wg.Wait()
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(after OnReply): %v", err)
	}
	tg.wg.Wait()

	if len(rs.calls()) != 1 {
		t.Fatalf("ForceReply answer must submit once, got %d submits", len(rs.calls()))
	}
	_, msgs := rt.snapshot()
	for _, m := range msgs {
		if m == "Davide" {
			t.Fatalf("OnText must not drive a fresh turn for a ForceReply answer after OnReply; msgs=%v", msgs)
		}
	}
}

// TestOnTextPendingPauseRoutesToHITL proves a non-command message routed while a
// pause is pending is consumed by HITL (a free-text answer), NOT driven as a fresh
// turn.
func TestOnTextPendingPauseRoutesToHITL(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{
		remaining: 0,
		pending:   []askuser.Pending{{Token: "tok-c", Kind: "clarification", Question: "Nome?"}},
	}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Resume = rs
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
	})

	bot := &dispatchBot{}
	msg := chatMsg(61)
	msg.Text = "Davide"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(pending): %v", err)
	}
	tg.wg.Wait()

	if len(rs.calls()) != 1 {
		t.Fatalf("a pending pause must consume the message via HITL, got %d submits", len(rs.calls()))
	}
	// The fresh-turn driver must NOT have been called for the answer itself; only the
	// resume continuation turn (nil userMsg) runs.
	_, msgs := rt.snapshot()
	for _, m := range msgs {
		if m == "Davide" {
			t.Error("a pending-pause answer must NOT drive a fresh turn on the answer text")
		}
	}
}
