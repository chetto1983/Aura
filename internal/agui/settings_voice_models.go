package agui

// settings_voice_models.go answers the cockpit's speech model boxes (GET
// /api/settings/transcription-models and /speech-models) the way image-models and
// video-models answer theirs: same picker rows, same route rule. The cloud STT and TTS
// clients ride the daemon's OpenRouter route, so their models are listed from it and a
// local route is refused rather than answered with a list nothing could call.

import (
	"context"
	"errors"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
)

// VoiceCatalogLister lists the OpenRouter models of one output modality on the daemon's
// live route; a route that is not OpenRouter answers ErrMediaCatalogLocalRoute.
type VoiceCatalogLister interface {
	List(ctx context.Context, modality string) ([]llm.ModelCatalogEntry, error)
}

// The OpenRouter output_modalities values, which are also the picker rows' kind.
const (
	voiceModalityTranscription = "transcription"
	voiceModalitySpeech        = "speech"
)

// SetVoiceCatalog wires the speech model catalogue. Until set, both routes answer 503.
func (s *Server) SetVoiceCatalog(lister VoiceCatalogLister) { s.voiceCatalog = lister }

func (s *Server) handleListTranscriptionModels(w http.ResponseWriter, r *http.Request) {
	s.listVoiceModels(w, r, voiceModalityTranscription)
}

func (s *Server) handleListSpeechModels(w http.ResponseWriter, r *http.Request) {
	s.listVoiceModels(w, r, voiceModalitySpeech)
}

func (s *Server) listVoiceModels(w http.ResponseWriter, r *http.Request, modality string) {
	if s.voiceCatalog == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "voice model catalog not configured"})
		return
	}
	models, err := s.voiceCatalog.List(r.Context(), modality)
	switch {
	case errors.Is(err, ErrMediaCatalogLocalRoute):
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	case err != nil:
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	out := mediaCatalogDTO{Models: make([]mediaCatalogModelDTO, 0, len(models))}
	for _, model := range models {
		row := mediaCatalogModelDTO{ID: model.ID, Kind: modality}
		if modality == voiceModalitySpeech {
			// OpenRouter requires a model-specific voice on /audio/speech (documented at
			// https://openrouter.ai/docs/guides/overview/multimodal/tts). A row with no
			// published voice cannot be made valid by Aura's generic picker, so do not offer
			// it as though selecting the model alone were sufficient.
			if len(model.SupportedVoices) == 0 {
				continue
			}
			row.Voices = model.SupportedVoices
		}
		out.Models = append(out.Models, row)
	}
	writeJSON(w, out)
}
