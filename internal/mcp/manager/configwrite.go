package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/jackc/pgx/v5/pgxpool"
)

// configwrite.go is the atomic temp→tx→rename config-write wrapper every MCP write verb
// funnels through (D-04 atomicity + the MCPW-01 concurrency edge in one mechanism).
// SaveManagedConfig is a DIRECT os.WriteFile (no temp+rename), so an interrupted write
// could in theory truncate the prior config; WriteConfigWithAudit fixes both problems at
// once (RESEARCH Pitfall 1): it marshals+validates `next` into a SIBLING temp file, runs
// the caller's audit INSERT inside db.WithTx, and only os.Renames the temp over the live
// path when the tx commits — discarding the temp on tx failure. The result is all-or-
// nothing: no applied-but-unaudited config, no audited-but-not-applied config, and a
// crash mid-write leaves the prior servers.json byte-identical (the rename is atomic on
// POSIX and best-effort-atomic elsewhere).

// ErrServerNotFound is the sentinel for an env-edit / mutation targeting a server name
// absent from the managed config. The handler maps it to a clean 404.
var ErrServerNotFound = errors.New("mcp server not found")

// WriteConfigWithAudit persists `next` to `path` atomically and records `in` in the
// append-only mcp_audit ledger in the SAME transaction, so a config mutation and its audit
// row commit together (D-04). The ordering (RESEARCH Pitfall 1) is:
//
//  1. marshal+validate `next` and write it to a sibling temp file (0o600);
//  2. db.WithTx(ctx, pool, InsertMCPAuditTx(in)) — the audit INSERT is the tx;
//  3. on tx error: os.Remove the temp and return (nothing applied);
//  4. on tx success: os.Rename(temp, path) — the config flips atomically.
//
// The signature mirrors db.WithTx verbatim: ctx is context.Context and pool is
// *pgxpool.Pool (NOT strings); only path is a string.
func WriteConfigWithAudit(ctx context.Context, pool *pgxpool.Pool, path string, next mcp.ManagedConfig, in MCPAuditInsert) error {
	tmp, err := writeConfigTemp(path, next)
	if err != nil {
		return err
	}
	if err := db.WithTx(ctx, pool, func(q *sqlc.Queries) error {
		return InsertMCPAuditTx(ctx, q, in)
	}); err != nil {
		_ = os.Remove(tmp) // no applied-but-unaudited: discard the staged config
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit MCP config %s: %w", path, err)
	}
	return nil
}

// writeConfigTemp marshals+validates `doc` (via SaveManagedConfig, which normalizes,
// validates, MkdirAll's the dir, and writes 0o600) into a UNIQUE sibling temp file beside
// `path`, returning its name. Writing the temp in the same directory keeps os.Rename a
// same-filesystem atomic move. The caller renames it over `path` on tx success or removes
// it on failure.
func writeConfigTemp(path string, doc mcp.ManagedConfig) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create MCP config dir: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create MCP config temp: %w", err)
	}
	tmp := f.Name()
	// Close the placeholder handle immediately; SaveManagedConfig owns the real write
	// (marshal+validate+0o600). On any failure the partial temp is removed.
	_ = f.Close()
	if err := mcp.SaveManagedConfig(tmp, doc); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}
