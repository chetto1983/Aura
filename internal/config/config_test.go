package config

import (
	"os"
	"testing"
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
	os.Unsetenv("OLLAMA_WEB_BASE_URL")
	os.Unsetenv("WEB_SEARCH_PROVIDER")
	os.Unsetenv("SEARXNG_BASE_URL")
	os.Unsetenv("GARAGE_S3_ENDPOINT")
	os.Unsetenv("GARAGE_S3_REGION")
	os.Unsetenv("GARAGE_S3_BUCKET")
	os.Unsetenv("GARAGE_S3_ACCESS_KEY")
	os.Unsetenv("GARAGE_S3_SECRET_KEY")
	os.Unsetenv("MAX_TOOL_ITERATIONS")
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
	os.Unsetenv("MISTRAL_OCR_INCLUDE_IMAGES")
	os.Unsetenv("MISTRAL_OCR_EXTRACT_HEADER")
	os.Unsetenv("MISTRAL_OCR_EXTRACT_FOOTER")
	os.Unsetenv("OCR_ENABLED")
	os.Unsetenv("OCR_MAX_PAGES")
	os.Unsetenv("OCR_MAX_FILE_MB")
	os.Unsetenv("HTTP_PORT")
	os.Unsetenv("AURA_HEADLESS")
	os.Unsetenv("AURA_ENV_PATH")
	os.Unsetenv("DASHBOARD_TOKEN_TTL_HOURS")
	os.Unsetenv("SANDBOX_ENABLED")
	os.Unsetenv("SANDBOX_RUNTIME_DIR")
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
	if cfg.OllamaWebBaseURL != DefaultOllamaWebBaseURL {
		t.Errorf("OllamaWebBaseURL = %q, want %q", cfg.OllamaWebBaseURL, DefaultOllamaWebBaseURL)
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
	if cfg.MaxToolIterations != 10 {
		t.Errorf("MaxToolIterations = %d, want 10", cfg.MaxToolIterations)
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
	if cfg.EmbeddingBaseURL != "https://api.mistral.ai/v1" {
		t.Errorf("EmbeddingBaseURL = %q, want Mistral API", cfg.EmbeddingBaseURL)
	}
	if cfg.EmbeddingModel != "mistral-embed" {
		t.Errorf("EmbeddingModel = %q, want mistral-embed", cfg.EmbeddingModel)
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
	if cfg.MistralOCRIncludeImages {
		t.Errorf("MistralOCRIncludeImages = true, want false by default")
	}
	if cfg.MistralOCRExtractHeader {
		t.Errorf("MistralOCRExtractHeader = true, want false by default")
	}
	if cfg.MistralOCRExtractFooter {
		t.Errorf("MistralOCRExtractFooter = true, want false by default")
	}
	if !cfg.OCREnabled {
		t.Errorf("OCREnabled = false, want true by default")
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
	if cfg.EnvPath != ".env" {
		t.Errorf("EnvPath = %q, want .env", cfg.EnvPath)
	}
	if cfg.DashboardTokenTTLHours != 720 {
		t.Errorf("DashboardTokenTTLHours = %d, want 720", cfg.DashboardTokenTTLHours)
	}
	if !cfg.SandboxEnabled {
		t.Errorf("SandboxEnabled = false, want true by default")
	}
	if cfg.SandboxRuntimeDir != DefaultSandboxRuntimeDir {
		t.Errorf("SandboxRuntimeDir = %q, want %q", cfg.SandboxRuntimeDir, DefaultSandboxRuntimeDir)
	}
	if cfg.SandboxTimeoutSec != DefaultSandboxTimeoutSec {
		t.Errorf("SandboxTimeoutSec = %d, want %d", cfg.SandboxTimeoutSec, DefaultSandboxTimeoutSec)
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

func TestLoadSandboxRuntimeDir(t *testing.T) {
	os.Setenv("SANDBOX_RUNTIME_DIR", "D:/Aura/runtime/pyodide")
	defer os.Unsetenv("SANDBOX_RUNTIME_DIR")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SandboxRuntimeDir != "D:/Aura/runtime/pyodide" {
		t.Fatalf("SandboxRuntimeDir = %q", cfg.SandboxRuntimeDir)
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
