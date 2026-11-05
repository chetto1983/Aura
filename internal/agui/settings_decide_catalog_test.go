package agui

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func decisionsCatalogue(t *testing.T, s *Server) (rows []map[string]any, localSidecar bool) {
	t.Helper()
	rr := getMediaModels(t, s, "/api/settings/decisions-models")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rr.Code, rr.Body.String())
	}
	var body struct {
		Models       []map[string]any `json:"models"`
		LocalSidecar bool             `json:"local_sidecar"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body.Models, body.LocalSidecar
}

func TestDecisionsCatalogueAsksForDecisionsAndSaysWhetherALocalSidecarExists(t *testing.T) {
	for _, tc := range []struct {
		sidecar bool
	}{{true}, {false}} {
		catalog := &fakeModalityCatalog{models: []llm.ModelCatalogEntry{{ID: "liquid/d1.1"}}}
		s := modalityCatalogServer(catalog)
		s.SetDecideLocalSidecar(tc.sidecar)
		rows, local := decisionsCatalogue(t, s)
		if len(catalog.asked) != 1 || catalog.asked[0] != "decisions" {
			t.Fatalf("catalog asked for %v, want exactly [decisions]", catalog.asked)
		}
		if len(rows) != 1 || rows[0]["id"] != "liquid/d1.1" || rows[0]["kind"] != "decisions" {
			t.Fatalf("rows = %v, want the listed model as a decisions row", rows)
		}
		if local != tc.sidecar {
			t.Fatalf("local_sidecar = %v, want %v", local, tc.sidecar)
		}
	}
}

func TestDecisionsCatalogueAnswersUnavailableUntilTheCatalogIsWired(t *testing.T) {
	rr := getMediaModels(t, NewServer(nil, nil, ServerConfig{}), "/api/settings/decisions-models")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestDecisionsCatalogueKeepsTheCatalogsReasonOnFailure(t *testing.T) {
	s := modalityCatalogServer(&fakeModalityCatalog{err: llm.ErrModelCatalogUnavailable})
	if rr := getMediaModels(t, s, "/api/settings/decisions-models"); rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rr.Code)
	}
}
