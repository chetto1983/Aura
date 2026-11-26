//go:build db_integration

package main

import (
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/testenv"
)

// requireConfiguredPostgres ends the test unless config.LoadDB has a database to reach: AURA_DB_URL,
// or POSTGRES_PASSWORD with the PG* defaults it composes the DSN from.
func requireConfiguredPostgres(t *testing.T, what string) {
	t.Helper()
	if os.Getenv("AURA_DB_URL") == "" && os.Getenv("POSTGRES_PASSWORD") == "" {
		testenv.Unavailable(t, "%s needs Postgres (AURA_DB_URL or POSTGRES_PASSWORD)", what)
	}
}
