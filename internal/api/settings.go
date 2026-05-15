package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/secrets"
)

// secretRouteForCatalogKey maps an overridable config key (uppercase, formerly
// written to the settings table) to its canonical secrets-table key (lowercase)
// when the boot loader expects the value from the secrets table. The keys that
// applySecretsToConfig (cmd/aura/secrets_boot.go) overrides are listed here.
// MistralAPIKey is IsSecret=true for UI redaction only and stays single-table
// in settings.
var secretRouteForCatalogKey = map[string]string{
	config.KeyTelegramToken:     secrets.KeyTelegramToken,
	config.KeyLLMAPIKey:         secrets.KeyLLMAPIKey,
	config.KeyEmbeddingAPIKey:   secrets.KeyEmbeddingAPIKey,
	config.KeyGarageS3AccessKey: secrets.KeyGarageS3AccessKey,
	config.KeyGarageS3SecretKey: secrets.KeyGarageS3SecretKey,
	config.KeyQdrantAPIKey:      secrets.KeyQdrantAPIKey,
}

// routeToSecretsStore reports whether a dashboard catalog key should read/write
// through the secrets store rather than the settings store.
func routeToSecretsStore(catalogKey string) (secretKey string, ok bool) {
	v, ok := secretRouteForCatalogKey[catalogKey]
	return v, ok
}

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
// config.item.<KEY>.{label,hint} entries in the i18n locale files for
// localized strings; the Label/Hint fields are fallbacks for clients that
// don't ship locale bundles.
var settingsCatalog = []SettingItem{
	{Key: config.KeyTimezone, Group: "runtime", Kind: "text", Label: "Scheduler timezone", Hint: "IANA name like Europe/Rome"},

	{Key: config.KeyLLMBaseURL, Group: "provider", Kind: "url", Label: "LLM base URL", Hint: "OpenAI-compatible endpoint (e.g. https://api.openai.com/v1)"},
	{Key: config.KeyLLMModel, Group: "provider", Kind: "text", Label: "LLM model", Hint: "Model name as the provider expects it"},
	{Key: config.KeyLLMAPIKey, Group: "provider", Kind: "text", IsSecret: true, Label: "LLM API key"},
	{Key: config.KeyLLMMaxRetries, Group: "provider", Kind: "int", Min: floatPtr(0), Max: floatPtr(20), Label: "LLM max retries"},

	{Key: config.KeyMaxContextTokens, Group: "budget", Kind: "int", Min: floatPtr(1024), Label: "Max context tokens"},
	{Key: config.KeyMaxHistoryMessages, Group: "budget", Kind: "int", Min: floatPtr(1), Label: "Max in-flight messages"},
	{Key: config.KeySoftBudget, Group: "budget", Kind: "float", Min: floatPtr(0), Label: "Soft budget (USD)"},
	{Key: config.KeyHardBudget, Group: "budget", Kind: "float", Min: floatPtr(0), Label: "Hard budget (USD)"},
	{Key: config.KeyCostInputPerMTokens, Group: "budget", Kind: "float", Min: floatPtr(0), Label: "Input price (USD / 1M tokens)"},
	{Key: config.KeyCostOutputPerMTokens, Group: "budget", Kind: "float", Min: floatPtr(0), Label: "Output price (USD / 1M tokens)"},

	{Key: config.KeyPromptVersion, Group: "agent", Kind: "text", Label: "Prompt version"},
	{Key: config.KeySkillRoutingMode, Group: "agent", Kind: "enum", Options: []string{"manifest", "manifest_llm_review"}, Label: "Skill routing mode"},
	{Key: config.KeyAgentLoopMaxSteps, Group: "agent", Kind: "int", Min: floatPtr(1), Max: floatPtr(10000), Label: "Agent loop max steps"},
	{Key: config.KeyReasoningEffort, Group: "agent", Kind: "enum", Options: []string{"", "enabled", "minimal", "low", "medium", "high", "xhigh"}, Label: "Reasoning effort", Hint: "Provider-side chain-of-thought. Empty disables. 'enabled' turns reasoning on with provider default depth (use this for DeepSeek V4 Flash). 'low'..'xhigh' set explicit depth on models that support it (OpenAI o-series, gpt-5*). Unknown providers ignore the field."},
	{Key: config.KeyToolSearchTopK, Group: "agent", Kind: "int", Min: floatPtr(1), Max: floatPtr(50), Label: "Tool search top-K", Hint: "How many retrieved tools to expose per turn on top of the always-on core. Raise on large-context models so web_fetch/web_search aren't crowded out"},
	{Key: config.KeyToolSearchBackend, Group: "agent", Kind: "enum", Options: []string{"hybrid", "vector", "fts"}, Label: "Tool search backend", Hint: "Restart required: rebuilds the tool vector index. hybrid mixes BM25 + embeddings; vector is pure semantic; fts is keyword-only"},
	{Key: config.KeyMaxToolResultChars, Group: "agent", Kind: "int", Min: floatPtr(1000), Max: floatPtr(500000), Label: "Max tool result chars", Hint: "Cap per tool message before the LLM call; raise on large-context models"},
	{Key: config.KeyMicrocompactKeepRecent, Group: "agent", Kind: "int", Min: floatPtr(1), Max: floatPtr(500), Label: "Microcompact keep recent", Hint: "Tool results older than the N most recent get collapsed to a one-line stub"},
	{Key: config.KeyMicrocompactMinChars, Group: "agent", Kind: "int", Min: floatPtr(100), Max: floatPtr(100000), Label: "Microcompact min chars", Hint: "Tool results smaller than this are never compacted"},
	{Key: config.KeyTerminalToolPolicy, Group: "agent", Kind: "enum", Options: []string{"on", "off"}, Label: "Terminal tool policy"},
	{Key: config.KeyDelegationMode, Group: "agent", Kind: "enum", Options: []string{"fast", "bounded", "async"}, Label: "Delegation mode"},
	{Key: config.KeySkillsAdmin, Group: "agent", Kind: "bool", Label: "Skills admin (install/delete)"},

	{Key: config.KeyWebSearchProvider, Group: "search", Kind: "enum", Options: []string{"disabled", "searxng"}, Label: "Web search provider", Hint: "SearXNG is the supported web search provider"},
	{Key: config.KeySearXNGBaseURL, Group: "search", Kind: "url", Label: "SearXNG base URL", Hint: "Compose uses http://searxng:8080; local debug commonly uses http://127.0.0.1:8088"},

	{Key: config.KeyQdrantURL, Group: "storage", Kind: "url", Label: "Qdrant URL", Hint: "Compose uses http://qdrant:6333; local debug commonly uses http://127.0.0.1:6333"},
	{Key: config.KeyQdrantCollection, Value: "aura_memory_v1", Group: "storage", Kind: "text", Label: "Qdrant collection"},
	{Key: config.KeyQdrantAPIKey, Group: "storage", Kind: "text", IsSecret: true, Label: "Qdrant API key"},

	{Key: config.KeyEmbeddingBaseURL, Group: "embeddings", Kind: "url", Label: "Embeddings base URL"},
	{Key: config.KeyEmbeddingModel, Group: "embeddings", Kind: "text", Label: "Embeddings model"},
	{Key: config.KeyEmbeddingAPIKey, Group: "embeddings", Kind: "text", IsSecret: true, Label: "Embeddings API key"},
	{Key: config.KeyEmbeddingOutputDim, Group: "embeddings", Kind: "int", Min: floatPtr(0), Max: floatPtr(768), Label: "Embeddings output dim (MRL)", Hint: "0 = native dim. For embeddinggemma-300m, 256 is Aura's locked production target (MRL truncation client-side: slice + L2 renorm). Changing this requires reindexing all Qdrant collections."},

	{Key: config.KeyMistralAPIKey, Group: "ocr", Kind: "text", IsSecret: true, Label: "Mistral OCR API key"},
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
			// DB row wins. Secret-shaped keys route through the secrets
			// store (matches the boot loader's applySecretsToConfig
			// precedence — see docs/settings-audit-2026-05-15.md).
			// Else fall back to the env value the bot loaded at startup
			// so the form reflects effective state. Source flag tells
			// the UI which it is.
			if secretKey, ok := routeToSecretsStore(meta.Key); ok && deps.SecretsStore != nil {
				if v, err := deps.SecretsStore.Get(ctx, secretKey); err == nil && v != "" {
					it.Value = v
					it.Source = "db"
				} else if v, err := deps.Settings.Get(ctx, meta.Key); err == nil && v != "" {
					it.Value = v
					it.Source = "db"
				} else if envVal := os.Getenv(meta.Key); envVal != "" {
					it.Value = envVal
					it.Source = "env"
				} else {
					it.Source = "default"
				}
			} else if v, err := deps.Settings.Get(ctx, meta.Key); err == nil && v != "" {
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
	case config.KeyAllowlist:
		return strings.Join(cfg.Allowlist, ",")
	case config.KeyTelegramToken:
		return cfg.TelegramToken
	case config.KeyHTTPPort:
		return cfg.HTTPPort
	case config.KeyTimezone:
		return cfg.Timezone
	case config.KeyHeadless:
		return strconv.FormatBool(cfg.Headless)
	case config.KeyEnvPath:
		return cfg.EnvPath
	case config.KeyDBPath:
		return cfg.DBPath
	case config.KeyLogLevel:
		return cfg.LogLevel
	case config.KeyLogDir:
		return cfg.LogDir
	case config.KeyWikiPath:
		return cfg.WikiPath
	case config.KeySkillsPath:
		return cfg.SkillsPath
	case config.KeySkillsInstallProjectDir:
		return cfg.SkillsInstallProjectDir
	case config.KeyMCPServersPath:
		return cfg.MCPServersPath
	case config.KeyPromptOverlayPath:
		return cfg.PromptOverlayPath
	case config.KeyDashboardTokenTTLHours:
		return strconv.Itoa(cfg.DashboardTokenTTLHours)
	case config.KeyMaxContextTokens:
		return strconv.Itoa(cfg.MaxContextTokens)
	case config.KeyMaxHistoryMessages:
		return strconv.Itoa(cfg.MaxHistoryMessages)
	case config.KeySoftBudget:
		return strconv.FormatFloat(cfg.SoftBudget, 'f', -1, 64)
	case config.KeyHardBudget:
		return strconv.FormatFloat(cfg.HardBudget, 'f', -1, 64)
	case config.KeyCostInputPerMTokens:
		return strconv.FormatFloat(cfg.CostInputPerMTokens, 'f', -1, 64)
	case config.KeyCostOutputPerMTokens:
		return strconv.FormatFloat(cfg.CostOutputPerMTokens, 'f', -1, 64)
	case config.KeyLLMAPIKey:
		return cfg.LLMAPIKey
	case config.KeyLLMBaseURL:
		return cfg.LLMBaseURL
	case config.KeyLLMModel:
		return cfg.LLMModel
	case config.KeyLLMMaxRetries:
		return strconv.Itoa(cfg.LLMMaxRetries)
	case config.KeyWebSearchProvider:
		return cfg.WebSearchProvider
	case config.KeySearXNGBaseURL:
		return cfg.SearXNGBaseURL
	case config.KeyGarageS3Endpoint:
		return cfg.GarageS3Endpoint
	case config.KeyGarageS3Region:
		return cfg.GarageS3Region
	case config.KeyGarageS3Bucket:
		return cfg.GarageS3Bucket
	case config.KeyGarageS3AccessKey:
		return cfg.GarageS3AccessKey
	case config.KeyGarageS3SecretKey:
		return cfg.GarageS3SecretKey
	case config.KeyQdrantURL:
		return cfg.QdrantURL
	case config.KeyQdrantCollection:
		return cfg.QdrantCollection
	case config.KeyQdrantAPIKey:
		return cfg.QdrantAPIKey
	case config.KeyMemorySearchTimeoutMS:
		return strconv.Itoa(cfg.MemorySearchTimeoutMS)
	case config.KeySkillsCatalogURL:
		return cfg.SkillsCatalogURL
	case config.KeySkillsAdmin:
		return strconv.FormatBool(cfg.SkillsAdmin)
	case config.KeyAuraBotEnabled:
		return strconv.FormatBool(cfg.AuraBotEnabled)
	case config.KeyAuraBotMaxActive:
		return strconv.Itoa(cfg.AuraBotMaxActive)
	case config.KeyAuraBotMaxDepth:
		return strconv.Itoa(cfg.AuraBotMaxDepth)
	case config.KeyAuraBotTimeoutSec:
		return strconv.Itoa(cfg.AuraBotTimeoutSec)
	case config.KeyAuraBotMaxIterations:
		return strconv.Itoa(cfg.AuraBotMaxIterations)
	case config.KeyEmbeddingAPIKey:
		return cfg.EmbeddingAPIKey
	case config.KeyEmbeddingBaseURL:
		return cfg.EmbeddingBaseURL
	case config.KeyEmbeddingModel:
		return cfg.EmbeddingModel
	case config.KeyEmbeddingOutputDim:
		return strconv.Itoa(cfg.EmbeddingOutputDim)
	case config.KeyPromptVersion:
		return cfg.PromptVersion
	case config.KeySkillRoutingMode:
		return cfg.SkillRoutingMode
	case config.KeyAgentLoopMaxSteps:
		return strconv.Itoa(cfg.AgentLoopMaxSteps)
	case config.KeyReasoningEffort:
		return cfg.ReasoningEffort
	case config.KeyToolSearchTopK:
		return strconv.Itoa(cfg.ToolSearchTopK)
	case config.KeyToolSearchBackend:
		return cfg.ToolSearchBackend
	case config.KeyMaxToolResultChars:
		return strconv.Itoa(cfg.MaxToolResultChars)
	case config.KeyMicrocompactKeepRecent:
		return strconv.Itoa(cfg.MicrocompactKeepRecent)
	case config.KeyMicrocompactMinChars:
		return strconv.Itoa(cfg.MicrocompactMinChars)
	case config.KeyTerminalToolPolicy:
		return cfg.TerminalToolPolicy
	case config.KeyDelegationMode:
		return cfg.DelegationMode
	case config.KeyTraceRetentionDays:
		return strconv.Itoa(cfg.TraceRetentionDays)
	case config.KeyWorkspaceTools:
		return cfg.WorkspaceTools
	case config.KeyWorkspaceRoot:
		return cfg.WorkspaceRoot
	case config.KeyMistralAPIKey:
		return cfg.MistralAPIKey
	case config.KeyMistralOCRModel:
		return cfg.MistralOCRModel
	case config.KeyMistralOCRBaseURL:
		return cfg.MistralOCRBaseURL
	case config.KeyMistralOCRTableFormat:
		return cfg.MistralOCRTableFormat
	case config.KeyMistralOCRExtractHeader:
		return strconv.FormatBool(cfg.MistralOCRExtractHeader)
	case config.KeyMistralOCRExtractFooter:
		return strconv.FormatBool(cfg.MistralOCRExtractFooter)
	case config.KeyOCRMaxPages:
		return strconv.Itoa(cfg.OCRMaxPages)
	case config.KeyOCRMaxFileMB:
		return strconv.Itoa(cfg.OCRMaxFileMB)
	case config.KeyConvArchiveEnabled:
		return strconv.FormatBool(cfg.ConvArchiveEnabled)
	case config.KeySandboxEnabled:
		return strconv.FormatBool(cfg.SandboxEnabled)
	case config.KeySandboxTimeoutSec:
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
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if len(req.Updates) == 0 {
			writeJSON(w, deps.Logger, http.StatusOK, SettingsUpdateResponse{OK: true})
			return
		}
		// Validate every key first so we don't half-apply.
		for k := range req.Updates {
			if !config.IsOverridable(k) {
				writeError(w, deps.Logger, http.StatusBadRequest, "key not overridable: "+k)
				return
			}
		}
		ctx := r.Context()
		applied := make([]string, 0, len(req.Updates))
		errs := []string{}
		for k, v := range req.Updates {
			v = strings.TrimSpace(v)
			// Secret-shaped keys route through the secrets store so the
			// dashboard rotates the same row applySecretsToConfig reads at
			// boot (Phase-H closure). Also delete any legacy settings-table
			// row so the precedence collision documented in
			// docs/settings-audit-2026-05-15.md cannot recur.
			if secretKey, isSecret := routeToSecretsStore(k); isSecret {
				if deps.SecretsStore == nil {
					errs = append(errs, k+": secrets store unavailable")
					continue
				}
				if v == "" {
					if err := deps.SecretsStore.Delete(ctx, secretKey); err != nil && !errors.Is(err, secrets.ErrSecretNotFound) {
						errs = append(errs, k+": "+err.Error())
						continue
					}
				} else {
					if err := deps.SecretsStore.Set(ctx, secretKey, v); err != nil {
						errs = append(errs, k+": "+err.Error())
						continue
					}
				}
				// Clean legacy settings row so the old write target stops
				// shadowing future reads (best-effort: ignore "not found").
				_ = deps.Settings.Delete(ctx, k)
				applied = append(applied, k)
				continue
			}
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
		case config.KeyAuraBotMaxActive, config.KeyAuraBotMaxDepth, config.KeyAuraBotTimeoutSec, config.KeyAuraBotMaxIterations,
			config.KeySoftBudget, config.KeyHardBudget, config.KeyCostInputPerMTokens, config.KeyCostOutputPerMTokens,
			config.KeySkillRoutingMode, config.KeyAgentLoopMaxSteps, config.KeyReasoningEffort,
			config.KeyToolSearchTopK,
			config.KeyMaxToolResultChars, config.KeyMicrocompactKeepRecent, config.KeyMicrocompactMinChars,
			config.KeyTerminalToolPolicy,
			config.KeyDelegationMode, config.KeyTraceRetentionDays:
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
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		probePath := req.ProbePath
		if probePath == "" {
			probePath = "/models"
		}
		// Re-use the wizard's probe so behavior matches first-run setup.
		// 6s timeout is enforced inside ProbeProvider.
		result := SetupProbeProvider(context.Background(), req.BaseURL, req.APIKey, probePath)
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
