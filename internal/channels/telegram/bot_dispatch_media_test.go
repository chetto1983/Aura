package telegram

import (
	"context"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
)

// TestOnVoiceRoutesToTranscribe proves a voice update downloads the OGG bytes and
// drives a turn on the transcript (OnVoice → voiceClient.Transcribe → turn).
func TestOnVoiceRoutesToTranscribe(t *testing.T) {
	t.Parallel()
	ogg := []byte("OggS\x00opus")
	srv := httptest.NewServer(sttHandler(t, ogg, "ciao da nota vocale", http.StatusOK))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{STTBaseURL: srv.URL, STTModel: "large-v3-turbo", STTLanguage: "it"}
	})

	bot := &dispatchBot{ogg: ogg}
	msg := chatMsg(11)
	msg.Voice = &tele.Voice{}
	if err := tg.onVoice(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onVoice: %v", err)
	}
	tg.wg.Wait() // the turn now runs async off the poller — join it before asserting

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
	tg.wg.Wait() // the turn now runs async off the poller — join it before asserting

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
	tg.wg.Wait() // the turn now runs async off the poller — join it before asserting

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

// TestOnDocumentAsyncResultHonorsBusyGate proves the async document completion
// path uses the same per-chat busy gate as ordinary inbound turns.
func TestOnDocumentAsyncResultHonorsBusyGate(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "# Relazione async", http.StatusOK))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{DocumentsBaseURL: srv.URL}
	})
	defer tg.docs.Stop(context.Background())

	const chatID int64 = 32
	if !tg.cmds.registerTurn(chatID, func() {}) {
		t.Fatal("failed to pre-register busy turn")
	}
	defer tg.cmds.unregisterTurn(chatID)

	bot := &dispatchBot{ogg: make([]byte, asyncTierMinBytes+1)}
	msg := chatMsg(chatID)
	msg.Document = &tele.Document{FileName: "async.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument(async busy): %v", err)
	}
	tg.docs.Stop(context.Background())
	tg.wg.Wait()

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("async document result must not bypass busy gate, got %d turn calls", calls)
	}
	var sawBusy bool
	for _, text := range bot.sentTexts() {
		if strings.Contains(text, turnBusyMessage) {
			sawBusy = true
		}
	}
	if !sawBusy {
		t.Fatalf("async document busy result must send busy copy, sent=%v", bot.sentTexts())
	}
	if hits.Load() != 1 {
		t.Errorf("async document should still convert once, got %d hits", hits.Load())
	}
}

// TestOnDocumentAsyncConvertFailureNotifiesUser proves an async (5-50MB)
// conversion that fails notifies the user with convertFailMessage instead of
// leaving them on "📄 …elaborando…" forever (H3). BEFORE the fix the async callback
// logged convErr and returned silently; AFTER, the captured sender delivers the
// fail copy. The sync ≤5MB sibling already did this — the async tier now matches.
func TestOnDocumentAsyncConvertFailureNotifiesUser(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // drain the >5MB upload before responding
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{DocumentsBaseURL: srv.URL}
	})
	defer tg.docs.Stop(context.Background())

	bot := &dispatchBot{ogg: make([]byte, asyncTierMinBytes+1)} // > 5MB → async tier
	msg := chatMsg(35)
	msg.Document = &tele.Document{FileName: "async-fail.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument(async fail): %v", err)
	}
	tg.docs.Stop(context.Background()) // drain the async convert goroutine
	tg.wg.Wait()

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a failed async conversion must NOT drive a turn, got %d", calls)
	}
	var sawFail, sawAccepted bool
	for _, text := range bot.sentTexts() {
		if strings.Contains(text, convertFailMessage) {
			sawFail = true
		}
		if strings.Contains(text, documentAcceptedMessage) {
			sawAccepted = true
		}
	}
	if !sawAccepted {
		t.Errorf("the async tier must first acknowledge the document, sent=%v", bot.sentTexts())
	}
	if !sawFail {
		t.Fatalf("a failed async conversion must notify the user with the fail copy, sent=%v", bot.sentTexts())
	}
}

func TestStopDrainsAsyncDocumentTurn(t *testing.T) {
	convertEntered := make(chan struct{})
	allowConvert := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		select {
		case <-convertEntered:
		default:
			close(convertEntered)
		}
		<-allowConvert
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"markdown":"# Relazione async"}`))
	}))
	defer srv.Close()

	turnStarted := make(chan struct{})
	releaseTurn := make(chan struct{})
	rt := &recordingTurn{}
	tg := NewChannel(Deps{
		Offline:         true,
		consumerFactory: recordingFactory(),
		Multimodal:      MultimodalConfig{DocumentsBaseURL: srv.URL},
		profileAccounts: profileAccountFake{acct: profileAccount()},
		Turn: func(ctx context.Context, _ string, _ *string) iter.Seq2[*agent.Event, error] {
			return func(yield func(*agent.Event, error) bool) {
				rt.mu.Lock()
				rt.calls++
				rt.mu.Unlock()
				select {
				case <-turnStarted:
				default:
					close(turnStarted)
				}
				select {
				case <-releaseTurn:
				case <-ctx.Done():
				}
				yield(textEvent("ok"), nil)
			}
		},
	})
	if err := tg.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	bot := &dispatchBot{ogg: make([]byte, asyncTierMinBytes+1)}
	msg := chatMsg(34)
	msg.Document = &tele.Document{FileName: "async.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument(async): %v", err)
	}
	<-convertEntered

	stopDone := make(chan error, 1)
	go func() { stopDone <- tg.Stop(context.Background()) }()
	close(allowConvert)
	<-turnStarted

	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before draining async document turn: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseTurn)
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if calls, _ := rt.snapshot(); calls != 1 {
		t.Fatalf("async document should drive exactly one drained turn, got %d", calls)
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
