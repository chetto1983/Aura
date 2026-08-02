//go:build multimodal_integration

// Live multimodal sidecar integration tier for the ONE sidecar leg this channel
// still owns — outbound text-to-speech:
//
//	TTS_BASE_URL — aura-tts (Kokoro), /audio/speech response_format=opus
//	STT_BASE_URL — aura-stt (faster-whisper), the read-back half of the audio loop
//
// Run via (the audio sidecars must be up — `make sidecars-up` or compose):
//
//	go test -tags multimodal_integration -race ./internal/channels/telegram
//
// The marquee round-trip is TTS→STT: synthesize an Italian phrase to opus via the
// channel's voice-note client, then transcribe it back, asserting the transcript is
// non-empty (the full audio loop the operator hears, machine-asserted). The STT half
// goes through the SHARED multimodal client directly: inbound speech is no longer
// this channel's concern (a voice note is ingested as an asset and transcribed by
// assets.AudioProcessor), so there is no channel-local STT client to drive.
//
// The live vision and document legs left with the clients that used to be here. They
// belong to the asset pipeline now and are exercised where that pipeline lives.
//
// NO-SKIP-AS-GREEN: sidecarEnvOrSkip t.Fatals under $CI when the sidecar base URL
// is SET but unreachable, so a skipped tier under CI fails rather than passing
// green (CLAUDE.md / VALIDATION). Locally it skips when the env is unset.
package telegram

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/multimodal"
)

// sidecarEnvOrSkip resolves a required sidecar base URL. Unset locally → skip;
// unset under $CI → t.Fatal (the sidecar-gated CI job exports these, so a skip
// under CI means a sidecar is down or the wiring rotted, never a silent green).
func sidecarEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("multimodal_integration requires %s, but it is unset under CI — "+
				"a skipped sidecar tier must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("multimodal_integration requires %s; bring up the audio sidecars and set the base URLs", key)
	}
	return v
}

// liveTTSConfig builds the channel's TTS config from the live sidecar env.
func liveTTSConfig() MultimodalConfig {
	return MultimodalConfig{
		TTSBaseURL: os.Getenv("TTS_BASE_URL"),
		TTSVoice:   envOr("TTS_VOICE", "if_sara"),
		TTSFormat:  envOr("TTS_FORMAT", "opus"),
		// A live sidecar round-trip can take seconds on CPU; keep the per-request
		// ceiling generous so a healthy-but-slow synthesis is not aborted mid-read.
		TimeoutSec: 60,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TestLiveTTSThenSTTRoundTrip is the marquee audio loop: aura-tts synthesizes an
// Italian phrase to opus, aura-stt transcribes it back, and the transcript must be
// non-empty (the full voice round-trip the operator hears, machine-asserted).
func TestLiveTTSThenSTTRoundTrip(t *testing.T) {
	ttsURL := sidecarEnvOrSkip(t, "TTS_BASE_URL")
	sttURL := sidecarEnvOrSkip(t, "STT_BASE_URL")

	cfg := liveTTSConfig()
	cfg.TTSBaseURL = ttsURL

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const phrase = "Ciao Aura, questo è un test di trascrizione vocale."
	opus, err := newTTSClient(cfg).synthesize(ctx, phrase)
	if err != nil {
		t.Fatalf("live aura-tts synthesize: %v", err)
	}
	if len(opus) == 0 {
		t.Fatal("aura-tts returned zero opus bytes — TTS leg produced no audio")
	}

	stt := multimodal.NewSTTClient(multimodal.STTConfig{
		LocalBaseURL: sttURL,
		LocalModel:   os.Getenv("STT_MODEL"),
		Language:     envOr("STT_LANGUAGE", "it"),
		TimeoutSec:   60,
	})
	transcript, err := stt.Transcribe(ctx, opus, "voice.ogg", "ogg")
	if err != nil {
		t.Fatalf("live aura-stt transcribe: %v", err)
	}
	if transcript == "" {
		t.Fatal("aura-stt returned an empty transcript for a non-trivial opus utterance")
	}
}

// TestLiveTTSVoiceBytes proves aura-tts alone produces voice-note-ready opus bytes
// (the sendVoice payload the operator hears; the live sendVoice Bot-API leg is in
// integration_test.go).
func TestLiveTTSVoiceBytes(t *testing.T) {
	ttsURL := sidecarEnvOrSkip(t, "TTS_BASE_URL")

	cfg := liveTTSConfig()
	cfg.TTSBaseURL = ttsURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	opus, err := newTTSClient(cfg).synthesize(ctx, "Test sintesi vocale.")
	if err != nil {
		t.Fatalf("live aura-tts synthesize: %v", err)
	}
	if len(opus) == 0 {
		t.Fatal("aura-tts produced zero opus bytes")
	}
}
