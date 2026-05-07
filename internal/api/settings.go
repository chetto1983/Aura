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

// settingsCatalog is the master list of editable keys with their UI
// metadata. Only fields the operator should reasonably change live are
// here; LLM_MAX_RETRIES and other fine-tuning knobs stay overridable
// programmatically but aren't surfaced in the dashboard form.
var settingsCatalog = []SettingItem{
	{Key: settings.KeyTelegramToken, Group: "runtime", Kind: "text", IsSecret: true, Label: "Telegram bot token", Hint: "Saved as an override; restart Aura after changing the bot token"},
	{Key: settings.KeyHTTPPort, Group: "runtime", Kind: "text", Label: "Dashboard bind address", Hint: "Restart Aura after changing the bind address"},
	{Key: settings.KeyTimezone, Group: "runtime", Kind: "text", Label: "Scheduler timezone", Hint: "IANA name like Europe/Rome; restart Aura after changing"},
	{Key: settings.KeyHeadless, Group: "runtime", Kind: "bool", Label: "Headless/container mode", Hint: "Takes effect on next process start"},
	{Key: settings.KeyEnvPath, Group: "runtime", Kind: "text", Label: "Env file path", Hint: "Early boot setting; use carefully and restart after changing"},
	{Key: settings.KeyDBPath, Group: "runtime", Kind: "text", Label: "SQLite database path", Hint: "Early boot setting; use carefully and restart after changing"},
	{Key: settings.KeyLogLevel, Group: "runtime", Kind: "enum", Options: []string{"debug", "info", "warn", "error"}, Label: "Log level"},
	{Key: settings.KeyLogDir, Group: "runtime", Kind: "text", Label: "Log directory"},
	{Key: settings.KeyWikiPath, Group: "runtime", Kind: "text", Label: "Wiki path", Hint: "Restart Aura after moving the wiki root"},
	{Key: settings.KeySkillsPath, Group: "runtime", Kind: "text", Label: "Skills path", Hint: "Restart Aura after moving skill roots"},
	{Key: settings.KeySkillsInstallProjectDir, Group: "runtime", Kind: "text", Label: "Skills install project directory", Hint: "Container default is /skills so catalog installs persist in the mounted volume"},
	{Key: settings.KeyMCPServersPath, Group: "runtime", Kind: "text", Label: "MCP servers config path", Hint: "Restart Aura after changing MCP config path"},
	{Key: settings.KeyPromptOverlayPath, Group: "runtime", Kind: "text", Label: "Prompt overlay path"},
	{Key: settings.KeyDashboardTokenTTLHours, Group: "runtime", Kind: "int", Label: "Dashboard token TTL (hours)"},

	{Key: settings.KeyLLMBaseURL, Group: "provider", Kind: "url", Label: "LLM base URL", Hint: "OpenAI-compatible endpoint (e.g. https://api.openai.com/v1)"},
	{Key: settings.KeyLLMModel, Group: "provider", Kind: "text", Label: "LLM model", Hint: "Model name as the provider expects it"},
	{Key: settings.KeyLLMAPIKey, Group: "provider", Kind: "text", IsSecret: true, Label: "LLM API key"},
	{Key: settings.KeyLLMMaxRetries, Group: "provider", Kind: "int", Label: "LLM max retries"},
	{Key: settings.KeyOllamaBaseURL, Group: "provider", Kind: "url", Label: "Ollama base URL (failover)", Hint: "Bare host, e.g. http://localhost:11434"},
	{Key: settings.KeyOllamaModel, Group: "provider", Kind: "text", Label: "Ollama model"},
	{Key: settings.KeyOllamaAPIKey, Group: "provider", Kind: "text", IsSecret: true, Label: "Ollama API key (rarely needed)"},
	{Key: settings.KeyOllamaWebBaseURL, Group: "provider", Kind: "url", Label: "Ollama web API base URL"},
	{Key: settings.KeyWebSearchProvider, Group: "search", Kind: "enum", Options: []string{"disabled", "searxng", "ollama"}, Label: "Web search provider", Hint: "SearXNG is the recommended container provider; Ollama needs OLLAMA_API_KEY"},
	{Key: settings.KeySearXNGBaseURL, Group: "search", Kind: "url", Label: "SearXNG base URL", Hint: "Compose uses http://searxng:8080; local debug commonly uses http://127.0.0.1:8088"},

	{Key: settings.KeyGarageS3Endpoint, Group: "storage", Kind: "url", Label: "Garage S3 endpoint", Hint: "Compose uses http://garage:3900"},
	{Key: settings.KeyGarageS3Region, Group: "storage", Kind: "text", Label: "Garage S3 region"},
	{Key: settings.KeyGarageS3Bucket, Group: "storage", Kind: "text", Label: "Garage S3 bucket"},
	{Key: settings.KeyGarageS3AccessKey, Group: "storage", Kind: "text", IsSecret: true, Label: "Garage S3 access key"},
	{Key: settings.KeyGarageS3SecretKey, Group: "storage", Kind: "text", IsSecret: true, Label: "Garage S3 secret key"},
	{Key: settings.KeyQdrantURL, Group: "storage", Kind: "url", Label: "Qdrant URL", Hint: "Compose uses http://qdrant:6333; local debug commonly uses http://127.0.0.1:6333"},
	{Key: settings.KeyQdrantCollection, Value: "aura_memory_v1", Group: "storage", Kind: "text", Label: "Qdrant collection"},
	{Key: settings.KeyQdrantAPIKey, Group: "storage", Kind: "text", IsSecret: true, Label: "Qdrant API key"},
	{Key: settings.KeySearchBackend, Value: "chromem", Group: "storage", Kind: "enum", Options: []string{"chromem", "qdrant"}, Label: "Search backend", Hint: "Keep chromem for local default; choose qdrant to query the Qdrant sidecar first with local fallback"},
	{Key: settings.KeySpeculativeSearchTimeoutMS, Value: "1500", Group: "storage", Kind: "int", Label: "Speculative search timeout (ms)", Hint: "Caps pre-LLM memory injection so a slow sidecar cannot stall Telegram turns"},
	{Key: settings.KeyMemorySearchTimeoutMS, Value: "5000", Group: "storage", Kind: "int", Label: "Memory search tool timeout (ms)", Hint: "Caps search_memory calls inside agent tool loops"},

	{Key: settings.KeyEmbeddingBaseURL, Group: "embeddings", Kind: "url", Label: "Embeddings base URL"},
	{Key: settings.KeyEmbeddingModel, Group: "embeddings", Kind: "text", Label: "Embeddings model"},
	{Key: settings.KeyEmbeddingAPIKey, Group: "embeddings", Kind: "text", IsSecret: true, Label: "Embeddings API key"},

	{Key: settings.KeyMistralAPIKey, Group: "ocr", Kind: "text", IsSecret: true, Label: "Mistral OCR API key"},
	{Key: settings.KeyMistralOCRModel, Group: "ocr", Kind: "text", Label: "OCR model"},
	{Key: settings.KeyMistralOCRBaseURL, Group: "ocr", Kind: "url", Label: "OCR base URL"},
	{Key: settings.KeyMistralOCRTableFormat, Group: "ocr", Kind: "enum", Options: []string{"markdown", "html"}, Label: "OCR table format"},
	{Key: settings.KeyMistralOCRIncludeImages, Group: "ocr", Kind: "bool", Label: "OCR include images"},
	{Key: settings.KeyMistralOCRExtractHeader, Group: "ocr", Kind: "bool", Label: "OCR extract headers"},
	{Key: settings.KeyMistralOCRExtractFooter, Group: "ocr", Kind: "bool", Label: "OCR extract footers"},
	{Key: settings.KeyOCREnabled, Group: "ocr", Kind: "bool", Label: "OCR enabled"},
	{Key: settings.KeyOCRMaxPages, Group: "ocr", Kind: "int", Label: "OCR max pages", Hint: "Aura refuses PDFs longer than this"},
	{Key: settings.KeyOCRMaxFileMB, Group: "ocr", Kind: "int", Label: "OCR max file size (MB)"},

	{Key: settings.KeySoftBudget, Group: "budget", Kind: "float", Label: "Soft budget (USD)", Hint: "Telegram warning fires once this is crossed"},
	{Key: settings.KeyHardBudget, Group: "budget", Kind: "float", Label: "Hard budget (USD)", Hint: "Bot refuses LLM calls past this"},
	{Key: settings.KeyCostInputPerMTokens, Group: "budget", Kind: "float", Label: "Input price (USD / 1M tokens)", Hint: "Set from the selected provider/model pricing"},
	{Key: settings.KeyCostOutputPerMTokens, Group: "budget", Kind: "float", Label: "Output price (USD / 1M tokens)", Hint: "Set from the selected provider/model pricing"},
	{Key: settings.KeyMaxContextTokens, Group: "budget", Kind: "int", Label: "Max context tokens", Hint: "Summarization fires at 80% of this"},
	{Key: settings.KeyMaxHistoryMessages, Group: "budget", Kind: "int", Label: "Max in-flight messages", Hint: "Hard cap; oldest evicted first"},
	{Key: settings.KeyMaxToolIterations, Group: "budget", Kind: "int", Label: "Max tool iterations / turn"},

	{Key: settings.KeySummarizerEnabled, Group: "summarizer", Kind: "bool", Label: "Automatic memory capture enabled"},
	{Key: settings.KeySummarizerMode, Value: config.DefaultSummarizerMode, Group: "summarizer", Kind: "enum", Options: []string{"off", "review", "auto"}, Label: "Memory capture mode", Hint: "review = queue for dashboard approval; auto = direct wiki write"},
	{Key: settings.KeySummarizerTurnInterval, Value: strconv.Itoa(config.DefaultSummarizerTurnInterval), Group: "summarizer", Kind: "int", Label: "Run every N archived turns", Hint: "Default 2 captures after a normal user/assistant turn"},
	{Key: settings.KeySummarizerMinSalience, Group: "summarizer", Kind: "float", Label: "Min salience"},
	{Key: settings.KeySummarizerLookbackTurns, Group: "summarizer", Kind: "int", Label: "Lookback turns"},
	{Key: settings.KeySummarizerCooldownSeconds, Value: strconv.Itoa(config.DefaultSummarizerCooldownSeconds), Group: "summarizer", Kind: "int", Label: "Cooldown (s)"},

	{Key: settings.KeyAuraBotEnabled, Value: "false", Group: "aurabot", Kind: "bool", Label: "AuraBot swarm enabled", Hint: "Enables bounded background agents and swarm tools. Restart Aura after changing."},
	{Key: settings.KeyAuraBotMaxActive, Value: "4", Group: "aurabot", Kind: "int", Label: "Max active workers", Hint: "Parallel workers per swarm run. Applies to new runs when AuraBot is already enabled."},
	{Key: settings.KeyAuraBotMaxDepth, Value: "1", Group: "aurabot", Kind: "int", Label: "Max delegation depth", Hint: "Current safe default is 1: manager plus direct workers. Applies to new runs."},
	{Key: settings.KeyAuraBotTimeoutSec, Value: "300", Group: "aurabot", Kind: "int", Label: "Worker timeout (seconds)", Hint: "Wall-clock budget for valuable research. Applies to new workers when AuraBot is already enabled."},
	{Key: settings.KeyAuraBotMaxIterations, Value: "5", Group: "aurabot", Kind: "int", Label: "Max model/tool iterations", Hint: "Caps each worker loop so longer timeouts do not become endless tool loops. Applies to new workers."},

	{Key: settings.KeySandboxEnabled, Group: "sandbox", Kind: "bool", Label: "Sandbox enabled", Hint: "Restart Aura after enabling or disabling the code execution tool"},
	{Key: settings.KeySandboxRuntimeMode, Group: "sandbox", Kind: "enum", Options: []string{"auto", "container", "local"}, Label: "Sandbox runtime mode", Hint: "Container uses the Pyodide sidecar; local uses the bundled desktop runner"},
	{Key: settings.KeySandboxRuntimeURL, Group: "sandbox", Kind: "url", Label: "Sandbox runtime URL", Hint: "Compose default is http://pyodide:8787"},
	{Key: settings.KeySandboxRuntimeDir, Group: "sandbox", Kind: "text", Label: "Sandbox runtime directory", Hint: "Container default is /app/runtime/pyodide"},
	{Key: settings.KeySandboxTimeoutSec, Group: "sandbox", Kind: "int", Label: "Sandbox timeout (seconds)"},
	{Key: settings.KeySandboxAutoImproveMode, Group: "sandbox", Kind: "enum", Options: []string{"off", "dry_run", "auto"}, Label: "Sandbox auto-improve mode"},

	{Key: settings.KeyConvArchiveEnabled, Group: "other", Kind: "bool", Label: "Conversation archive enabled"},
	{Key: settings.KeyOTelEnabled, Group: "other", Kind: "bool", Label: "OpenTelemetry tracing enabled"},
	{Key: settings.KeyPromptVersion, Group: "agent", Kind: "text", Label: "Prompt version", Hint: "Default is aura-agent-v1; restart Aura after changing"},
	{Key: settings.KeyToolProfileMode, Group: "agent", Kind: "enum", Options: []string{"auto", "default", "memory", "swarm_research", "sandbox_compute", "document", "admin_review"}, Label: "Tool profile mode", Hint: "auto selects a focused profile per turn"},
	{Key: settings.KeyOrchestrationLogLevel, Group: "agent", Kind: "enum", Options: []string{"summary", "debug"}, Label: "Orchestration log level"},
	{Key: settings.KeySkillPreflight, Value: "required", Group: "agent", Kind: "enum", Options: []string{"required", "advisory", "off"}, Label: "Skill preflight", Hint: "required blocks capability tools until a relevant skill is read"},
	{Key: settings.KeySkillsAdmin, Group: "other", Kind: "bool", Label: "Skills admin (catalog install/delete)"},
	{Key: settings.KeySkillsCatalogURL, Group: "other", Kind: "url", Label: "Skills catalog URL"},
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
	case settings.KeyOllamaBaseURL:
		return cfg.OllamaBaseURL
	case settings.KeyOllamaModel:
		return cfg.OllamaModel
	case settings.KeyOllamaAPIKey:
		return cfg.OllamaAPIKey
	case settings.KeyOllamaWebBaseURL:
		return cfg.OllamaWebBaseURL
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
	case settings.KeySearchBackend:
		return cfg.SearchBackend
	case settings.KeySpeculativeSearchTimeoutMS:
		return strconv.Itoa(cfg.SpeculativeSearchTimeoutMS)
	case settings.KeyMemorySearchTimeoutMS:
		return strconv.Itoa(cfg.MemorySearchTimeoutMS)
	case settings.KeyMaxToolIterations:
		return strconv.Itoa(cfg.MaxToolIterations)
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
	case settings.KeyOTelEnabled:
		return strconv.FormatBool(cfg.OTelEnabled)
	case settings.KeyPromptVersion:
		return cfg.PromptVersion
	case settings.KeyToolProfileMode:
		return cfg.ToolProfileMode
	case settings.KeyOrchestrationLogLevel:
		return cfg.OrchestrationLogLevel
	case settings.KeySkillPreflight:
		return cfg.SkillPreflight
	case settings.KeyMistralAPIKey:
		return cfg.MistralAPIKey
	case settings.KeyMistralOCRModel:
		return cfg.MistralOCRModel
	case settings.KeyMistralOCRBaseURL:
		return cfg.MistralOCRBaseURL
	case settings.KeyMistralOCRTableFormat:
		return cfg.MistralOCRTableFormat
	case settings.KeyMistralOCRIncludeImages:
		return strconv.FormatBool(cfg.MistralOCRIncludeImages)
	case settings.KeyMistralOCRExtractHeader:
		return strconv.FormatBool(cfg.MistralOCRExtractHeader)
	case settings.KeyMistralOCRExtractFooter:
		return strconv.FormatBool(cfg.MistralOCRExtractFooter)
	case settings.KeyOCREnabled:
		return strconv.FormatBool(cfg.OCREnabled)
	case settings.KeyOCRMaxPages:
		return strconv.Itoa(cfg.OCRMaxPages)
	case settings.KeyOCRMaxFileMB:
		return strconv.Itoa(cfg.OCRMaxFileMB)
	case settings.KeyConvArchiveEnabled:
		return strconv.FormatBool(cfg.ConvArchiveEnabled)
	case settings.KeySummarizerEnabled:
		return strconv.FormatBool(cfg.SummarizerEnabled)
	case settings.KeySummarizerMode:
		return cfg.SummarizerMode
	case settings.KeySummarizerTurnInterval:
		return strconv.Itoa(cfg.SummarizerTurnInterval)
	case settings.KeySummarizerMinSalience:
		return strconv.FormatFloat(cfg.SummarizerMinSalience, 'f', -1, 64)
	case settings.KeySummarizerLookbackTurns:
		return strconv.Itoa(cfg.SummarizerLookbackTurns)
	case settings.KeySummarizerCooldownSeconds:
		return strconv.Itoa(cfg.SummarizerCooldownSeconds)
	case settings.KeySandboxEnabled:
		return strconv.FormatBool(cfg.SandboxEnabled)
	case settings.KeySandboxRuntimeMode:
		return cfg.SandboxRuntimeMode
	case settings.KeySandboxRuntimeURL:
		return cfg.SandboxRuntimeURL
	case settings.KeySandboxRuntimeDir:
		return cfg.SandboxRuntimeDir
	case settings.KeySandboxTimeoutSec:
		return strconv.Itoa(cfg.SandboxTimeoutSec)
	case settings.KeySandboxAutoImproveMode:
		return cfg.SandboxAutoImproveMode
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
			settings.KeySoftBudget, settings.KeyHardBudget, settings.KeyCostInputPerMTokens, settings.KeyCostOutputPerMTokens:
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
