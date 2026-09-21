package agui

// settings_voice_models.go answers the cockpit's speech model boxes (GET
// /api/settings/transcription-models and /speech-models) the way image-models and
// video-models answer theirs: same picker rows, same route rule. The cloud STT and TTS
// clients ride the daemon's OpenRouter route, so their models are listed from it and a
// local route is refused rather than answered with a list nothing could call.

import (
	"context"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
)

// ModalityCatalogLister lists the OpenRouter models of one output modality on the daemon's
// live route; a route that is not OpenRouter answers ErrCatalogLocalRoute.
type ModalityCatalogLister interface {
	List(ctx context.Context, modality string) ([]llm.ModelCatalogEntry, error)
}

// The OpenRouter output_modalities values, which are also the picker rows' kind and the
// leading segment of each route. They are the exact spellings the upstream filter accepts:
// measured 2026-09-21, "embeddings" lists 37 models while the singular "embedding" is
// rejected by name with the valid set in the error.
const (
	modalityTranscription = "transcription"
	modalitySpeech        = "speech"
	modalityEmbeddings    = "embeddings"
)

// SetModalityCatalog wires the output-modality catalogue. Until set, every route answers 503.
func (s *Server) SetModalityCatalog(lister ModalityCatalogLister) { s.modalityCatalog = lister }

func (s *Server) handleListTranscriptionModels(w http.ResponseWriter, r *http.Request) {
	s.listModalityModels(w, r, modalityTranscription)
}

func (s *Server) handleListSpeechModels(w http.ResponseWriter, r *http.Request) {
	s.listModalityModels(w, r, modalitySpeech)
}

func (s *Server) handleListEmbeddingModels(w http.ResponseWriter, r *http.Request) {
	s.listModalityModels(w, r, modalityEmbeddings)
}

func (s *Server) listModalityModels(w http.ResponseWriter, r *http.Request, modality string) {
	writeCatalogRows(w, s.modalityCatalog != nil, "modality",
		func() ([]llm.ModelCatalogEntry, error) { return s.modalityCatalog.List(r.Context(), modality) },
		func(model llm.ModelCatalogEntry) (catalogModelDTO, bool) {
			row := catalogModelDTO{ID: model.ID, Kind: modality}
			if modality != modalitySpeech {
				return row, true
			}
			// OpenRouter requires a model-specific voice on /audio/speech (documented at
			// https://openrouter.ai/docs/guides/overview/multimodal/tts). A row with no published
			// voice cannot be made valid by Aura's generic picker, so do not offer it as though
			// selecting the model alone were sufficient.
			if len(model.SupportedVoices) == 0 {
				return row, false
			}
			row.Voices = model.SupportedVoices
			return row, true
		})
}
