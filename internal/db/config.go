package db

import "time"

// Config carries both DSNs (D-07: separate runtime vs DDL role) plus pool tuning
// fields. Zero-valued tuning fields are filled in by Open with sensible defaults
// (MaxConns=10, MinConns=1, MaxConnIdleTime=30s).
type Config struct {
	URL             string        // role aura_app — runtime
	MigrateURL      string        // role aura_migrate — DDL only (PRD amendment #17, D-07)
	MaxConns        int32         // default 10
	MinConns        int32         // default 1
	MaxConnIdleTime time.Duration // default 30s
}
