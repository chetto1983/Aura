package agui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// Per-item application state (amendment #188): a hot key is "live" whatever its
// row says, a boot-bound key persisted after boot is "restart" and is named in
// restart_keys, and an untouched boot-bound key is "boot".
func TestHandleListSettingsReportsPerItemApplicationState(t *testing.T) {
	t.Setenv("AURA_VISION_CLOUD", "false")
	t.Setenv("AURA_EMBED_DIMENSIONS", "768")
	t.Setenv("AURA_LOOP_MAX_STEPS", "25")
	s := &Server{
		settings: &fakeSettingsStore{rows: []sqlc.AuraSettings{
			{Key: "AURA_VISION_CLOUD", Value: "true"},
			{Key: "AURA_EMBED_DIMENSIONS", Value: "768"},
			{Key: "AURA_LOOP_MAX_STEPS", Value: "60"},
		}},
		llmRouteReloader: &fakeLLMRouteReloader{},
	}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	var got settingsListDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"AURA_VISION_CLOUD":     appliedRestart,
		"AURA_EMBED_DIMENSIONS": appliedBoot,
		"AURA_LOOP_MAX_STEPS":   appliedLive,
		"AURA_TTS_MODEL":        appliedBoot,
	}
	for _, item := range got.Settings {
		if state, ok := want[item.Key]; ok && item.Applied != state {
			t.Errorf("%s applied = %q, want %q", item.Key, item.Applied, state)
		}
	}
	if !got.RestartRequired || !slices.Equal(got.RestartKeys, []string{"AURA_VISION_CLOUD"}) {
		t.Fatalf("restart = %v keys %v, want true [AURA_VISION_CLOUD]", got.RestartRequired, got.RestartKeys)
	}
}

// An unpinned loop budget falls through to the process env in the GET, exactly
// like before it became a setting; a reloader that pins it wins.
func TestHandleListSettingsLoopBudgetEffectiveValue(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "25")
	s := &Server{settings: &fakeSettingsStore{}, llmRouteReloader: &fakeLLMRouteReloader{}}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	item := settingItemByKey(t, rr.Body.Bytes(), "AURA_LOOP_MAX_STEPS")
	if item.Value != "25" || item.Overridden || item.Applied != appliedLive {
		t.Fatalf("unpinned loop item = %+v, want env 25 / live", item)
	}
	s.llmRouteReloader = &fakeLLMRouteReloader{effective: map[string]string{"AURA_LOOP_MAX_STEPS": "60"}}
	rr = httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if item := settingItemByKey(t, rr.Body.Bytes(), "AURA_LOOP_MAX_STEPS"); item.Value != "60" {
		t.Fatalf("pinned loop item value = %q, want the runtime's 60", item.Value)
	}
}

// The batch route accepts the four keys that joined the hot profile and rejects a
// boot-bound key, so the cockpit can save them in one prepare→persist→publish.
func TestHandlePutLLMProfileAcceptsLoopBudgetTriggerAndKey(t *testing.T) {
	store := &fakeSettingsStore{}
	reloader := &fakeLLMRouteReloader{}
	s := &Server{settings: store, llmRouteReloader: reloader}
	body := strings.NewReader(`{"settings":{"AURA_LOOP_MAX_STEPS":"60","AURA_LOOP_MAX_WALLCLOCK_SEC":"1200",` +
		`"AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT":"40","OPENROUTER_API_KEY":"sk-rotated"}}`)
	rr := httptest.NewRecorder()
	s.handlePutLLMProfile(rr, withPrincipal(httptest.NewRequest(http.MethodPut, "/api/settings/llm-profile", body), "op-1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	if len(reloader.applied) != 1 || reloader.applied[0]["AURA_LOOP_MAX_STEPS"] != "60" || store.upserted["OPENROUTER_API_KEY"] != "sk-rotated" {
		t.Fatalf("applied %v upserted %v", reloader.applied, store.upserted)
	}

	rr = httptest.NewRecorder()
	body = strings.NewReader(`{"settings":{"AURA_VISION_CLOUD":"true"}}`)
	s.handlePutLLMProfile(rr, withPrincipal(httptest.NewRequest(http.MethodPut, "/api/settings/llm-profile", body), "op-1"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("boot-bound key in the hot batch: status = %d, want 400", rr.Code)
	}
}
