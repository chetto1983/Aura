package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/aura/aura/internal/settings"
	"github.com/aura/aura/internal/setup"
)

// SettingItem is one row in the GET /settings response.
type SettingItem struct {
	Key      string `json:"key"`
	Value    string `json:"value"`              // empty when not set
	IsSecret bool   `json:"is_secret"`          // hint for the UI input type
	Label    string `json:"label,omitempty"`    // human-friendly label
	Group    string `json:"group,omitempty"`    // ui section: provider | budget | embeddings | ocr | summarizer | other
}

// SettingsListResponse is the GET /settings body.
type SettingsListResponse struct {
	Items []SettingItem `json:"items"`
}

// SettingsUpdateRequest is the POST /settings body.
type SettingsUpdateRequest struct {
	// Updates is a map of key -> new value. Empty value deletes the row
	// (returning the field to its env / default value). Unknown keys are
	// rejected with 400.
	Updates map[string]string `json:"updates"`
}

// SettingsUpdateResponse is the POST /settings body.
type SettingsUpdateResponse struct {
	OK      bool     `json:"ok"`
	Applied []string `json:"applied,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

// SettingsTestRequest is the POST /settings/test body.
type SettingsTestRequest struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	ProbePath string `json:"probe_path,omitempty"`
}

// settingsCatalog is the master list of editable keys with their UI
// metadata. Only fields the operator should reasonably change live are
// here; LLM_MAX_RETRIES and other fine-tuning knobs stay overridable
// programmatically but aren't surfaced in the dashboard form.
var settingsCatalog = []SettingItem{
	{Key: settings.KeyLLMBaseURL, Group: "provider", Label: "LLM base URL"},
	{Key: settings.KeyLLMModel, Group: "provider", Label: "LLM model"},
	{Key: settings.KeyLLMAPIKey, Group: "provider", Label: "LLM API key", IsSecret: true},
	{Key: settings.KeyOllamaBaseURL, Group: "provider", Label: "Ollama base URL (failover)"},
	{Key: settings.KeyOllamaModel, Group: "provider", Label: "Ollama model"},
	{Key: settings.KeyOllamaAPIKey, Group: "provider", Label: "Ollama API key", IsSecret: true},

	{Key: settings.KeyEmbeddingBaseURL, Group: "embeddings", Label: "Embeddings base URL"},
	{Key: settings.KeyEmbeddingModel, Group: "embeddings", Label: "Embeddings model"},
	{Key: settings.KeyEmbeddingAPIKey, Group: "embeddings", Label: "Embeddings API key", IsSecret: true},

	{Key: settings.KeyMistralAPIKey, Group: "ocr", Label: "Mistral OCR API key", IsSecret: true},
	{Key: settings.KeyMistralOCRModel, Group: "ocr", Label: "OCR model"},
	{Key: settings.KeyOCREnabled, Group: "ocr", Label: "OCR enabled (true/false)"},
	{Key: settings.KeyOCRMaxPages, Group: "ocr", Label: "OCR max pages"},
	{Key: settings.KeyOCRMaxFileMB, Group: "ocr", Label: "OCR max file MB"},

	{Key: settings.KeySoftBudget, Group: "budget", Label: "Soft budget (USD)"},
	{Key: settings.KeyHardBudget, Group: "budget", Label: "Hard budget (USD)"},
	{Key: settings.KeyCostPerToken, Group: "budget", Label: "Cost per token (USD)"},
	{Key: settings.KeyMaxContextTokens, Group: "budget", Label: "Max context tokens"},
	{Key: settings.KeyMaxHistoryMessages, Group: "budget", Label: "Max in-flight messages"},
	{Key: settings.KeyMaxToolIterations, Group: "budget", Label: "Max tool iterations / turn"},

	{Key: settings.KeySummarizerEnabled, Group: "summarizer", Label: "Summarizer enabled"},
	{Key: settings.KeySummarizerMode, Group: "summarizer", Label: "Summarizer mode (off/review/auto)"},
	{Key: settings.KeySummarizerTurnInterval, Group: "summarizer", Label: "Summarizer turn interval"},
	{Key: settings.KeySummarizerMinSalience, Group: "summarizer", Label: "Min salience"},
	{Key: settings.KeySummarizerLookbackTurns, Group: "summarizer", Label: "Lookback turns"},
	{Key: settings.KeySummarizerCooldownSeconds, Group: "summarizer", Label: "Cooldown seconds"},

	{Key: settings.KeyConvArchiveEnabled, Group: "other", Label: "Conversation archive enabled"},
	{Key: settings.KeyOTelEnabled, Group: "other", Label: "OpenTelemetry tracing enabled"},
	{Key: settings.KeySkillsAdmin, Group: "other", Label: "Skills admin (catalog install/delete)"},
	{Key: settings.KeyAllowlist, Group: "other", Label: "Telegram allowlist (comma-separated IDs)"},
}

func handleSettingsList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Settings == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "settings store unavailable")
			return
		}
		ctx := r.Context()
		items := make([]SettingItem, 0, len(settingsCatalog))
		for _, meta := range settingsCatalog {
			it := meta
			if v, err := deps.Settings.Get(ctx, meta.Key); err == nil {
				it.Value = v
			}
			items = append(items, it)
		}
		writeJSON(w, deps.Logger, http.StatusOK, SettingsListResponse{Items: items})
	}
}

func handleSettingsUpdate(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Settings == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "settings store unavailable")
			return
		}
		var req SettingsUpdateRequest
		// Cap the body so a runaway client can't OOM the parser.
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		if err := dec.Decode(&req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if len(req.Updates) == 0 {
			writeJSON(w, deps.Logger, http.StatusOK, SettingsUpdateResponse{OK: true})
			return
		}
		// Validate every key first so we don't half-apply.
		for k := range req.Updates {
			if !settings.IsOverridable(k) {
				writeError(w, deps.Logger, http.StatusBadRequest, "key not overridable: "+k)
				return
			}
		}
		ctx := r.Context()
		applied := make([]string, 0, len(req.Updates))
		errs := []string{}
		for k, v := range req.Updates {
			v = strings.TrimSpace(v)
			if v == "" {
				if err := deps.Settings.Delete(ctx, k); err != nil {
					errs = append(errs, k+": "+err.Error())
					continue
				}
			} else {
				if err := deps.Settings.Set(ctx, k, v); err != nil {
					errs = append(errs, k+": "+err.Error())
					continue
				}
			}
			applied = append(applied, k)
		}
		writeJSON(w, deps.Logger, http.StatusOK, SettingsUpdateResponse{
			OK:      len(errs) == 0,
			Applied: applied,
			Errors:  errs,
		})
	}
}

func handleSettingsTest(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// No deps.Settings dependency — this just runs an outbound probe
		// against (base_url, key) so the user can validate before saving.
		var req SettingsTestRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		if err := dec.Decode(&req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		probePath := req.ProbePath
		if probePath == "" {
			probePath = "/models"
		}
		// Re-use the wizard's probe so behavior matches first-run setup.
		// 6s timeout is enforced inside ProbeProvider.
		result := setup.ProbeProvider(context.Background(), req.BaseURL, req.APIKey, probePath)
		writeJSON(w, deps.Logger, http.StatusOK, result)
	}
}
