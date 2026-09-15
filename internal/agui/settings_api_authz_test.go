package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

func TestMemberCannotChangeTheRoute(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, "AURA_LLM_MODEL", "other/model", "member-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusForbidden || len(store.upserted) != 0 {
		t.Fatalf("status = %d upserted = %v, want 403 and nothing written", rr.Code, store.upserted)
	}
}

func TestAdminSetsTheManagementKey(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, "AURA_OPENROUTER_MANAGEMENT_KEY", "sk-or-v1-mgmt", "admin-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusOK || store.upserted["AURA_OPENROUTER_MANAGEMENT_KEY"] != "sk-or-v1-mgmt" {
		t.Fatalf("status = %d upserted = %v, want 200 and the key stored", rr.Code, store.upserted)
	}
	if strings.Contains(rr.Body.String(), "sk-or-v1-mgmt") {
		t.Fatal("the response echoes the management key")
	}
}

func TestNobodyWritesTheServicesKey(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, "OPENROUTER_API_KEY", "sk-or-v1-pasted", "admin-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusForbidden || len(store.upserted) != 0 {
		t.Fatalf("status = %d upserted = %v, want 403 and nothing written", rr.Code, store.upserted)
	}
}

func TestAdminCannotStoreAServicesCapTheProviderRefuses(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, "AURA_OPENROUTER_SERVICES_CAP_USD", "ten", "admin-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusBadRequest || len(store.upserted) != 0 {
		t.Fatalf("status = %d upserted = %v, want 400 and nothing written", rr.Code, store.upserted)
	}
}

func TestMemberCannotDeleteTheManagementKey(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	r := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/settings/AURA_OPENROUTER_MANAGEMENT_KEY", nil), "member-1")
	r.SetPathValue("key", "AURA_OPENROUTER_MANAGEMENT_KEY")
	rr := httptest.NewRecorder()
	s.handleDeleteSetting(rr, r)
	if rr.Code != http.StatusForbidden || len(store.deleted) != 0 {
		t.Fatalf("status = %d deleted = %v, want 403 and nothing deleted", rr.Code, store.deleted)
	}
}

func TestMemberCannotPutAnLLMProfile(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, llmRouteReloader: &fakeLLMRouteReloader{}, idAdmin: adminCaps("admin-1")}
	body := `{"settings":{"AURA_LOOP_MAX_STEPS":"40"}}`
	r := withPrincipal(httptest.NewRequest(http.MethodPut, "/api/settings/llm-profile", strings.NewReader(body)), "member-1")
	rr := httptest.NewRecorder()
	s.handlePutLLMProfile(rr, r)
	if rr.Code != http.StatusForbidden || len(store.upserted) != 0 {
		t.Fatalf("status = %d upserted = %v, want 403 and nothing written", rr.Code, store.upserted)
	}
}

func TestMemberStillTunesAnOrdinarySetting(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, "AURA_TTS_MODEL", "tts-x", "member-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: members keep governance.write for the rest", rr.Code)
	}
}

// TestMemberCannotChangeMediaSettings and TestAdminSetsMediaSettings pin the
// image/video plan's ruling R3: all four media keys are admin-only, exactly
// like the primary LLM route, because they pick the model (and spend) every
// identity's generation calls use.
func TestMemberCannotChangeMediaSettings(t *testing.T) {
	for _, key := range []string{
		"AURA_IMAGE_MODEL", "AURA_VIDEO_MODEL", "AURA_VIDEO_INLINE_WAIT_SEC", "AURA_ASSET_MAX_VIDEO_BYTES",
	} {
		store := &fakeSettingsStore{}
		s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
		rr, r := putReq(t, key, "45", "member-1")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusForbidden || len(store.upserted) != 0 {
			t.Fatalf("%s: status = %d upserted = %v, want 403 and nothing written", key, rr.Code, store.upserted)
		}
	}
}

func TestAdminSetsMediaSettings(t *testing.T) {
	cases := []struct{ key, value string }{
		{"AURA_IMAGE_MODEL", "vendor/other-image-model"},
		{"AURA_VIDEO_MODEL", "vendor/other-video-model"},
		{"AURA_VIDEO_INLINE_WAIT_SEC", "60"},
		{"AURA_ASSET_MAX_VIDEO_BYTES", "104857600"},
	}
	for _, tc := range cases {
		store := &fakeSettingsStore{}
		s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
		rr, r := putReq(t, tc.key, tc.value, "admin-1")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusOK || store.upserted[tc.key] != tc.value {
			t.Fatalf("%s: status = %d upserted = %v, want 200 and the value stored", tc.key, rr.Code, store.upserted)
		}
	}
}

func TestMemberCannotDeleteMediaSettings(t *testing.T) {
	for _, key := range []string{
		"AURA_IMAGE_MODEL", "AURA_VIDEO_MODEL", "AURA_VIDEO_INLINE_WAIT_SEC", "AURA_ASSET_MAX_VIDEO_BYTES",
	} {
		store := &fakeSettingsStore{}
		s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
		r := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/settings/"+key, nil), "member-1")
		r.SetPathValue("key", key)
		rr := httptest.NewRecorder()
		s.handleDeleteSetting(rr, r)
		if rr.Code != http.StatusForbidden || len(store.deleted) != 0 {
			t.Fatalf("%s: status = %d deleted = %v, want 403 and nothing deleted", key, rr.Code, store.deleted)
		}
	}
}

// TestMediaModelsAndWaitReadAsLive pins the three call-time media keys
// (the two models plus the inline wait) as "live", matching ruling R3's
// per-call read; the byte ceiling stays deliberately absent, per
// TestAssetMaxVideoBytesIsBootBound below.
func TestMediaModelsAndWaitReadAsLive(t *testing.T) {
	store := &fakeSettingsStore{rows: []sqlc.AuraSettings{
		{Key: "AURA_IMAGE_MODEL", Value: "vendor/other-image-model"},
		{Key: "AURA_VIDEO_MODEL", Value: "vendor/other-video-model"},
		{Key: "AURA_VIDEO_INLINE_WAIT_SEC", Value: "60"},
	}}
	s := &Server{settings: store}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	for _, key := range []string{"AURA_IMAGE_MODEL", "AURA_VIDEO_MODEL", "AURA_VIDEO_INLINE_WAIT_SEC"} {
		if item := settingItemByKey(t, rr.Body.Bytes(), key); item.Applied != appliedLive {
			t.Fatalf("%s: applied = %q, want live: it is read on every call", key, item.Applied)
		}
	}
}

// A call-time key is read on every use whether or not a row exists, so an unsaved one is
// "live" too; the boot-bound rows around it keep their labels.
func TestCallTimeSettingsReadAsLiveWithNoSavedValue(t *testing.T) {
	s := &Server{settings: &fakeSettingsStore{}}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	for key, want := range map[string]string{
		"AURA_IMAGE_MODEL":                 appliedLive,
		"AURA_VIDEO_MODEL":                 appliedLive,
		"AURA_VIDEO_INLINE_WAIT_SEC":       appliedLive,
		"AURA_OPENROUTER_MANAGEMENT_KEY":   appliedLive,
		"AURA_OPENROUTER_SERVICES_CAP_USD": appliedLive,
		"AURA_ASSET_MAX_VIDEO_BYTES":       appliedBoot,
		"AURA_TTS_MODEL":                   appliedBoot,
		// A hot profile key, not a call-time one: live only through the wired reloader.
		"AURA_LLM_MODEL": appliedBoot,
	} {
		if item := settingItemByKey(t, rr.Body.Bytes(), key); item.Applied != want || item.Overridden {
			t.Errorf("%s: applied = %q overridden = %v, want %q with no saved row", key, item.Applied, item.Overridden, want)
		}
	}
}

// TestAssetMaxVideoBytesIsBootBound pins the byte ceiling's deliberate absence
// from callTimeSettingKeys: it is read once at boot, so a persisted change
// reports "restart", not "live", unlike its three siblings above.
func TestAssetMaxVideoBytesIsBootBound(t *testing.T) {
	if isCallTimeSetting("AURA_ASSET_MAX_VIDEO_BYTES") {
		t.Fatal("AURA_ASSET_MAX_VIDEO_BYTES must not be a call-time setting: it is read once at boot")
	}
}

func TestCallTimeSettingsReadAsLive(t *testing.T) {
	store := &fakeSettingsStore{rows: []sqlc.AuraSettings{{Key: "AURA_OPENROUTER_MANAGEMENT_KEY", Value: "sk-or-v1-mgmt", IsSecret: true}}}
	s := &Server{settings: store}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if item := settingItemByKey(t, rr.Body.Bytes(), "AURA_OPENROUTER_MANAGEMENT_KEY"); item.Applied != appliedLive {
		t.Fatalf("applied = %q, want live: the key is read on every call", item.Applied)
	}
}
