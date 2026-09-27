package main

// serve_voice.go is the `aura serve` composition-root wiring for the 37C web-voice
// backend (WEBVOICE-01/02/03, D-13). It builds a DEDICATED web TTSClient with
// TTSConfig.Format="mp3" (distinct from Telegram's opus client, which multimodalConfig
// leaves at cfg.TTSFormat — RESEARCH Landmine #2) + an STTClient, and injects them into
// the agui Server via SetVoice (D-13). A nil client ⇒ that capability is absent ⇒ its
// POST answers 503; GET /api/voice/capabilities reflects presence.
//
// Local↔cloud SELECTABLE (the same knob the rest of multimodal uses): each client
// defaults to the LOCAL sidecar (aura-tts Kokoro / aura-stt faster-whisper at
// cfg.TTSBaseURL / cfg.STTBaseURL) and switches to OpenRouter only when its cloud model
// (AURA_TTS_MODEL / AURA_STT_CLOUD_MODEL) is set. This supersedes the original
// cloud-only gating (D-12): the deployment ships healthy local voice sidecars, so the
// web lane uses them by default rather than degrading. It lives here (not serve.go) so
// the mp3-vs-opus split + the local/cloud selection are unit-testable via
// buildWebTTSClient / buildWebSTTClient with no live call, and serve.go stays under the
// 600-LOC ceiling.

import (
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/multimodal"
)

// wireVoiceProviders builds the web voice clients from config and injects them via
// SetVoice. Each client is built when EITHER its local sidecar base URL OR its cloud
// model is configured (local↔cloud selectable); it is nil only when neither is set, so
// that capability stays false (its POST 503s). When BOTH clients are nil SetVoice is not
// called at all — the three routes degrade (the POSTs 503, capabilities reports
// {false,false}).
//
// A nil concrete client is passed to SetVoice as an untyped-nil literal (never a
// typed-nil *multimodal.TTSClient), so s.tts != nil / s.stt != nil report presence
// correctly rather than wrapping a nil pointer in a non-nil interface (the typed-nil
// footgun that would make a capability report true and then panic on the first call).
func wireVoiceProviders(server *agui.Server, cfg *config.Config) {
	tts := buildWebTTSClient(cfg)
	stt := buildWebSTTClient(cfg)
	switch {
	case tts == nil && stt == nil:
		return
	case tts == nil:
		server.SetVoice(nil, stt, cfg.TTSMaxChars)
	case stt == nil:
		server.SetVoice(tts, nil, cfg.TTSMaxChars)
	default:
		server.SetVoice(tts, stt, cfg.TTSMaxChars)
	}
}

// buildWebTTSClient builds the DEDICATED mp3 web TTS client (D-02): Format="mp3" (the
// web override — distinct from Telegram's opus client, which multimodalConfig leaves at
// cfg.TTSFormat). Local↔cloud SELECTABLE: the aura-tts Kokoro sidecar is the default, and
// cfg.TTSModel switches it to OpenRouter with the services key, whatever route the chat runs
// on (config.SpeechCloudRoute). Returns nil only when NEITHER a local base URL NOR a cloud
// model is set, so the caller leaves the tts capability absent.
func buildWebTTSClient(cfg *config.Config) *multimodal.TTSClient {
	voiceCfg := multimodal.TTSConfigFrom(cfg, "mp3")
	if !voiceCfg.Configured() {
		return nil
	}
	return multimodal.NewTTSClient(voiceCfg)
}

// buildWebSTTClient builds the web STT client over the same projection the asset pipeline
// uses: the aura-stt faster-whisper sidecar by default, OpenRouter's JSON transcription route
// once cfg.STTCloudModel is set. Returns nil only when NEITHER a local base URL NOR a cloud
// model is set, leaving the stt capability absent.
func buildWebSTTClient(cfg *config.Config) *multimodal.STTClient {
	voiceCfg := multimodal.STTConfigFrom(cfg)
	if !voiceCfg.Configured() {
		return nil
	}
	return multimodal.NewSTTClient(voiceCfg)
}
