package config

import "testing"

func isolateConfigHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(home)
}

func TestLoadServe_AllowsEmptyLLMKey(t *testing.T) {
	isolateConfigHome(t)
	clearPostgresEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("POSTGRES_PASSWORD", "s3cret")

	cfg, err := LoadServe()
	if err != nil {
		t.Fatalf("LoadServe: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadServe returned nil config")
	}
	if cfg.LLM.APIKey != "" {
		t.Fatalf("LoadServe LLM.APIKey = %q, want empty", cfg.LLM.APIKey)
	}
	if cfg.DB.MigrateURL == "" {
		t.Fatal("LoadServe must still compose the DB tier")
	}

	if _, err := Load(); err == nil {
		t.Fatal("Load() must still fail-fast with an empty OPENROUTER_API_KEY")
	}
	if dbOnly := LoadDB(); dbOnly.LLM.APIKey != "" {
		t.Fatalf("LoadDB LLM.APIKey = %q, want zero-value", dbOnly.LLM.APIKey)
	}
}
