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
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("OLLAMA_WEB_BASE_URL")
	os.Unsetenv("MAX_TOOL_ITERATIONS")
	os.Unsetenv("SKILLS_PATH")
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
	if cfg.OllamaWebBaseURL != DefaultOllamaWebBaseURL {
		t.Errorf("OllamaWebBaseURL = %q, want %q", cfg.OllamaWebBaseURL, DefaultOllamaWebBaseURL)
	}
	if cfg.MaxToolIterations != 10 {
		t.Errorf("MaxToolIterations = %d, want 10", cfg.MaxToolIterations)
	}
	if cfg.SkillsPath != "./skills" {
		t.Errorf("SkillsPath = %q, want ./skills", cfg.SkillsPath)
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
	if !cfg.SandboxEnabled {
		t.Errorf("SandboxEnabled = false, want true by default")
	}
	if cfg.SandboxRuntimeDir != DefaultSandboxRuntimeDir {
		t.Errorf("SandboxRuntimeDir = %q, want %q", cfg.SandboxRuntimeDir, DefaultSandboxRuntimeDir)
	}
	if cfg.SandboxTimeoutSec != 15 {
		t.Errorf("SandboxTimeoutSec = %d, want 15", cfg.SandboxTimeoutSec)
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
