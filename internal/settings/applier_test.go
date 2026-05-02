package settings

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/config"
)

func TestApplyToConfigEmptyStoreIsNoOp(t *testing.T) {
	s := openTestStore(t)
	cfg := &config.Config{
		LLMAPIKey:        "env-key",
		LLMBaseURL:       "https://api.example.com",
		LLMModel:         "gpt-x",
		LLMMaxRetries:    5,
		MaxContextTokens: 8000,
		SoftBudget:       10.0,
		HardBudget:       20.0,
		OCREnabled:       true,
		SkillsAdmin:      false,
		SummarizerMode:   "off",
	}

	ApplyToConfig(context.Background(), s, cfg)

	// Spot-check the fields the applier walks. Empty store + identical
	// fallbacks => identical values out.
	if cfg.LLMAPIKey != "env-key" || cfg.LLMBaseURL != "https://api.example.com" ||
		cfg.LLMModel != "gpt-x" || cfg.LLMMaxRetries != 5 ||
		cfg.MaxContextTokens != 8000 || cfg.SoftBudget != 10.0 || cfg.HardBudget != 20.0 ||
		!cfg.OCREnabled || cfg.SkillsAdmin || cfg.SummarizerMode != "off" {
		t.Errorf("empty store mutated cfg: %+v", *cfg)
	}
}

func TestApplyToConfigDBOverridesEnv(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	cfg := &config.Config{
		LLMAPIKey:        "env-key",
		LLMBaseURL:       "https://api.example.com",
		LLMModel:         "gpt-x",
		LLMMaxRetries:    5,
		MaxContextTokens: 8000,
		SoftBudget:       10.0,
		HardBudget:       20.0,
		OCREnabled:       true,
		SkillsAdmin:      false,
		SummarizerMode:   "off",
	}

	// DB writes should win.
	_ = s.Set(ctx, KeyLLMAPIKey, "db-key")
	_ = s.Set(ctx, KeyLLMBaseURL, "https://override.example.com")
	_ = s.Set(ctx, KeyLLMModel, "gpt-y")
	_ = s.Set(ctx, KeyLLMMaxRetries, "9")
	_ = s.Set(ctx, KeyMaxContextTokens, "16000")
	_ = s.Set(ctx, KeySoftBudget, "5.5")
	_ = s.Set(ctx, KeyHardBudget, "12.5")
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

func TestApplyToConfigKeepsBootstrapFields(t *testing.T) {
	// TelegramToken / HTTPPort / DBPath / LogLevel / WikiPath / SkillsPath /
	// MCPServersPath / PromptOverlayPath are bootstrap-only — even if rows
	// exist with those names, the applier must NOT touch them. (We don't
	// emit Set calls for those keys here; we just verify ApplyToConfig
	// leaves them alone with the most paranoid possible store state — a
	// row written under each key.)
	s := openTestStore(t)
	ctx := context.Background()

	cfg := &config.Config{
		TelegramToken:     "bootstrap-token",
		HTTPPort:          "127.0.0.1:8080",
		DBPath:            "./aura.db",
		LogLevel:          "info",
		WikiPath:          "./wiki",
		SkillsPath:        "./skills",
		MCPServersPath:    "./mcp.json",
		PromptOverlayPath: ".",
	}

	for _, k := range []string{
		"TELEGRAM_TOKEN", "HTTP_PORT", "DB_PATH", "LOG_LEVEL",
		"WIKI_PATH", "SKILLS_PATH", "MCP_SERVERS_PATH", "PROMPT_OVERLAY_PATH",
	} {
		_ = s.Set(ctx, k, "ATTACKER_VALUE")
	}

	ApplyToConfig(ctx, s, cfg)

	if cfg.TelegramToken != "bootstrap-token" {
		t.Errorf("TelegramToken overridden: %q", cfg.TelegramToken)
	}
	if cfg.HTTPPort != "127.0.0.1:8080" {
		t.Errorf("HTTPPort overridden: %q", cfg.HTTPPort)
	}
	if cfg.DBPath != "./aura.db" {
		t.Errorf("DBPath overridden: %q", cfg.DBPath)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel overridden: %q", cfg.LogLevel)
	}
	if cfg.WikiPath != "./wiki" {
		t.Errorf("WikiPath overridden: %q", cfg.WikiPath)
	}
	if cfg.SkillsPath != "./skills" {
		t.Errorf("SkillsPath overridden: %q", cfg.SkillsPath)
	}
	if cfg.MCPServersPath != "./mcp.json" {
		t.Errorf("MCPServersPath overridden: %q", cfg.MCPServersPath)
	}
	if cfg.PromptOverlayPath != "." {
		t.Errorf("PromptOverlayPath overridden: %q", cfg.PromptOverlayPath)
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
	if IsOverridable("TELEGRAM_TOKEN") {
		t.Errorf("TELEGRAM_TOKEN must NOT be overridable")
	}
	if IsOverridable("HTTP_PORT") {
		t.Errorf("HTTP_PORT must NOT be overridable")
	}
	if IsOverridable("DB_PATH") {
		t.Errorf("DB_PATH must NOT be overridable")
	}
	if IsOverridable("WIKI_PATH") {
		t.Errorf("WIKI_PATH must NOT be overridable")
	}
	if IsOverridable("LOG_LEVEL") {
		t.Errorf("LOG_LEVEL must NOT be overridable")
	}
	if IsOverridable("PROMPT_OVERLAY_PATH") {
		t.Errorf("PROMPT_OVERLAY_PATH must NOT be overridable")
	}
	if IsOverridable("MCP_SERVERS_PATH") {
		t.Errorf("MCP_SERVERS_PATH must NOT be overridable")
	}
	if IsOverridable("SKILLS_PATH") {
		t.Errorf("SKILLS_PATH must NOT be overridable")
	}
	if IsOverridable("UNRELATED") {
		t.Errorf("random key shouldn't be overridable")
	}
}
