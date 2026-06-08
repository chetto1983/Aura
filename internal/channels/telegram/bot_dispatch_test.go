package telegram

import (
	"context"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
	responds  int
}

func (b *dispatchBot) Send(_ tele.Recipient, what any, _ ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := what.(string); ok {
		b.sends = append(b.sends, s)
	}
	return &tele.Message{ID: len(b.sends)}, nil
}

func (b *dispatchBot) Edit(_ tele.Editable, _ any, _ ...any) (*tele.Message, error) {
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

func (b *dispatchBot) Respond(_ *tele.Callback, _ ...*tele.CallbackResponse) error {
	b.mu.Lock()
	b.responds++
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
	return func(_ botSender, _ tele.Recipient) (eventConsumer, eventConsumer) {
		return newRecordingConsumer(), newRecordingConsumer()
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

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("a plain message must drive exactly 1 turn, got %d", calls)
	}
	if len(msgs) != 1 || msgs[0] != "ciao Aura" {
		t.Errorf("turn userMsg = %v, want [ciao Aura]", msgs)
	}
}

// TestOnVoiceRoutesToTranscribe proves a voice update downloads the OGG bytes and
// drives a turn on the transcript (OnVoice → voiceClient.Transcribe → turn).
func TestOnVoiceRoutesToTranscribe(t *testing.T) {
	t.Parallel()
	ogg := []byte("OggS\x00opus")
	srv := httptest.NewServer(sttHandler(t, ogg, "ciao da nota vocale", http.StatusOK))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{STTBaseURL: srv.URL, STTModel: "large-v3-turbo"}
	})

	bot := &dispatchBot{ogg: ogg}
	msg := chatMsg(11)
	msg.Voice = &tele.Voice{}
	if err := tg.onVoice(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onVoice: %v", err)
	}

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("a voice note must drive 1 turn on the transcript, got %d", calls)
	}
	if msgs[0] != "ciao da nota vocale" {
		t.Errorf("turn userMsg = %q, want the transcript", msgs[0])
	}
}

// TestOnVoiceHardFailReactsAndDoesNotTurn proves a persistent STT 5xx sends the IT
// copy + the 😵 reaction and drives NO turn.
func TestOnVoiceHardFailReactsAndDoesNotTurn(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		mm := MultimodalConfig{STTBaseURL: srv.URL, RetryBackoff: []int{1, 1}}
		d.Multimodal = mm
	})

	bot := &dispatchBot{ogg: []byte("OggS")}
	msg := chatMsg(13)
	msg.Voice = &tele.Voice{}
	if err := tg.onVoice(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onVoice(hardfail): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a hard STT failure must NOT drive a turn, got %d", calls)
	}
	if react := bot.recordedReactions(); len(react) != 1 || react[0] != hardFailReaction {
		t.Errorf("hard-fail reaction = %v, want [%q]", react, hardFailReaction)
	}
	if texts := bot.sentTexts(); len(texts) == 0 || !strings.Contains(texts[0], "Trascrizione non disponibile") {
		t.Errorf("hard-fail must send the IT copy, got %v", texts)
	}
}

// TestOnPhotoRoutesToDescribe proves a photo update downloads the bytes and drives
// a turn on the vision description (OnPhoto → photoClient.Describe → turn).
func TestOnPhotoRoutesToDescribe(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(visionHandler(t, &hits, "glm-ocr", "una foto di un gatto"))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{VisionCloud: false, MultimodalBaseURL: srv.URL, MultimodalModel: "glm-ocr"}
	})

	bot := &dispatchBot{ogg: []byte("\x89PNGfake")}
	msg := chatMsg(21)
	msg.Photo = &tele.Photo{}
	msg.Caption = "cosa c'è qui?"
	if err := tg.onPhoto(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onPhoto: %v", err)
	}

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("a photo must drive 1 turn on the description, got %d", calls)
	}
	if msgs[0] != "una foto di un gatto" {
		t.Errorf("turn userMsg = %q, want the description", msgs[0])
	}
	if hits.Load() != 1 {
		t.Errorf("vision sidecar hits = %d, want 1", hits.Load())
	}
}

// TestOnDocumentSyncRoutesToConvert proves a ≤5MB document downloads + converts
// inline and drives a turn on the markdown (OnDocument → documentsClient.Convert →
// turn). Stop drains any async work goleak-clean.
func TestOnDocumentSyncRoutesToConvert(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "# Relazione", http.StatusOK))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{DocumentsBaseURL: srv.URL}
	})
	defer tg.docs.Stop(context.Background())

	bot := &dispatchBot{ogg: []byte("small pdf")}
	msg := chatMsg(31)
	msg.Document = &tele.Document{FileName: "doc.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument: %v", err)
	}

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("a ≤5MB document must drive 1 turn on the markdown, got %d", calls)
	}
	if !strings.Contains(msgs[0], "Relazione") {
		t.Errorf("turn userMsg = %q, want the converted markdown", msgs[0])
	}
	if hits.Load() != 1 {
		t.Errorf("convert hits = %d, want 1", hits.Load())
	}
}

// TestOnDocumentRefuseTier proves a >50MB document is refused with the user-facing
// message and drives NO turn + NO sidecar call.
func TestOnDocumentRefuseTier(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "nope", http.StatusOK))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{DocumentsBaseURL: srv.URL}
	})
	defer tg.docs.Stop(context.Background())

	bot := &dispatchBot{ogg: make([]byte, refuseTierMinBytes+1)}
	msg := chatMsg(33)
	msg.Document = &tele.Document{FileName: "huge.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument(refuse): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a refused document must NOT drive a turn, got %d", calls)
	}
	if hits.Load() != 0 {
		t.Errorf("a refused document must NOT hit the sidecar, got %d", hits.Load())
	}
	if texts := bot.sentTexts(); len(texts) != 1 || !strings.Contains(texts[0], "troppo grande") {
		t.Errorf("a refused document must send the refuse copy, got %v", texts)
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

	calls := rs.calls()
	if len(calls) != 1 || calls[0].content != "Davide" || calls[0].action != askuser.ActionAccept {
		t.Errorf("ForceReply submit = %+v, want {tok-c accept Davide}", calls)
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
