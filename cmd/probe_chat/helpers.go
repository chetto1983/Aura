package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/aura/aura/internal/db"
)

// =========================================================================
// HELPERS
// =========================================================================

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func openReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	// Route through internal/db.OpenReadOnly to honor the shared driver
	// policy (the TestProductionSQLiteOpensGoThroughSharedDBPackage gate).
	return db.OpenReadOnly(path)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "probe_chat: "+msg)
	os.Exit(2)
}
