// Cypher migration runner (Pattern 4). .cypher files numbered like SQL
// migrations are embedded into the binary; applied versions are audited in the
// Postgres aura.knowledge_migrations table (created by Slice 0.5), consumed here
// via the sqlc-generated bindings. New migrations execute statement-by-statement
// through the driver-backed SchemaExecutor (auto-commit — see schema.go for why
// MCP cannot run schema DDL); on success an audit row is written. Re-running is
// idempotent: already-applied versions are skipped, and a checksum drift on an
// applied version is a hard error (history corruption).
package knowledge

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.cypher
var cypherFS embed.FS

// migration is one parsed .cypher file: numeric version, name, body, checksum.
type migration struct {
	version  int32
	name     string
	body     string
	checksum string
}

// Migrate applies every embedded .cypher migration not yet recorded in
// aura.knowledge_migrations and returns the count newly applied. Idempotent.
func Migrate(ctx context.Context, schema *SchemaExecutor, pool *pgxpool.Pool) (int, error) {
	migs, err := loadMigrations(cypherFS)
	if err != nil {
		return 0, err
	}
	q := sqlc.New(pool)
	applied, err := q.ListAppliedKnowledgeMigrations(ctx)
	if err != nil {
		return 0, fmt.Errorf("list applied knowledge migrations: %w", err)
	}
	appliedSum := make(map[int32]string, len(applied))
	for _, a := range applied {
		appliedSum[a.Version] = a.Checksum
	}

	count := 0
	for _, m := range migs {
		if sum, ok := appliedSum[m.version]; ok {
			if sum != m.checksum {
				return count, fmt.Errorf("migration %04d checksum mismatch (history corruption)", m.version)
			}
			continue
		}
		for _, stmt := range splitCypherStatements(m.body) {
			if err := schema.Exec(ctx, stmt); err != nil {
				return count, fmt.Errorf("apply migration %04d (%s): %w", m.version, m.name, err)
			}
		}
		if err := q.RecordKnowledgeMigration(ctx, sqlc.RecordKnowledgeMigrationParams{
			Version:  m.version,
			Name:     m.name,
			Checksum: m.checksum,
		}); err != nil {
			return count, fmt.Errorf("record migration %04d: %w", m.version, err)
		}
		count++
	}
	return count, nil
}

// loadMigrations reads, parses, and checksums every .cypher file under
// "migrations" in fsys, sorted ascending by numeric version prefix. fsys is
// injected (production passes the embedded cypherFS) so the error branches are
// unit-testable with a synthetic filesystem.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	var migs []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".cypher") {
			continue
		}
		version, name, err := parseMigrationName(e.Name())
		if err != nil {
			return nil, err
		}
		body, err := fs.ReadFile(fsys, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		sum := sha256.Sum256(body)
		migs = append(migs, migration{
			version:  version,
			name:     name,
			body:     string(body),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}

// parseMigrationName splits "0001_init.cypher" into version 1 and name "0001_init".
func parseMigrationName(filename string) (int32, string, error) {
	stem := strings.TrimSuffix(filename, ".cypher")
	prefix, _, found := strings.Cut(stem, "_")
	if !found {
		return 0, "", fmt.Errorf("migration filename %q must be NNNN_name.cypher", filename)
	}
	n, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, "", fmt.Errorf("migration filename %q has non-numeric version prefix: %w", filename, err)
	}
	// Domain guard: versions are small positive integers (NNNN prefix). Bounding
	// here keeps the int32 conversion provably safe (gosec G109).
	if n < 1 || n > 999999 {
		return 0, "", fmt.Errorf("migration filename %q version %d out of range [1,999999]", filename, n)
	}
	return int32(n), stem, nil //nolint:gosec // G109: n is bounded to [1,999999] above, conversion is safe
}

// splitCypherStatements splits a .cypher body into individual statements on ';',
// dropping fragments that are empty or contain only // line comments. Phase 1's
// migration bodies contain no string literals with semicolons, so a plain split
// is correct (D-08 minimal scope).
func splitCypherStatements(body string) []string {
	var out []string
	for raw := range strings.SplitSeq(body, ";") {
		var keep []string
		for line := range strings.SplitSeq(raw, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			keep = append(keep, line)
		}
		if len(keep) > 0 {
			out = append(out, strings.TrimSpace(strings.Join(keep, "\n")))
		}
	}
	return out
}
