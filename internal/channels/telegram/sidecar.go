// Package telegram — this file is the sidecar config for the ONE sidecar leg the
// channel still owns: outbound text-to-speech.
//
// It used to carry STT, vision and document-conversion knobs too, for four media
// clients living in this package. Those are gone: inbound voice, photos and
// documents are ingested as assets and processed by internal/assets, which reads
// the same knobs from internal/config itself. What remains is the voice-note reply
// — a Telegram-only surface (no other channel sends one), whose wire format is
// Telegram's and whose synthesis is the shared multimodal.TTSClient's.
package telegram

// MultimodalConfig is the telegram-package projection of the central internal/config
// TTS knobs (the upstream-named TTS_*/AURA_TTS_MODEL vars). It is populated by the
// composition root (serve_channels.go); keeping it local frees the telegram package
// from an internal/config import (the established config.go pattern). A zero value
// means TTS is unconfigured and the auto-speak path no-ops.
type MultimodalConfig struct {
	// TTSBaseURL/Voice/Format are the text-to-speech sidecar (aura-tts, Kokoro).
	// TTSCaption is the (optional) ASCII-safe caption put on the voice note.
	TTSBaseURL string
	TTSVoice   string
	TTSFormat  string
	TTSCaption string
	// TTSModel — AURA_TTS_MODEL — cloud TTS model; empty = local Kokoro sidecar.
	TTSModel string

	// OpenRouterBaseURL/APIKey are the shared cloud endpoint + key (the same
	// credential the agent loop uses), used by the cloud TTS leg. The key is set ONLY
	// on the Authorization header at request-build time, never logged or serialized
	// (the openai_compat D-28 discipline, enforced inside internal/multimodal).
	OpenRouterBaseURL string
	OpenRouterAPIKey  string

	// TimeoutSec bounds each sidecar request (T-13-08-SidecarDoS, 30s default when
	// zero). The timeout rides the request ctx, not http.Client.Timeout, so a
	// healthy slow body is not aborted mid-read.
	TimeoutSec int
}
