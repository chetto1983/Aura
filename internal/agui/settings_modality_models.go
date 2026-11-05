package agui

// settings_modality_models.go answers the cockpit's speech and embedding model boxes (GET
// /api/settings/transcription-models, /speech-models and /embeddings-models) the way
// image-models and video-models answer theirs: same picker rows, same route rule. The cloud
// STT, TTS and embedding clients run on OpenRouter whatever route the chat runs on, so their
// models are listed from OpenRouter.

import (
	"context"
	"errors"
	"net/http"

	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/llm"
)

// ModalityCatalogLister lists the OpenRouter models of one output modality.
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
	modalityDecisions     = "decisions"
)

// SetDecideLocalSidecar says whether the installer put a decision sidecar on this appliance: the
// boot snapshot of AURA_DECIDE_BASE_URL, which no settings row can change.
func (s *Server) SetDecideLocalSidecar(installed bool) { s.decideSidecar = installed }

// SetModalityCatalog wires the output-modality catalogue. Until set, every route answers 503.
func (s *Server) SetModalityCatalog(lister ModalityCatalogLister) { s.modalityCatalog = lister }

func (s *Server) handleListTranscriptionModels(w http.ResponseWriter, r *http.Request) {
	s.listModalityModels(w, r, modalityTranscription)
}

func (s *Server) handleListSpeechModels(w http.ResponseWriter, r *http.Request) {
	s.listModalityModels(w, r, modalitySpeech)
}

// handleListDecisionModels lists the models OpenRouter publishes with the decisions output, and
// says whether the Locale option exists on this appliance, on every answer: the flag is a boot
// fact, so it survives the list being unreachable.
func (s *Server) handleListDecisionModels(w http.ResponseWriter, r *http.Request) {
	installed := s.decideSidecar
	s.writeModalityRows(w, modalityDecisions, func() ([]llm.ModelCatalogEntry, error) {
		return s.modalityCatalog.List(r.Context(), modalityDecisions)
	}, func(dto *catalogDTO) { dto.LocalSidecar = &installed })
}

func (s *Server) handleListEmbeddingModels(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	s.writeModalityRows(w, modalityEmbeddings, func() ([]llm.ModelCatalogEntry, error) {
		return s.workingEmbeddingModels(r.Context(), refresh)
	})
}

// workingEmbeddingModels is the catalogue less what a route could not use, so the operator picks
// only what works (2026-10-20): a model taking fewer than minEmbeddingInputLimit tokens would cut
// every passage, and one that does not answer at the stored width fails every request once
// applied, as mistral-embed-2312 did on the lab VM. Without a key nothing can be checked, and
// the route preview still refuses a model that does not answer.
func (s *Server) workingEmbeddingModels(ctx context.Context, refresh bool) ([]llm.ModelCatalogEntry, error) {
	models, err := s.modalityCatalog.List(ctx, modalityEmbeddings)
	if err != nil {
		return nil, err
	}
	long := make([]llm.ModelCatalogEntry, 0, len(models))
	ids := make([]string, 0, len(models))
	for _, model := range models {
		if limit, err := embeddings.InputLimitOf(model); err == nil && limit >= minEmbeddingInputLimit {
			long = append(long, model)
			ids = append(ids, model.ID)
		}
	}
	if s.embeddingRoutes == nil {
		return long, nil
	}
	working, err := s.embeddingRoutes.CompatibleModels(ctx, ids, refresh)
	if errors.Is(err, embeddings.ErrNoCredential) {
		return long, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]llm.ModelCatalogEntry, 0, len(long))
	for _, model := range long {
		if working[model.ID] {
			out = append(out, model)
		}
	}
	return out, nil
}

func (s *Server) listModalityModels(w http.ResponseWriter, r *http.Request, modality string) {
	s.writeModalityRows(w, modality, func() ([]llm.ModelCatalogEntry, error) {
		return s.modalityCatalog.List(r.Context(), modality)
	})
}

func (s *Server) writeModalityRows(
	w http.ResponseWriter, modality string, list func() ([]llm.ModelCatalogEntry, error), annotate ...func(*catalogDTO),
) {
	writeCatalogRows(w, s.modalityCatalog != nil, "modality", list,
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
		}, annotate...)
}
