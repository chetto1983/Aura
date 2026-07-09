package agui

// voice_api.go is the 37C web-voice backend (WEBVOICE-01/02/03): three thin,
// identity-scoped handlers over narrow interface seams —
//
//   - POST /api/tts — text → audio/mpeg synthesized bytes, with a soft rune char cap
//     (AURA_TTS_MAX_CHARS, D-05) that truncates the input to the ttsMaxChars-length
//     prefix and signals it with an X-Aura-TTS-Truncated: true response header. The
//     audio is streamed straight to the caller and NEVER persisted.
//   - POST /api/stt — multipart audio → transcript, transcribe-and-DISCARD (D-08): the
//     handler reads the `audio` part, maps its Content-Type to an STT container format
//     via assets.AudioFormat (which strips a MediaRecorder ;codecs= suffix), transcribes,
//     and returns {"text":...}. It NEVER touches the asset service — no assets.Asset, no
//     Garage object, no DB row, no async poll. An EMPTY transcript is a clean 200
//     {"text":""}, never a 5xx (the Nyquist edge: STT returns "", session ends clean).
//   - GET /api/voice/capabilities — a SELF-scoped {tts,stt} presence probe the SPA reads
//     once on load to gate the voice UI (D-11). It is ALWAYS 200 (never 503) so an
//     unconfigured stack reports {false,false} cleanly.
//
// The clients are injected via SetVoice (D-13) as narrow seams so the handlers unit-test
// with no network; the daemon composition root builds a DEDICATED mp3 web TTSClient
// (Format="mp3") + a cloud-only STTClient and calls SetVoice (cmd/aura/serve_voice.go).
// The parent-mux mounts (the two cost-bearing POSTs behind agentRunCapability, the
// capabilities read behind RequireAuth only) live in cmd/aura/serve_webui_voice.go; the
// whole origin inherits RequireAuth from the parent-mux wrap.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/assets"
)

// maxSTTAudioBytes bounds the POST /api/stt multipart upload (T-12-12 DoS guard): 25
// MiB is the OpenAI-compatible /audio/transcriptions ceiling OpenRouter proxies —
// generous for a short dictation clip while defanging a hostile oversized body before
// it is read into memory.
const maxSTTAudioBytes = 25 << 20

// ttsSynthesizer is the narrow text-to-speech seam handleTTS consumes
// (*multimodal.TTSClient satisfies it). Declared consumer-side (D-A2-02) so
// voice_api_test.go injects a fake with no network. AudioFormat reports the configured
// container — the web client is built with Format="mp3", distinct from the Telegram
// opus client (RESEARCH Landmine #2).
type ttsSynthesizer interface {
	Synthesize(ctx context.Context, text string) ([]byte, error)
	AudioFormat() string
}

// sttTranscriber is the narrow speech-to-text seam handleSTT consumes
// (*multimodal.STTClient satisfies it). Transcribe is the ONLY thing done with the
// uploaded bytes — they are never persisted (transcribe-and-discard, D-08).
type sttTranscriber interface {
	Transcribe(ctx context.Context, audio []byte, fileName, format string) (string, error)
}

// SetVoice wires the web voice clients (D-13). The daemon composition root builds them
// cloud-only (only when the model is configured — a nil client ⇒ that capability is
// false ⇒ that POST route answers 503); until set, all three voice routes degrade (the
// two POSTs 503, capabilities reports {false,false}). Kept off the constructor so
// existing NewServer callers/tests stay unchanged (the SetAssetService precedent).
func (s *Server) SetVoice(tts ttsSynthesizer, stt sttTranscriber, maxChars int) {
	s.tts, s.stt, s.ttsMaxChars = tts, stt, maxChars
}

func (s *Server) registerVoiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/tts", s.handleTTS)
	mux.HandleFunc("POST /api/stt", s.handleSTT)
	mux.HandleFunc("GET /api/voice/capabilities", s.handleVoiceCapabilities)
}

type ttsRequestBody struct {
	Text string `json:"text"`
}

// handleTTS synthesizes speech for an authed request over the mp3 web client: nil
// client → 503, no principal → 401, empty text → 400. The text is rune-capped at
// ttsMaxChars (the soft D-05 cap; an X-Aura-TTS-Truncated: true header rides the
// response when it overflowed) and synthesized; the audio bytes are written as
// audio/mpeg (the canonical mp3 MIME HTMLAudioElement decodes incl. Safari/iOS) plus
// nosniff. It NEVER persists — the synthesized audio is streamed straight to the caller.
func (s *Server) handleTTS(w http.ResponseWriter, r *http.Request) {
	if s.tts == nil {
		http.Error(w, "tts unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := principalIdentityID(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	raw, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	var body ttsRequestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		http.Error(w, "text required", http.StatusBadRequest)
		return
	}
	text, truncated := capText(body.Text, s.ttsMaxChars)
	audio, err := s.tts.Synthesize(r.Context(), text)
	if err != nil {
		http.Error(w, "tts synthesis failed", http.StatusBadGateway)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "audio/mpeg")
	h.Set("X-Content-Type-Options", "nosniff")
	if truncated {
		h.Set("X-Aura-TTS-Truncated", "true")
	}
	_, _ = w.Write(audio)
}

// capText returns the rune-safe prefix of text bounded to maxChars and whether it was
// truncated. A non-positive maxChars disables the cap (the whole text passes). Runes
// (not bytes) are counted so a multi-byte input is never split mid-character.
func capText(text string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		return text, false
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, false
	}
	return string(runes[:maxChars]), true
}

// handleSTT transcribes an uploaded audio part and DISCARDS it (D-08): nil client →
// 503, no principal → 401. It reads the `audio` multipart file, maps its part
// Content-Type to an STT container format via assets.AudioFormat (which strips the
// MediaRecorder ;codecs= suffix so audio/webm;codecs=opus → webm), transcribes, and
// returns {"text":...}. It NEVER touches the asset service — no asset row, no Garage
// object, no DB row, no async poll. An empty transcript is a clean 200 {"text":""}.
func (s *Server) handleSTT(w http.ResponseWriter, r *http.Request) {
	if s.stt == nil {
		http.Error(w, "stt unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := principalIdentityID(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSTTAudioBytes)
	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "audio file required", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()
	audioBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read audio failed", http.StatusBadRequest)
		return
	}
	format := assets.AudioFormat(header.Header.Get("Content-Type"))
	transcript, err := s.stt.Transcribe(r.Context(), audioBytes, header.Filename, format)
	if err != nil {
		http.Error(w, "stt transcription failed", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"text": transcript})
}

// voiceCapabilitiesDTO is the SELF-scoped presence probe the SPA reads once on load to
// gate the voice UI (D-11): tts/stt reflect whether each client is wired. It is ALWAYS
// 200 (never 503) so an unconfigured stack reports {false,false} cleanly.
type voiceCapabilitiesDTO struct {
	TTS bool `json:"tts"`
	STT bool `json:"stt"`
}

// handleVoiceCapabilities returns the caller's voice-capability presence. It requires a
// principal (401 without — inheriting the whole-mux RequireAuth) but, unlike the two
// POSTs, NEVER 503s: an unconfigured stack answers 200 {false,false} so the SPA can hide
// the speaker control / keep the mic in attachment mode without an error (D-11).
func (s *Server) handleVoiceCapabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := principalIdentityID(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, voiceCapabilitiesDTO{TTS: s.tts != nil, STT: s.stt != nil})
}
