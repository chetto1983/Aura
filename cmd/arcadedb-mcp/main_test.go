package main

import (
	"bytes"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setEnv(t *testing.T, pairs map[string]string) {
	t.Helper()
	for _, key := range []string{
		"ARCADEDB_URL", "ARCADEDB_DATABASE", "ARCADEDB_USER", "ARCADEDB_PASSWORD",
		"ARCADEDB_TIMEOUT_SECONDS", "AURA_ARCADEDB_MCP_PORT", "AURA_ARCADEDB_MCP_HOST",
		"AURA_MEMORY_QUERY_MAX_RUNES", "AURA_MEMORY_ENTITY_MAX_RUNES",
		"AURA_MEMORY_STATEMENT_MAX_RUNES", "AURA_MEMORY_DIGEST_SCAN_MAX_COUNT",
		"AURA_MEMORY_HYBRID_CANDIDATE_MAX_COUNT", "AURA_MEMORY_DENSE_MAX_DISTANCE_RATIO",
		"AURA_MEMORY_LEXICAL_MIN_SCORE", "AURA_ARCADEDB_MCP_BODY_MAX_BYTES",
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

func TestConfigFromEnvParsesMemoryLimits(t *testing.T) {
	env := validEnv()
	maps.Copy(env, map[string]string{
		"AURA_MEMORY_QUERY_MAX_RUNES":            "1024",
		"AURA_MEMORY_ENTITY_MAX_RUNES":           "256",
		"AURA_MEMORY_STATEMENT_MAX_RUNES":        "2048",
		"AURA_MEMORY_DIGEST_SCAN_MAX_COUNT":      "900",
		"AURA_MEMORY_HYBRID_CANDIDATE_MAX_COUNT": "80",
		"AURA_MEMORY_DENSE_MAX_DISTANCE_RATIO":   "0.42",
		"AURA_MEMORY_LEXICAL_MIN_SCORE":          "3.5",
	})
	setEnv(t, env)
	cfg, _, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}
	want := []float64{1024, 256, 2048, 900, 80, 0.42, 3.5}
	got := []float64{
		float64(cfg.MemoryLimits.QueryRunes), float64(cfg.MemoryLimits.EntityRunes),
		float64(cfg.MemoryLimits.StatementRunes), float64(cfg.MemoryLimits.DigestScan),
		float64(cfg.MemoryLimits.HybridCandidates), cfg.MemoryLimits.DenseMaxDistance,
		cfg.MemoryLimits.LexicalMinScore,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("memory limits = %v, want %v", got, want)
		}
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
		"query zero":           {"AURA_MEMORY_QUERY_MAX_RUNES": "0"},
		"entity junk":          {"AURA_MEMORY_ENTITY_MAX_RUNES": "many"},
		"statement negative":   {"AURA_MEMORY_STATEMENT_MAX_RUNES": "-1"},
		"digest zero":          {"AURA_MEMORY_DIGEST_SCAN_MAX_COUNT": "0"},
		"candidate negative":   {"AURA_MEMORY_HYBRID_CANDIDATE_MAX_COUNT": "-4"},
		"distance nan":         {"AURA_MEMORY_DENSE_MAX_DISTANCE_RATIO": "NaN"},
		"lexical infinity":     {"AURA_MEMORY_LEXICAL_MIN_SCORE": "+Inf"},
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

func TestPositiveInt64EnvDefaultsAndRejectsInvalidValues(t *testing.T) {
	setEnv(t, validEnv())
	if got, err := positiveInt64Env("AURA_ARCADEDB_MCP_BODY_MAX_BYTES", 123); err != nil || got != 123 {
		t.Fatalf("default body cap = %d, %v", got, err)
	}
	for _, raw := range []string{"0", "-1", "many"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("AURA_ARCADEDB_MCP_BODY_MAX_BYTES", raw)
			if _, err := positiveInt64Env("AURA_ARCADEDB_MCP_BODY_MAX_BYTES", 123); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCappedBodyHandlerRejectsOversizedBodyBeforeDispatch(t *testing.T) {
	called := false
	handler := cappedBodyHandler(4, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(http.MethodPost, "/mcp/", strings.NewReader("12345"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
	if called {
		t.Fatal("oversized request reached MCP decoder")
	}
}

func TestCappedBodyHandlerRestoresAcceptedBody(t *testing.T) {
	handler := cappedBodyHandler(4, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read restored body: %v", err)
		}
		_, _ = w.Write(body)
	}))
	request := httptest.NewRequest(http.MethodPost, "/mcp/", bytes.NewBufferString("1234"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "1234" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestConfigFromEnvRejectsInvalidBaseURL(t *testing.T) {
	for name, baseURL := range map[string]string{
		"empty":      "",
		"blank":      "   ",
		"bad scheme": "ftp://arcadedb:2480",
		"unparsable": "http://[::1",
	} {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			env["ARCADEDB_URL"] = baseURL
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
