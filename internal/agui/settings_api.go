package agui

// settings_api.go is the cockpit "Settings" page backend (SETTINGS-01): the model-
// backend knobs the operator swaps local↔cloud (embed/STT/TTS/vision), the single
// OpenRouter key, and the embed dimension. Rows live in aura.settings and are
// overlaid onto the environment at boot (internal/settings.OverlayEnv). The primary
// LLM profile is additionally hot-published; restart_required covers
// only a persisted difference whose runtime remains boot-bound.
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
	"maps"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/settings"
)

var (
	errInvalidInt  = errors.New("value must be an integer")
	errInvalidBool = errors.New("value must be a boolean (true/false)")
)

var hotLLMProfileKeys = map[string]struct{}{
	"AURA_LLM_PROVIDER":            {},
	"AURA_LLM_BASE_URL":            {},
	"AURA_LLM_MODEL":               {},
	"AURA_LLM_MAX_TOKENS":          {},
	"AURA_MODEL_CONTEXT_WINDOW":    {},
	"AURA_MODEL_MAX_OUTPUT_TOKENS": {},
}

// settingsStore is the aura.settings CRUD seam the handlers need
// (internal/settings.Store satisfies it). Kept narrow so tests inject a fake.
type settingsStore interface {
	List(ctx context.Context) ([]sqlc.AuraSettings, error)
	Upsert(ctx context.Context, key, value, by string) (sqlc.AuraSettings, error)
	ReplaceMany(ctx context.Context, values map[string]string, deletes []string, by string) ([]sqlc.AuraSettings, error)
	Delete(ctx context.Context, key string) error
}

// llmRouteReloader validates and publishes the complete persisted primary-profile
// override set. The composition root supplies the boot fallback and concrete client.
type llmRouteReloader interface {
	Prepare(ctx context.Context, overrides map[string]string, resetKeys []string) (apply func(), err error)
	EffectiveValue(key string) (string, bool)
}

// TelegramBotProbe validates a Telegram Bot API token and returns the bot username.
// The token is a secret and must never be logged or echoed by the probe caller.
type TelegramBotProbe func(ctx context.Context, token string) (username string, err error)

// SetSettingsStore wires the aura.settings store the Settings routes use. Set by
// the daemon composition root (serve) after NewServer; until set, the routes
// answer 503 so a stack without a pool degrades gracefully (the SetCalendarMCP
// precedent).
func (s *Server) SetSettingsStore(store settingsStore) { s.settings = store }

// SetLLMRouteReloader wires the hot primary-LLM profile publisher.
func (s *Server) SetLLMRouteReloader(reloader llmRouteReloader) { s.llmRouteReloader = reloader }

// SetTelegramBotProbe wires the live getMe validator used by the Settings Telegram
// recovery panel. Until set, the availability check answers 503.
func (s *Server) SetTelegramBotProbe(probe TelegramBotProbe) { s.telegramProbe = probe }

func (s *Server) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", s.handleListSettings)
	mux.HandleFunc("PUT /api/settings/llm-profile", s.handlePutLLMProfile)
	mux.HandleFunc("POST /api/settings/telegram/check", s.handleCheckTelegramAvailability)
	mux.HandleFunc("POST /api/settings/telegram/link", s.handleCreateSettingsTelegramLink)
	mux.HandleFunc("GET /api/settings/telegram/{sessionToken}/status", s.handleSettingsTelegramStatus)
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
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
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
		// Effective value: the DB value when overridden, else the active runtime for a
		// hot model-profile key, else the process env. A
		// post-boot difference flags restart_required unless this key is one of the
		// primary-route values the wired runtime reloader has already published.
		effective := os.Getenv(key)
		if !overridden && s.hotLLMRouteEnabled(key) {
			if value, ok := s.llmRouteReloader.EffectiveValue(key); ok {
				effective = value
			}
		}
		if overridden {
			effective = row.Value
			if row.UpdatedAt.Valid {
				item.UpdatedAt = row.UpdatedAt.Time.UTC().Format(time.RFC3339)
			}
			if row.UpdatedBy.Valid {
				item.UpdatedBy = row.UpdatedBy.String
			}
			if row.Value != os.Getenv(key) && !s.hotLLMRouteEnabled(key) {
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

type putLLMProfileBody struct {
	Settings map[string]string `json:"settings"`
}

// handlePutLLMProfile prepares one complete model profile, persists every edited
// row in one transaction, then publishes the already-prepared snapshot. The cockpit
// uses this route for cloud/local changes so no mixed provider/base/model state can
// become visible between sequential key writes.
func (s *Server) handlePutLLMProfile(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil || s.llmRouteReloader == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "LLM profile settings not configured"})
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
	var body putLLMProfileBody
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Settings) == 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid LLM profile body"})
		return
	}
	for key, value := range body.Settings {
		if _, hot := hotLLMProfileKeys[key]; !hot {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "LLM profile contains an unsupported key"})
			return
		}
		meta := settings.AllowedKeys[key]
		if err := validateSettingValue(meta.Kind, value); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	rows, err := s.settings.List(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
		return
	}
	overrides := llmProfileOverrides(rows)
	maps.Copy(overrides, body.Settings)
	resetKeys := modelLimitResetKeys(rows, body.Settings)
	for _, key := range resetKeys {
		delete(overrides, key)
	}
	apply, err := s.llmRouteReloader.Prepare(r.Context(), overrides, resetKeys)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if _, err := s.settings.ReplaceMany(r.Context(), body.Settings, resetKeys, actor); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
		return
	}
	apply()
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"updated": len(body.Settings), "restart_required": false,
	})
}

var modelRouteKeys = []string{"AURA_LLM_PROVIDER", "AURA_LLM_BASE_URL", "AURA_LLM_MODEL"}
var modelLimitKeys = []string{"AURA_MODEL_CONTEXT_WINDOW", "AURA_MODEL_MAX_OUTPUT_TOKENS"}

func modelLimitResetKeys(rows []sqlc.AuraSettings, next map[string]string) []string {
	current := make(map[string]string, len(rows))
	for _, row := range rows {
		current[row.Key] = row.Value
	}
	routeChanged := false
	for _, key := range modelRouteKeys {
		if value, present := next[key]; present && strings.TrimSpace(value) != strings.TrimSpace(current[key]) {
			routeChanged = true
			break
		}
	}
	if !routeChanged {
		return nil
	}
	reset := make([]string, 0, len(modelLimitKeys))
	for _, key := range modelLimitKeys {
		if _, explicit := next[key]; explicit {
			continue
		}
		reset = append(reset, key)
	}
	return reset
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
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	var rows []sqlc.AuraSettings
	if s.hotLLMRouteEnabled(key) || isLLMTokenSetting(key) {
		var err error
		rows, err = s.settings.List(r.Context())
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
			return
		}
	}
	if isLLMTokenSetting(key) {
		if err := validatePendingLLMTokenSetting(rows, key, body.Value); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	var routeOverrides map[string]string
	var applyRoute func()
	var err error
	if s.hotLLMRouteEnabled(key) {
		routeOverrides = llmProfileOverrides(rows)
		routeOverrides[key] = body.Value
		applyRoute, err = s.llmRouteReloader.Prepare(r.Context(), routeOverrides, nil)
		if err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	row, err := s.settings.Upsert(r.Context(), key, body.Value, actor)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
		return
	}
	if applyRoute != nil {
		applyRoute()
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
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	var routeOverrides map[string]string
	var applyRoute func()
	if s.hotLLMRouteEnabled(key) {
		rows, err := s.settings.List(r.Context())
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
			return
		}
		routeOverrides = llmProfileOverrides(rows)
		delete(routeOverrides, key)
		applyRoute, err = s.llmRouteReloader.Prepare(r.Context(), routeOverrides, nil)
		if err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := s.settings.Delete(r.Context(), key); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
		return
	}
	if applyRoute != nil {
		applyRoute()
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"key": key, "deleted": true, "restart_required": routeOverrides == nil,
	})
}

func (s *Server) hotLLMRouteEnabled(key string) bool {
	_, hot := hotLLMProfileKeys[key]
	return hot && s.llmRouteReloader != nil
}

func llmProfileOverrides(rows []sqlc.AuraSettings) map[string]string {
	overrides := make(map[string]string, len(hotLLMProfileKeys))
	for _, row := range rows {
		if _, hot := hotLLMProfileKeys[row.Key]; hot {
			overrides[row.Key] = row.Value
		}
	}
	return overrides
}

type telegramAvailabilityRequest struct {
	Token string `json:"token,omitempty"`
}

type telegramAvailabilityDTO struct {
	Configured      bool   `json:"configured"`
	Available       bool   `json:"available"`
	BotUsername     string `json:"botUsername,omitempty"`
	RequiresRestart bool   `json:"requiresRestart"`
	Error           string `json:"error,omitempty"`
}

func (s *Server) handleCheckTelegramAvailability(w http.ResponseWriter, r *http.Request) {
	if s.telegramProbe == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "telegram validation not configured"})
		return
	}
	raw, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	var req telegramAvailabilityRequest
	if strings.TrimSpace(string(raw)) != "" {
		if err := json.Unmarshal(raw, &req); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		var err error
		token, err = s.effectiveSettingValue(r.Context(), "TELEGRAM_BOT_TOKEN")
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
			return
		}
	}
	if token == "" {
		writeJSON(w, telegramAvailabilityDTO{Configured: false, Available: false})
		return
	}
	username, err := s.telegramProbe(r.Context(), token)
	requiresRestart := token != os.Getenv("TELEGRAM_BOT_TOKEN")
	if err != nil {
		writeJSON(w, telegramAvailabilityDTO{
			Configured:      true,
			Available:       false,
			RequiresRestart: requiresRestart,
			Error:           "bot token validation failed",
		})
		return
	}
	writeJSON(w, telegramAvailabilityDTO{
		Configured:      true,
		Available:       true,
		BotUsername:     username,
		RequiresRestart: requiresRestart,
	})
}

func (s *Server) effectiveSettingValue(ctx context.Context, key string) (string, error) {
	if s.settings != nil {
		rows, err := s.settings.List(ctx)
		if err != nil {
			return "", err
		}
		for _, row := range rows {
			if row.Key == key {
				return strings.TrimSpace(row.Value), nil
			}
		}
	}
	return strings.TrimSpace(os.Getenv(key)), nil
}

// handleCreateSettingsTelegramLink mints the one-time Telegram linking code for the
// AUTHENTICATED caller (D-02: a normal self-scoped USER action, each user links their
// own Telegram to their OWN identity — NEVER operator-pinned). It is the web half of the
// D-24 web-initiated linking flow: CreateTelegramLink scopes to `requester` (the bound
// principal, never the seeded local admin), and the returned deep-link carries the code
// ONLY on the <=1h `?start=` setup-bootstrap URL — no long-lived session token ever
// crosses a URL/query string (MUSR-06). The bot then binds this sender's chat-id to
// `requester`'s identity when the code arrives via /start (telegram onboarding consume).
func (s *Server) handleCreateSettingsTelegramLink(w http.ResponseWriter, r *http.Request) {
	if s.onboarding == nil {
		http.Error(w, "onboarding service not configured", http.StatusServiceUnavailable)
		return
	}
	requester, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	link, err := s.onboarding.CreateTelegramLink(r.Context(), requester)
	if err != nil {
		s.writeOnboardingError(w, err)
		return
	}
	writeJSON(w, link)
}

func (s *Server) handleSettingsTelegramStatus(w http.ResponseWriter, r *http.Request) {
	handleOnboardingSessionRequest(s, w, r, func(ctx context.Context, requester, token string) (OnboardingTelegramStatus, error) {
		return s.onboarding.TelegramStatus(ctx, requester, token)
	})
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

func isLLMTokenSetting(key string) bool {
	switch key {
	case "AURA_LLM_MAX_TOKENS",
		"AURA_MODEL_CONTEXT_WINDOW",
		"AURA_MODEL_MAX_OUTPUT_TOKENS":
		return true
	default:
		return false
	}
}

func validatePendingLLMTokenSetting(rows []sqlc.AuraSettings, key, value string) error {
	cfg, err := llm.LoadAllowEmptyKey()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if isLLMTokenSetting(row.Key) {
			if err := applyLLMTokenSetting(cfg, row.Key, row.Value); err != nil {
				return err
			}
		}
	}
	if err := applyLLMTokenSetting(cfg, key, value); err != nil {
		return err
	}
	return cfg.Validate()
}

func applyLLMTokenSetting(cfg *llm.Config, key, value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return errInvalidInt
	}
	switch key {
	case "AURA_LLM_MAX_TOKENS":
		cfg.MaxTokens = parsed
	case "AURA_MODEL_CONTEXT_WINDOW":
		cfg.ContextWindow = parsed
		cfg.ContextWindowConfigured = true
	case "AURA_MODEL_MAX_OUTPUT_TOKENS":
		cfg.MaxOutputTokens = parsed
		cfg.MaxOutputTokensConfigured = true
	}
	return nil
}

// settingItemFromRow projects a stored row to the redacted DTO returned by a write.
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
