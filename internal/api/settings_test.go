package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/settings"
)

func newSettingsEnv(t *testing.T) (http.Handler, *settings.Store) {
	t.Helper()
	store, err := settings.OpenStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	router := NewRouter(Deps{Settings: store})
	return router, store
}

func TestSettingsList_HappyPath(t *testing.T) {
	router, store := newSettingsEnv(t)
	ctx := context.Background()
	if err := store.Set(ctx, settings.KeyLLMAPIKey, "sk-test"); err != nil {
		t.Fatalf("set: %v", err)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var resp SettingsListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatal("expected items in catalog")
	}
	var found bool
	for _, it := range resp.Items {
		if it.Key == settings.KeyLLMAPIKey {
			found = true
			if it.Value != "sk-test" {
				t.Errorf("LLM_API_KEY value = %q, want sk-test", it.Value)
			}
			if !it.IsSecret {
				t.Errorf("LLM_API_KEY should be marked is_secret")
			}
		}
	}
	if !found {
		t.Errorf("LLM_API_KEY not in items")
	}
}

func TestSettingsList_NoStore503(t *testing.T) {
	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	if rr.Code != 503 {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

func TestSettingsUpdate_HappyPath(t *testing.T) {
	router, store := newSettingsEnv(t)

	body := `{"updates":{"LLM_API_KEY":"sk-new","LLM_MODEL":"gpt-4o"}}`
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(body))))
	if rr.Code != 200 {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var resp SettingsUpdateResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp.OK || len(resp.Errors) != 0 {
		t.Errorf("update result: %+v", resp)
	}

	if got, _ := store.Get(context.Background(), "LLM_API_KEY"); got != "sk-new" {
		t.Errorf("LLM_API_KEY persisted = %q", got)
	}
	if got, _ := store.Get(context.Background(), "LLM_MODEL"); got != "gpt-4o" {
		t.Errorf("LLM_MODEL persisted = %q", got)
	}
}

func TestSettingsUpdate_BlankValueDeletes(t *testing.T) {
	router, store := newSettingsEnv(t)
	_ = store.Set(context.Background(), "LLM_API_KEY", "sk-old")

	body := `{"updates":{"LLM_API_KEY":""}}`
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(body))))
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	if _, err := store.Get(context.Background(), "LLM_API_KEY"); err != settings.ErrNotFound {
		t.Errorf("expected ErrNotFound after blank update, got %v", err)
	}
}

func TestSettingsUpdate_RejectsBootstrapKey(t *testing.T) {
	router, store := newSettingsEnv(t)
	body := `{"updates":{"TELEGRAM_TOKEN":"hijack"}}`
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(body))))
	if rr.Code != 400 {
		t.Errorf("status = %d, want 400 (bootstrap key not overridable)", rr.Code)
	}
	// Sanity: nothing got written.
	if _, err := store.Get(context.Background(), "TELEGRAM_TOKEN"); err != settings.ErrNotFound {
		t.Errorf("TELEGRAM_TOKEN was accepted into store")
	}
}

func TestSettingsUpdate_RejectsUnknownKey(t *testing.T) {
	router, _ := newSettingsEnv(t)
	body := `{"updates":{"GARBAGE_KEY":"x"}}`
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(body))))
	if rr.Code != 400 {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestSettingsUpdate_RejectsBadJSON(t *testing.T) {
	router, _ := newSettingsEnv(t)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(`{not json`))))
	if rr.Code != 400 {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestSettingsTest_RoundTrip(t *testing.T) {
	// Real probe target via httptest so we don't depend on probe.go internals.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"x"}]}`))
	}))
	defer srv.Close()

	router, _ := newSettingsEnv(t)
	body := `{"base_url":"` + srv.URL + `","api_key":"k","probe_path":"/v1/models"}`
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings/test", bytes.NewReader([]byte(body))))
	if rr.Code != 200 {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Errorf("expected ok=true, got %v", resp)
	}
}
