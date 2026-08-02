package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

type fakeSettingsStore struct {
	rows     []sqlc.AuraSettings
	upserted map[string]string
	deleted  []string
}

func (f *fakeSettingsStore) List(context.Context) ([]sqlc.AuraSettings, error) { return f.rows, nil }

func (f *fakeSettingsStore) Upsert(_ context.Context, key, value, _ string) (sqlc.AuraSettings, error) {
	if f.upserted == nil {
		f.upserted = map[string]string{}
	}
	f.upserted[key] = value
	return sqlc.AuraSettings{Key: key, Value: value}, nil
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
		{"no prompt budget", []sqlc.AuraSettings{
			{Key: "AURA_LLM_MAX_TOKENS", Value: "1"},
			{Key: "AURA_MODEL_MAX_OUTPUT_TOKENS", Value: "1"},
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
