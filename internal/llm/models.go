package llm

import "strings"

// modelCapabilities is the net-new model→capability table (Phase 13 / UX-04).
// There is no Model struct in this package — Config.Model is a bare string
// (config.go) — so vision/audio routing is decided by this small lookup instead
// of an instance method. The photo client (plan 13-08) reads SupportsVision to
// choose the AURA_VISION_CLOUD=true cloud branch (OpenRouter image_url) vs the
// local GLM-OCR sidecar; an unknown model conservatively reports false so a
// typo never routes an image to a non-vision model (threat T-13-03-UnknownModelVision).
//
// Audio is sidecar-routed this phase (faster-whisper STT / Kokoro TTS), so no
// current model reports audio support; SupportsAudio exists for forward-compat
// symmetry and to give the renderer one stable seam when a native-audio model
// is added.
type modelCapabilities struct {
	vision bool
	audio  bool
}

var modelCapabilityTable = map[string]modelCapabilities{
	"deepseek/deepseek-v4-flash": {vision: false, audio: false},
	"minimax/minimax-m3":         {vision: true, audio: false},
}

// normalizeModelID strips an OpenRouter routing suffix (the part after the first
// ':', e.g. ':exacto' / ':flash' / ':thinking') and surrounding whitespace so a
// suffixed id resolves to the same base capability entry. The match is on the
// full base id, never a substring, so a misleading suffix or prefix cannot
// promote an unknown model.
func normalizeModelID(model string) string {
	model = strings.TrimSpace(model)
	if i := strings.IndexByte(model, ':'); i >= 0 {
		model = model[:i]
	}
	return model
}

// SupportsVision reports whether the model can accept image input directly. It
// is true for minimax/minimax-m3 (incl. any ':'-suffixed routing variant) and
// false for deepseek/deepseek-v4-flash and every unknown model (conservative
// default). The photo client gates the AURA_VISION_CLOUD cloud branch on this.
func SupportsVision(model string) bool {
	return modelCapabilityTable[normalizeModelID(model)].vision
}

// SupportsAudio reports whether the model can accept audio input directly. It is
// false for both current models (audio is sidecar-routed this phase) and for
// unknown models; the function exists for forward-compat symmetry.
func SupportsAudio(model string) bool {
	return modelCapabilityTable[normalizeModelID(model)].audio
}
