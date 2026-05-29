// Package config is the thin root composite read by every cmd/aura subcommand.
// Per CONTEXT.md D-row "Composition": per-subsystem configs (db, knowledge, llm)
// live in their owning packages; this file only wires the top-level fields.
//
// Slice 0.5 form: DB only. Slice 0.7 will add `Knowledge knowledge.Config` +
// `Embed embed.Config`; Phase 2 will add `LLM llm.Config`. No new fields land
// here without an owning slice plan.
package config

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/chetto1983/aura/internal/db"
	"github.com/joho/godotenv"
)

// Config is the root composite. Subsystem configs live in their packages.
type Config struct {
	DB             db.Config
	RunDir         string
	ToolPreviewCap int
}

// Load reads .env (best-effort) then populates a Config from environment
// variables. A missing .env file is not an error — production deployments
// rely on real environment variables, not on the .env shim.
func Load() (*Config, error) {
	_ = godotenv.Load() // best-effort; missing .env is not fatal
	return &Config{
		DB: db.Config{
			URL:        os.Getenv("AURA_DB_URL"),
			MigrateURL: os.Getenv("AURA_DB_MIGRATE_URL"),
			// pool tuning left at zero; db.Open applies defaults
		},
		RunDir:         envDefault("AURA_RUN_DIR", defaultRunDir()),
		ToolPreviewCap: envIntDefault("AURA_CONTEXT_PREVIEW_CAP_BYTES", 2048),
	}, nil
}

// envDefault returns the value of `key` from the environment, falling back to
// `fallback` when the variable is unset or empty.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envIntDefault returns the integer value of `key`, falling back to `fallback`
// when the variable is unset, empty, or fails to parse as an int. Parsing
// failures are silently absorbed — the fallback is preferable to a fatal boot
// error on a misformatted ad-hoc env tweak.
func envIntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// defaultRunDir returns a sensible per-user run directory for sidecar tool
// outputs. Falls back to a tmp-based path if user cache is unavailable.
func defaultRunDir() string {
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, "aura")
	}
	return filepath.Join(os.TempDir(), "aura")
}
