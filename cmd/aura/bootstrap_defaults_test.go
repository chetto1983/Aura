package main

import (
	"testing"

	"github.com/aura/aura/internal/config"
)

// clearBootstrapEnv clears the bootstrap meta-config env vars so each test
// starts from a clean slate. t.Setenv restores originals at end of test.
func clearBootstrapEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AURA_HEADLESS", "")
	t.Setenv("DB_PATH", "")
	t.Setenv("HTTP_PORT", "")
}

func TestBootstrapMetaDefaults_FreshInstall_Desktop(t *testing.T) {
	clearBootstrapEnv(t)
	setBootstrapMetaConfig(func() bool { return false }) // desktop: has TTY

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.DBPath != defaultDBPath {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, defaultDBPath)
	}
	if cfg.HTTPPort != defaultHTTPPort {
		t.Errorf("HTTPPort = %q, want %q", cfg.HTTPPort, defaultHTTPPort)
	}
	if cfg.Headless {
		t.Error("Headless = true, want false (desktop)")
	}
}

func TestBootstrapMetaDefaults_FreshInstall_Container(t *testing.T) {
	clearBootstrapEnv(t)
	setBootstrapMetaConfig(func() bool { return true }) // container: no TTY

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.DBPath != defaultContainerDBPath {
		t.Errorf("DBPath = %q, want %q (container default)", cfg.DBPath, defaultContainerDBPath)
	}
	if !cfg.Headless {
		t.Error("Headless = false, want true (container auto-detect)")
	}
}

func TestBootstrapMetaDefaults_DBPathEnvOverride(t *testing.T) {
	clearBootstrapEnv(t)
	t.Setenv("DB_PATH", "/tmp/foo.db")
	setBootstrapMetaConfig(func() bool { return true }) // container: would default to data path

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.DBPath != "/tmp/foo.db" {
		t.Errorf("DBPath = %q, want /tmp/foo.db (env override wins over bootstrap default)", cfg.DBPath)
	}
}

func TestBootstrapMetaDefaults_HTTPPortEnvOverride(t *testing.T) {
	clearBootstrapEnv(t)
	t.Setenv("HTTP_PORT", ":9999")
	setBootstrapMetaConfig(func() bool { return false })

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.HTTPPort != ":9999" {
		t.Errorf("HTTPPort = %q, want :9999 (env override wins)", cfg.HTTPPort)
	}
}

func TestBootstrapMetaDefaults_HeadlessEnvOverride(t *testing.T) {
	clearBootstrapEnv(t)
	t.Setenv("AURA_HEADLESS", "true") // explicit: headless even if TTY present
	setBootstrapMetaConfig(func() bool { return false }) // TTY present, but env says headless

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if !cfg.Headless {
		t.Error("Headless = false, want true (AURA_HEADLESS=true env wins over auto-detect)")
	}
}
