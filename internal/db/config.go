package db

import "time"

// Config carries the composed Postgres DSNs (D-07: separate runtime vs DDL
// role) plus pool tuning fields. Zero-valued tuning fields are filled in by
// Open with sensible defaults (MaxConns=10, MinConns=1, MaxConnIdleTime=30s).
//
// URL / MigrateURL are composed by internal/config from POSTGRES_* primitives
// (single password fans out to both roles) OR populated verbatim from the
// AURA_DB_URL / AURA_DB_MIGRATE_URL overrides for managed-Postgres setups.
//
// BootstrapURL is the superuser DSN used by EnsureRoles for CREATE ROLE DDL —
// composed from POSTGRES_USER (superuser) by default; override via
// AURA_DB_BOOTSTRAP_URL. Password is carried separately so cmd/aura/db.go can
// pass it to EnsureRoles without re-parsing the URL.
type Config struct {
	URL             string        // role aura_app — runtime
	MigrateURL      string        // role aura_migrate — DDL only (PRD amendment #17, D-07)
	BootstrapURL    string        // role POSTGRES_USER (superuser) — CREATE ROLE bootstrap
	Password        string        // shared password for both application roles
	MaxConns        int32         // default 10
	MinConns        int32         // default 1
	MaxConnIdleTime time.Duration // default 30s
}
