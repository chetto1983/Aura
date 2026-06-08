//go:build multimodal_integration

// Live multimodal sidecar integration tier (UX-04, NEW). Round-trips the three 9c
// CPU sidecars end-to-end:
//
//	STT_BASE_URL        — aura-stt (faster-whisper), /audio/transcriptions
//	MULTIMODAL_BASE_URL  — aura-ocr-vl (GLM-OCR), /chat/completions image_url
//	TTS_BASE_URL        — aura-tts (Kokoro), /audio/speech response_format=opus
//
// Run via (the 3 sidecars must be up — `make sidecars-up` or compose):
//
//	go test -tags multimodal_integration -race ./internal/channels/telegram
//
// The marquee round-trip is TTS→STT: synthesize an Italian phrase to opus via
// aura-tts, then transcribe it back via aura-stt, asserting the transcript is
// non-empty (the full audio loop the operator hears, machine-asserted).
//
// NO-SKIP-AS-GREEN: sidecarEnvOrSkip t.Fatals under $CI when the sidecar base URL
// is SET but unreachable, so a skipped tier under CI fails rather than passing
// green (CLAUDE.md / VALIDATION). Locally it skips when the env is unset.
package telegram

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"
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
		t.Skipf("multimodal_integration requires %s; bring up the 9c sidecars and set the base URLs", key)
	}
	return v
}

// liveMultimodalConfig builds a MultimodalConfig from the live sidecar env. Each
// caller pulls only the base URLs it needs (a TTS-only test does not require STT).
func liveMultimodalConfig() MultimodalConfig {
	return MultimodalConfig{
		STTBaseURL:        os.Getenv("STT_BASE_URL"),
		STTModel:          os.Getenv("STT_MODEL"),
		MultimodalBaseURL: os.Getenv("MULTIMODAL_BASE_URL"),
		MultimodalModel:   envOr("MULTIMODAL_MODEL", "glm-ocr"),
		TTSBaseURL:        os.Getenv("TTS_BASE_URL"),
		TTSVoice:          envOr("TTS_VOICE", "if_sara"),
		TTSFormat:         envOr("TTS_FORMAT", "opus"),
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

	cfg := liveMultimodalConfig()
	cfg.TTSBaseURL = ttsURL
	cfg.STTBaseURL = sttURL

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

	transcript, err := newVoiceClient(cfg).postTranscription(ctx, opus)
	if err != nil {
		t.Fatalf("live aura-stt transcribe: %v", err)
	}
	if transcript == "" {
		t.Fatal("aura-stt returned an empty transcript for a non-trivial opus utterance")
	}
}

// TestLivePhotoOCRRoundTrip sends a synthetic PNG to aura-ocr-vl and asserts the
// description/OCR text is non-empty (the local vision leg, AURA_VISION_CLOUD=false).
func TestLivePhotoOCRRoundTrip(t *testing.T) {
	ocrURL := sidecarEnvOrSkip(t, "MULTIMODAL_BASE_URL")

	cfg := liveMultimodalConfig()
	cfg.VisionCloud = false // local aura-ocr-vl sidecar (the unchanged default)
	cfg.MultimodalBaseURL = ocrURL

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	desc, err := newPhotoClient(cfg).Describe(ctx, syntheticPNG(), "image/png", "Cosa c'è in questa immagine?")
	if err != nil {
		t.Fatalf("live aura-ocr-vl Describe: %v", err)
	}
	if desc == "" {
		t.Fatal("aura-ocr-vl returned an empty description for a non-trivial image")
	}
}

// TestLiveTTSVoiceBytes proves aura-tts alone produces voice-note-ready opus bytes
// (the sendVoice payload the operator hears; the live sendVoice Bot-API leg is in
// integration_test.go).
func TestLiveTTSVoiceBytes(t *testing.T) {
	ttsURL := sidecarEnvOrSkip(t, "TTS_BASE_URL")

	cfg := liveMultimodalConfig()
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

// syntheticPNG renders a small solid-color PNG so the OCR round-trip has a real
// image payload without committing a binary fixture. (The operator exercises a
// text-bearing photo live; this proves the vision sidecar response leg.)
func syntheticPNG() []byte {
	const w, h = 64, 64
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 0x20, G: 0x80, B: 0xC0, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
