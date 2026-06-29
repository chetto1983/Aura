package agui

// settings_api.go is the cockpit "Settings" page backend (SETTINGS-01): the model-
// backend knobs the operator swaps local↔cloud (rerank/embed/STT/TTS/vision), the
// single OpenRouter key, and the embed dimension. Rows live in aura.settings and
// are overlaid onto the environment at boot (internal/settings.OverlayEnv), so a
// change takes effect on the NEXT RESTART — the GET response carries
// restart_required so the page shows a "restart to apply" banner.
//
// GET returns the allowlist + current effective values with SECRETS REDACTED (the
// real value never crosses the wire on read). PUT/DELETE are operator write-class
// actions gated by RequireCapability(governance.write) at the parent-mux mount
// (serve_webui.go); GET is gated by governance.read. Every key is validated
// against the static allowlist + its Kind before persisting, so the API can never
// write a non-model key (the allowlist already excludes connection/security env).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/settings"
)

var (
	errInvalidInt  = errors.New("value must be an integer")
	errInvalidBool = errors.New("value must be a boolean (true/false)")
)

// settingsStore is the aura.settings CRUD seam the handlers need
// (internal/settings.Store satisfies it). Kept narrow so tests inject a fake.
type settingsStore interface {
	List(ctx context.Context) ([]sqlc.AuraSettings, error)
	Upsert(ctx context.Context, key, value, by string) (sqlc.AuraSettings, error)
	Delete(ctx context.Context, key string) error
}

// SetSettingsStore wires the aura.settings store the Settings routes use. Set by
// the daemon composition root (serve) after NewServer; until set, the routes
// answer 503 so a stack without a pool degrades gracefully (the SetCalendarMCP
// precedent).
func (s *Server) SetSettingsStore(store settingsStore) { s.settings = store }

func (s *Server) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", s.handleListSettings)
	mux.HandleFunc("PUT /api/settings/{key}", s.handlePutSetting)
	mux.HandleFunc("DELETE /api/settings/{key}", s.handleDeleteSetting)
}

// settingItemDTO is one row of the Settings page: the allowlist metadata + the
// current effective value (redacted when secret). overridden marks a value set via
// aura.settings; updated_at/by are present only for overridden rows.
type settingItemDTO struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Kind       string `json:"kind"`
	Secret     bool   `json:"secret"`
	Value      string `json:"value"`
	HasValue   bool   `json:"has_value"`
	Overridden bool   `json:"overridden"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	UpdatedBy  string `json:"updated_by,omitempty"`
}

type settingsListDTO struct {
	Settings        []settingItemDTO `json:"settings"`
	RestartRequired bool             `json:"restart_required"`
}

func (s *Server) handleListSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "settings not configured"})
		return
	}
	rows, err := s.settings.List(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
		return
	}
	byKey := make(map[string]sqlc.AuraSettings, len(rows))
	for _, row := range rows {
		byKey[row.Key] = row
	}

	keys := make([]string, 0, len(settings.AllowedKeys))
	for k := range settings.AllowedKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := settingsListDTO{Settings: make([]settingItemDTO, 0, len(keys))}
	for _, key := range keys {
		meta := settings.AllowedKeys[key]
		row, overridden := byKey[key]
		item := settingItemDTO{
			Key: key, Label: meta.Label, Kind: string(meta.Kind), Secret: meta.Secret, Overridden: overridden,
		}
		// Effective value: the pending DB value when overridden, else the current
		// process env (which the boot overlay already reflects). A change made after
		// boot (DB value != live env) flags restart_required.
		effective := os.Getenv(key)
		if overridden {
			effective = row.Value
			if row.UpdatedAt.Valid {
				item.UpdatedAt = row.UpdatedAt.Time.UTC().Format(time.RFC3339)
			}
			if row.UpdatedBy.Valid {
				item.UpdatedBy = row.UpdatedBy.String
			}
			if row.Value != os.Getenv(key) {
				out.RestartRequired = true
			}
		}
		item.HasValue = effective != ""
		if !meta.Secret {
			item.Value = effective // a secret's value NEVER crosses the wire on read
		}
		out.Settings = append(out.Settings, item)
	}
	writeJSON(w, out)
}

type putSettingBody struct {
	Value string `json:"value"`
}

func (s *Server) handlePutSetting(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "settings not configured"})
		return
	}
	key := r.PathValue("key")
	meta, ok := settings.Allowed(key)
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "unknown setting key"})
		return
	}
	actor, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	raw, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	var body putSettingBody
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if err := validateSettingValue(meta.Kind, body.Value); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	row, err := s.settings.Upsert(r.Context(), key, body.Value, actor)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
		return
	}
	writeJSON(w, settingItemFromRow(meta, row))
}

func (s *Server) handleDeleteSetting(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "settings not configured"})
		return
	}
	key := r.PathValue("key")
	if _, ok := settings.Allowed(key); !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "unknown setting key"})
		return
	}
	if _, ok := principalIdentityID(r); !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if err := s.settings.Delete(r.Context(), key); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"key": key, "deleted": true, "restart_required": true})
}

// validateSettingValue rejects a value that does not parse for its Kind (an int
// knob like AURA_EMBED_DIMENSIONS must be an int; a bool like AURA_VISION_CLOUD
// must parse) so a bad value never reaches config.Load's silent default fallback.
func validateSettingValue(kind settings.Kind, value string) error {
	switch kind {
	case settings.KindInt:
		if _, err := strconv.Atoi(value); err != nil {
			return errInvalidInt
		}
	case settings.KindBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return errInvalidBool
		}
	}
	return nil
}

// settingItemFromRow projects a stored row to the redacted DTO returned by a write
// (a freshly-set value is always overridden + pending a restart).
func settingItemFromRow(meta settings.KeyMeta, row sqlc.AuraSettings) settingItemDTO {
	item := settingItemDTO{
		Key: row.Key, Label: meta.Label, Kind: string(meta.Kind), Secret: meta.Secret,
		HasValue: row.Value != "", Overridden: true,
	}
	if !meta.Secret {
		item.Value = row.Value
	}
	if row.UpdatedAt.Valid {
		item.UpdatedAt = row.UpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	if row.UpdatedBy.Valid {
		item.UpdatedBy = row.UpdatedBy.String
	}
	return item
}
