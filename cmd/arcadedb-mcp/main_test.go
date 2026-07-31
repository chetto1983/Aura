package main

import (
	"io"
	"log/slog"
	"maps"
	"testing"
	"time"
)

func setEnv(t *testing.T, pairs map[string]string) {
	t.Helper()
	for _, key := range []string{
		"ARCADEDB_URL", "ARCADEDB_DATABASE", "ARCADEDB_USER", "ARCADEDB_PASSWORD",
		"ARCADEDB_TIMEOUT_SECONDS", "AURA_ARCADEDB_MCP_PORT", "AURA_ARCADEDB_MCP_HOST",
	} {
		t.Setenv(key, "")
	}
	for key, value := range pairs {
		t.Setenv(key, value)
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"ARCADEDB_URL":      "http://arcadedb:2480",
		"ARCADEDB_DATABASE": "aura",
		"ARCADEDB_USER":     "root",
		"ARCADEDB_PASSWORD": "pw",
	}
}

func TestConfigFromEnvReadsCredentialsAndDefaultsTheAddress(t *testing.T) {
	setEnv(t, validEnv())
	cfg, addr, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}
	if cfg.BaseURL != "http://arcadedb:2480" || cfg.Database != "aura" || cfg.User != "root" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Password != "pw" {
		t.Fatal("password not read")
	}
	if addr != "0.0.0.0:8096" {
		t.Fatalf("addr = %q, want 0.0.0.0:8096", addr)
	}
	if cfg.Timeout != 0 {
		t.Fatalf("timeout = %v, want zero so the client applies its default", cfg.Timeout)
	}
}

func TestConfigFromEnvHonoursHostAndPort(t *testing.T) {
	env := validEnv()
	env["AURA_ARCADEDB_MCP_HOST"] = "127.0.0.1"
	env["AURA_ARCADEDB_MCP_PORT"] = "9100"
	setEnv(t, env)
	_, addr, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}
	if addr != "127.0.0.1:9100" {
		t.Fatalf("addr = %q", addr)
	}
}

func TestConfigFromEnvParsesTimeout(t *testing.T) {
	env := validEnv()
	env["ARCADEDB_TIMEOUT_SECONDS"] = "45"
	setEnv(t, env)
	cfg, _, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}
	if cfg.Timeout != 45*time.Second {
		t.Fatalf("timeout = %v, want 45s", cfg.Timeout)
	}
}

// A bad port or timeout must stop the process at boot rather than silently
// falling back: a server listening somewhere unexpected is worse than one that
// refuses to start.
func TestConfigFromEnvRejectsBadNumbers(t *testing.T) {
	cases := map[string]map[string]string{
		"port not a number":    {"AURA_ARCADEDB_MCP_PORT": "eight"},
		"port zero":            {"AURA_ARCADEDB_MCP_PORT": "0"},
		"port too large":       {"AURA_ARCADEDB_MCP_PORT": "70000"},
		"port negative":        {"AURA_ARCADEDB_MCP_PORT": "-1"},
		"timeout not a number": {"ARCADEDB_TIMEOUT_SECONDS": "soon"},
		"timeout zero":         {"ARCADEDB_TIMEOUT_SECONDS": "0"},
		"timeout negative":     {"ARCADEDB_TIMEOUT_SECONDS": "-5"},
	}
	for name, overrides := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			maps.Copy(env, overrides)
			setEnv(t, env)
			if _, _, err := configFromEnv(); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

// Whitespace-only values are what a compose file with an unset variable
// produces; they must behave as absent, not as a parse failure.
func TestConfigFromEnvTreatsBlankOptionalsAsUnset(t *testing.T) {
	env := validEnv()
	env["AURA_ARCADEDB_MCP_PORT"] = "   "
	env["AURA_ARCADEDB_MCP_HOST"] = "  "
	env["ARCADEDB_TIMEOUT_SECONDS"] = " "
	setEnv(t, env)
	cfg, addr, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}
	if addr != "0.0.0.0:8096" {
		t.Fatalf("addr = %q", addr)
	}
	if cfg.Timeout != 0 {
		t.Fatalf("timeout = %v", cfg.Timeout)
	}
}

// run() must fail fast on an unusable config instead of listening on a port
// while every tool call errors.
func TestRunRejectsAnUnusableConfig(t *testing.T) {
	setEnv(t, map[string]string{"ARCADEDB_DATABASE": "aura", "ARCADEDB_USER": "root"})
	if err := run(testLogger()); err == nil {
		t.Fatal("expected run to reject an empty base URL")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
