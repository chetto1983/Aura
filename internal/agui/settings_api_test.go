package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

type fakeSettingsStore struct {
	rows     []sqlc.AuraSettings
	upserted map[string]string
	deleted  []string
}

type fakeLLMRouteReloader struct {
	validated []map[string]string
	resets    [][]string
	applied   []map[string]string
	effective map[string]string
	err       error
}

func (f *fakeLLMRouteReloader) Prepare(_ context.Context, overrides map[string]string, resetKeys []string) (func(), error) {
	f.validated = append(f.validated, maps.Clone(overrides))
	f.resets = append(f.resets, slices.Clone(resetKeys))
	if f.err != nil {
		return nil, f.err
	}
	prepared := maps.Clone(overrides)
	return func() { f.applied = append(f.applied, prepared) }, nil
}

func (f *fakeLLMRouteReloader) EffectiveValue(key string) (string, bool) {
	value, ok := f.effective[key]
	return value, ok
}

func (f *fakeSettingsStore) List(context.Context) ([]sqlc.AuraSettings, error) { return f.rows, nil }

func (f *fakeSettingsStore) Upsert(_ context.Context, key, value, _ string) (sqlc.AuraSettings, error) {
	if f.upserted == nil {
		f.upserted = map[string]string{}
	}
	f.upserted[key] = value
	return sqlc.AuraSettings{Key: key, Value: value}, nil
}

func (f *fakeSettingsStore) ReplaceMany(
	ctx context.Context, values map[string]string, deletes []string, by string,
) ([]sqlc.AuraSettings, error) {
	for _, key := range deletes {
		if err := f.Delete(ctx, key); err != nil {
			return nil, err
		}
	}
	rows := make([]sqlc.AuraSettings, 0, len(values))
	for key, value := range values {
		row, err := f.Upsert(ctx, key, value, by)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (f *fakeSettingsStore) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

func settingItemByKey(t *testing.T, body []byte, key string) settingItemDTO {
	t.Helper()
	var out settingsListDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode settings list: %v", err)
	}
	for _, it := range out.Settings {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("key %q not in settings response", key)
	return settingItemDTO{}
}

func TestHandleListSettingsRedactsSecrets(t *testing.T) {
	s := &Server{settings: &fakeSettingsStore{rows: []sqlc.AuraSettings{
		{Key: "OPENROUTER_API_KEY", Value: "sk-super-secret", IsSecret: true},
		{Key: "TELEGRAM_BOT_TOKEN", Value: "123456:telegram-secret", IsSecret: true},
		{Key: "AURA_TTS_MODEL", Value: "openai/tts-1"},
	}}}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	secret := settingItemByKey(t, rr.Body.Bytes(), "OPENROUTER_API_KEY")
	if secret.Value != "" {
		t.Errorf("secret value leaked on read: %q", secret.Value)
	}
	if !secret.Secret || !secret.HasValue || !secret.Overridden {
		t.Errorf("secret item = %+v, want secret+has_value+overridden", secret)
	}
	telegramSecret := settingItemByKey(t, rr.Body.Bytes(), "TELEGRAM_BOT_TOKEN")
	if telegramSecret.Value != "" {
		t.Errorf("telegram token leaked on read: %q", telegramSecret.Value)
	}
	if !telegramSecret.Secret || !telegramSecret.HasValue || !telegramSecret.Overridden {
		t.Errorf("telegram token item = %+v, want secret+has_value+overridden", telegramSecret)
	}
	plain := settingItemByKey(t, rr.Body.Bytes(), "AURA_TTS_MODEL")
	if plain.Value != "openai/tts-1" {
		t.Errorf("non-secret value = %q, want the stored value", plain.Value)
	}
}

func TestHandleListSettingsNilStore(t *testing.T) {
	rr := httptest.NewRecorder()
	(&Server{}).handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when store unwired", rr.Code)
	}
}

func TestHandleListSettingsDoesNotRequireRestartForHotLLMRoute(t *testing.T) {
	t.Setenv("AURA_LLM_PROVIDER", "openrouter")
	t.Setenv("AURA_LLM_BASE_URL", "https://openrouter.ai/api/v1")
	t.Setenv("AURA_LLM_MODEL", "cloud-model")
	s := &Server{
		settings: &fakeSettingsStore{rows: []sqlc.AuraSettings{
			{Key: "AURA_LLM_PROVIDER", Value: "llamacpp"},
			{Key: "AURA_LLM_BASE_URL", Value: "http://aura-llm:8084/v1"},
			{Key: "AURA_LLM_MODEL", Value: "gemma-4-12b"},
		}},
		llmRouteReloader: &fakeLLMRouteReloader{},
	}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))

	var got settingsListDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.RestartRequired {
		t.Fatal("primary LLM route is hot but restart_required is true")
	}
}

func TestHandleListSettingsUsesActiveProfileAfterDeleteInsteadOfBootOverlayEnv(t *testing.T) {
	t.Setenv("AURA_LLM_MODEL", "stale-db-model-copied-at-boot")
	s := &Server{
		settings: &fakeSettingsStore{},
		llmRouteReloader: &fakeLLMRouteReloader{effective: map[string]string{
			"AURA_LLM_MODEL": "boot-fallback-model",
		}},
	}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	item := settingItemByKey(t, rr.Body.Bytes(), "AURA_LLM_MODEL")
	if item.Value != "boot-fallback-model" || item.Overridden {
		t.Fatalf("model item = %+v, want active non-overridden fallback", item)
	}
}

func TestHandlePutLLMProfilePreparesThenPersistsAndPublishesOnce(t *testing.T) {
	store := &fakeSettingsStore{rows: []sqlc.AuraSettings{
		{Key: "AURA_LLM_PROVIDER", Value: "openrouter"},
		{Key: "AURA_LLM_BASE_URL", Value: "https://openrouter.ai/api/v1"},
		{Key: "AURA_LLM_MODEL", Value: "cloud-model"},
	}}
	reloader := &fakeLLMRouteReloader{}
	s := &Server{settings: store, llmRouteReloader: reloader}
	body := strings.NewReader(`{"settings":{` +
		`"AURA_LLM_PROVIDER":"llamacpp",` +
		`"AURA_LLM_BASE_URL":"http://aura-llm:8084/v1",` +
		`"AURA_LLM_MODEL":"gemma-4-12b"}}`)
	r := withPrincipal(httptest.NewRequest(http.MethodPut, "/api/settings/llm-profile", body), "op-1")
	rr := httptest.NewRecorder()

	s.handlePutLLMProfile(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	if len(reloader.validated) != 1 || len(reloader.applied) != 1 {
		t.Fatalf("prepare/apply calls = %d/%d, want 1/1", len(reloader.validated), len(reloader.applied))
	}
	if len(store.upserted) != 3 || store.upserted["AURA_LLM_MODEL"] != "gemma-4-12b" {
		t.Fatalf("atomic profile rows = %v", store.upserted)
	}
}

func TestHandlePutLLMProfileDropsPreviousModelLimitsOnRouteChange(t *testing.T) {
	store := &fakeSettingsStore{rows: []sqlc.AuraSettings{
		{Key: "AURA_LLM_PROVIDER", Value: "openrouter"},
		{Key: "AURA_LLM_BASE_URL", Value: "https://openrouter.ai/api/v1"},
		{Key: "AURA_LLM_MODEL", Value: "cloud-model"},
		{Key: "AURA_MODEL_CONTEXT_WINDOW", Value: "1000000"},
		{Key: "AURA_MODEL_MAX_OUTPUT_TOKENS", Value: "32768"},
	}}
	reloader := &fakeLLMRouteReloader{}
	s := &Server{settings: store, llmRouteReloader: reloader}
	body := strings.NewReader(`{"settings":{` +
		`"AURA_LLM_PROVIDER":"ollama",` +
		`"AURA_LLM_BASE_URL":"http://host.docker.internal:11434/v1",` +
		`"AURA_LLM_MODEL":"gemma4:31b-cloud"}}`)
	r := withPrincipal(httptest.NewRequest(http.MethodPut, "/api/settings/llm-profile", body), "op-1")
	rr := httptest.NewRecorder()

	s.handlePutLLMProfile(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	if len(reloader.validated) != 1 {
		t.Fatalf("prepare calls = %d, want 1", len(reloader.validated))
	}
	for _, key := range []string{"AURA_MODEL_CONTEXT_WINDOW", "AURA_MODEL_MAX_OUTPUT_TOKENS"} {
		if _, present := reloader.validated[0][key]; present {
			t.Fatalf("new model inherited %s: %v", key, reloader.validated[0])
		}
		if !slices.Contains(store.deleted, key) {
			t.Fatalf("stale model limit %s was not deleted: %v", key, store.deleted)
		}
		if !slices.Contains(reloader.resets[0], key) {
			t.Fatalf("stale model limit %s was not reset at runtime: %v", key, reloader.resets[0])
		}
	}
}

func TestModelLimitResetKeysIncludesStartupOnlyLimits(t *testing.T) {
	rows := []sqlc.AuraSettings{
		{Key: "AURA_LLM_PROVIDER", Value: "llamacpp"},
		{Key: "AURA_LLM_MODEL", Value: "local-model"},
	}
	got := modelLimitResetKeys(rows, map[string]string{
		"AURA_LLM_PROVIDER": "ollama",
		"AURA_LLM_MODEL":    "gemma4:31b-cloud",
	})
	if !slices.Equal(got, modelLimitKeys) {
		t.Fatalf("route switch resets = %v, want startup model limits %v", got, modelLimitKeys)
	}
}

func TestModelLimitResetKeysPreservesExplicitLimitsAndSameRoute(t *testing.T) {
	rows := []sqlc.AuraSettings{
		{Key: "AURA_LLM_PROVIDER", Value: "openrouter"},
		{Key: "AURA_LLM_MODEL", Value: "cloud-model"},
		{Key: "AURA_MODEL_CONTEXT_WINDOW", Value: "1000000"},
		{Key: "AURA_MODEL_MAX_OUTPUT_TOKENS", Value: "32768"},
	}
	withExplicitContext := modelLimitResetKeys(rows, map[string]string{
		"AURA_LLM_MODEL":            "local-model",
		"AURA_MODEL_CONTEXT_WINDOW": "81920",
	})
	if !slices.Equal(withExplicitContext, []string{"AURA_MODEL_MAX_OUTPUT_TOKENS"}) {
		t.Fatalf("route switch resets = %v, want only inherited output limit", withExplicitContext)
	}
	if got := modelLimitResetKeys(rows, map[string]string{"AURA_LLM_MODEL": "cloud-model"}); len(got) != 0 {
		t.Fatalf("unchanged route reset model limits: %v", got)
	}
}

func putReq(t *testing.T, key, value, principal string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	body, _ := json.Marshal(putSettingBody{Value: value})
	r := httptest.NewRequest(http.MethodPut, "/api/settings/"+key, bytes.NewReader(body))
	r.SetPathValue("key", key)
	if principal != "" {
		r = withPrincipal(r, principal)
	}
	return httptest.NewRecorder(), r
}

func TestHandlePutSetting(t *testing.T) {
	t.Run("unknown key 400", func(t *testing.T) {
		store := &fakeSettingsStore{}
		s := &Server{settings: store}
		rr, r := putReq(t, "POSTGRES_PASSWORD", "evil", "op-1")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for non-allowlisted key", rr.Code)
		}
		if len(store.upserted) != 0 {
			t.Errorf("a non-allowlisted key was written: %v", store.upserted)
		}
	})

	t.Run("no principal 401", func(t *testing.T) {
		s := &Server{settings: &fakeSettingsStore{}}
		rr, r := putReq(t, "AURA_TTS_MODEL", "openai/tts-1", "")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 without a principal", rr.Code)
		}
	})

	t.Run("invalid int 400", func(t *testing.T) {
		store := &fakeSettingsStore{}
		s := &Server{settings: store}
		rr, r := putReq(t, "AURA_EMBED_DIMENSIONS", "not-a-number", "op-1")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for a non-int dimension", rr.Code)
		}
		if len(store.upserted) != 0 {
			t.Errorf("an invalid value was persisted: %v", store.upserted)
		}
	})

	t.Run("valid upsert 200", func(t *testing.T) {
		store := &fakeSettingsStore{}
		s := &Server{settings: store}
		rr, r := putReq(t, "AURA_EMBED_MODEL", "qwen/qwen3-embedding-8b", "op-1")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		if store.upserted["AURA_EMBED_MODEL"] != "qwen/qwen3-embedding-8b" {
			t.Errorf("upserted = %v, want the new value", store.upserted)
		}
	})

	t.Run("primary route validates and publishes complete overrides", func(t *testing.T) {
		store := &fakeSettingsStore{rows: []sqlc.AuraSettings{
			{Key: "AURA_LLM_PROVIDER", Value: "llamacpp"},
			{Key: "AURA_LLM_BASE_URL", Value: "http://aura-llm:8084/v1"},
			{Key: "AURA_LLM_MODEL", Value: "old-model"},
		}}
		reloader := &fakeLLMRouteReloader{}
		s := &Server{settings: store, llmRouteReloader: reloader}
		rr, r := putReq(t, "AURA_LLM_MODEL", "gemma-4-12b", "op-1")

		s.handlePutSetting(rr, r)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
		}
		if len(reloader.validated) != 1 || len(reloader.applied) != 1 {
			t.Fatalf("reload calls = validate %d apply %d, want 1/1", len(reloader.validated), len(reloader.applied))
		}
		if got := reloader.applied[0]["AURA_LLM_MODEL"]; got != "gemma-4-12b" {
			t.Fatalf("applied model = %q, want gemma-4-12b", got)
		}
		if got := reloader.applied[0]["AURA_LLM_BASE_URL"]; got != "http://aura-llm:8084/v1" {
			t.Fatalf("applied base URL = %q, want existing local override", got)
		}
	})

	t.Run("invalid primary route is rejected before persistence", func(t *testing.T) {
		store := &fakeSettingsStore{}
		reloader := &fakeLLMRouteReloader{err: errors.New("invalid route")}
		s := &Server{settings: store, llmRouteReloader: reloader}
		rr, r := putReq(t, "AURA_LLM_MODEL", "bad-model", "op-1")

		s.handlePutSetting(rr, r)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rr.Code)
		}
		if len(store.upserted) != 0 || len(reloader.applied) != 0 {
			t.Fatalf("invalid route persisted/applied: store=%v apply=%v", store.upserted, reloader.applied)
		}
	})
}

func TestHandleDeleteSettingHotLLMRoutePublishesFallbackOverrides(t *testing.T) {
	store := &fakeSettingsStore{rows: []sqlc.AuraSettings{
		{Key: "AURA_LLM_PROVIDER", Value: "llamacpp"},
		{Key: "AURA_LLM_BASE_URL", Value: "http://aura-llm:8084/v1"},
		{Key: "AURA_LLM_MODEL", Value: "gemma-4-12b"},
	}}
	reloader := &fakeLLMRouteReloader{}
	s := &Server{settings: store, llmRouteReloader: reloader}
	r := httptest.NewRequest(http.MethodDelete, "/api/settings/AURA_LLM_MODEL", nil)
	r.SetPathValue("key", "AURA_LLM_MODEL")
	r = withPrincipal(r, "op-1")
	rr := httptest.NewRecorder()

	s.handleDeleteSetting(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if len(reloader.validated) != 1 || len(reloader.applied) != 1 {
		t.Fatalf("reload calls = validate %d apply %d, want 1/1", len(reloader.validated), len(reloader.applied))
	}
	if _, present := reloader.applied[0]["AURA_LLM_MODEL"]; present {
		t.Fatal("deleted model remained in the persisted override set")
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if restart, _ := body["restart_required"].(bool); restart {
		t.Fatal("hot route delete requested a restart")
	}
}

func TestHandlePutSettingRejectsInvalidLLMTokenBudget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("AURA_LLM_MAX_TOKENS", "")
	t.Setenv("AURA_MODEL_CONTEXT_WINDOW", "")
	t.Setenv("AURA_MODEL_MAX_OUTPUT_TOKENS", "")
	t.Chdir(t.TempDir())

	cases := []struct {
		name  string
		rows  []sqlc.AuraSettings
		key   string
		value string
	}{
		{"zero max tokens", nil, "AURA_LLM_MAX_TOKENS", "0"},
		{"negative context", nil, "AURA_MODEL_CONTEXT_WINDOW", "-1"},
		{"under reserved output", []sqlc.AuraSettings{
			{Key: "AURA_LLM_MAX_TOKENS", Value: "5000"},
		}, "AURA_MODEL_MAX_OUTPUT_TOKENS", "4096"},
		// The window that leaves nothing for the prompt is now one swallowed by the
		// CONFIGURED output cap, not by the reserves: since 4a679e394 both reserves are the
		// smaller of their constant and a share of the window, so 33,000 with a 1-token
		// output cap keeps half the window as prompt budget instead of going negative. A
		// 30,000-token answer inside a 33,000-token window still leaves none, which is the
		// rejection this case has always been about.
		{"no prompt budget", []sqlc.AuraSettings{
			{Key: "AURA_LLM_MAX_TOKENS", Value: "30000"},
			{Key: "AURA_MODEL_MAX_OUTPUT_TOKENS", Value: "30000"},
		}, "AURA_MODEL_CONTEXT_WINDOW", "33000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeSettingsStore{rows: tc.rows}
			s := &Server{settings: store}
			rr, r := putReq(t, tc.key, tc.value, "op-1")

			s.handlePutSetting(rr, r)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rr.Code, rr.Body.String())
			}
			if len(store.upserted) != 0 {
				t.Fatalf("invalid token budget persisted: %v", store.upserted)
			}
		})
	}
}

func TestHandleCheckTelegramAvailability(t *testing.T) {
	const secret = "123456:telegram-secret"
	var probedToken string
	store := &fakeSettingsStore{rows: []sqlc.AuraSettings{
		{Key: "TELEGRAM_BOT_TOKEN", Value: secret, IsSecret: true},
	}}
	s := &Server{
		settings: store,
		telegramProbe: func(_ context.Context, token string) (string, error) {
			probedToken = token
			return "AuraBot", nil
		},
	}

	rr := httptest.NewRecorder()
	s.handleCheckTelegramAvailability(rr, httptest.NewRequest(http.MethodPost, "/api/settings/telegram/check", strings.NewReader(`{}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if probedToken != secret {
		t.Fatalf("probe token = %q, want stored secret", probedToken)
	}
	if strings.Contains(rr.Body.String(), secret) {
		t.Fatalf("availability response leaked the token: %s", rr.Body.String())
	}
	var got telegramAvailabilityDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Configured || !got.Available || got.BotUsername != "AuraBot" {
		t.Fatalf("availability = %+v, want configured+available AuraBot", got)
	}
}

func TestHandleCheckTelegramAvailabilityDoesNotLeakProbeError(t *testing.T) {
	const secret = "987:secret-token"
	s := &Server{
		telegramProbe: func(_ context.Context, token string) (string, error) {
			return "", errors.New("telegram rejected token " + token)
		},
	}

	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"token":"` + secret + `"}`)
	s.handleCheckTelegramAvailability(rr, httptest.NewRequest(http.MethodPost, "/api/settings/telegram/check", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if strings.Contains(rr.Body.String(), secret) {
		t.Fatalf("availability error leaked the token: %s", rr.Body.String())
	}
	var got telegramAvailabilityDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Available || got.Error != "bot token validation failed" {
		t.Fatalf("availability = %+v, want fixed validation failure", got)
	}
}

func TestHandleCreateSettingsTelegramLink(t *testing.T) {
	fake := &fakeOnboarding{linkResp: OnboardingTelegramLink{
		SessionToken: "sess-1",
		DeepLink:     "https://t.me/AuraBot?start=onb-1",
		QRSVG:        "<svg/>",
	}}
	s := &Server{onboarding: fake}
	r := httptest.NewRequest(http.MethodPost, "/api/settings/telegram/link", nil)
	r = withPrincipal(r, "operator-1")
	rr := httptest.NewRecorder()

	s.handleCreateSettingsTelegramLink(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if fake.gotRequester != "operator-1" {
		t.Fatalf("CreateTelegramLink requester = %q, want operator-1", fake.gotRequester)
	}
	var got OnboardingTelegramLink
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionToken != "sess-1" || got.DeepLink == "" || got.QRSVG == "" {
		t.Fatalf("telegram link response = %+v", got)
	}
}

func TestHandleSettingsTelegramStatus(t *testing.T) {
	fake := &fakeOnboarding{statusResp: OnboardingTelegramStatus{Linked: true}}
	s := &Server{onboarding: fake}
	r := httptest.NewRequest(http.MethodGet, "/api/settings/telegram/sess-1/status", nil)
	r.SetPathValue("sessionToken", "sess-1")
	r = withPrincipal(r, "operator-1")
	rr := httptest.NewRecorder()

	s.handleSettingsTelegramStatus(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if fake.gotRequester != "operator-1" || fake.gotToken != "sess-1" {
		t.Fatalf("TelegramStatus requester=%q token=%q, want operator-1/sess-1", fake.gotRequester, fake.gotToken)
	}
	var got OnboardingTelegramStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Linked {
		t.Fatal("linked = false, want true")
	}
}

func TestHandleDeleteSetting(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store}
	r := httptest.NewRequest(http.MethodDelete, "/api/settings/AURA_TTS_MODEL", nil)
	r.SetPathValue("key", "AURA_TTS_MODEL")
	r = withPrincipal(r, "op-1")
	rr := httptest.NewRecorder()
	s.handleDeleteSetting(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "AURA_TTS_MODEL" {
		t.Errorf("deleted = %v, want [AURA_TTS_MODEL]", store.deleted)
	}
}
