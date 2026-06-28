// Package telegram — this file is the shared 9c multimodal sidecar config the four
// media clients (voice/tts/photo/documents) project onto the central
// internal/config knobs. There is ZERO Go ML here: the sidecars own the models
// (faster-whisper STT, Kokoro TTS, GLM-OCR vision, markitdown). The wire format,
// HTTP client, request-timeout ctx and typed status error now live in the shared
// internal/multimodal package; the media clients wrap its VisionClient/STTClient/
// TTSClient (and documents.go calls its HTTPClient/TimeoutContext/StatusError
// directly for the LOCAL-only markitdown /convert leg).
package telegram

// MultimodalConfig is the telegram-package projection of the central
// internal/config multimodal knobs (AURA_VISION_CLOUD + the upstream-named
// MULTIMODAL_*/STT_*/TTS_* sidecar vars). It is populated by the composition root
// (plan 13-09) and passed to the media clients; keeping it local frees the
// telegram package from an internal/config import (the established config.go
// pattern). Zero values are sensible: an empty base URL means the corresponding
// modality is unconfigured (the handler degrades, never panics).
type MultimodalConfig struct {
	// VisionCloud routes image understanding: false (default) → the local
	// aura-ocr-vl sidecar; true → OpenRouter cloud vision. One env branch, zero
	// code dup (#60 / Pitfall 6).
	VisionCloud bool

	// Model is the primary LLM id (config.Model). photo.go reads SupportsVision on
	// it to decide whether the cloud branch attaches the image to the primary turn
	// or falls back to FallbackModel.
	Model string

	// MultimodalBaseURL/Model are the local vision sidecar (aura-ocr-vl) base +
	// model id. FallbackModel is the cloud vision model used when VisionCloud is
	// true and the primary Model lacks SupportsVision.
	MultimodalBaseURL string
	MultimodalModel   string
	FallbackModel     string

	// OpenRouterBaseURL/APIKey are the shared cloud endpoint + key (the same
	// credential the agent loop uses), reused by every cloud media leg
	// (VisionCloud, STTCloudModel, TTSModel). The key is set ONLY on the
	// Authorization header at request-build time, never logged or serialized (the
	// openai_compat D-28 discipline, enforced inside internal/multimodal).
	OpenRouterBaseURL string
	OpenRouterAPIKey  string

	// STTBaseURL/Model are the speech-to-text sidecar (aura-stt, faster-whisper).
	// STTLanguage pins the transcription language ("it"); empty = whisper
	// auto-detect, which mis-detects short clips (spike-027: probe used language=it).
	STTBaseURL  string
	STTModel    string
	STTLanguage string
	// STTCloudModel — AURA_STT_CLOUD_MODEL — cloud STT model; empty = local
	// faster-whisper sidecar.
	STTCloudModel string

	// TTSBaseURL/Voice/Format are the text-to-speech sidecar (aura-tts, Kokoro).
	// TTSCaption is the (optional) ASCII-safe caption put on the voice note.
	TTSBaseURL string
	TTSVoice   string
	TTSFormat  string
	TTSCaption string
	// TTSModel — AURA_TTS_MODEL — cloud TTS model; empty = local Kokoro sidecar.
	TTSModel string

	// DocumentsBaseURL is the markitdown /convert base.
	DocumentsBaseURL string

	// RetryBackoff is the per-retry sleep schedule in MILLISECONDS (voice.go: the
	// PRD 1s/2s default when nil). Exposed so tests pin a fast schedule.
	RetryBackoff []int

	// TimeoutSec bounds each sidecar request (T-13-08-SidecarDoS, 30s default when
	// zero). The timeout rides the request ctx, not http.Client.Timeout, so a
	// healthy slow body is not aborted mid-read.
	TimeoutSec int
}
