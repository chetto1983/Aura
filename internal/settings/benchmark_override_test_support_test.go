package settings

import (
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A settings integration run rewrites live rows and restores them, so it must never point
// at a real deployment database. `aura_cov` and `aura_settings_*` are disposable by
// construction. Under GitHub Actions the compose database named `aura` is disposable too —
// it is created per job and dies with the runner — which is the same exemption
// scripts/coverage_gate.sh makes for GITHUB_ACTIONS. Without it the CI coverage gate,
// which runs the whole matrix against that one DSN, fails instead of running.
func disposableSettingsDatabase(name string) bool {
	switch {
	case name == "aura_cov", strings.HasPrefix(name, "aura_settings_"):
		return true
	case name == "aura" && os.Getenv("GITHUB_ACTIONS") != "":
		return true
	default:
		return false
	}
}

func validateBenchmarkIntegrationDSNs(appURL, migrateURL string) error {
	app, err := pgxpool.ParseConfig(appURL)
	if err != nil {
		return fmt.Errorf("parse AURA_DB_URL: %w", err)
	}
	migrate, err := pgxpool.ParseConfig(migrateURL)
	if err != nil {
		return fmt.Errorf("parse AURA_DB_MIGRATE_URL: %w", err)
	}
	appDatabase := app.ConnConfig.Database
	migrateDatabase := migrate.ConnConfig.Database
	if appDatabase != migrateDatabase {
		return fmt.Errorf(
			"settings integration databases differ: app=%q migrate=%q",
			appDatabase,
			migrateDatabase,
		)
	}
	if !disposableSettingsDatabase(appDatabase) {
		return fmt.Errorf(
			"refusing settings integration database %q; use aura_cov or aura_settings_*",
			appDatabase,
		)
	}
	return nil
}
