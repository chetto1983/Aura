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
	"fmt"

	"github.com/chetto1983/aura/internal/multimodal"
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

// ttsClient wraps the shared TTS client and sends the returned audio as a Telegram
// voice note. It owns only the telegram glue (the ASCII-safe caption + the
// configured guard the auto-speak path reads). Zero Go ML; the local↔cloud TTS
// branch lives inside multimodal.TTSClient.
type ttsClient struct {
	tts     *multimodal.TTSClient
	caption string
	// configured reports whether TTS can produce audio at all (a local sidecar base
	// URL OR a cloud model). speakIfNeeded reads it to no-op when TTS is unwired.
	// The local arm is byte-identical to the old TTSBaseURL!="" guard; the cloud
	// arm additionally fires when only AURA_TTS_MODEL is set.
	configured bool
}

// newTTSClient builds a TTS client over the multimodal config.
func newTTSClient(cfg MultimodalConfig) *ttsClient {
	return &ttsClient{
		tts: multimodal.NewTTSClient(multimodal.TTSConfig{
			LocalBaseURL:      cfg.TTSBaseURL,
			Voice:             cfg.TTSVoice,
			Format:            cfg.TTSFormat,
			CloudModel:        cfg.TTSModel,
			OpenRouterBaseURL: cfg.OpenRouterBaseURL,
			OpenRouterAPIKey:  cfg.OpenRouterAPIKey,
			TimeoutSec:        cfg.TimeoutSec,
		}),
		caption:    cfg.TTSCaption,
		configured: cfg.TTSBaseURL != "" || cfg.TTSModel != "",
	}
}

// Speak synthesizes text via the aura-tts sidecar and sends the opus bytes as a
// Telegram voice note, returning the Send RESPONSE (the spike ground truth — the
// msg.Voice the caller asserts on). The caption is ASCII-sanitized so it never
// 400s. A sidecar error surfaces and no voice note is sent.
func (t *ttsClient) Speak(ctx context.Context, bot botSender, to tele.Recipient, text string) (*tele.Message, error) {
	spoken := sanitizeForSpeech(text)
	if spoken == "" {
		return nil, nil // nothing speakable (e.g. an emoji-only reply) — skip the voice note
	}
	opus, err := t.synthesize(ctx, spoken)
	if err != nil {
		return nil, err
	}

	voice := &tele.Voice{
		File:    tele.FromReader(bytes.NewReader(opus)),
		Caption: asciiCaption(t.caption),
		MIME:    "audio/" + t.tts.AudioFormat(),
	}
	msg, err := bot.Send(to, voice)
	if err != nil {
		return nil, fmt.Errorf("telegram tts: sendVoice: %w", err)
	}
	return msg, nil
}

// synthesize delegates to the shared TTS client, returning the raw audio bytes
// (the AudioFormat container). A non-2xx response is a *multimodal.StatusError.
func (t *ttsClient) synthesize(ctx context.Context, text string) ([]byte, error) {
	return t.tts.Synthesize(ctx, text)
}
