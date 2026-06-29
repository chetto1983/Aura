package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		{Key: "AURA_RERANK_MODEL", Value: "cohere/rerank-4-fast"},
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
	plain := settingItemByKey(t, rr.Body.Bytes(), "AURA_RERANK_MODEL")
	if plain.Value != "cohere/rerank-4-fast" {
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
		rr, r := putReq(t, "AURA_RERANK_MODEL", "cohere/rerank-4-fast", "")
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
