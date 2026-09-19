package agui

// Handler tests for GET /api/settings/transcription-models and /speech-models, over a
// recording VoiceCatalogLister so no branch touches the network. The route rule and the
// real OpenRouter read are exercised in cmd/aura and internal/llm.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

type fakeVoiceCatalog struct {
	models []llm.ModelCatalogEntry
	err    error
	asked  []string
}

func (f *fakeVoiceCatalog) List(_ context.Context, modality string) ([]llm.ModelCatalogEntry, error) {
	f.asked = append(f.asked, modality)
	return f.models, f.err
}

func voiceCatalogServer(catalog VoiceCatalogLister) *Server {
	s := NewServer(nil, nil, ServerConfig{})
	s.SetVoiceCatalog(catalog)
	return s
}

func TestVoiceModelRoutesAskForTheirOwnModalityAndReturnPickerRows(t *testing.T) {
	for _, tc := range []struct{ target, modality string }{
		{"/api/settings/transcription-models", "transcription"},
		{"/api/settings/speech-models", "speech"},
		// The browser cannot point the daemon at a host: a base_url is simply not read.
		{"/api/settings/speech-models?base_url=http%3A%2F%2Fattacker.test%2Fv1", "speech"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			catalog := &fakeVoiceCatalog{models: []llm.ModelCatalogEntry{{ID: "openai/whisper-1"}, {ID: "qwen/qwen3-asr-1.7b"}}}
			rows := decodeMediaRows(t, getMediaModels(t, voiceCatalogServer(catalog), tc.target))
			if len(catalog.asked) != 1 || catalog.asked[0] != tc.modality {
				t.Fatalf("catalog asked for %v, want exactly [%s]", catalog.asked, tc.modality)
			}
			if len(rows) != 2 {
				t.Fatalf("rows = %v, want the two listed models", rows)
			}
			// No unit is published for a speech model's rate, so no price rides the row.
			assertRow(t, rows[0], map[string]any{"id": "openai/whisper-1", "kind": tc.modality, "has_price": false})
		})
	}
}

func TestVoiceModelsRefuseALocalRouteWithTheWayOut(t *testing.T) {
	rr := getMediaModels(t, voiceCatalogServer(&fakeVoiceCatalog{err: ErrMediaCatalogLocalRoute}), "/api/settings/transcription-models")
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d (%s), want 409", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "voice") || !strings.Contains(body, "Cloud") {
		t.Fatalf("body = %s, want it to name voice models and the way out", body)
	}
}

func TestVoiceModelsMapCatalogFailuresToBadGateway(t *testing.T) {
	err := fmt.Errorf("%w: %w", llm.ErrModelCatalogUnavailable, errors.New("GET /models returned 503"))
	rr := getMediaModels(t, voiceCatalogServer(&fakeVoiceCatalog{err: err}), "/api/settings/speech-models")
	if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "returned 503") {
		t.Fatalf("status = %d body = %s, want 502 carrying the catalog's reason", rr.Code, rr.Body.String())
	}
}

func TestVoiceModelsAnswerUnavailableUntilTheCatalogIsWired(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	for _, target := range []string{"/api/settings/transcription-models", "/api/settings/speech-models"} {
		if rr := getMediaModels(t, s, target); rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want 503", target, rr.Code)
		}
	}
}
