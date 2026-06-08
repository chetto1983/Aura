// Package telegram — this file is the text-to-speech sidecar client (UX-04). It
// POSTs the agent's reply text to the aura-tts /v1/audio/speech endpoint
// (response_format=opus, Kokoro voice if_sara) and replies via sendVoice with an
// ASCII-clean caption (Pitfall 4 — a non-ASCII caption byte 400s a voice note).
//
// Trigger (OQ2 — NO explicit send_voice tool this phase): the handler speaks a
// reply when a voice_mode preference is on OR the inbound message was a voice note
// (echo the user's modality). Slice 10 preferences are not shipped yet, so
// VoiceModePref is a stub that returns false until Phase 14.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	tele "gopkg.in/telebot.v4"
)

// ShouldSpeak is the TTS trigger predicate (OQ2): reply with a voice note when the
// voice_mode preference is enabled OR the inbound message was itself a voice note
// (echo modality). There is deliberately no explicit send_voice tool this phase.
func ShouldSpeak(voiceMode, inboundWasVoice bool) bool {
	return voiceMode || inboundWasVoice
}

// VoiceModePref reads the per-conversation voice_mode preference. Slice 10
// preferences are NOT shipped (Phase 14), so this is a stub returning the default
// (false) — wired to the real preference store when it lands. Kept as a function
// (not a const) so the call sites are already correct when the store arrives.
func VoiceModePref(_ string) bool {
	return false
}

// ttsClient POSTs reply text to the aura-tts sidecar and sends the returned opus
// as a Telegram voice note. Thin OpenAI-compat client (zero Go ML).
type ttsClient struct {
	cfg        MultimodalConfig
	httpClient *http.Client
}

// newTTSClient builds a TTS client over the multimodal config.
func newTTSClient(cfg MultimodalConfig) *ttsClient {
	return &ttsClient{cfg: cfg, httpClient: newSidecarHTTPClient()}
}

// ttsRequest is the OpenAI /audio/speech body. response_format=opus produces a
// voice-note-ready container (no transcode); voice is the Kokoro id (if_sara).
type ttsRequest struct {
	Model          string `json:"model,omitempty"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format"`
}

// Speak synthesizes text via the aura-tts sidecar and sends the opus bytes as a
// Telegram voice note, returning the Send RESPONSE (the spike ground truth — the
// msg.Voice the caller asserts on). The caption is ASCII-sanitized so it never
// 400s. A sidecar error surfaces and no voice note is sent.
func (t *ttsClient) Speak(ctx context.Context, bot botSender, to tele.Recipient, text string) (*tele.Message, error) {
	opus, err := t.synthesize(ctx, text)
	if err != nil {
		return nil, err
	}

	format := t.cfg.TTSFormat
	if format == "" {
		format = "opus"
	}
	voice := &tele.Voice{
		File:    tele.FromReader(bytes.NewReader(opus)),
		Caption: asciiCaption(t.cfg.TTSCaption),
		MIME:    "audio/" + format,
	}
	msg, err := bot.Send(to, voice)
	if err != nil {
		return nil, fmt.Errorf("telegram tts: sendVoice: %w", err)
	}
	return msg, nil
}

// synthesize POSTs the reply text to TTS_BASE_URL/audio/speech and returns the
// raw opus bytes. A non-2xx response is a *sidecarStatusError.
func (t *ttsClient) synthesize(ctx context.Context, text string) ([]byte, error) {
	reqCtx, cancel := t.cfg.withTimeout(ctx)
	defer cancel()

	format := t.cfg.TTSFormat
	if format == "" {
		format = "opus"
	}
	body, err := json.Marshal(ttsRequest{
		// Model is omitted: Kokoro is single-model and the TTS sidecar selects the
		// voice via the `voice` field, not a model id (there is no TTS model knob).
		Input:          text,
		Voice:          t.cfg.TTSVoice,
		ResponseFormat: format,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		t.cfg.TTSBaseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, &sidecarStatusError{endpoint: "tts", statusCode: resp.StatusCode}
	}
	return io.ReadAll(resp.Body)
}
