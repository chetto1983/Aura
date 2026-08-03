package agui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/settings"
)

// settings_api_branches_test.go covers the settings-page handler branches the happy-path
// tests in settings_api_test.go leave uncovered: the DI setters, the 503/401/400/502 error
// legs of put/delete/create-link, the bool-kind validation, and the unwired/empty-token
// legs of the Telegram availability probe. Every case drives the real handler with the
// existing fakeSettingsStore / fakeOnboarding, no live pool.

// errSettingsStore is a settingsStore whose reads/writes fail, exercising the 502 store-
// error legs the always-succeeding fakeSettingsStore cannot reach.
type errSettingsStore struct{ err error }

func (e errSettingsStore) List(context.Context) ([]sqlc.AuraSettings, error) { return nil, e.err }
func (e errSettingsStore) Upsert(context.Context, string, string, string) (sqlc.AuraSettings, error) {
	return sqlc.AuraSettings{}, e.err
}
func (e errSettingsStore) Delete(context.Context, string) error { return e.err }

func TestSetSettingsStoreAndProbeOffConstructor(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if s.settings != nil || s.telegramProbe != nil {
		t.Fatalf("NewServer must leave settings store + telegram probe nil")
	}
	s.SetSettingsStore(&fakeSettingsStore{})
	s.SetTelegramBotProbe(func(context.Context, string) (string, error) { return "AuraBot", nil })
	if s.settings == nil || s.telegramProbe == nil {
		t.Fatal("setters did not wire the settings store + telegram probe")
	}
}

func TestValidateSettingValueBoolAndDefault(t *testing.T) {
	cases := []struct {
		name    string
		kind    settings.Kind
		value   string
		wantErr error
	}{
		{"bool ok", settings.KindBool, "true", nil},
		{"bool bad", settings.KindBool, "notbool", errInvalidBool},
		{"int ok", settings.KindInt, "768", nil},
		{"int bad", settings.KindInt, "x", errInvalidInt},
		{"string passthrough", settings.KindString, "anything", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSettingValue(tc.kind, tc.value); !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateSettingValue(%v,%q) = %v, want %v", tc.kind, tc.value, err, tc.wantErr)
			}
		})
	}
}

func TestHandlePutSettingBranches(t *testing.T) {
	t.Run("503 unwired", func(t *testing.T) {
		rec, r := putReq(t, "AURA_TTS_MODEL", "x", "op-1")
		(&Server{}).handlePutSetting(rec, r)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("invalid JSON body 400", func(t *testing.T) {
		s := &Server{settings: &fakeSettingsStore{}}
		r := httptest.NewRequest(http.MethodPut, "/api/settings/AURA_TTS_MODEL", strings.NewReader(`{bad`))
		r.SetPathValue("key", "AURA_TTS_MODEL")
		r = withPrincipal(r, "op-1")
		rec := httptest.NewRecorder()
		s.handlePutSetting(rec, r)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for malformed JSON", rec.Code)
		}
	})

	t.Run("502 on store error", func(t *testing.T) {
		s := &Server{settings: errSettingsStore{err: errors.New("db down")}}
		rec, r := putReq(t, "AURA_TTS_MODEL", "openai/tts-1", "op-1")
		s.handlePutSetting(rec, r)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502 on upsert failure", rec.Code)
		}
	})
}

func TestHandleDeleteSettingBranches(t *testing.T) {
	t.Run("503 unwired", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodDelete, "/api/settings/AURA_TTS_MODEL", nil)
		r.SetPathValue("key", "AURA_TTS_MODEL")
		rec := httptest.NewRecorder()
		(&Server{}).handleDeleteSetting(rec, r)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("unknown key 400", func(t *testing.T) {
		s := &Server{settings: &fakeSettingsStore{}}
		r := httptest.NewRequest(http.MethodDelete, "/api/settings/POSTGRES_PASSWORD", nil)
		r.SetPathValue("key", "POSTGRES_PASSWORD")
		r = withPrincipal(r, "op-1")
		rec := httptest.NewRecorder()
		s.handleDeleteSetting(rec, r)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for a non-allowlisted key", rec.Code)
		}
	})

	t.Run("401 without principal", func(t *testing.T) {
		s := &Server{settings: &fakeSettingsStore{}}
		r := httptest.NewRequest(http.MethodDelete, "/api/settings/AURA_TTS_MODEL", nil)
		r.SetPathValue("key", "AURA_TTS_MODEL")
		rec := httptest.NewRecorder()
		s.handleDeleteSetting(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("502 on store error", func(t *testing.T) {
		s := &Server{settings: errSettingsStore{err: errors.New("db down")}}
		r := httptest.NewRequest(http.MethodDelete, "/api/settings/AURA_TTS_MODEL", nil)
		r.SetPathValue("key", "AURA_TTS_MODEL")
		r = withPrincipal(r, "op-1")
		rec := httptest.NewRecorder()
		s.handleDeleteSetting(rec, r)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502 on delete failure", rec.Code)
		}
	})
}

func TestHandleCreateSettingsTelegramLinkBranches(t *testing.T) {
	t.Run("503 unwired", func(t *testing.T) {
		r := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/settings/telegram/link", nil), "op-1")
		rec := httptest.NewRecorder()
		(&Server{}).handleCreateSettingsTelegramLink(rec, r)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("401 without principal", func(t *testing.T) {
		s := &Server{onboarding: &fakeOnboarding{}}
		rec := httptest.NewRecorder()
		s.handleCreateSettingsTelegramLink(rec, httptest.NewRequest(http.MethodPost, "/api/settings/telegram/link", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("service error maps through writeOnboardingError", func(t *testing.T) {
		s := &Server{onboarding: &fakeOnboarding{linkErr: errOnboardingForbidden}}
		r := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/settings/telegram/link", nil), "op-1")
		rec := httptest.NewRecorder()
		s.handleCreateSettingsTelegramLink(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestHandleCheckTelegramAvailabilityBranches(t *testing.T) {
	t.Run("503 when probe unwired", func(t *testing.T) {
		rec := httptest.NewRecorder()
		(&Server{}).handleCheckTelegramAvailability(rec, httptest.NewRequest(http.MethodPost, "/api/settings/telegram/check", strings.NewReader(`{}`)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("invalid JSON body 400", func(t *testing.T) {
		s := &Server{telegramProbe: func(context.Context, string) (string, error) { return "AuraBot", nil }}
		rec := httptest.NewRecorder()
		s.handleCheckTelegramAvailability(rec, httptest.NewRequest(http.MethodPost, "/api/settings/telegram/check", strings.NewReader(`{bad`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for malformed JSON", rec.Code)
		}
	})

	t.Run("no token configured reports not-configured", func(t *testing.T) {
		// Empty body + no stored/env token → configured:false, probe never called.
		// The env token is pinned empty: effectiveSettingValue falls back to
		// os.Getenv, so on any host that actually has a bot configured this case
		// probed a real token and failed for a reason unrelated to the branch.
		t.Setenv("TELEGRAM_BOT_TOKEN", "")
		probed := false
		s := &Server{
			settings:      &fakeSettingsStore{},
			telegramProbe: func(context.Context, string) (string, error) { probed = true; return "", nil },
		}
		rec := httptest.NewRecorder()
		s.handleCheckTelegramAvailability(rec, httptest.NewRequest(http.MethodPost, "/api/settings/telegram/check", strings.NewReader(`{}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if probed {
			t.Fatal("probe was called with no token configured")
		}
		if !strings.Contains(rec.Body.String(), `"configured":false`) {
			t.Fatalf("body = %s, want configured:false", rec.Body.String())
		}
	})

	t.Run("502 when store read fails resolving token", func(t *testing.T) {
		s := &Server{
			settings:      errSettingsStore{err: errors.New("db down")},
			telegramProbe: func(context.Context, string) (string, error) { return "AuraBot", nil },
		}
		rec := httptest.NewRecorder()
		s.handleCheckTelegramAvailability(rec, httptest.NewRequest(http.MethodPost, "/api/settings/telegram/check", strings.NewReader(`{}`)))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502 when the settings read fails", rec.Code)
		}
	})
}
