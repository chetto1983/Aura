package main

// serve_voice.go is the `aura serve` composition-root wiring for the 37C web-voice
// backend (WEBVOICE-01/02/03, D-12/D-13). It builds a DEDICATED web TTSClient with
// TTSConfig.Format="mp3" (distinct from Telegram's opus client, which multimodalConfig
// leaves at cfg.TTSFormat — RESEARCH Landmine #2) + a cloud-only STTClient, each built
// ONLY when its cloud model is configured (D-12), and injects them into the agui Server
// via SetVoice (D-13). A nil client ⇒ that capability is absent ⇒ its POST answers 503;
// GET /api/voice/capabilities reflects presence. It lives here (not serve.go) so the
// mp3-vs-opus split + the cloud-only gating are unit-testable via buildWebTTSClient /
// buildWebSTTClient with no live call, and serve.go stays under the 600-LOC ceiling.

import (
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/multimodal"
)

// wireVoiceProviders builds the web voice clients from config and injects them via
// SetVoice. Cloud-only (D-12): each client is built ONLY when its cloud model is set,
// so an unconfigured stack leaves that capability at false (its POST 503s). When
// NEITHER model is set SetVoice is not called at all — the three routes degrade (the
// POSTs 503, capabilities reports {false,false}).
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

// buildWebTTSClient builds the DEDICATED mp3 web TTS client (D-02): CloudModel from
// cfg.TTSModel, Format="mp3" (the web override — distinct from Telegram's opus client,
// which multimodalConfig leaves at cfg.TTSFormat), over the shared OpenRouter credential
// (the same LLM.BaseURL/APIKey serve_channels.go maps for Telegram). Cloud-only (D-12):
// returns nil when cfg.TTSModel is empty, so the caller leaves the tts capability absent.
// Extracted so serve_voice_test.go asserts AudioFormat()=="mp3" with no live call.
func buildWebTTSClient(cfg *config.Config) *multimodal.TTSClient {
	if cfg.TTSModel == "" {
		return nil
	}
	return multimodal.NewTTSClient(multimodal.TTSConfig{
		CloudModel:        cfg.TTSModel,
		Voice:             cfg.TTSVoice,
		Format:            "mp3",
		OpenRouterBaseURL: cfg.LLM.BaseURL,
		OpenRouterAPIKey:  cfg.LLM.APIKey,
		TimeoutSec:        cfg.MultimodalTimeoutSec,
	})
}

// buildWebSTTClient builds the cloud-only web STT client (D-12): CloudModel from
// cfg.STTCloudModel over the shared OpenRouter credential. Returns nil when
// cfg.STTCloudModel is empty (cloud-only — the web lane has no local sidecar fallback),
// leaving the stt capability absent.
func buildWebSTTClient(cfg *config.Config) *multimodal.STTClient {
	if cfg.STTCloudModel == "" {
		return nil
	}
	return multimodal.NewSTTClient(multimodal.STTConfig{
		CloudModel:        cfg.STTCloudModel,
		Language:          cfg.STTLanguage,
		OpenRouterBaseURL: cfg.LLM.BaseURL,
		OpenRouterAPIKey:  cfg.LLM.APIKey,
		TimeoutSec:        cfg.MultimodalTimeoutSec,
	})
}
