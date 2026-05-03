package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/aura/aura/internal/settings"
	"github.com/aura/aura/internal/setup"
)

// SettingItem is one row in the GET /settings response.
//
// Value is what the user should see in the form input — the effective
// value the bot is currently using. Source explains where it came from
// so the UI can label rows that are still env-controlled vs. dashboard
// controlled. Saving a non-empty Value via POST /settings always lands
// in the DB and flips Source to "db".
//
// Kind hints the UI which input control to render:
//   - "text"   (default) — text input
//   - "bool"   — toggle switch; value is "true" / "false"
//   - "int"    — number input
//   - "float"  — number input with decimals
//   - "enum"   — dropdown; Options carries the choices
//   - "url"    — text input with type="url"
type SettingItem struct {
	Key      string   `json:"key"`
	Value    string   `json:"value"`             // effective value (DB row, else env, else blank)
	Source   string   `json:"source"`            // "db" | "env" | "default"
	IsSecret bool     `json:"is_secret"`         // hint for the UI input type
	Kind     string   `json:"kind,omitempty"`    // text | bool | int | float | enum | url (default "text")
	Options  []string `json:"options,omitempty"` // populated only when kind=enum
	Label    string   `json:"label,omitempty"`
	Hint     string   `json:"hint,omitempty"` // optional one-line help under the input
	Group    string   `json:"group,omitempty"`
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
	{Key: settings.KeyLLMBaseURL, Group: "provider", Kind: "url", Label: "LLM base URL", Hint: "OpenAI-compatible endpoint (e.g. https://api.openai.com/v1)"},
	{Key: settings.KeyLLMModel, Group: "provider", Kind: "text", Label: "LLM model", Hint: "Model name as the provider expects it"},
	{Key: settings.KeyLLMAPIKey, Group: "provider", Kind: "text", IsSecret: true, Label: "LLM API key"},
	{Key: settings.KeyOllamaBaseURL, Group: "provider", Kind: "url", Label: "Ollama base URL (failover)", Hint: "Bare host, e.g. http://localhost:11434"},
	{Key: settings.KeyOllamaModel, Group: "provider", Kind: "text", Label: "Ollama model"},
	{Key: settings.KeyOllamaAPIKey, Group: "provider", Kind: "text", IsSecret: true, Label: "Ollama API key (rarely needed)"},

	{Key: settings.KeyEmbeddingBaseURL, Group: "embeddings", Kind: "url", Label: "Embeddings base URL"},
	{Key: settings.KeyEmbeddingModel, Group: "embeddings", Kind: "text", Label: "Embeddings model"},
	{Key: settings.KeyEmbeddingAPIKey, Group: "embeddings", Kind: "text", IsSecret: true, Label: "Embeddings API key"},

	{Key: settings.KeyMistralAPIKey, Group: "ocr", Kind: "text", IsSecret: true, Label: "Mistral OCR API key"},
	{Key: settings.KeyMistralOCRModel, Group: "ocr", Kind: "text", Label: "OCR model"},
	{Key: settings.KeyOCREnabled, Group: "ocr", Kind: "bool", Label: "OCR enabled"},
	{Key: settings.KeyOCRMaxPages, Group: "ocr", Kind: "int", Label: "OCR max pages", Hint: "Aura refuses PDFs longer than this"},
	{Key: settings.KeyOCRMaxFileMB, Group: "ocr", Kind: "int", Label: "OCR max file size (MB)"},

	{Key: settings.KeySoftBudget, Group: "budget", Kind: "float", Label: "Soft budget (USD)", Hint: "Telegram warning fires once this is crossed"},
	{Key: settings.KeyHardBudget, Group: "budget", Kind: "float", Label: "Hard budget (USD)", Hint: "Bot refuses LLM calls past this"},
	{Key: settings.KeyCostPerToken, Group: "budget", Kind: "float", Label: "Cost per token (USD)", Hint: "Used to estimate spend; provider-specific"},
	{Key: settings.KeyMaxContextTokens, Group: "budget", Kind: "int", Label: "Max context tokens", Hint: "Summarization fires at 80% of this"},
	{Key: settings.KeyMaxHistoryMessages, Group: "budget", Kind: "int", Label: "Max in-flight messages", Hint: "Hard cap; oldest evicted first"},
	{Key: settings.KeyMaxToolIterations, Group: "budget", Kind: "int", Label: "Max tool iterations / turn"},

	{Key: settings.KeySummarizerEnabled, Group: "summarizer", Kind: "bool", Label: "Summarizer enabled"},
	{Key: settings.KeySummarizerMode, Group: "summarizer", Kind: "enum", Options: []string{"off", "review", "auto"}, Label: "Summarizer mode", Hint: "review = queue for dashboard approval; auto = direct wiki write"},
	{Key: settings.KeySummarizerTurnInterval, Group: "summarizer", Kind: "int", Label: "Run every N turns"},
	{Key: settings.KeySummarizerMinSalience, Group: "summarizer", Kind: "float", Label: "Min salience"},
	{Key: settings.KeySummarizerLookbackTurns, Group: "summarizer", Kind: "int", Label: "Lookback turns"},
	{Key: settings.KeySummarizerCooldownSeconds, Group: "summarizer", Kind: "int", Label: "Cooldown (s)"},

	{Key: settings.KeyConvArchiveEnabled, Group: "other", Kind: "bool", Label: "Conversation archive enabled"},
	{Key: settings.KeyOTelEnabled, Group: "other", Kind: "bool", Label: "OpenTelemetry tracing enabled"},
	{Key: settings.KeySkillsAdmin, Group: "other", Kind: "bool", Label: "Skills admin (catalog install/delete)"},
	{Key: settings.KeyAuraBotEnabled, Group: "other", Kind: "bool", Label: "AuraBot swarm enabled", Hint: "Enables bounded background agents and spawn_aurabot tools"},
	{Key: settings.KeyAuraBotMaxActive, Group: "other", Kind: "int", Label: "AuraBot max active"},
	{Key: settings.KeyAuraBotMaxDepth, Group: "other", Kind: "int", Label: "AuraBot max depth"},
	{Key: settings.KeyAuraBotTimeoutSec, Group: "other", Kind: "int", Label: "AuraBot timeout (seconds)"},
	{Key: settings.KeyAuraBotMaxIterations, Group: "other", Kind: "int", Label: "AuraBot max iterations"},
	{Key: settings.KeyAllowlist, Group: "other", Kind: "text", Label: "Telegram allowlist", Hint: "Comma-separated user IDs; leave blank for first-run bootstrap"},
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
			// DB row wins. Else fall back to the env value the bot
			// loaded at startup so the form reflects effective state.
			// Source flag tells the UI which it is.
			if v, err := deps.Settings.Get(ctx, meta.Key); err == nil && v != "" {
				it.Value = v
				it.Source = "db"
			} else if envVal := os.Getenv(meta.Key); envVal != "" {
				it.Value = envVal
				it.Source = "env"
			} else {
				it.Source = "default"
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
