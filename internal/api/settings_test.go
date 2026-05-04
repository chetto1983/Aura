package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/config"
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
			if it.Source != "db" {
				t.Errorf("LLM_API_KEY source = %q, want db", it.Source)
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

func TestSettingsList_FallsBackToEnv(t *testing.T) {
	// Settings store has no row for LLM_BASE_URL, but the bot is running
	// with an env value. The dashboard should show that effective value
	// with source="env" so the operator can see what's actually loaded
	// before deciding whether to override it.
	t.Setenv(settings.KeyLLMBaseURL, "https://from.env.example/v1")
	router, _ := newSettingsEnv(t)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	var resp SettingsListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	for _, it := range resp.Items {
		if it.Key == settings.KeyLLMBaseURL {
			if it.Value != "https://from.env.example/v1" {
				t.Errorf("env fallback value = %q", it.Value)
			}
			if it.Source != "env" {
				t.Errorf("env fallback source = %q, want env", it.Source)
			}
			return
		}
	}
	t.Errorf("LLM_BASE_URL not in items")
}

func TestSettingsList_DBOverridesEnv(t *testing.T) {
	t.Setenv(settings.KeyLLMBaseURL, "https://from.env.example/v1")
	router, store := newSettingsEnv(t)
	_ = store.Set(context.Background(), settings.KeyLLMBaseURL, "https://from.db.example/v1")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	var resp SettingsListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	for _, it := range resp.Items {
		if it.Key == settings.KeyLLMBaseURL {
			if it.Value != "https://from.db.example/v1" {
				t.Errorf("DB-wins value = %q", it.Value)
			}
			if it.Source != "db" {
				t.Errorf("DB-wins source = %q, want db", it.Source)
			}
			return
		}
	}
}

func TestSettingsList_DefaultSourceWhenNoEnvOrDB(t *testing.T) {
	// Make sure no leaked env var fights us.
	for _, k := range []string{settings.KeyLLMBaseURL, settings.KeyLLMAPIKey, settings.KeyLLMModel} {
		t.Setenv(k, "")
	}
	router, _ := newSettingsEnv(t)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	var resp SettingsListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	for _, it := range resp.Items {
		if it.Key == settings.KeyLLMBaseURL {
			if it.Source != "default" {
				t.Errorf("source = %q, want default", it.Source)
			}
			if it.Value != "" {
				t.Errorf("value = %q, want empty", it.Value)
			}
		}
	}
}

func TestSettingsList_AuraBotShowsEditableDefaults(t *testing.T) {
	for _, k := range []string{
		settings.KeyAuraBotEnabled,
		settings.KeyAuraBotMaxActive,
		settings.KeyAuraBotMaxDepth,
		settings.KeyAuraBotTimeoutSec,
		settings.KeyAuraBotMaxIterations,
	} {
		t.Setenv(k, "")
	}
	router, _ := newSettingsEnv(t)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	var resp SettingsListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	want := map[string]string{
		settings.KeyAuraBotEnabled:       "false",
		settings.KeyAuraBotMaxActive:     "4",
		settings.KeyAuraBotMaxDepth:      "1",
		settings.KeyAuraBotTimeoutSec:    "300",
		settings.KeyAuraBotMaxIterations: "5",
	}
	for key, value := range want {
		var found bool
		for _, it := range resp.Items {
			if it.Key != key {
				continue
			}
			found = true
			if it.Value != value || it.Source != "default" || it.Group != "aurabot" {
				t.Fatalf("%s = value:%q source:%q group:%q, want value:%q source:default group:aurabot", key, it.Value, it.Source, it.Group, value)
			}
		}
		if !found {
			t.Fatalf("%s not in settings catalog", key)
		}
	}
}

func TestSettingsList_ShowsRestartRequiredWhenSavedDiffersFromRuntime(t *testing.T) {
	_, store := newSettingsEnv(t)
	ctx := context.Background()
	if err := store.Set(ctx, settings.KeyAuraBotTimeoutSec, "600"); err != nil {
		t.Fatalf("set timeout: %v", err)
	}
	router := NewRouter(Deps{
		Settings:      store,
		RuntimeConfig: &config.Config{AuraBotTimeoutSec: 300},
	})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	var resp SettingsListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	for _, it := range resp.Items {
		if it.Key != settings.KeyAuraBotTimeoutSec {
			continue
		}
		if it.Value != "600" || it.ActiveValue != "300" || !it.RestartRequired {
			t.Fatalf("timeout row = value:%q active:%q restart:%v", it.Value, it.ActiveValue, it.RestartRequired)
		}
		return
	}
	t.Fatal("AURABOT_TIMEOUT_SEC not in items")
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

func TestSettingsUpdate_AppliesRuntimeSettingsHook(t *testing.T) {
	_, store := newSettingsEnv(t)
	cfg := &config.Config{AuraBotTimeoutSec: 300}
	var calls int
	router := NewRouter(Deps{
		Settings:      store,
		RuntimeConfig: cfg,
		ApplyRuntimeSettings: func(ctx context.Context) error {
			calls++
			cfg.AuraBotTimeoutSec = store.GetInt(ctx, settings.KeyAuraBotTimeoutSec, cfg.AuraBotTimeoutSec)
			return nil
		},
	})

	body := `{"updates":{"AURABOT_TIMEOUT_SEC":"600"}}`
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(body))))
	if rr.Code != 200 {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var update SettingsUpdateResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &update)
	if !update.OK || !update.RuntimeApplied || calls != 1 {
		t.Fatalf("update = %+v calls=%d", update, calls)
	}

	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	var list SettingsListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	for _, it := range list.Items {
		if it.Key != settings.KeyAuraBotTimeoutSec {
			continue
		}
		if it.Value != "600" || it.ActiveValue != "600" || it.RestartRequired {
			t.Fatalf("timeout row = value:%q active:%q restart:%v", it.Value, it.ActiveValue, it.RestartRequired)
		}
		return
	}
	t.Fatal("AURABOT_TIMEOUT_SEC not in items")
}

func TestSettingsUpdate_DoesNotApplyRuntimeHookForRestartOnlyAuraBotEnable(t *testing.T) {
	_, store := newSettingsEnv(t)
	var calls int
	router := NewRouter(Deps{
		Settings: store,
		ApplyRuntimeSettings: func(context.Context) error {
			calls++
			return nil
		},
	})

	body := `{"updates":{"AURABOT_ENABLED":"true"}}`
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(body))))
	if rr.Code != 200 {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if calls != 0 {
		t.Fatalf("runtime hook calls = %d, want 0 for restart-only enable toggle", calls)
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
