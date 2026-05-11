package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/config"
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
	Key             string   `json:"key"`
	Value           string   `json:"value"`             // saved/effective-on-next-start value (DB row, else env/default)
	Source          string   `json:"source"`            // "db" | "env" | "default"
	ActiveValue     string   `json:"active_value"`      // value used by this running process after startup config overlay
	RestartRequired bool     `json:"restart_required"`  // true when Value differs from ActiveValue
	IsSecret        bool     `json:"is_secret"`         // hint for the UI input type
	ReadOnly        bool     `json:"read_only"`         // visible diagnostics that must stay in .env / process env
	Kind            string   `json:"kind,omitempty"`    // text | bool | int | float | enum | url (default "text")
	Options         []string `json:"options,omitempty"` // populated only when kind=enum
	Min             *float64 `json:"min,omitempty"`     // numeric lower bound for int/float controls
	Max             *float64 `json:"max,omitempty"`     // numeric upper bound for int/float controls
	Label           string   `json:"label,omitempty"`
	Hint            string   `json:"hint,omitempty"` // optional one-line help under the input
	Group           string   `json:"group,omitempty"`
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
	OK             bool     `json:"ok"`
	Applied        []string `json:"applied,omitempty"`
	Errors         []string `json:"errors,omitempty"`
	RuntimeApplied bool     `json:"runtime_applied,omitempty"`
	RuntimeError   string   `json:"runtime_error,omitempty"`
}

// SettingsTestRequest is the POST /settings/test body.
type SettingsTestRequest struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	ProbePath string `json:"probe_path,omitempty"`
}

// settingsCatalog is the dashboard-facing catalog. The settings applier
// supports more keys than are surfaced here; new rows should still be
// editable from the .env file but the UI exposes the operator knobs that
// matter during normal Aura operation. Each key relies on the matching
// settings.item.<KEY>.{label,hint} entries in the i18n locale files for
// localized strings; the Label/Hint fields are fallbacks for clients that
// don't ship locale bundles.
var settingsCatalog = []SettingItem{
	{Key: settings.KeyTimezone, Group: "runtime", Kind: "text", Label: "Scheduler timezone", Hint: "IANA name like Europe/Rome"},

	{Key: settings.KeyLLMBaseURL, Group: "provider", Kind: "url", Label: "LLM base URL", Hint: "OpenAI-compatible endpoint (e.g. https://api.openai.com/v1)"},
	{Key: settings.KeyLLMModel, Group: "provider", Kind: "text", Label: "LLM model", Hint: "Model name as the provider expects it"},
	{Key: settings.KeyLLMAPIKey, Group: "provider", Kind: "text", IsSecret: true, Label: "LLM API key"},
	{Key: settings.KeyLLMMaxRetries, Group: "provider", Kind: "int", Min: floatPtr(0), Max: floatPtr(20), Label: "LLM max retries"},

	{Key: settings.KeyMaxContextTokens, Group: "budget", Kind: "int", Min: floatPtr(1024), Label: "Max context tokens"},
	{Key: settings.KeyMaxHistoryMessages, Group: "budget", Kind: "int", Min: floatPtr(1), Label: "Max in-flight messages"},
	{Key: settings.KeySoftBudget, Group: "budget", Kind: "float", Min: floatPtr(0), Label: "Soft budget (USD)"},
	{Key: settings.KeyHardBudget, Group: "budget", Kind: "float", Min: floatPtr(0), Label: "Hard budget (USD)"},
	{Key: settings.KeyCostInputPerMTokens, Group: "budget", Kind: "float", Min: floatPtr(0), Label: "Input price (USD / 1M tokens)"},
	{Key: settings.KeyCostOutputPerMTokens, Group: "budget", Kind: "float", Min: floatPtr(0), Label: "Output price (USD / 1M tokens)"},

	{Key: settings.KeyPromptVersion, Group: "agent", Kind: "text", Label: "Prompt version"},
	{Key: settings.KeySkillRoutingMode, Group: "agent", Kind: "enum", Options: []string{"manifest", "manifest_llm_review"}, Label: "Skill routing mode"},
	{Key: settings.KeyAgentLoopMaxSteps, Group: "agent", Kind: "int", Min: floatPtr(1), Max: floatPtr(10000), Label: "Agent loop max steps"},
	{Key: settings.KeyReasoningEffort, Group: "agent", Kind: "enum", Options: []string{"", "enabled", "minimal", "low", "medium", "high", "xhigh"}, Label: "Reasoning effort", Hint: "Provider-side chain-of-thought. Empty disables. 'enabled' turns reasoning on with provider default depth (use this for DeepSeek V4 Flash). 'low'..'xhigh' set explicit depth on models that support it (OpenAI o-series, gpt-5*). Unknown providers ignore the field."},
	{Key: settings.KeyToolSearchTopK, Group: "agent", Kind: "int", Min: floatPtr(1), Max: floatPtr(50), Label: "Tool search top-K", Hint: "How many retrieved tools to expose per turn on top of the always-on core. Raise on large-context models so web_fetch/web_search aren't crowded out"},
	{Key: settings.KeyToolSearchBackend, Group: "agent", Kind: "enum", Options: []string{"hybrid", "vector", "fts"}, Label: "Tool search backend", Hint: "Restart required: rebuilds the tool vector index. hybrid mixes BM25 + embeddings; vector is pure semantic; fts is keyword-only"},
	{Key: settings.KeyMaxToolResultChars, Group: "agent", Kind: "int", Min: floatPtr(1000), Max: floatPtr(500000), Label: "Max tool result chars", Hint: "Cap per tool message before the LLM call; raise on large-context models"},
	{Key: settings.KeyMicrocompactKeepRecent, Group: "agent", Kind: "int", Min: floatPtr(1), Max: floatPtr(500), Label: "Microcompact keep recent", Hint: "Tool results older than the N most recent get collapsed to a one-line stub"},
	{Key: settings.KeyMicrocompactMinChars, Group: "agent", Kind: "int", Min: floatPtr(100), Max: floatPtr(100000), Label: "Microcompact min chars", Hint: "Tool results smaller than this are never compacted"},
	{Key: settings.KeyTerminalToolPolicy, Group: "agent", Kind: "enum", Options: []string{"on", "off"}, Label: "Terminal tool policy"},
	{Key: settings.KeyDelegationMode, Group: "agent", Kind: "enum", Options: []string{"fast", "bounded", "async"}, Label: "Delegation mode"},
	{Key: settings.KeySkillsAdmin, Group: "agent", Kind: "bool", Label: "Skills admin (install/delete)"},

	{Key: settings.KeyWebSearchProvider, Group: "search", Kind: "enum", Options: []string{"disabled", "searxng"}, Label: "Web search provider", Hint: "SearXNG is the supported web search provider"},
	{Key: settings.KeySearXNGBaseURL, Group: "search", Kind: "url", Label: "SearXNG base URL", Hint: "Compose uses http://searxng:8080; local debug commonly uses http://127.0.0.1:8088"},

	{Key: settings.KeyQdrantURL, Group: "storage", Kind: "url", Label: "Qdrant URL", Hint: "Compose uses http://qdrant:6333; local debug commonly uses http://127.0.0.1:6333"},
	{Key: settings.KeyQdrantCollection, Value: "aura_memory_v1", Group: "storage", Kind: "text", Label: "Qdrant collection"},
	{Key: settings.KeyQdrantAPIKey, Group: "storage", Kind: "text", IsSecret: true, Label: "Qdrant API key"},

	{Key: settings.KeyEmbeddingBaseURL, Group: "embeddings", Kind: "url", Label: "Embeddings base URL"},
	{Key: settings.KeyEmbeddingModel, Group: "embeddings", Kind: "text", Label: "Embeddings model"},
	{Key: settings.KeyEmbeddingAPIKey, Group: "embeddings", Kind: "text", IsSecret: true, Label: "Embeddings API key"},

	{Key: settings.KeyMistralAPIKey, Group: "ocr", Kind: "text", IsSecret: true, Label: "Mistral OCR API key"},
}

func floatPtr(v float64) *float64 { return &v }

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
			} else if meta.Value != "" {
				it.Source = "default"
			} else {
				it.Source = "default"
			}
			activeValue := activeSettingValue(deps.RuntimeConfig, meta.Key, it.Value)
			if it.Value == "" && activeValue != "" {
				it.Value = activeValue
			}
			it.RestartRequired = activeValue != "" && normalizeSettingValue(it.Value) != normalizeSettingValue(activeValue)
			it.ActiveValue = activeValue
			redactSecretSetting(&it)
			items = append(items, it)
		}
		writeJSON(w, deps.Logger, http.StatusOK, SettingsListResponse{Items: items})
	}
}

func redactSecretSetting(it *SettingItem) {
	if it == nil || !it.IsSecret {
		return
	}
	if it.Value != "" {
		it.Value = ""
	}
	if it.ActiveValue != "" {
		it.ActiveValue = "(configured)"
	}
}

func normalizeSettingValue(value string) string {
	return strings.TrimSpace(value)
}

func activeSettingValue(cfg *config.Config, key, fallback string) string {
	if cfg == nil {
		return fallback
	}
	switch key {
	case settings.KeyAllowlist:
		return strings.Join(cfg.Allowlist, ",")
	case settings.KeyTelegramToken:
		return cfg.TelegramToken
	case settings.KeyHTTPPort:
		return cfg.HTTPPort
	case settings.KeyTimezone:
		return cfg.Timezone
	case settings.KeyHeadless:
		return strconv.FormatBool(cfg.Headless)
	case settings.KeyEnvPath:
		return cfg.EnvPath
	case settings.KeyDBPath:
		return cfg.DBPath
	case settings.KeyLogLevel:
		return cfg.LogLevel
	case settings.KeyLogDir:
		return cfg.LogDir
	case settings.KeyWikiPath:
		return cfg.WikiPath
	case settings.KeySkillsPath:
		return cfg.SkillsPath
	case settings.KeySkillsInstallProjectDir:
		return cfg.SkillsInstallProjectDir
	case settings.KeyMCPServersPath:
		return cfg.MCPServersPath
	case settings.KeyPromptOverlayPath:
		return cfg.PromptOverlayPath
	case settings.KeyDashboardTokenTTLHours:
		return strconv.Itoa(cfg.DashboardTokenTTLHours)
	case settings.KeyMaxContextTokens:
		return strconv.Itoa(cfg.MaxContextTokens)
	case settings.KeyMaxHistoryMessages:
		return strconv.Itoa(cfg.MaxHistoryMessages)
	case settings.KeySoftBudget:
		return strconv.FormatFloat(cfg.SoftBudget, 'f', -1, 64)
	case settings.KeyHardBudget:
		return strconv.FormatFloat(cfg.HardBudget, 'f', -1, 64)
	case settings.KeyCostInputPerMTokens:
		return strconv.FormatFloat(cfg.CostInputPerMTokens, 'f', -1, 64)
	case settings.KeyCostOutputPerMTokens:
		return strconv.FormatFloat(cfg.CostOutputPerMTokens, 'f', -1, 64)
	case settings.KeyLLMAPIKey:
		return cfg.LLMAPIKey
	case settings.KeyLLMBaseURL:
		return cfg.LLMBaseURL
	case settings.KeyLLMModel:
		return cfg.LLMModel
	case settings.KeyLLMMaxRetries:
		return strconv.Itoa(cfg.LLMMaxRetries)
	case settings.KeyWebSearchProvider:
		return cfg.WebSearchProvider
	case settings.KeySearXNGBaseURL:
		return cfg.SearXNGBaseURL
	case settings.KeyGarageS3Endpoint:
		return cfg.GarageS3Endpoint
	case settings.KeyGarageS3Region:
		return cfg.GarageS3Region
	case settings.KeyGarageS3Bucket:
		return cfg.GarageS3Bucket
	case settings.KeyGarageS3AccessKey:
		return cfg.GarageS3AccessKey
	case settings.KeyGarageS3SecretKey:
		return cfg.GarageS3SecretKey
	case settings.KeyQdrantURL:
		return cfg.QdrantURL
	case settings.KeyQdrantCollection:
		return cfg.QdrantCollection
	case settings.KeyQdrantAPIKey:
		return cfg.QdrantAPIKey
	case settings.KeyMemorySearchTimeoutMS:
		return strconv.Itoa(cfg.MemorySearchTimeoutMS)
	case settings.KeySkillsCatalogURL:
		return cfg.SkillsCatalogURL
	case settings.KeySkillsAdmin:
		return strconv.FormatBool(cfg.SkillsAdmin)
	case settings.KeyAuraBotEnabled:
		return strconv.FormatBool(cfg.AuraBotEnabled)
	case settings.KeyAuraBotMaxActive:
		return strconv.Itoa(cfg.AuraBotMaxActive)
	case settings.KeyAuraBotMaxDepth:
		return strconv.Itoa(cfg.AuraBotMaxDepth)
	case settings.KeyAuraBotTimeoutSec:
		return strconv.Itoa(cfg.AuraBotTimeoutSec)
	case settings.KeyAuraBotMaxIterations:
		return strconv.Itoa(cfg.AuraBotMaxIterations)
	case settings.KeyEmbeddingAPIKey:
		return cfg.EmbeddingAPIKey
	case settings.KeyEmbeddingBaseURL:
		return cfg.EmbeddingBaseURL
	case settings.KeyEmbeddingModel:
		return cfg.EmbeddingModel
	case settings.KeyPromptVersion:
		return cfg.PromptVersion
	case settings.KeySkillRoutingMode:
		return cfg.SkillRoutingMode
	case settings.KeyAgentLoopMaxSteps:
		return strconv.Itoa(cfg.AgentLoopMaxSteps)
	case settings.KeyReasoningEffort:
		return cfg.ReasoningEffort
	case settings.KeyToolSearchTopK:
		return strconv.Itoa(cfg.ToolSearchTopK)
	case settings.KeyToolSearchBackend:
		return cfg.ToolSearchBackend
	case settings.KeyMaxToolResultChars:
		return strconv.Itoa(cfg.MaxToolResultChars)
	case settings.KeyMicrocompactKeepRecent:
		return strconv.Itoa(cfg.MicrocompactKeepRecent)
	case settings.KeyMicrocompactMinChars:
		return strconv.Itoa(cfg.MicrocompactMinChars)
	case settings.KeyTerminalToolPolicy:
		return cfg.TerminalToolPolicy
	case settings.KeyDelegationMode:
		return cfg.DelegationMode
	case settings.KeyTraceRetentionDays:
		return strconv.Itoa(cfg.TraceRetentionDays)
	case settings.KeyWorkspaceTools:
		return cfg.WorkspaceTools
	case settings.KeyWorkspaceRoot:
		return cfg.WorkspaceRoot
	case settings.KeyMistralAPIKey:
		return cfg.MistralAPIKey
	case settings.KeyMistralOCRModel:
		return cfg.MistralOCRModel
	case settings.KeyMistralOCRBaseURL:
		return cfg.MistralOCRBaseURL
	case settings.KeyMistralOCRTableFormat:
		return cfg.MistralOCRTableFormat
	case settings.KeyMistralOCRExtractHeader:
		return strconv.FormatBool(cfg.MistralOCRExtractHeader)
	case settings.KeyMistralOCRExtractFooter:
		return strconv.FormatBool(cfg.MistralOCRExtractFooter)
	case settings.KeyOCRMaxPages:
		return strconv.Itoa(cfg.OCRMaxPages)
	case settings.KeyOCRMaxFileMB:
		return strconv.Itoa(cfg.OCRMaxFileMB)
	case settings.KeyConvArchiveEnabled:
		return strconv.FormatBool(cfg.ConvArchiveEnabled)
	case settings.KeySandboxEnabled:
		return strconv.FormatBool(cfg.SandboxEnabled)
	case settings.KeySandboxTimeoutSec:
		return strconv.Itoa(cfg.SandboxTimeoutSec)
	default:
		return fallback
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
		var runtimeApplied bool
		var runtimeErr string
		if len(errs) == 0 && deps.ApplyRuntimeSettings != nil && touchesLiveRuntimeSetting(applied) {
			if err := deps.ApplyRuntimeSettings(ctx); err != nil {
				runtimeErr = err.Error()
			} else {
				runtimeApplied = true
			}
		}
		writeJSON(w, deps.Logger, http.StatusOK, SettingsUpdateResponse{
			OK:             len(errs) == 0,
			Applied:        applied,
			Errors:         errs,
			RuntimeApplied: runtimeApplied,
			RuntimeError:   runtimeErr,
		})
	}
}

func touchesLiveRuntimeSetting(keys []string) bool {
	for _, key := range keys {
		switch key {
		case settings.KeyAuraBotMaxActive, settings.KeyAuraBotMaxDepth, settings.KeyAuraBotTimeoutSec, settings.KeyAuraBotMaxIterations,
			settings.KeySoftBudget, settings.KeyHardBudget, settings.KeyCostInputPerMTokens, settings.KeyCostOutputPerMTokens,
			settings.KeySkillRoutingMode, settings.KeyAgentLoopMaxSteps, settings.KeyReasoningEffort,
			settings.KeyToolSearchTopK,
			settings.KeyMaxToolResultChars, settings.KeyMicrocompactKeepRecent, settings.KeyMicrocompactMinChars,
			settings.KeyTerminalToolPolicy,
			settings.KeyDelegationMode, settings.KeyTraceRetentionDays:
			return true
		}
	}
	return false
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

func handleRestart(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Restart == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "restart unavailable")
			return
		}
		if err := deps.Restart(r.Context()); err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "restart failed: "+err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"ok": true, "restarting": true})
	}
}
