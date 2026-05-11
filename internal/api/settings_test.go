package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/settings"
)

type fakeSettingsRepository struct {
	values map[string]string
}

func (f *fakeSettingsRepository) Get(_ context.Context, key string) (string, error) {
	v, ok := f.values[key]
	if !ok {
		return "", settings.ErrNotFound
	}
	return v, nil
}

func (f *fakeSettingsRepository) Set(_ context.Context, key, value string) error {
	if key == "FAIL_SET" {
		return errors.New("forced set failure")
	}
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[key] = value
	return nil
}

func (f *fakeSettingsRepository) Delete(_ context.Context, key string) error {
	delete(f.values, key)
	return nil
}

func (f *fakeSettingsRepository) All(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(f.values))
	for k, v := range f.values {
		out[k] = v
	}
	return out, nil
}

func newSettingsEnv(t *testing.T) (http.Handler, *settings.Store) {
	t.Helper()
	store := mustSettingsStore(t)
	router := NewRouter(Deps{Settings: store})
	return router, store
}

func mustSettingsStore(t *testing.T) *settings.Store {
	t.Helper()
	store, err := settings.OpenStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
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
			if it.Value != "" {
				t.Errorf("LLM_API_KEY value = %q, want redacted empty edit field", it.Value)
			}
			if it.ActiveValue != "(configured)" {
				t.Errorf("LLM_API_KEY active_value = %q, want configured placeholder", it.ActiveValue)
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

func TestSettingsHandlersAcceptRepositoryInterface(t *testing.T) {
	repo := &fakeSettingsRepository{values: map[string]string{
		settings.KeyLLMModel: "fake-model",
	}}
	router := NewRouter(Deps{Settings: repo})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status %d, body %s", rr.Code, rr.Body)
	}
	var list SettingsListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var found bool
	for _, it := range list.Items {
		if it.Key == settings.KeyLLMModel {
			found = true
			if it.Value != "fake-model" || it.Source != "db" {
				t.Fatalf("LLM_MODEL row = value:%q source:%q", it.Value, it.Source)
			}
		}
	}
	if !found {
		t.Fatal("LLM_MODEL row missing")
	}

	body := `{"updates":{"LLM_MODEL":"updated-model"}}`
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(body))))
	if rr.Code != http.StatusOK {
		t.Fatalf("POST status %d, body %s", rr.Code, rr.Body)
	}
	if repo.values[settings.KeyLLMModel] != "updated-model" {
		t.Fatalf("repo LLM_MODEL = %q, want updated-model", repo.values[settings.KeyLLMModel])
	}
}

func TestSettingsCatalogCoversLLMAndIsEditable(t *testing.T) {
	catalog := map[string]SettingItem{}
	for _, item := range settingsCatalog {
		if !settings.IsOverridable(item.Key) {
			t.Fatalf("%s is in settings catalog but is not overridable", item.Key)
		}
		if item.ReadOnly {
			t.Fatalf("%s is in settings catalog but marked read-only", item.Key)
		}
		catalog[item.Key] = item
	}
	// Every key the dashboard must surface so the LLM stack is fully
	// configurable without editing .env. New LLM-adjacent knobs should be
	// added here so they don't silently drop out of the UI.
	for _, key := range []string{
		settings.KeyTimezone,

		settings.KeyLLMBaseURL,
		settings.KeyLLMModel,
		settings.KeyLLMAPIKey,
		settings.KeyLLMMaxRetries,

		settings.KeyMaxContextTokens,
		settings.KeyMaxHistoryMessages,
		settings.KeySoftBudget,
		settings.KeyHardBudget,
		settings.KeyCostInputPerMTokens,
		settings.KeyCostOutputPerMTokens,

		settings.KeyPromptVersion,
		settings.KeySkillRoutingMode,
		settings.KeyAgentLoopMaxSteps,
		settings.KeyTerminalToolPolicy,
		settings.KeyDelegationMode,
		settings.KeySkillsAdmin,

		settings.KeyWebSearchProvider,
		settings.KeySearXNGBaseURL,

		settings.KeyQdrantURL,
		settings.KeyQdrantCollection,
		settings.KeyQdrantAPIKey,

		settings.KeyEmbeddingBaseURL,
		settings.KeyEmbeddingModel,
		settings.KeyEmbeddingAPIKey,

		settings.KeyMistralAPIKey,
	} {
		if _, ok := catalog[key]; !ok {
			t.Fatalf("%s missing from settings catalog", key)
		}
	}
}

func TestSettingsList_ShowsQdrantKeysEditableAndRedacted(t *testing.T) {
	store := mustSettingsStore(t)
	router := NewRouter(Deps{
		Settings: store,
		RuntimeConfig: &config.Config{
			QdrantURL:        "http://qdrant:6333",
			QdrantCollection: "aura_memory_v1",
			QdrantAPIKey:     "secret",
		},
	})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp SettingsListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	want := map[string]string{
		settings.KeyQdrantURL:        "http://qdrant:6333",
		settings.KeyQdrantCollection: "aura_memory_v1",
	}
	for key, value := range want {
		found := false
		for _, it := range resp.Items {
			if it.Key != key {
				continue
			}
			found = true
			if it.Value != value || it.ActiveValue != value || it.ReadOnly {
				t.Fatalf("%s = value:%q active:%q readonly:%v", key, it.Value, it.ActiveValue, it.ReadOnly)
			}
		}
		if !found {
			t.Fatalf("%s not in settings response", key)
		}
	}
	foundSecret := false
	for _, it := range resp.Items {
		if it.Key != settings.KeyQdrantAPIKey {
			continue
		}
		foundSecret = true
		if it.Value != "" || it.ActiveValue != "(configured)" || !it.IsSecret {
			t.Fatalf("QDRANT_API_KEY leaked: value=%q active=%q secret=%v", it.Value, it.ActiveValue, it.IsSecret)
		}
	}
	if !foundSecret {
		t.Fatal("QDRANT_API_KEY not in settings response")
	}
	for _, it := range resp.Items {
		if it.Key == "AURA_TOOLSET_MODE" || it.Key == "AURA_ORCHESTRATION_LOG_LEVEL" {
			t.Fatalf("removed orchestration setting leaked into settings response: %s", it.Key)
		}
	}
}

func TestSettingsUpdate_AcceptsRuntimeAndSandboxKeys(t *testing.T) {
	router, store := newSettingsEnv(t)
	body := `{"updates":{"HTTP_PORT":"0.0.0.0:9090","SANDBOX_TIMEOUT_SEC":"45"}}`
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(body))))
	if rr.Code != 200 {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if got, _ := store.Get(context.Background(), settings.KeyHTTPPort); got != "0.0.0.0:9090" {
		t.Fatalf("HTTP_PORT persisted = %q", got)
	}
	if got, _ := store.Get(context.Background(), settings.KeySandboxTimeoutSec); got != "45" {
		t.Fatalf("SANDBOX_TIMEOUT_SEC persisted = %q", got)
	}
}

func TestSettingsList_RedactsEnvSecrets(t *testing.T) {
	t.Setenv(settings.KeyEmbeddingAPIKey, "embed-secret")
	router, _ := newSettingsEnv(t)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var resp SettingsListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, it := range resp.Items {
		if it.Key != settings.KeyEmbeddingAPIKey {
			continue
		}
		if it.Value != "" || it.ActiveValue != "(configured)" {
			t.Fatalf("secret row leaked: value=%q active_value=%q", it.Value, it.ActiveValue)
		}
		if it.Source != "env" || !it.IsSecret {
			t.Fatalf("secret row metadata = source:%q is_secret:%v", it.Source, it.IsSecret)
		}
		return
	}
	t.Fatal("EMBEDDING_API_KEY not in items")
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

func TestSettingsList_ShowsRestartRequiredWhenSavedDiffersFromRuntime(t *testing.T) {
	_, store := newSettingsEnv(t)
	ctx := context.Background()
	if err := store.Set(ctx, settings.KeyLLMModel, "next-model"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	router := NewRouter(Deps{
		Settings:      store,
		RuntimeConfig: &config.Config{LLMModel: "running-model"},
	})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/settings", nil))
	var resp SettingsListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	for _, it := range resp.Items {
		if it.Key != settings.KeyLLMModel {
			continue
		}
		if it.Value != "next-model" || it.ActiveValue != "running-model" || !it.RestartRequired {
			t.Fatalf("model row = value:%q active:%q restart:%v", it.Value, it.ActiveValue, it.RestartRequired)
		}
		return
	}
	t.Fatal("LLM_MODEL not in items")
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
	if cfg.AuraBotTimeoutSec != 600 {
		t.Fatalf("cfg.AuraBotTimeoutSec = %d, want 600", cfg.AuraBotTimeoutSec)
	}
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

func TestRestartEndpointCallsRestartHook(t *testing.T) {
	var calls int
	router := NewRouter(Deps{
		Restart: func(context.Context) error {
			calls++
			return nil
		},
	})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/restart", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if calls != 1 {
		t.Fatalf("restart calls = %d, want 1", calls)
	}
}

func TestRestartEndpointUnavailableWithoutHook(t *testing.T) {
	router := NewRouter(Deps{})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/restart", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503; body=%s", rr.Code, rr.Body)
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

func TestSettingsUpdate_AcceptsTelegramTokenOverride(t *testing.T) {
	router, store := newSettingsEnv(t)
	body := `{"updates":{"TELEGRAM_TOKEN":"123456:override"}}`
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/settings", bytes.NewReader([]byte(body))))
	if rr.Code != 200 {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if got, _ := store.Get(context.Background(), settings.KeyTelegramToken); got != "123456:override" {
		t.Errorf("TELEGRAM_TOKEN persisted = %q", got)
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
