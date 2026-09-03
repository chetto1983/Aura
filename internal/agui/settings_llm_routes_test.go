package agui

// Handler tests for GET /api/settings/llm-routes and for the write path that fills it:
// every persisted route change must leave behind a row the cockpit can restore.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type fakeRouteStore struct {
	rows      []sqlc.AuraLlmProviderRoutes
	upserted  []sqlc.AuraLlmProviderRoutes
	listErr   error
	upsertErr error
}

func (f *fakeRouteStore) ListRoutes(context.Context) ([]sqlc.AuraLlmProviderRoutes, error) {
	return f.rows, f.listErr
}

func (f *fakeRouteStore) UpsertRoute(
	_ context.Context, provider, baseURL, model, by string,
) (sqlc.AuraLlmProviderRoutes, error) {
	if f.upsertErr != nil {
		return sqlc.AuraLlmProviderRoutes{}, f.upsertErr
	}
	row := sqlc.AuraLlmProviderRoutes{
		Provider: provider, BaseUrl: baseURL, Model: model,
		UpdatedBy: pgtype.Text{String: by, Valid: by != ""},
	}
	f.upserted = append(f.upserted, row)
	return row, nil
}

func TestHandleListLLMRoutesProjectsStoredRoutes(t *testing.T) {
	s := &Server{llmRoutes: &fakeRouteStore{rows: []sqlc.AuraLlmProviderRoutes{
		{Provider: "llamacpp", BaseUrl: "http://host.docker.internal:8084/v1", Model: "gemma-4-12b",
			UpdatedBy: pgtype.Text{String: "operator", Valid: true}},
		{Provider: "openrouter", BaseUrl: "https://openrouter.ai/api/v1", Model: "z-ai/glm-5.3"},
	}}}
	rr := httptest.NewRecorder()
	s.handleListLLMRoutes(rr, httptest.NewRequest(http.MethodGet, "/api/settings/llm-routes", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rr.Code, rr.Body.String())
	}
	var out llmProviderRoutesDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Routes) != 2 {
		t.Fatalf("routes = %+v, want 2", out.Routes)
	}
	if out.Routes[0].Provider != "llamacpp" ||
		out.Routes[0].BaseURL != "http://host.docker.internal:8084/v1" ||
		out.Routes[0].Model != "gemma-4-12b" || out.Routes[0].UpdatedBy != "operator" {
		t.Fatalf("first route = %+v", out.Routes[0])
	}
}

func TestHandleListLLMRoutesUnwiredAndFailing(t *testing.T) {
	unwired := httptest.NewRecorder()
	(&Server{}).handleListLLMRoutes(unwired, httptest.NewRequest(http.MethodGet, "/api/settings/llm-routes", nil))
	if unwired.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired status = %d, want 503", unwired.Code)
	}
	failing := httptest.NewRecorder()
	s := &Server{llmRoutes: &fakeRouteStore{listErr: errors.New("pool closed")}}
	s.handleListLLMRoutes(failing, httptest.NewRequest(http.MethodGet, "/api/settings/llm-routes", nil))
	if failing.Code != http.StatusBadGateway {
		t.Fatalf("failing status = %d, want 502", failing.Code)
	}
}

func putLLMProfile(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/settings/llm-profile", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handlePutLLMProfile(rr, withPrincipal(req, "operator"))
	return rr
}

func TestPutLLMProfileRemembersTheRouteItPersisted(t *testing.T) {
	routes := &fakeRouteStore{}
	s := &Server{
		settings:         &fakeSettingsStore{},
		llmRoutes:        routes,
		llmRouteReloader: &fakeLLMRouteReloader{},
	}
	rr := putLLMProfile(t, s, `{"settings":{
		"AURA_LLM_PROVIDER":"ollama",
		"AURA_LLM_BASE_URL":"http://host.docker.internal:11434/v1",
		"AURA_LLM_MODEL":"qwen4:14b"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rr.Code, rr.Body.String())
	}
	if len(routes.upserted) != 1 {
		t.Fatalf("remembered routes = %+v, want exactly the saved one", routes.upserted)
	}
	saved := routes.upserted[0]
	if saved.Provider != "ollama" || saved.BaseUrl != "http://host.docker.internal:11434/v1" ||
		saved.Model != "qwen4:14b" || saved.UpdatedBy.String != "operator" {
		t.Fatalf("remembered route = %+v", saved)
	}
}

func TestPutLLMProfileSurvivesARouteMemoryFailure(t *testing.T) {
	// The profile is already live when the memory is written: losing the convenience must
	// not turn a successful switch into an error the operator has to interpret.
	s := &Server{
		settings:         &fakeSettingsStore{},
		llmRoutes:        &fakeRouteStore{upsertErr: errors.New("pool closed")},
		llmRouteReloader: &fakeLLMRouteReloader{},
	}
	rr := putLLMProfile(t, s, `{"settings":{
		"AURA_LLM_PROVIDER":"llamacpp",
		"AURA_LLM_BASE_URL":"http://aura-llm:8084/v1",
		"AURA_LLM_MODEL":"gemma-4-12b"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200 — the route was published", rr.Code, rr.Body.String())
	}
}

func TestPutSettingRemembersOnlyRouteKeys(t *testing.T) {
	for _, tc := range []struct {
		name     string
		key      string
		value    string
		rows     []sqlc.AuraSettings
		wantRows int
	}{
		{
			name: "a model change re-reads the provider and base URL from the stored profile",
			key:  "AURA_LLM_MODEL", value: "qwen4:14b",
			rows: []sqlc.AuraSettings{
				{Key: "AURA_LLM_PROVIDER", Value: "ollama"},
				{Key: "AURA_LLM_BASE_URL", Value: "http://host.docker.internal:11434/v1"},
				{Key: "AURA_LLM_MODEL", Value: "gemma4:31b-cloud"},
			},
			wantRows: 1,
		},
		{
			name: "a loop budget is not a route",
			key:  "AURA_LOOP_MAX_STEPS", value: "40",
			rows: []sqlc.AuraSettings{
				{Key: "AURA_LLM_PROVIDER", Value: "ollama"},
				{Key: "AURA_LLM_BASE_URL", Value: "http://host.docker.internal:11434/v1"},
				{Key: "AURA_LLM_MODEL", Value: "gemma4:31b-cloud"},
			},
			wantRows: 0,
		},
		{
			name: "an incomplete profile remembers nothing",
			key:  "AURA_LLM_MODEL", value: "qwen4:14b",
			rows:     []sqlc.AuraSettings{{Key: "AURA_LLM_PROVIDER", Value: "ollama"}},
			wantRows: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AURA_LLM_BASE_URL", "")
			routes := &fakeRouteStore{}
			s := &Server{
				settings:         &fakeSettingsStore{rows: tc.rows},
				llmRoutes:        routes,
				llmRouteReloader: &fakeLLMRouteReloader{},
			}
			req := httptest.NewRequest(http.MethodPut, "/api/settings/"+tc.key,
				strings.NewReader(`{"value":"`+tc.value+`"}`))
			req.SetPathValue("key", tc.key)
			rr := httptest.NewRecorder()
			s.handlePutSetting(rr, withPrincipal(req, "operator"))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d (%s), want 200", rr.Code, rr.Body.String())
			}
			if len(routes.upserted) != tc.wantRows {
				t.Fatalf("remembered routes = %+v, want %d", routes.upserted, tc.wantRows)
			}
			if tc.wantRows == 1 && routes.upserted[0].Model != tc.value {
				t.Fatalf("remembered model = %q, want the value just written", routes.upserted[0].Model)
			}
		})
	}
}
