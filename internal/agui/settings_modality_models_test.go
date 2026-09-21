package agui

// Handler tests for GET /api/settings/transcription-models, /speech-models and
// /embeddings-models, over a recording ModalityCatalogLister so no branch touches the
// network. The route rule and the real OpenRouter read are exercised in cmd/aura and
// internal/llm.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

type fakeModalityCatalog struct {
	models []llm.ModelCatalogEntry
	err    error
	asked  []string
}

func (f *fakeModalityCatalog) List(_ context.Context, modality string) ([]llm.ModelCatalogEntry, error) {
	f.asked = append(f.asked, modality)
	return f.models, f.err
}

func modalityCatalogServer(catalog ModalityCatalogLister) *Server {
	s := NewServer(nil, nil, ServerConfig{})
	s.SetModalityCatalog(catalog)
	return s
}

func TestModalityModelRoutesAskForTheirOwnModalityAndReturnPickerRows(t *testing.T) {
	for _, tc := range []struct{ target, modality string }{
		{"/api/settings/transcription-models", "transcription"},
		{"/api/settings/speech-models", "speech"},
		{"/api/settings/embeddings-models", "embeddings"},
		// The browser cannot point the daemon at a host: a base_url is simply not read.
		{"/api/settings/speech-models?base_url=http%3A%2F%2Fattacker.test%2Fv1", "speech"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			catalog := &fakeModalityCatalog{models: []llm.ModelCatalogEntry{
				{ID: "openai/whisper-1", SupportedVoices: []string{"alloy"}},
				{ID: "qwen/qwen3-asr-1.7b", SupportedVoices: []string{"nova"}},
			}}
			rows := decodeMediaRows(t, getMediaModels(t, modalityCatalogServer(catalog), tc.target))
			if len(catalog.asked) != 1 || catalog.asked[0] != tc.modality {
				t.Fatalf("catalog asked for %v, want exactly [%s]", catalog.asked, tc.modality)
			}
			if len(rows) != 2 {
				t.Fatalf("rows = %v, want the two listed models", rows)
			}
			// No unit is published for a speech model's rate, so no price rides the row.
			want := map[string]any{"id": "openai/whisper-1", "kind": tc.modality, "has_price": false}
			if tc.modality == modalitySpeech {
				want["voices"] = []any{"alloy"}
			}
			assertRow(t, rows[0], want)
		})
	}
}

func TestSpeechModelsReturnSupportedVoicesAndDropUncallableRows(t *testing.T) {
	catalog := &fakeModalityCatalog{models: []llm.ModelCatalogEntry{
		{ID: "fish-audio/s1"},
		{ID: "qwen/qwen-audio-3.0-tts-flash", SupportedVoices: []string{"loongjohn", "longanhuan_v3.6"}},
	}}
	rows := decodeMediaRows(t, getMediaModels(t, modalityCatalogServer(catalog), "/api/settings/speech-models"))
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want only the model with published voices", rows)
	}
	assertRow(t, rows[0], map[string]any{
		"id": "qwen/qwen-audio-3.0-tts-flash", "kind": "speech", "has_price": false,
		"voices": []any{"loongjohn", "longanhuan_v3.6"},
	})
}

func TestModalityModelsRefuseALocalRouteWithTheWayOut(t *testing.T) {
	rr := getMediaModels(t, modalityCatalogServer(&fakeModalityCatalog{err: ErrCatalogLocalRoute}), "/api/settings/transcription-models")
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d (%s), want 409", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "voice") || !strings.Contains(body, "Cloud") {
		t.Fatalf("body = %s, want it to name voice models and the way out", body)
	}
}

func TestModalityModelsMapCatalogFailuresToBadGateway(t *testing.T) {
	err := fmt.Errorf("%w: %w", llm.ErrModelCatalogUnavailable, errors.New("GET /models returned 503"))
	rr := getMediaModels(t, modalityCatalogServer(&fakeModalityCatalog{err: err}), "/api/settings/speech-models")
	if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "returned 503") {
		t.Fatalf("status = %d body = %s, want 502 carrying the catalog's reason", rr.Code, rr.Body.String())
	}
}

func TestModalityModelsAnswerUnavailableUntilTheCatalogIsWired(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	for _, target := range []string{
		"/api/settings/transcription-models",
		"/api/settings/speech-models",
		"/api/settings/embeddings-models",
	} {
		if rr := getMediaModels(t, s, target); rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want 503", target, rr.Code)
		}
	}
}
