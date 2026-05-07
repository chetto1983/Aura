package settings

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/config"
)

func TestApplyToConfigEmptyStoreIsNoOp(t *testing.T) {
	s := openTestStore(t)
	cfg := &config.Config{
		LLMAPIKey:            "env-key",
		LLMBaseURL:           "https://api.example.com",
		LLMModel:             "gpt-x",
		LLMMaxRetries:        5,
		MaxContextTokens:     8000,
		SoftBudget:           10.0,
		HardBudget:           20.0,
		CostInputPerMTokens:  0.20,
		CostOutputPerMTokens: 0.80,
		OCREnabled:           true,
		SkillsAdmin:          false,
		SummarizerMode:       "off",
	}

	ApplyToConfig(context.Background(), s, cfg)

	// Spot-check the fields the applier walks. Empty store + identical
	// fallbacks => identical values out.
	if cfg.LLMAPIKey != "env-key" || cfg.LLMBaseURL != "https://api.example.com" ||
		cfg.LLMModel != "gpt-x" || cfg.LLMMaxRetries != 5 ||
		cfg.MaxContextTokens != 8000 || cfg.SoftBudget != 10.0 || cfg.HardBudget != 20.0 ||
		cfg.CostInputPerMTokens != 0.20 || cfg.CostOutputPerMTokens != 0.80 ||
		!cfg.OCREnabled || cfg.SkillsAdmin || cfg.SummarizerMode != "off" {
		t.Errorf("empty store mutated cfg: %+v", *cfg)
	}
}

func TestApplyToConfigDBOverridesEnv(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	cfg := &config.Config{
		LLMAPIKey:            "env-key",
		LLMBaseURL:           "https://api.example.com",
		LLMModel:             "gpt-x",
		LLMMaxRetries:        5,
		MaxContextTokens:     8000,
		SoftBudget:           10.0,
		HardBudget:           20.0,
		CostInputPerMTokens:  0.20,
		CostOutputPerMTokens: 0.80,
		OCREnabled:           true,
		SkillsAdmin:          false,
		SummarizerMode:       "off",
	}

	// DB writes should win.
	_ = s.Set(ctx, KeyLLMAPIKey, "db-key")
	_ = s.Set(ctx, KeyLLMBaseURL, "https://override.example.com")
	_ = s.Set(ctx, KeyLLMModel, "gpt-y")
	_ = s.Set(ctx, KeyLLMMaxRetries, "9")
	_ = s.Set(ctx, KeyMaxContextTokens, "16000")
	_ = s.Set(ctx, KeySoftBudget, "5.5")
	_ = s.Set(ctx, KeyHardBudget, "12.5")
	_ = s.Set(ctx, KeyCostInputPerMTokens, "0.28")
	_ = s.Set(ctx, KeyCostOutputPerMTokens, "0.42")
	_ = s.Set(ctx, KeyOCREnabled, "false")
	_ = s.Set(ctx, KeySkillsAdmin, "true")
	_ = s.Set(ctx, KeySummarizerMode, "review")

	ApplyToConfig(ctx, s, cfg)

	if cfg.LLMAPIKey != "db-key" {
		t.Errorf("LLMAPIKey = %q, want db-key", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "https://override.example.com" {
		t.Errorf("LLMBaseURL = %q", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != "gpt-y" {
		t.Errorf("LLMModel = %q", cfg.LLMModel)
	}
	if cfg.LLMMaxRetries != 9 {
		t.Errorf("LLMMaxRetries = %d", cfg.LLMMaxRetries)
	}
	if cfg.MaxContextTokens != 16000 {
		t.Errorf("MaxContextTokens = %d", cfg.MaxContextTokens)
	}
	if cfg.SoftBudget != 5.5 {
		t.Errorf("SoftBudget = %v", cfg.SoftBudget)
	}
	if cfg.HardBudget != 12.5 {
		t.Errorf("HardBudget = %v", cfg.HardBudget)
	}
	if cfg.CostInputPerMTokens != 0.28 {
		t.Errorf("CostInputPerMTokens = %v", cfg.CostInputPerMTokens)
	}
	if cfg.CostOutputPerMTokens != 0.42 {
		t.Errorf("CostOutputPerMTokens = %v", cfg.CostOutputPerMTokens)
	}
	if cfg.OCREnabled {
		t.Errorf("OCREnabled = true, want false")
	}
	if !cfg.SkillsAdmin {
		t.Errorf("SkillsAdmin = false, want true")
	}
	if cfg.SummarizerMode != "review" {
		t.Errorf("SummarizerMode = %q", cfg.SummarizerMode)
	}
}

func TestApplyToConfigAppliesOrchestrationSettings(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	cfg := &config.Config{
		PromptVersion:         "aura-agent-v1",
		ToolProfileMode:       "auto",
		OrchestrationLogLevel: "summary",
	}

	_ = s.Set(ctx, KeyPromptVersion, "aura-agent-v2")
	_ = s.Set(ctx, KeyToolProfileMode, "SWARM_RESEARCH")
	_ = s.Set(ctx, KeyOrchestrationLogLevel, "DEBUG")

	ApplyToConfig(ctx, s, cfg)

	if cfg.PromptVersion != "aura-agent-v2" {
		t.Fatalf("PromptVersion = %q", cfg.PromptVersion)
	}
	if cfg.ToolProfileMode != "swarm_research" {
		t.Fatalf("ToolProfileMode = %q", cfg.ToolProfileMode)
	}
	if cfg.OrchestrationLogLevel != "debug" {
		t.Fatalf("OrchestrationLogLevel = %q", cfg.OrchestrationLogLevel)
	}
	if !IsOverridable(KeyPromptVersion) || !IsOverridable(KeyToolProfileMode) || !IsOverridable(KeyOrchestrationLogLevel) {
		t.Fatal("orchestration settings must be dashboard-overridable")
	}
}

func TestApplyToConfigAppliesQdrantSettings(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	cfg := &config.Config{
		QdrantURL:        "http://127.0.0.1:6333",
		QdrantCollection: "aura_memory_v1",
		QdrantAPIKey:     "",
		SearchBackend:    "chromem",
	}

	_ = s.Set(ctx, KeyQdrantURL, "http://qdrant:6333")
	_ = s.Set(ctx, KeyQdrantCollection, "aura_memory_v2")
	_ = s.Set(ctx, KeyQdrantAPIKey, "secret")
	_ = s.Set(ctx, KeySearchBackend, " qdrant ")

	ApplyToConfig(ctx, s, cfg)

	if cfg.QdrantURL != "http://qdrant:6333" {
		t.Fatalf("QdrantURL = %q", cfg.QdrantURL)
	}
	if cfg.QdrantCollection != "aura_memory_v2" {
		t.Fatalf("QdrantCollection = %q", cfg.QdrantCollection)
	}
	if cfg.QdrantAPIKey != "secret" {
		t.Fatalf("QdrantAPIKey not applied")
	}
	if cfg.SearchBackend != "qdrant" {
		t.Fatalf("SearchBackend = %q, want qdrant", cfg.SearchBackend)
	}
	if !IsOverridable(KeyQdrantURL) || !IsOverridable(KeyQdrantCollection) || !IsOverridable(KeyQdrantAPIKey) || !IsOverridable(KeySearchBackend) {
		t.Fatal("Qdrant settings must be dashboard-overridable")
	}
}

func TestApplyToConfigAppliesRuntimeAndSandboxFields(t *testing.T) {
	// Container-first installs use the dashboard as the single settings
	// surface, so runtime and sandbox rows are deliberate overrides.
	s := openTestStore(t)
	ctx := context.Background()

	cfg := &config.Config{
		TelegramToken:           "bootstrap-token",
		HTTPPort:                "127.0.0.1:8080",
		Headless:                false,
		EnvPath:                 ".env",
		DBPath:                  "./aura.db",
		LogLevel:                "info",
		LogDir:                  "./logs",
		WikiPath:                "./wiki",
		SkillsPath:              "./skills",
		SkillsInstallProjectDir: "",
		MCPServersPath:          "./mcp.json",
		PromptOverlayPath:       ".",
		DashboardTokenTTLHours:  720,
		SandboxEnabled:          true,
		SandboxRuntimeDir:       "./runtime/pyodide",
		SandboxTimeoutSec:       120,
		SandboxAutoImproveMode:  "dry_run",
	}

	_ = s.Set(ctx, KeyTelegramToken, "override-token")
	_ = s.Set(ctx, KeyHTTPPort, "0.0.0.0:9090")
	_ = s.Set(ctx, KeyHeadless, "true")
	_ = s.Set(ctx, KeyEnvPath, "/data/.env")
	_ = s.Set(ctx, KeyDBPath, "/data/aura-next.db")
	_ = s.Set(ctx, KeyLogLevel, "debug")
	_ = s.Set(ctx, KeyLogDir, "/data/logs")
	_ = s.Set(ctx, KeyWikiPath, "/wiki")
	_ = s.Set(ctx, KeySkillsPath, "/skills")
	_ = s.Set(ctx, KeySkillsInstallProjectDir, "/skills")
	_ = s.Set(ctx, KeyMCPServersPath, "/data/mcp.json")
	_ = s.Set(ctx, KeyPromptOverlayPath, "/data")
	_ = s.Set(ctx, KeyDashboardTokenTTLHours, "24")
	_ = s.Set(ctx, KeySandboxEnabled, "false")
	_ = s.Set(ctx, KeySandboxRuntimeDir, "/app/runtime/pyodide")
	_ = s.Set(ctx, KeySandboxTimeoutSec, "45")
	_ = s.Set(ctx, KeySandboxAutoImproveMode, "off")

	ApplyToConfig(ctx, s, cfg)

	if cfg.TelegramToken != "override-token" {
		t.Errorf("TelegramToken = %q", cfg.TelegramToken)
	}
	if cfg.HTTPPort != "0.0.0.0:9090" {
		t.Errorf("HTTPPort = %q", cfg.HTTPPort)
	}
	if !cfg.Headless {
		t.Errorf("Headless = false")
	}
	if cfg.EnvPath != "/data/.env" {
		t.Errorf("EnvPath = %q", cfg.EnvPath)
	}
	if cfg.DBPath != "/data/aura-next.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.LogDir != "/data/logs" {
		t.Errorf("LogDir = %q", cfg.LogDir)
	}
	if cfg.WikiPath != "/wiki" {
		t.Errorf("WikiPath = %q", cfg.WikiPath)
	}
	if cfg.SkillsPath != "/skills" {
		t.Errorf("SkillsPath = %q", cfg.SkillsPath)
	}
	if cfg.SkillsInstallProjectDir != "/skills" {
		t.Errorf("SkillsInstallProjectDir = %q", cfg.SkillsInstallProjectDir)
	}
	if cfg.MCPServersPath != "/data/mcp.json" {
		t.Errorf("MCPServersPath = %q", cfg.MCPServersPath)
	}
	if cfg.PromptOverlayPath != "/data" {
		t.Errorf("PromptOverlayPath = %q", cfg.PromptOverlayPath)
	}
	if cfg.DashboardTokenTTLHours != 24 {
		t.Errorf("DashboardTokenTTLHours = %d", cfg.DashboardTokenTTLHours)
	}
	if cfg.SandboxEnabled {
		t.Errorf("SandboxEnabled = true")
	}
	if cfg.SandboxRuntimeDir != "/app/runtime/pyodide" {
		t.Errorf("SandboxRuntimeDir = %q", cfg.SandboxRuntimeDir)
	}
	if cfg.SandboxTimeoutSec != 45 {
		t.Errorf("SandboxTimeoutSec = %d", cfg.SandboxTimeoutSec)
	}
	if cfg.SandboxAutoImproveMode != "off" {
		t.Errorf("SandboxAutoImproveMode = %q", cfg.SandboxAutoImproveMode)
	}
}

func TestApplyToConfigAllowlistOverride(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	cfg := &config.Config{
		Allowlist:           []string{"111"},
		AllowlistConfigured: true,
	}

	_ = s.Set(ctx, KeyAllowlist, "222, 333,  ,444")

	ApplyToConfig(ctx, s, cfg)

	if len(cfg.Allowlist) != 3 ||
		cfg.Allowlist[0] != "222" || cfg.Allowlist[1] != "333" || cfg.Allowlist[2] != "444" {
		t.Errorf("Allowlist = %v, want [222 333 444]", cfg.Allowlist)
	}
	if !cfg.AllowlistConfigured {
		t.Errorf("AllowlistConfigured = false, want true after override")
	}
}

func TestApplyToConfigBlankAllowlistRowClearsList(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	cfg := &config.Config{
		Allowlist:           []string{"111", "222"},
		AllowlistConfigured: true,
	}

	// An explicit empty value is a deliberate "clear the allowlist" signal
	// (returns Aura to first-run bootstrap mode). Distinguish from "no row"
	// which would leave the env value intact.
	_ = s.Set(ctx, KeyAllowlist, "")

	ApplyToConfig(ctx, s, cfg)

	if len(cfg.Allowlist) != 0 {
		t.Errorf("Allowlist = %v, want empty (DB empty string clears)", cfg.Allowlist)
	}
	if cfg.AllowlistConfigured {
		t.Errorf("AllowlistConfigured = true after clear, want false")
	}
}

func TestApplyToConfigUnparseableLeavesEnv(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	cfg := &config.Config{
		MaxContextTokens: 8000,
		SoftBudget:       10.0,
		OCREnabled:       true,
	}

	_ = s.Set(ctx, KeyMaxContextTokens, "not-a-number")
	_ = s.Set(ctx, KeySoftBudget, "huh")
	_ = s.Set(ctx, KeyOCREnabled, "perhaps")

	ApplyToConfig(ctx, s, cfg)

	if cfg.MaxContextTokens != 8000 {
		t.Errorf("MaxContextTokens overwritten by garbage: %d", cfg.MaxContextTokens)
	}
	if cfg.SoftBudget != 10.0 {
		t.Errorf("SoftBudget overwritten by garbage: %v", cfg.SoftBudget)
	}
	if !cfg.OCREnabled {
		t.Errorf("OCREnabled overwritten by garbage")
	}
}

func TestApplyToConfigNilStoreOrConfigNoCrash(t *testing.T) {
	ApplyToConfig(context.Background(), nil, &config.Config{})
	ApplyToConfig(context.Background(), nil, nil)
	s := openTestStore(t)
	ApplyToConfig(context.Background(), s, nil)
}

func TestIsOverridable(t *testing.T) {
	if !IsOverridable(KeyLLMAPIKey) {
		t.Errorf("LLM_API_KEY should be overridable")
	}
	for _, key := range []string{
		KeyTelegramToken, KeyHTTPPort, KeyDBPath, KeyWikiPath,
		KeyLogLevel, KeyPromptOverlayPath, KeyMCPServersPath,
		KeySkillsPath, KeySkillsInstallProjectDir, KeyCostInputPerMTokens, KeyCostOutputPerMTokens,
		KeySandboxEnabled, KeySandboxTimeoutSec,
	} {
		if !IsOverridable(key) {
			t.Errorf("%s should be overridable", key)
		}
	}
	if IsOverridable("UNRELATED") {
		t.Errorf("random key shouldn't be overridable")
	}
}
