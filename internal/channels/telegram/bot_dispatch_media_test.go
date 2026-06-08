package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	tele "gopkg.in/telebot.v4"
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
