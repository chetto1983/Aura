package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// fakeTelegramChannel stands in for the daemon's hot-swappable Telegram channel: it
// records every activation and reports as running the last token it accepted.
type fakeTelegramChannel struct {
	err     error
	calls   []string
	running string
}

func (f *fakeTelegramChannel) activate(_ context.Context, token string) error {
	f.calls = append(f.calls, token)
	if f.err != nil {
		return f.err
	}
	f.running = token
	return nil
}

func (f *fakeTelegramChannel) runs(token string) bool { return token != "" && token == f.running }

func telegramServer(store settingsStore, tg *fakeTelegramChannel) *Server {
	s := &Server{
		settings:      store,
		telegramProbe: func(context.Context, string) (string, error) { return "AuraBot", nil },
	}
	if tg != nil {
		s.SetTelegramActivator(tg.activate, tg.runs)
	}
	return s
}

func decodeBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %s: %v", rr.Body.String(), err)
	}
	return got
}

func checkTelegram(t *testing.T, s *Server, token string) telegramAvailabilityDTO {
	t.Helper()
	body, _ := json.Marshal(telegramAvailabilityRequest{Token: token})
	rr := httptest.NewRecorder()
	s.handleCheckTelegramAvailability(rr, httptest.NewRequest(http.MethodPost, "/api/settings/telegram/check", strings.NewReader(string(body))))
	if rr.Code != http.StatusOK {
		t.Fatalf("check status = %d: %s", rr.Code, rr.Body.String())
	}
	var got telegramAvailabilityDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode check: %v", err)
	}
	return got
}

func TestHandlePutSettingTelegramTokenActivatesChannel(t *testing.T) {
	const secret = "123456:hot-swap-secret"
	store := &fakeSettingsStore{}
	tg := &fakeTelegramChannel{}
	s := telegramServer(store, tg)
	rr, r := putReq(t, "TELEGRAM_BOT_TOKEN", secret, "op-1")

	s.handlePutSetting(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	if !slices.Equal(tg.calls, []string{secret}) {
		t.Fatalf("activations = %q, want exactly the saved token", tg.calls)
	}
	if store.upserted["TELEGRAM_BOT_TOKEN"] != secret {
		t.Fatalf("upserted = %v, want the token saved", store.upserted)
	}
	if strings.Contains(rr.Body.String(), secret) {
		t.Fatalf("PUT response echoed the token: %s", rr.Body.String())
	}
	got := decodeBody(t, rr)
	if got["key"] != "TELEGRAM_BOT_TOKEN" || got["has_value"] != true {
		t.Fatalf("existing row fields lost: %v", got)
	}
	if got["channel_active"] != true || got["restart_required"] != false {
		t.Fatalf("channel_active=%v restart_required=%v, want true/false", got["channel_active"], got["restart_required"])
	}
	if _, present := got["channel_error"]; present {
		t.Fatalf("channel_error present on success: %v", got)
	}
}

func TestHandlePutSettingTelegramTokenReportsStartFailure(t *testing.T) {
	const secret = "123456:unstartable-secret"
	store := &fakeSettingsStore{}
	tg := &fakeTelegramChannel{err: errors.New("telegram channel failed to start")}
	s := telegramServer(store, tg)
	rr, r := putReq(t, "TELEGRAM_BOT_TOKEN", secret, "op-1")

	s.handlePutSetting(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	if store.upserted["TELEGRAM_BOT_TOKEN"] != secret {
		t.Fatalf("a failed start must keep the token saved: %v", store.upserted)
	}
	got := decodeBody(t, rr)
	if got["channel_active"] != false || got["channel_error"] != "telegram channel failed to start" {
		t.Fatalf("channel_active=%v channel_error=%v, want false + the activator's reason", got["channel_active"], got["channel_error"])
	}
	if got["restart_required"] != true {
		t.Fatalf("restart_required = %v, want true while the saved token is not running", got["restart_required"])
	}
}

func TestHandlePutSettingActivatesTelegramOnlyForItsTokenAfterSave(t *testing.T) {
	t.Run("other keys never activate", func(t *testing.T) {
		tg := &fakeTelegramChannel{}
		s := telegramServer(&fakeSettingsStore{}, tg)
		rr, r := putReq(t, "AURA_TTS_MODEL", "openai/tts-1", "op-1")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusOK || len(tg.calls) != 0 {
			t.Fatalf("status=%d activations=%q, want 200 and none", rr.Code, tg.calls)
		}
		if _, present := decodeBody(t, rr)["channel_active"]; present {
			t.Fatal("a non-Telegram key reported channel_active")
		}
	})

	t.Run("failed save never activates", func(t *testing.T) {
		tg := &fakeTelegramChannel{}
		s := telegramServer(errSettingsStore{err: errors.New("db down")}, tg)
		rr, r := putReq(t, "TELEGRAM_BOT_TOKEN", "123:unsaved", "op-1")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusBadGateway || len(tg.calls) != 0 {
			t.Fatalf("status=%d activations=%q, want 502 and none", rr.Code, tg.calls)
		}
	})

	t.Run("unwired activator keeps the plain row response", func(t *testing.T) {
		s := telegramServer(&fakeSettingsStore{}, nil)
		rr, r := putReq(t, "TELEGRAM_BOT_TOKEN", "123:no-daemon", "op-1")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d", rr.Code)
		}
		if _, present := decodeBody(t, rr)["channel_active"]; present {
			t.Fatal("channel_active reported with no channel to activate")
		}
	})
}

func TestHandleDeleteSettingTelegramTokenStopsChannel(t *testing.T) {
	tg := &fakeTelegramChannel{running: "123:old"}
	s := telegramServer(&fakeSettingsStore{}, tg)
	del := func(key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodDelete, "/api/settings/"+key, nil)
		r.SetPathValue("key", key)
		rr := httptest.NewRecorder()
		s.handleDeleteSetting(rr, withPrincipal(r, "op-1"))
		if rr.Code != http.StatusOK {
			t.Fatalf("DELETE %s status = %d: %s", key, rr.Code, rr.Body.String())
		}
		return rr
	}

	got := decodeBody(t, del("TELEGRAM_BOT_TOKEN"))

	if !slices.Equal(tg.calls, []string{""}) {
		t.Fatalf("activations = %q, want one stop (empty token)", tg.calls)
	}
	if got["deleted"] != true || got["channel_active"] != false || got["restart_required"] != false {
		t.Fatalf("delete response = %v, want deleted, channel inactive, no restart", got)
	}
	del("AURA_TTS_MODEL")
	if len(tg.calls) != 1 {
		t.Fatalf("deleting another key activated Telegram: %q", tg.calls)
	}
}

func TestHandleCheckTelegramNoRestartOnceChannelRunsSavedToken(t *testing.T) {
	const secret = "123456:appliance-secret"
	t.Setenv("TELEGRAM_BOT_TOKEN", "") // a fresh appliance boots with no token
	tg := &fakeTelegramChannel{}
	s := telegramServer(&fakeSettingsStore{}, tg)
	if !checkTelegram(t, s, secret).RequiresRestart {
		t.Fatal("before activation: requiresRestart = false, want true")
	}

	rr, r := putReq(t, "TELEGRAM_BOT_TOKEN", secret, "op-1")
	s.handlePutSetting(rr, r)

	if got := checkTelegram(t, s, secret); got.RequiresRestart || !got.Available {
		t.Fatalf("after activation: %+v, want available and no restart", got)
	}
}

// The boot env is the pre-hot-swap oracle this check used to trust. A channel that is
// not running the token needs a restart even when the env happens to hold it.
func TestHandleCheckTelegramIgnoresBootEnvToken(t *testing.T) {
	const secret = "123456:env-secret"
	t.Setenv("TELEGRAM_BOT_TOKEN", secret)
	s := telegramServer(&fakeSettingsStore{}, &fakeTelegramChannel{})
	if !checkTelegram(t, s, secret).RequiresRestart {
		t.Fatal("requiresRestart = false for a token no channel runs")
	}
}

func TestHandleListSettingsTelegramTokenLiveWhileChannelRunsIt(t *testing.T) {
	const secret = "123456:list-secret"
	t.Setenv("TELEGRAM_BOT_TOKEN", "") // booted without it: without the channel this row needs a restart
	rows := []sqlc.AuraSettings{{Key: "TELEGRAM_BOT_TOKEN", Value: secret, IsSecret: true}}
	list := func(tg *fakeTelegramChannel) settingsListDTO {
		rr := httptest.NewRecorder()
		telegramServer(&fakeSettingsStore{rows: rows}, tg).handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
		var out settingsListDTO
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}
	applied := func(out settingsListDTO) string {
		for _, it := range out.Settings {
			if it.Key == "TELEGRAM_BOT_TOKEN" {
				return it.Applied
			}
		}
		t.Fatal("TELEGRAM_BOT_TOKEN missing from the list")
		return ""
	}

	live := list(&fakeTelegramChannel{running: secret})
	if applied(live) != appliedLive || live.RestartRequired || slices.Contains(live.RestartKeys, "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("running token: applied=%q restart=%v keys=%v, want live and no restart", applied(live), live.RestartRequired, live.RestartKeys)
	}
	stale := list(&fakeTelegramChannel{running: "999:other"})
	if applied(stale) != appliedRestart || !slices.Contains(stale.RestartKeys, "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("other token running: applied=%q keys=%v, want restart", applied(stale), stale.RestartKeys)
	}
}
