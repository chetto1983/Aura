package settings

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	if appDatabase != "aura_cov" && !strings.HasPrefix(appDatabase, "aura_settings_") {
		return fmt.Errorf(
			"refusing settings integration database %q; use aura_cov or aura_settings_*",
			appDatabase,
		)
	}
	return nil
}
