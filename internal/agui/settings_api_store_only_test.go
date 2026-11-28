package agui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/settings"
)

const houseKey = settings.AnthropicKeySetting

func TestHouseKeyIgnoresTheEnvironment(t *testing.T) {
	t.Setenv(houseKey, "sk-ant-from-the-environment")
	s := &Server{settings: &fakeSettingsStore{}}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	item := settingItemByKey(t, rr.Body.Bytes(), houseKey)
	if item.HasValue || item.Value != "" || item.Overridden {
		t.Fatalf("item = %+v, want no value: the environment never supplies the house key", item)
	}
	got, err := s.effectiveSettingValue(context.Background(), houseKey)
	if err != nil || got != "" {
		t.Fatalf("effective = %q, %v, want empty", got, err)
	}
}

func TestHouseKeyIsSealedAndLive(t *testing.T) {
	if meta, ok := settings.AllowedKeys[houseKey]; !ok || !meta.Secret {
		t.Fatalf("meta = %+v, ok = %v, want a secret key the store seals", meta, ok)
	}
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, houseKey, "sk-ant-house", "admin-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusOK || store.upserted[houseKey] != "sk-ant-house" {
		t.Fatalf("status = %d upserted = %v, want 200 and the key stored", rr.Code, store.upserted)
	}
	if strings.Contains(rr.Body.String(), "sk-ant-house") {
		t.Fatalf("body = %s, want an item that never echoes the key", rr.Body.String())
	}

	list := httptest.NewRecorder()
	s.handleListSettings(list, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if item := settingItemByKey(t, list.Body.Bytes(), houseKey); item.Applied != appliedLive || item.Value != "" {
		t.Fatalf("item = %+v, want live with no value on the wire", item)
	}

	del := httptest.NewRecorder()
	dr := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/settings/"+houseKey, nil), "admin-1")
	dr.SetPathValue("key", houseKey)
	s.handleDeleteSetting(del, dr)
	if del.Code >= 300 || len(store.deleted) != 1 || store.deleted[0] != houseKey {
		t.Fatalf("status = %d deleted = %v, want the row cleared", del.Code, store.deleted)
	}
}

func TestMemberCannotSetTheHouseKey(t *testing.T) {
	store := &fakeSettingsStore{rows: []sqlc.AuraSettings{}}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, houseKey, "sk-ant-house", "member-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusForbidden || len(store.upserted) != 0 {
		t.Fatalf("status = %d upserted = %v, want 403 and nothing written", rr.Code, store.upserted)
	}
}
