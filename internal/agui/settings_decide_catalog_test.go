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

// The flag is a boot fact, not an OpenRouter one: an appliance with no connection to OpenRouter
// still has its local sidecar, and the cockpit must be told so on every answer it gets.
func TestDecisionsCatalogueSaysWhetherASidecarExistsWhenItsListCannotBeRead(t *testing.T) {
	for _, sidecar := range []bool{true, false} {
		failing := modalityCatalogServer(&fakeModalityCatalog{err: llm.ErrModelCatalogUnavailable})
		unwired := NewServer(nil, nil, ServerConfig{})
		for name, s := range map[string]*Server{"502": failing, "503": unwired} {
			s.SetDecideLocalSidecar(sidecar)
			rr := getMediaModels(t, s, "/api/settings/decisions-models")
			want := map[string]int{"502": http.StatusBadGateway, "503": http.StatusServiceUnavailable}[name]
			if rr.Code != want {
				t.Fatalf("%s: status = %d, want %d", name, rr.Code, want)
			}
			var body map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s: decode: %v", name, err)
			}
			if body["local_sidecar"] != sidecar || body["error"] == nil {
				t.Fatalf("%s sidecar=%v: body = %v, want the error and local_sidecar=%v", name, sidecar, body, sidecar)
			}
		}
	}
}

func TestOtherCatalogueErrorsCarryNoSidecarFlag(t *testing.T) {
	s := modalityCatalogServer(&fakeModalityCatalog{err: llm.ErrModelCatalogUnavailable})
	rr := getMediaModels(t, s, "/api/settings/speech-models")
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := body["local_sidecar"]; present {
		t.Fatalf("body = %v: only the decisions catalogue says whether a sidecar exists", body)
	}
}
