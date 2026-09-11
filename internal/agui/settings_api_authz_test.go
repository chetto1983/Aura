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

func TestCallTimeSettingsReadAsLive(t *testing.T) {
	store := &fakeSettingsStore{rows: []sqlc.AuraSettings{{Key: "AURA_OPENROUTER_MANAGEMENT_KEY", Value: "sk-or-v1-mgmt", IsSecret: true}}}
	s := &Server{settings: store}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if item := settingItemByKey(t, rr.Body.Bytes(), "AURA_OPENROUTER_MANAGEMENT_KEY"); item.Applied != appliedLive {
		t.Fatalf("applied = %q, want live: the key is read on every call", item.Applied)
	}
}
