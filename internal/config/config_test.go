package config

import (
	"os"
	"testing"
	"time"

	"github.com/aura/aura/internal/scheduler"
)

func TestIsAllowlisted(t *testing.T) {
	cfg := &Config{
		Allowlist: []string{"123456", "789012"},
	}

	tests := []struct {
		userID string
		want   bool
	}{
		{"123456", true},
		{" 123456 ", true},
		{"789012", true},
		{"999999", false},
		{"", false},
	}

	for _, tt := range tests {
		got := cfg.IsAllowlisted(tt.userID)
		if got != tt.want {
			t.Errorf("IsAllowlisted(%q) = %v, want %v", tt.userID, got, tt.want)
		}
	}
}

func TestLoadMissingTokenIsAllowedForFirstRunSetup(t *testing.T) {
	// Slice 14b: blank TELEGRAM_TOKEN is no longer an error — it signals
	// first-run state so cmd/aura can launch the setup wizard.
	os.Unsetenv("TELEGRAM_TOKEN")
	os.Unsetenv("TELEGRAM_ALLOWLIST")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("blank token should not error: %v", err)
	}
	if cfg.IsBootstrapped() {
		t.Errorf("IsBootstrapped() = true with blank token, want false")
	}
}

func TestIsBootstrapped(t *testing.T) {
	if (&Config{TelegramToken: ""}).IsBootstrapped() {
		t.Errorf("blank token = bootstrapped")
	}
	if (&Config{TelegramToken: "   "}).IsBootstrapped() {
		t.Errorf("whitespace token = bootstrapped")
	}
	if !(&Config{TelegramToken: "abc:def"}).IsBootstrapped() {
		t.Errorf("real token != bootstrapped")
	}
}

func TestLoadAllowsEmptyAllowlistForFirstRunBootstrap(t *testing.T) {
	os.Setenv("TELEGRAM_TOKEN", "test-token")
	defer os.Unsetenv("TELEGRAM_TOKEN")
	os.Unsetenv("TELEGRAM_ALLOWLIST")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AllowlistConfigured {
		t.Fatal("AllowlistConfigured = true, want false")
	}
	if len(cfg.Allowlist) != 0 {
		t.Fatalf("Allowlist = %v, want empty", cfg.Allowlist)
	}
}

func TestLoadTimezoneResolvesDailySchedulesInConfiguredZone(t *testing.T) {
	t.Setenv("AURA_TIMEZONE", "Europe/Rome")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	loc, err := cfg.Location()
	if err != nil {
		t.Fatalf("Location: %v", err)
	}

	after := time.Date(2026, 5, 7, 7, 0, 0, 0, time.UTC)
	got, err := scheduler.NextDailyRun("10:00", loc, after)
	if err != nil {
		t.Fatalf("NextDailyRun: %v", err)
	}
	want := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next run = %v, want %v (10:00 Europe/Rome during CEST)", got, want)
	}
}

func TestLoadSuccess(t *testing.T) {
	os.Setenv("TELEGRAM_TOKEN", "test-token")
	os.Setenv("TELEGRAM_ALLOWLIST", "123,456")
	defer os.Unsetenv("TELEGRAM_TOKEN")
	defer os.Unsetenv("TELEGRAM_ALLOWLIST")
	os.Unsetenv("MAX_CONTEXT_TOKENS")
	os.Unsetenv("SOFT_BUDGET")
	os.Unsetenv("HARD_BUDGET")
	os.Unsetenv("COST_INPUT_PER_M_TOKENS")
	os.Unsetenv("COST_OUTPUT_PER_M_TOKENS")
	os.Unsetenv("COST_PER_TOKEN")
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("WEB_SEARCH_PROVIDER")
	os.Unsetenv("SEARXNG_BASE_URL")
	os.Unsetenv("GARAGE_S3_ENDPOINT")
	os.Unsetenv("GARAGE_S3_REGION")
	os.Unsetenv("GARAGE_S3_BUCKET")
	os.Unsetenv("GARAGE_S3_ACCESS_KEY")
	os.Unsetenv("GARAGE_S3_SECRET_KEY")
	os.Unsetenv("QDRANT_URL")
	os.Unsetenv("QDRANT_COLLECTION")
	os.Unsetenv("QDRANT_API_KEY")
	os.Unsetenv("MEMORY_SEARCH_TIMEOUT_MS")
	os.Unsetenv("SKILLS_PATH")
	os.Unsetenv("SKILLS_INSTALL_PROJECT_DIR")
	os.Unsetenv("SKILLS_CATALOG_URL")
	os.Unsetenv("AURABOT_ENABLED")
	os.Unsetenv("AURABOT_MAX_ACTIVE")
	os.Unsetenv("AURABOT_MAX_DEPTH")
	os.Unsetenv("AURABOT_TIMEOUT_SEC")
	os.Unsetenv("AURABOT_MAX_ITERATIONS")
	os.Unsetenv("EMBEDDING_BASE_URL")
	os.Unsetenv("EMBEDDING_MODEL")
	os.Unsetenv("MISTRAL_API_KEY")
	os.Unsetenv("MISTRAL_OCR_MODEL")
	os.Unsetenv("MISTRAL_OCR_BASE_URL")
	os.Unsetenv("MISTRAL_OCR_TABLE_FORMAT")
	os.Unsetenv("MISTRAL_OCR_EXTRACT_HEADER")
	os.Unsetenv("MISTRAL_OCR_EXTRACT_FOOTER")
	os.Unsetenv("OCR_MAX_PAGES")
	os.Unsetenv("OCR_MAX_FILE_MB")
	os.Unsetenv("HTTP_PORT")
	os.Unsetenv("AURA_TIMEZONE")
	os.Unsetenv("AURA_HEADLESS")
	os.Unsetenv("AURA_ENV_PATH")
	os.Unsetenv("DASHBOARD_TOKEN_TTL_HOURS")
	os.Unsetenv("AURA_SKILL_ROUTING_MODE")
	os.Unsetenv("AURA_AGENT_LOOP_MAX_STEPS")
	os.Unsetenv("AURA_TERMINAL_TOOL_POLICY")
	os.Unsetenv("AURA_DELEGATION_MODE")
	os.Unsetenv("AURA_TRACE_RETENTION_DAYS")
	os.Unsetenv("AURA_WORKSPACE_TOOLS")
	os.Unsetenv("AURA_WORKSPACE_ROOT")
	os.Unsetenv("AURA_RUNTIME_WORKSPACE_PATH")
	os.Unsetenv("SANDBOX_ENABLED")
	os.Unsetenv("SANDBOX_TIMEOUT_SEC")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelegramToken != "test-token" {
		t.Errorf("TelegramToken = %q, want %q", cfg.TelegramToken, "test-token")
	}
	if len(cfg.Allowlist) != 2 || cfg.Allowlist[0] != "123" || cfg.Allowlist[1] != "456" {
		t.Errorf("Allowlist = %v, want [123 456]", cfg.Allowlist)
	}
	if !cfg.AllowlistConfigured {
		t.Error("AllowlistConfigured = false, want true")
	}
	if cfg.MaxContextTokens != 4000 {
		t.Errorf("MaxContextTokens = %d, want 4000", cfg.MaxContextTokens)
	}
	if cfg.CostInputPerMTokens != DefaultCostInputPerMTokens {
		t.Errorf("CostInputPerMTokens = %v, want %v", cfg.CostInputPerMTokens, DefaultCostInputPerMTokens)
	}
	if cfg.CostOutputPerMTokens != DefaultCostOutputPerMTokens {
		t.Errorf("CostOutputPerMTokens = %v, want %v", cfg.CostOutputPerMTokens, DefaultCostOutputPerMTokens)
	}
	if cfg.WebSearchProvider != "disabled" {
		t.Errorf("WebSearchProvider = %q, want disabled", cfg.WebSearchProvider)
	}
	if cfg.SearXNGBaseURL != DefaultSearXNGBaseURL {
		t.Errorf("SearXNGBaseURL = %q, want %q", cfg.SearXNGBaseURL, DefaultSearXNGBaseURL)
	}
	if cfg.GarageS3Region != "garage" {
		t.Errorf("GarageS3Region = %q, want garage", cfg.GarageS3Region)
	}
	if cfg.GarageS3Bucket != "aura-artifacts" {
		t.Errorf("GarageS3Bucket = %q, want aura-artifacts", cfg.GarageS3Bucket)
	}
	if cfg.QdrantURL != "" {
		t.Errorf("QdrantURL = %q, want empty by default", cfg.QdrantURL)
	}
	if cfg.QdrantCollection != "aura_memory_v1" {
		t.Errorf("QdrantCollection = %q, want aura_memory_v1", cfg.QdrantCollection)
	}
	if cfg.MemorySearchTimeoutMS != DefaultMemorySearchTimeoutMS {
		t.Errorf("MemorySearchTimeoutMS = %d, want %d", cfg.MemorySearchTimeoutMS, DefaultMemorySearchTimeoutMS)
	}
	if cfg.SkillsPath != "./skills" {
		t.Errorf("SkillsPath = %q, want ./skills", cfg.SkillsPath)
	}
	if cfg.SkillsInstallProjectDir != "" {
		t.Errorf("SkillsInstallProjectDir = %q, want empty", cfg.SkillsInstallProjectDir)
	}
	if cfg.SkillsCatalogURL != "https://skills.sh/" {
		t.Errorf("SkillsCatalogURL = %q, want https://skills.sh/", cfg.SkillsCatalogURL)
	}
	if cfg.AuraBotEnabled {
		t.Errorf("AuraBotEnabled = true, want false by default")
	}
	if cfg.AuraBotMaxActive != 4 {
		t.Errorf("AuraBotMaxActive = %d, want 4", cfg.AuraBotMaxActive)
	}
	if cfg.AuraBotMaxDepth != 1 {
		t.Errorf("AuraBotMaxDepth = %d, want 1", cfg.AuraBotMaxDepth)
	}
	if cfg.AuraBotTimeoutSec != DefaultAuraBotTimeoutSec {
		t.Errorf("AuraBotTimeoutSec = %d, want %d", cfg.AuraBotTimeoutSec, DefaultAuraBotTimeoutSec)
	}
	if cfg.AuraBotMaxIterations != 5 {
		t.Errorf("AuraBotMaxIterations = %d, want 5", cfg.AuraBotMaxIterations)
	}
	if cfg.EmbeddingBaseURL != "http://aura-llama-embed:8080/v1" {
		t.Errorf("EmbeddingBaseURL = %q, want llama.cpp sidecar URL", cfg.EmbeddingBaseURL)
	}
	if cfg.EmbeddingModel != "embeddinggemma" {
		t.Errorf("EmbeddingModel = %q, want embeddinggemma", cfg.EmbeddingModel)
	}
	if cfg.MistralOCRModel != "mistral-ocr-latest" {
		t.Errorf("MistralOCRModel = %q, want mistral-ocr-latest", cfg.MistralOCRModel)
	}
	if cfg.MistralOCRBaseURL != "https://api.mistral.ai/v1" {
		t.Errorf("MistralOCRBaseURL = %q, want Mistral API", cfg.MistralOCRBaseURL)
	}
	if cfg.MistralOCRTableFormat != "markdown" {
		t.Errorf("MistralOCRTableFormat = %q, want markdown", cfg.MistralOCRTableFormat)
	}
	if cfg.MistralOCRExtractHeader {
		t.Errorf("MistralOCRExtractHeader = true, want false by default")
	}
	if cfg.MistralOCRExtractFooter {
		t.Errorf("MistralOCRExtractFooter = true, want false by default")
	}
	if cfg.OCRMaxPages != 500 {
		t.Errorf("OCRMaxPages = %d, want 500", cfg.OCRMaxPages)
	}
	if cfg.OCRMaxFileMB != 100 {
		t.Errorf("OCRMaxFileMB = %d, want 100", cfg.OCRMaxFileMB)
	}
	if cfg.HTTPPort != "127.0.0.1:8080" {
		t.Errorf("HTTPPort = %q, want 127.0.0.1:8080 (slice 10b: localhost-only by default)", cfg.HTTPPort)
	}
	if cfg.Headless {
		t.Errorf("Headless = true, want false by default")
	}
	if cfg.Timezone != "" {
		t.Errorf("Timezone = %q, want empty by default", cfg.Timezone)
	}
	if cfg.EnvPath != ".env" {
		t.Errorf("EnvPath = %q, want .env", cfg.EnvPath)
	}
	if cfg.DashboardTokenTTLHours != 720 {
		t.Errorf("DashboardTokenTTLHours = %d, want 720", cfg.DashboardTokenTTLHours)
	}
	if cfg.SkillRoutingMode != DefaultSkillRoutingMode {
		t.Errorf("SkillRoutingMode = %q, want %q", cfg.SkillRoutingMode, DefaultSkillRoutingMode)
	}
	if cfg.AgentLoopMaxSteps != DefaultAgentLoopMaxSteps {
		t.Errorf("AgentLoopMaxSteps = %d, want %d", cfg.AgentLoopMaxSteps, DefaultAgentLoopMaxSteps)
	}
	if cfg.TerminalToolPolicy != DefaultTerminalToolPolicy {
		t.Errorf("TerminalToolPolicy = %q, want %q", cfg.TerminalToolPolicy, DefaultTerminalToolPolicy)
	}
	if cfg.DelegationMode != DefaultDelegationMode {
		t.Errorf("DelegationMode = %q, want %q", cfg.DelegationMode, DefaultDelegationMode)
	}
	if cfg.TraceRetentionDays != DefaultTraceRetentionDays {
		t.Errorf("TraceRetentionDays = %d, want %d", cfg.TraceRetentionDays, DefaultTraceRetentionDays)
	}
	if cfg.WorkspaceTools != DefaultWorkspaceTools {
		t.Errorf("WorkspaceTools = %q, want %q", cfg.WorkspaceTools, DefaultWorkspaceTools)
	}
	if cfg.WorkspaceRoot != DefaultWorkspaceRoot {
		t.Errorf("WorkspaceRoot = %q, want %q", cfg.WorkspaceRoot, DefaultWorkspaceRoot)
	}
	if cfg.RuntimeWorkspacePath != DefaultRuntimeWorkspacePath {
		t.Errorf("RuntimeWorkspacePath = %q, want %q", cfg.RuntimeWorkspacePath, DefaultRuntimeWorkspacePath)
	}
	if !cfg.SandboxEnabled {
		t.Errorf("SandboxEnabled = false, want true by default")
	}
	if cfg.SandboxTimeoutSec != DefaultSandboxTimeoutSec {
		t.Errorf("SandboxTimeoutSec = %d, want %d", cfg.SandboxTimeoutSec, DefaultSandboxTimeoutSec)
	}
}

func TestLoadAgentRuntimeSettings(t *testing.T) {
	t.Setenv("AURA_SKILL_ROUTING_MODE", " MANIFEST_LLM_REVIEW ")
	t.Setenv("AURA_AGENT_LOOP_MAX_STEPS", "12")
	t.Setenv("AURA_TERMINAL_TOOL_POLICY", "OFF")
	t.Setenv("AURA_DELEGATION_MODE", "Bounded")
	t.Setenv("AURA_TRACE_RETENTION_DAYS", "90")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SkillRoutingMode != "manifest_llm_review" {
		t.Fatalf("SkillRoutingMode = %q", cfg.SkillRoutingMode)
	}
	if cfg.AgentLoopMaxSteps != 12 {
		t.Fatalf("AgentLoopMaxSteps = %d", cfg.AgentLoopMaxSteps)
	}
	if cfg.TerminalToolPolicy != "off" {
		t.Fatalf("TerminalToolPolicy = %q", cfg.TerminalToolPolicy)
	}
	if cfg.DelegationMode != "bounded" {
		t.Fatalf("DelegationMode = %q", cfg.DelegationMode)
	}
	if cfg.TraceRetentionDays != 90 {
		t.Fatalf("TraceRetentionDays = %d", cfg.TraceRetentionDays)
	}
}

func TestLoadInvalidAgentRuntimeSettingsDegradeToDefaults(t *testing.T) {
	t.Setenv("AURA_SKILL_ROUTING_MODE", "llm")
	t.Setenv("AURA_AGENT_LOOP_MAX_STEPS", "0")
	t.Setenv("AURA_TERMINAL_TOOL_POLICY", "always")
	t.Setenv("AURA_DELEGATION_MODE", "unbounded")
	t.Setenv("AURA_TRACE_RETENTION_DAYS", "366")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SkillRoutingMode != DefaultSkillRoutingMode ||
		cfg.AgentLoopMaxSteps != DefaultAgentLoopMaxSteps ||
		cfg.TerminalToolPolicy != DefaultTerminalToolPolicy ||
		cfg.DelegationMode != DefaultDelegationMode ||
		cfg.TraceRetentionDays != DefaultTraceRetentionDays {
		t.Fatalf("invalid agent runtime settings did not degrade to defaults: %+v", cfg)
	}
}

func TestLoadRuntimeWorkspacePaths(t *testing.T) {
	t.Setenv("AURA_RUNTIME_WORKSPACE_PATH", "/workspace")
	t.Setenv("AURA_WORKSPACE_ROOT", "/workspace")
	t.Setenv("PROMPT_OVERLAY_PATH", "/workspace")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeWorkspacePath != "/workspace" {
		t.Fatalf("RuntimeWorkspacePath = %q, want /workspace", cfg.RuntimeWorkspacePath)
	}
	if cfg.WorkspaceRoot != "/workspace" {
		t.Fatalf("WorkspaceRoot = %q, want /workspace", cfg.WorkspaceRoot)
	}
	if cfg.PromptOverlayPath != "/workspace" {
		t.Fatalf("PromptOverlayPath = %q, want /workspace", cfg.PromptOverlayPath)
	}
}

func TestLoadWorkspaceTools(t *testing.T) {
	t.Setenv("AURA_WORKSPACE_TOOLS", " ON ")
	t.Setenv("AURA_WORKSPACE_ROOT", "D:/Aura")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkspaceTools != "enabled" {
		t.Fatalf("WorkspaceTools = %q, want enabled", cfg.WorkspaceTools)
	}
	if cfg.WorkspaceRoot != "D:/Aura" {
		t.Fatalf("WorkspaceRoot = %q", cfg.WorkspaceRoot)
	}
}

func TestLoadInvalidWorkspaceToolsDegradesToDefault(t *testing.T) {
	t.Setenv("AURA_WORKSPACE_TOOLS", "shell")
	t.Setenv("AURA_WORKSPACE_ROOT", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkspaceTools != DefaultWorkspaceTools {
		t.Fatalf("WorkspaceTools = %q, want %q", cfg.WorkspaceTools, DefaultWorkspaceTools)
	}
	if cfg.WorkspaceRoot != DefaultWorkspaceRoot {
		t.Fatalf("WorkspaceRoot = %q, want %q", cfg.WorkspaceRoot, DefaultWorkspaceRoot)
	}
}

func TestLoadQdrantConfig(t *testing.T) {
	os.Setenv("QDRANT_URL", "http://qdrant:6333")
	os.Setenv("QDRANT_COLLECTION", "aura_memory_v2")
	os.Setenv("QDRANT_API_KEY", "secret")
	os.Setenv("MEMORY_SEARCH_TIMEOUT_MS", "2500")
	defer os.Unsetenv("QDRANT_URL")
	defer os.Unsetenv("QDRANT_COLLECTION")
	defer os.Unsetenv("QDRANT_API_KEY")
	defer os.Unsetenv("MEMORY_SEARCH_TIMEOUT_MS")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.QdrantURL != "http://qdrant:6333" {
		t.Fatalf("QdrantURL = %q", cfg.QdrantURL)
	}
	if cfg.QdrantCollection != "aura_memory_v2" {
		t.Fatalf("QdrantCollection = %q", cfg.QdrantCollection)
	}
	if cfg.QdrantAPIKey != "secret" {
		t.Fatalf("QdrantAPIKey not loaded")
	}
	if cfg.MemorySearchTimeoutMS != 2500 {
		t.Fatalf("MemorySearchTimeoutMS = %d, want 2500", cfg.MemorySearchTimeoutMS)
	}
}

func TestLoadGarageBackupConfig(t *testing.T) {
	os.Setenv("GARAGE_S3_ENDPOINT", "http://garage:3900")
	os.Setenv("GARAGE_S3_REGION", "garage")
	os.Setenv("GARAGE_S3_BUCKET", "aura-artifacts")
	os.Setenv("GARAGE_S3_ACCESS_KEY", "GKlocal")
	os.Setenv("GARAGE_S3_SECRET_KEY", "secret")
	defer os.Unsetenv("GARAGE_S3_ENDPOINT")
	defer os.Unsetenv("GARAGE_S3_REGION")
	defer os.Unsetenv("GARAGE_S3_BUCKET")
	defer os.Unsetenv("GARAGE_S3_ACCESS_KEY")
	defer os.Unsetenv("GARAGE_S3_SECRET_KEY")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GarageS3Endpoint != "http://garage:3900" || cfg.GarageS3AccessKey != "GKlocal" || cfg.GarageS3SecretKey != "secret" {
		t.Fatalf("garage config not loaded: %+v", cfg)
	}
}

func TestLoadGarageBackupConfigFromSecretFiles(t *testing.T) {
	dir := t.TempDir()
	accessPath := dir + "/garage_access"
	secretPath := dir + "/garage_secret"
	if err := os.WriteFile(accessPath, []byte("file-access\n"), 0o600); err != nil {
		t.Fatalf("write access secret: %v", err)
	}
	if err := os.WriteFile(secretPath, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	t.Setenv("GARAGE_S3_ENDPOINT", "http://garage:3900")
	t.Setenv("GARAGE_S3_ACCESS_KEY", "")
	t.Setenv("GARAGE_S3_SECRET_KEY", "")
	t.Setenv("GARAGE_S3_ACCESS_KEY_FILE", accessPath)
	t.Setenv("GARAGE_S3_SECRET_KEY_FILE", secretPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GarageS3AccessKey != "file-access" || cfg.GarageS3SecretKey != "file-secret" {
		t.Fatalf("garage file secrets not loaded: access=%q secret=%q", cfg.GarageS3AccessKey, cfg.GarageS3SecretKey)
	}
}

func TestLoadWebSearchProvider(t *testing.T) {
	os.Setenv("WEB_SEARCH_PROVIDER", " searxng ")
	os.Setenv("SEARXNG_BASE_URL", "http://searxng:8080")
	defer os.Unsetenv("WEB_SEARCH_PROVIDER")
	defer os.Unsetenv("SEARXNG_BASE_URL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WebSearchProvider != "searxng" {
		t.Fatalf("WebSearchProvider = %q, want searxng", cfg.WebSearchProvider)
	}
	if cfg.SearXNGBaseURL != "http://searxng:8080" {
		t.Fatalf("SearXNGBaseURL = %q", cfg.SearXNGBaseURL)
	}
}

func TestLoadDashboardTokenTTLHours(t *testing.T) {
	os.Setenv("DASHBOARD_TOKEN_TTL_HOURS", "24")
	defer os.Unsetenv("DASHBOARD_TOKEN_TTL_HOURS")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DashboardTokenTTLHours != 24 {
		t.Fatalf("DashboardTokenTTLHours = %d, want 24", cfg.DashboardTokenTTLHours)
	}
}

func TestLoadHeadless(t *testing.T) {
	os.Setenv("AURA_HEADLESS", "true")
	defer os.Unsetenv("AURA_HEADLESS")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Headless {
		t.Fatal("Headless = false, want true")
	}
}

func TestLoadEnvPath(t *testing.T) {
	os.Setenv("AURA_ENV_PATH", "/data/.env")
	defer os.Unsetenv("AURA_ENV_PATH")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EnvPath != "/data/.env" {
		t.Fatalf("EnvPath = %q, want /data/.env", cfg.EnvPath)
	}
}

func TestLoadSandboxEnabled(t *testing.T) {
	os.Setenv("SANDBOX_ENABLED", "false")
	defer os.Unsetenv("SANDBOX_ENABLED")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SandboxEnabled {
		t.Fatal("SandboxEnabled = true, want false")
	}
}

func TestLoadSandboxTimeout(t *testing.T) {
	os.Setenv("SANDBOX_TIMEOUT_SEC", "45")
	defer os.Unsetenv("SANDBOX_TIMEOUT_SEC")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SandboxTimeoutSec != 45 {
		t.Fatalf("SandboxTimeoutSec = %d, want 45", cfg.SandboxTimeoutSec)
	}
}

func TestLoadSkillsInstallProjectDir(t *testing.T) {
	os.Setenv("SKILLS_INSTALL_PROJECT_DIR", "/skills")
	defer os.Unsetenv("SKILLS_INSTALL_PROJECT_DIR")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SkillsInstallProjectDir != "/skills" {
		t.Fatalf("SkillsInstallProjectDir = %q, want /skills", cfg.SkillsInstallProjectDir)
	}
}

func TestLoadCostPerMillionTokens(t *testing.T) {
	os.Setenv("COST_INPUT_PER_M_TOKENS", "0.28")
	os.Setenv("COST_OUTPUT_PER_M_TOKENS", "0.42")
	defer os.Unsetenv("COST_INPUT_PER_M_TOKENS")
	defer os.Unsetenv("COST_OUTPUT_PER_M_TOKENS")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CostInputPerMTokens != 0.28 {
		t.Fatalf("CostInputPerMTokens = %v, want 0.28", cfg.CostInputPerMTokens)
	}
	if cfg.CostOutputPerMTokens != 0.42 {
		t.Fatalf("CostOutputPerMTokens = %v, want 0.42", cfg.CostOutputPerMTokens)
	}
}

func TestLoadLegacyCostPerToken(t *testing.T) {
	os.Setenv("COST_PER_TOKEN", "0.000001")
	defer os.Unsetenv("COST_PER_TOKEN")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CostInputPerMTokens != 1 || cfg.CostOutputPerMTokens != 1 {
		t.Fatalf("legacy costs = input %v output %v, want 1/1", cfg.CostInputPerMTokens, cfg.CostOutputPerMTokens)
	}
}

func TestLoadIgnoresOutOfScaleLegacyCostPerToken(t *testing.T) {
	os.Setenv("COST_PER_TOKEN", "2")
	defer os.Unsetenv("COST_PER_TOKEN")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CostInputPerMTokens != DefaultCostInputPerMTokens || cfg.CostOutputPerMTokens != DefaultCostOutputPerMTokens {
		t.Fatalf("costs = input %v output %v, want defaults", cfg.CostInputPerMTokens, cfg.CostOutputPerMTokens)
	}
}

func TestLoadToolSearchDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ToolSearchBackend != DefaultToolSearchBackend {
		t.Fatalf("ToolSearchBackend = %q, want %q", cfg.ToolSearchBackend, DefaultToolSearchBackend)
	}
	if cfg.ToolSearchTopK != DefaultToolSearchTopK {
		t.Fatalf("ToolSearchTopK = %d, want %d", cfg.ToolSearchTopK, DefaultToolSearchTopK)
	}
}

func TestLoadToolSearchBackend(t *testing.T) {
	tests := []struct {
		env  string
		want string
	}{
		{"fts", "fts"},
		{"vector", "vector"},
		{"hybrid", "hybrid"},
		{"FTS", "fts"},
		{"Vector", "vector"},
		{"Hybrid", "hybrid"},
		{"  hybrid  ", "hybrid"},
		{"unknown", "hybrid"},
		{"", "hybrid"},
	}
	for _, tt := range tests {
		t.Setenv("TOOL_SEARCH_BACKEND", tt.env)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("TOOL_SEARCH_BACKEND=%q: unexpected error: %v", tt.env, err)
		}
		if cfg.ToolSearchBackend != tt.want {
			t.Errorf("TOOL_SEARCH_BACKEND=%q -> %q, want %q", tt.env, cfg.ToolSearchBackend, tt.want)
		}
	}
}

func TestLoadToolSearchTopK(t *testing.T) {
	tests := []struct {
		env  string
		want int
	}{
		{"3", 3},
		{"5", 5},
		{"10", 10},
		{"30", 30},
		{"50", 50},
		{"0", DefaultToolSearchTopK},
		{"51", DefaultToolSearchTopK},
		{"-1", DefaultToolSearchTopK},
	}
	for _, tt := range tests {
		t.Setenv("TOOL_SEARCH_TOP_K", tt.env)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("TOOL_SEARCH_TOP_K=%q: unexpected error: %v", tt.env, err)
		}
		if cfg.ToolSearchTopK != tt.want {
			t.Errorf("TOOL_SEARCH_TOP_K=%q -> %d, want %d", tt.env, cfg.ToolSearchTopK, tt.want)
		}
	}
}

func TestNormalizeToolSearchBackend(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"fts", "fts"},
		{"vector", "vector"},
		{"hybrid", "hybrid"},
		{"FTS", "fts"},
		{"  vector  ", "vector"},
		{"bad", "hybrid"},
		{"", "hybrid"},
	}
	for _, tt := range tests {
		got := NormalizeToolSearchBackend(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeToolSearchBackend(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
