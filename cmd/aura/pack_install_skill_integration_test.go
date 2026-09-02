//go:build db_integration

// Regression test for the standing defect in packSkillInstaller (cmd/aura/pack_install.go):
// it called the strict config.Load(), which returns llm.ErrMissingAPIKey when
// OPENROUTER_API_KEY is unset, and swallowed that error by returning nil — so on every
// keyless local-model deployment (Ollama, llama.cpp — precisely the audience the pack
// installer's model-route choice exists to serve), `aura pack install` silently and
// permanently disabled skill installation. Nothing was logged.
//
// A unit test cannot reach the interesting branch: packSkillInstaller short-circuits to
// nil before config is ever loaded when pool == nil, so this needs a real *pgxpool.Pool —
// hence db_integration, following the package's existing envOrSkip pattern (e.g.
// serve_governance_write_skills_integration_test.go). No-skip-as-green: fails loud under
// $CI rather than reporting a skip as a pass.
package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
)

// packSkillEnvOrSkip mirrors the integration-tier discipline used throughout this
// package (see skillsBridgeEnvOrSkip): skip locally when the DSN is not exported, fail
// loud under $CI so a missing env var can never read as a passing test.
func packSkillEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// TestPackSkillInstallerIgnoresMissingAPIKey asserts what actually broke: with
// OPENROUTER_API_KEY unset and a real pool, packSkillInstaller must return a non-nil
// installer. Skill installation has nothing to do with the LLM route, so it must not be
// gated on a key that a local-model deployment never sets — the same reasoning
// config.LoadDB's doc comment gives for `aura db migrate`, and the same call
// cmd/aura/skills.go:68 already makes for the skills CLI.
func TestPackSkillInstallerIgnoresMissingAPIKey(t *testing.T) {
	withTempHome(t) // clears OPENROUTER_API_KEY + neutralizes AURA_LLM_*/llm.json (config_test.go)

	appURL := packSkillEnvOrSkip(t, "AURA_DB_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		t.Fatalf("precondition failed: OPENROUTER_API_KEY = %q, want empty", key)
	}

	if installer := packSkillInstaller(pool); installer == nil {
		t.Fatal("packSkillInstaller returned nil with a real pool and no OPENROUTER_API_KEY: " +
			"skill installation would be silently and permanently disabled on every keyless deployment")
	}
}
