// Package db provides Aura's shared production SQLite open path.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

var pragmas = []string{
	"journal_mode=WAL",
	"busy_timeout=5000",
	"foreign_keys=ON",
	"synchronous=NORMAL",
	"cache_size=-20000",
	"temp_store=MEMORY",
}

// Open opens a SQLite database configured with Aura's production PRAGMAs.
func Open(path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("db path is required")
	}

	db, err := sql.Open("sqlite", withPragmas(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	return db, nil
}

// CheckIntegrity runs SQLite's integrity check on the shared database pool.
func CheckIntegrity(ctx context.Context, db *sql.DB) (string, error) {
	if db == nil {
		return "", errors.New("db is required")
	}
	var status string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&status); err != nil {
		return "", fmt.Errorf("sqlite integrity check: %w", err)
	}
	return status, nil
}

func withPragmas(path string) string {
	values := url.Values{}
	for _, pragma := range pragmas {
		values.Add("_pragma", pragma)
	}

	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + values.Encode()
}
