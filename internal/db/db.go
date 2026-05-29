// Package db owns Postgres connectivity for Aura — pgxpool open, golang-migrate
// runner, ping/status/reset helpers, and the redactDSN error-wrap discipline
// (T-1.05-01 mitigation). Per CONTEXT.md D-07 the Config carries both URL
// (role aura_app, runtime) and MigrateURL (role aura_migrate, DDL only); each
// subcommand picks the one it needs.
package db

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool default tuning when Config fields are zero.
const (
	defaultMaxConns        = 10
	defaultMinConns        = 1
	defaultMaxConnIdleTime = 30 * time.Second
)

// Open returns a pgxpool.Pool against cfg.URL (role aura_app for the runtime
// path; cfg.MigrateURL is the migrate-only path consumed by Migrate). Errors
// always pass through redactDSN so POSTGRES_PASSWORD never leaks into logs
// (Pitfall #2 / T-1.05-01).
func Open(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
	if cfg == nil || cfg.URL == "" {
		return nil, fmt.Errorf("open pool: URL is empty (set AURA_DB_URL)")
	}
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse db url %s: %w", redactDSN(cfg.URL), err)
	}
	if cfg.MaxConns > 0 {
		pc.MaxConns = cfg.MaxConns
	} else {
		pc.MaxConns = defaultMaxConns
	}
	if cfg.MinConns > 0 {
		pc.MinConns = cfg.MinConns
	} else {
		pc.MinConns = defaultMinConns
	}
	if cfg.MaxConnIdleTime > 0 {
		pc.MaxConnIdleTime = cfg.MaxConnIdleTime
	} else {
		pc.MaxConnIdleTime = defaultMaxConnIdleTime
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("open pool to %s: %w", redactDSN(cfg.URL), err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping %s: %w", redactDSN(cfg.URL), err)
	}
	return pool, nil
}

// redactDSN masks the password portion of a Postgres DSN so error messages
// and slog logs never expose POSTGRES_PASSWORD. Returns "<unparseable-dsn>"
// when url.Parse fails so unbounded raw input never reaches a log line.
// T-1.05-01 mitigation — asserted by TestRedactDSN_StripsPassword.
//
// The redacted output uses the literal three-asterisk marker "***" rather
// than the URL-percent-encoded form url.URL.String() would otherwise emit
// ("%2A%2A%2A"). Error messages are human-read, not URL-fetched, so the
// literal form is the clearer signal.
func redactDSN(s string) string {
	if s == "" {
		return "<empty-dsn>"
	}
	u, err := url.Parse(s)
	if err != nil {
		return "<unparseable-dsn>"
	}
	if u.User == nil {
		return u.String()
	}
	if _, hasPassword := u.User.Password(); !hasPassword {
		return u.String()
	}
	// Strip user info entirely, then splice in `username:***@` so the asterisks
	// stay literal instead of going through url.URL percent-encoding.
	u.User = nil
	rebuilt := u.String()
	// rebuilt looks like "postgres://127.0.0.1:5432/aura?..."; insert userinfo.
	const sep = "://"
	idx := strings.Index(rebuilt, sep)
	if idx < 0 {
		// Defensive: scheme stripped unexpectedly, fall back to a safe marker.
		return "<unparseable-dsn>"
	}
	username := url.PathEscape(redactDSNUsername(s))
	return rebuilt[:idx+len(sep)] + username + ":***@" + rebuilt[idx+len(sep):]
}

// redactDSNUsername extracts the raw username from a DSN by reparsing.
// Kept as a separate tiny helper so the redactDSN body stays linear.
func redactDSNUsername(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return ""
	}
	return u.User.Username()
}
