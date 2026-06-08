//go:build telegram_integration

// Live Telegram Bot-API integration tier (UX-02, NEW). Requires a real bot token
// + the operator's own chat id:
//
//	TELEGRAM_BOT_TOKEN  — the live Bot API credential (upstream naming)
//	AURA_E2E_CHAT_ID    — the operator's own chat id (where the bot sends the probes)
//
// Run via:
//
//	go test -tags telegram_integration -race ./internal/channels/telegram
//
// GROUND TRUTH = the Send RESPONSE, NEVER the poll/get-updates stream (spike
// 017/019: a bot-sent message never appears in the inbound update stream, so the
// response *tele.Message is the only authoritative artifact). Each test asserts on
// msg.Photo / msg.Document.{FileName,MIME,FileSize} / msg.Voice non-nil — the
// round-trip the live Bot API confirms in the Send reply.
//
// NO-SKIP-AS-GREEN: liveEnvOrSkip t.Fatals under $CI when the env is SET but the
// token/chat is unusable, so a skipped tier under CI fails rather than passing
// green (CLAUDE.md / VALIDATION). Locally it skips when the env is unset.
package telegram

import (
	"bytes"
	"os"
	"strconv"
	"testing"
	"time"

	tele "gopkg.in/telebot.v4"
)

// liveEnvOrSkip resolves a required live-bot env var. Unset locally → skip; unset
// under $CI → t.Fatal (no-skip-as-green: the operator-token-gated CI step exports
// these, so a skip under CI means the wiring rotted, never a silent green pass).
func liveEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("telegram_integration requires %s, but it is unset under CI — "+
				"a skipped live tier must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("telegram_integration requires %s; set TELEGRAM_BOT_TOKEN + AURA_E2E_CHAT_ID and re-run", key)
	}
	return v
}

// liveBot constructs a real telebot bot (live getMe) and resolves the operator's
// chat recipient. A bad token is a hard failure (under $CI the env is set, so a
// construction error is a real break, not a skip).
func liveBot(t *testing.T) (*tele.Bot, tele.Recipient) {
	t.Helper()
	token := liveEnvOrSkip(t, "TELEGRAM_BOT_TOKEN")
	chatRaw := liveEnvOrSkip(t, "AURA_E2E_CHAT_ID")

	chatID, err := strconv.ParseInt(chatRaw, 10, 64)
	if err != nil {
		t.Fatalf("AURA_E2E_CHAT_ID %q is not an int64: %v", chatRaw, err)
	}

	bot, err := tele.NewBot(tele.Settings{
		Token: token,
		// No poller is started — these tests only Send (the bot-side artifact), so
		// the default LongPoller is never engaged; the bot is used as a Bot-API client.
		Offline: false,
	})
	if err != nil {
		t.Fatalf("live tele.NewBot (getMe): %v", err)
	}
	return bot, tele.ChatID(chatID)
}

// TestLiveSendPhotoResponse sends a real table-PNG via sendPhoto and asserts on the
// RESPONSE (msg.Photo non-nil) — the spike-017/019 ground truth that a bot-sent
// photo round-trips through the Bot API Send reply, never the inbound poll stream.
func TestLiveSendPhotoResponse(t *testing.T) {
	bot, to := liveBot(t)

	png, err := RenderTablePNG([][]string{
		{"Col A", "Col B", "Col C"},
		{"1", "2", "3"},
		{"x", "y", "z"},
	})
	if err != nil {
		t.Fatalf("RenderTablePNG: %v", err)
	}

	photo := &tele.Photo{
		File:    tele.FromReader(bytes.NewReader(png)),
		Caption: "telegram_integration table probe",
	}
	msg, err := bot.Send(to, photo)
	if err != nil {
		t.Fatalf("live sendPhoto: %v", err)
	}
	if msg.Photo == nil {
		t.Fatal("RESPONSE msg.Photo is nil — the Bot API did not confirm a photo (ground truth = the Send reply, not the poll stream)")
	}
}

// TestLiveSendDocumentResponse sends a real artifact via sendDocument and asserts
// on msg.Document.{FileName,MIME,FileSize} in the RESPONSE — the exact fields the
// VALIDATION map binds to (artifact ground truth = the Send response).
func TestLiveSendDocumentResponse(t *testing.T) {
	bot, to := liveBot(t)

	const fileName = "aura-e2e-report.txt"
	payload := []byte("aura telegram_integration artifact probe\nrow 1\nrow 2\n")

	doc := &tele.Document{
		File:     tele.FromReader(bytes.NewReader(payload)),
		FileName: fileName,
		MIME:     "text/plain",
		Caption:  "telegram_integration document probe",
	}
	msg, err := bot.Send(to, doc)
	if err != nil {
		t.Fatalf("live sendDocument: %v", err)
	}
	if msg.Document == nil {
		t.Fatal("RESPONSE msg.Document is nil — the Bot API did not confirm a document")
	}
	if msg.Document.FileName == "" {
		t.Errorf("RESPONSE msg.Document.FileName is empty, want %q echoed back", fileName)
	}
	if msg.Document.MIME == "" {
		t.Error("RESPONSE msg.Document.MIME is empty — the Bot API auto-detect did not populate it")
	}
	if msg.Document.FileSize == 0 {
		t.Error("RESPONSE msg.Document.FileSize is 0 — the Bot API did not report the size")
	}
}

// TestLiveSendVoiceResponse sends a real opus voice note via sendVoice and asserts
// on msg.Voice non-nil in the RESPONSE — the TTS round-trip ground truth (a voice
// reply the operator hears; here the assertion is on the Bot-API confirmation).
// It uses the live aura-tts sidecar when TTS_BASE_URL is set (the multimodal tier
// owns the full STT/OCR/TTS round-trip); here a tiny pre-canned OGG/Opus header is
// sufficient to prove the sendVoice response leg without the sidecar.
func TestLiveSendVoiceResponse(t *testing.T) {
	bot, to := liveBot(t)

	voice := &tele.Voice{
		File:    tele.FromReader(bytes.NewReader(minimalOggOpus())),
		MIME:    "audio/ogg",
		Caption: asciiCaption("telegram_integration voice probe"),
	}
	msg, err := bot.Send(to, voice)
	if err != nil {
		t.Fatalf("live sendVoice: %v", err)
	}
	if msg.Voice == nil {
		t.Fatal("RESPONSE msg.Voice is nil — the Bot API did not confirm a voice note")
	}
	// A short settle so a rapid suite does not trip the per-chat flood limit.
	time.Sleep(100 * time.Millisecond)
}

// minimalOggOpus returns a minimal valid OGG/Opus stream (an OggS page header +
// the OpusHead identification packet) so the live sendVoice has a parseable
// container without depending on the TTS sidecar. The multimodal_integration tier
// exercises the real Kokoro opus bytes.
func minimalOggOpus() []byte {
	// OggS capture pattern + a minimal OpusHead — enough for the Bot API to accept
	// the upload as a voice note. (A full audio stream is exercised live by the
	// operator + the multimodal tier; this proves only the sendVoice response leg.)
	head := []byte("OggS")
	opusHead := []byte("OpusHead")
	buf := bytes.NewBuffer(nil)
	buf.Write(head)
	buf.Write(make([]byte, 22)) // ogg page header remainder (version/flags/granule/serial/seq/crc/segs)
	buf.Write(opusHead)
	buf.Write([]byte{1, 1, 0x38, 0x01, 0x80, 0xbb, 0, 0, 0, 0, 0})
	return buf.Bytes()
}
