package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	backupRetentionPostgres = 14 * 24 * time.Hour
	backupRetentionNeo4j    = 7 * 24 * time.Hour

	backupMaxDuration = 30 * time.Minute

	// missedBackupAlertAfter is the SC#3 window: a nightly backup still missed this
	// long after its scheduled fire emits an alert log line.
	missedBackupAlertAfter = 24 * time.Hour
)

const neo4jExportCypherAll = `
CALL apoc.export.cypher.all(null, {
  stream: true,
  streamStatements: true,
  batchSize: 1000,
  format: 'cypher-shell'
})
YIELD cypherStatements
RETURN cypherStatements
`

// BackupVariant selects the database a BackupHandler dumps.
type BackupVariant string

const (
	// BackupPostgres selects the Postgres pg_dump backup.
	BackupPostgres BackupVariant = "postgres"
	// BackupNeo4j selects the Neo4j APOC Cypher export backup.
	BackupNeo4j BackupVariant = "neo4j"
)

type pgDumper func(context.Context, postgresDumpRequest) error
type neo4jDumper func(context.Context, neo4jDumpRequest) error

// BackupHandler dumps a database from inside the Aura box without a Docker socket.
//
// Postgres uses network pg_dump against the migrate-role DSN. Neo4j uses APOC over
// Bolt and writes cypher-shell-compatible statements. Both variants write directly
// into AURA_BACKUP_DIR, which compose bind-mounts to the host-visible backup dir.
type BackupHandler struct {
	Variant     BackupVariant
	pgDumper    pgDumper
	neo4jDumper neo4jDumper
}

// Meta declares the backup contract: a missed nightly backup reschedules on
// recovery and the wall budget is generous because dumps are I/O-bound.
func (h BackupHandler) Meta() HandlerMeta {
	kind := KindBackupPostgres
	if h.Variant == BackupNeo4j {
		kind = KindBackupNeo4j
	}
	return HandlerMeta{Kind: kind, MaxDuration: backupMaxDuration, ReschedulesOnRecovery: true}
}

// Run executes the fixed backup path, verifies the host-visible artifact exists,
// sweeps old dumps, and returns a concise operator summary.
func (h BackupHandler) Run(ctx context.Context, job Job) (string, error) {
	MissedBackupAlert(h.Variant, job.MissedSince, time.Now().UTC())

	dir, err := backupDir()
	if err != nil {
		return "", err
	}
	if mkErr := os.MkdirAll(dir, 0o750); mkErr != nil {
		return "", fmt.Errorf("backup: mkdir %s: %w", dir, mkErr)
	}

	dest := filepath.Join(dir, h.dumpFilename(time.Now().UTC()))
	runCtx, cancel := context.WithTimeout(ctx, backupMaxDuration)
	defer cancel()

	if err := h.dump(runCtx, dest); err != nil {
		return "", err
	}
	if _, statErr := os.Stat(dest); statErr != nil {
		return "", fmt.Errorf("backup %s: dump not found at %s after backup: %w", h.Variant, dest, statErr)
	}

	swept := sweepRetention(dir, h.filePrefix(), h.retention())
	return fmt.Sprintf("backup %s ok -> %s (pruned %d old dump(s))", h.Variant, dest, swept), nil
}

func (h BackupHandler) dump(ctx context.Context, dest string) error {
	if h.Variant == BackupNeo4j {
		req, err := neo4jDumpRequestFromEnv(dest)
		if err != nil {
			return err
		}
		dump := h.neo4jDumper
		if dump == nil {
			dump = defaultNeo4jDumper
		}
		if err := dump(ctx, req); err != nil {
			return fmt.Errorf("backup neo4j: APOC export failed: %w", err)
		}
		return nil
	}

	req, err := postgresDumpRequestFromEnv(dest)
	if err != nil {
		return err
	}
	dump := h.pgDumper
	if dump == nil {
		dump = defaultPostgresDumper
	}
	if err := dump(ctx, req); err != nil {
		return fmt.Errorf("backup postgres: pg_dump failed: %w", err)
	}
	return nil
}

type postgresDumpRequest struct {
	Dest     string
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

func (r postgresDumpRequest) argv() []string {
	return []string{
		"-h", r.Host,
		"-p", r.Port,
		"-U", r.User,
		"-Fc",
		"-f", r.Dest,
		"-d", r.Database,
	}
}

func postgresDumpRequestFromEnv(dest string) (postgresDumpRequest, error) {
	if raw := strings.TrimSpace(os.Getenv("AURA_DB_MIGRATE_URL")); raw != "" {
		return postgresDumpRequestFromURL(dest, raw)
	}

	req := postgresDumpRequest{
		Dest:     dest,
		Host:     envOr("POSTGRES_HOST", "postgres"),
		Port:     envOr("POSTGRES_PORT", "5432"),
		User:     envOr("AURA_DB_MIGRATE_ROLE", "aura_migrate"),
		Password: os.Getenv("POSTGRES_PASSWORD"),
		Database: envOr("POSTGRES_DB", "aura"),
	}
	return req, req.validate()
}

func postgresDumpRequestFromURL(dest, raw string) (postgresDumpRequest, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return postgresDumpRequest{}, fmt.Errorf("backup postgres: parse AURA_DB_MIGRATE_URL: %w", err)
	}
	password, _ := u.User.Password()
	if password == "" {
		password = os.Getenv("POSTGRES_PASSWORD")
	}
	req := postgresDumpRequest{
		Dest:     dest,
		Host:     u.Hostname(),
		Port:     u.Port(),
		User:     u.User.Username(),
		Password: password,
		Database: strings.TrimPrefix(u.Path, "/"),
	}
	if req.Port == "" {
		req.Port = "5432"
	}
	if req.User == "" {
		req.User = envOr("AURA_DB_MIGRATE_ROLE", "aura_migrate")
	}
	if req.Database == "" {
		req.Database = envOr("POSTGRES_DB", "aura")
	}
	return req, req.validate()
}

func (r postgresDumpRequest) validate() error {
	var missing []string
	if strings.TrimSpace(r.Host) == "" {
		missing = append(missing, "host")
	}
	if strings.TrimSpace(r.Port) == "" {
		missing = append(missing, "port")
	}
	if strings.TrimSpace(r.User) == "" {
		missing = append(missing, "user")
	}
	if strings.TrimSpace(r.Password) == "" {
		missing = append(missing, "password")
	}
	if strings.TrimSpace(r.Database) == "" {
		missing = append(missing, "database")
	}
	if len(missing) > 0 {
		return fmt.Errorf("backup postgres: missing %s (set AURA_DB_MIGRATE_URL or POSTGRES_*)", strings.Join(missing, ", "))
	}
	return nil
}

func defaultPostgresDumper(ctx context.Context, req postgresDumpRequest) error {
	// G204: pg_dump and argv are operator-configured infrastructure values, never
	// model output. The password is passed via PGPASSWORD, not argv.
	cmd := exec.CommandContext(ctx, "pg_dump", req.argv()...) //nolint:gosec
	cmd.Env = append(os.Environ(), "PGPASSWORD="+req.Password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

type neo4jDumpRequest struct {
	Dest     string
	BoltURL  string
	User     string
	Password string
	Database string
}

func neo4jDumpRequestFromEnv(dest string) (neo4jDumpRequest, error) {
	req := neo4jDumpRequest{
		Dest:     dest,
		BoltURL:  envOr("AURA_NEO4J_BOLT_URL", "bolt://neo4j:7687"),
		User:     envOr("NEO4J_USER", "neo4j"),
		Password: os.Getenv("NEO4J_PASSWORD"),
		Database: envOr("AURA_NEO4J_DATABASE", "neo4j"),
	}
	return req, req.validate()
}

func (r neo4jDumpRequest) validate() error {
	var missing []string
	if strings.TrimSpace(r.BoltURL) == "" {
		missing = append(missing, "bolt URL")
	}
	if strings.TrimSpace(r.User) == "" {
		missing = append(missing, "user")
	}
	if strings.TrimSpace(r.Password) == "" {
		missing = append(missing, "password")
	}
	if strings.TrimSpace(r.Database) == "" {
		missing = append(missing, "database")
	}
	if len(missing) > 0 {
		return fmt.Errorf("backup neo4j: missing %s (set AURA_NEO4J_BOLT_URL/NEO4J_*)", strings.Join(missing, ", "))
	}
	return nil
}

func defaultNeo4jDumper(ctx context.Context, req neo4jDumpRequest) error {
	driver, err := neo4j.NewDriverWithContext(req.BoltURL, neo4j.BasicAuth(req.User, req.Password, ""))
	if err != nil {
		return fmt.Errorf("neo4j driver init: %w", err)
	}
	defer func() { _ = driver.Close(ctx) }()

	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: req.Database,
		AccessMode:   neo4j.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, neo4jExportCypherAll, nil)
	if err != nil {
		return fmt.Errorf("run apoc export: %w", err)
	}

	var statements []string
	for result.Next(ctx) {
		raw, ok := result.Record().Get("cypherStatements")
		if !ok || raw == nil {
			continue
		}
		stmt, ok := raw.(string)
		if !ok {
			return fmt.Errorf("cypherStatements has type %T, want string", raw)
		}
		statements = append(statements, stmt)
	}
	if err := result.Err(); err != nil {
		return fmt.Errorf("read apoc export: %w", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("consume apoc export: %w", err)
	}
	return writeNeo4jCypherFile(req.Dest, statements)
}

func writeNeo4jCypherFile(dest string, statements []string) error {
	tmp := dest + ".tmp"
	// G304: dest is app-computed from backupDir plus a timestamped filename, not
	// model/user payload, and the backup must write to the operator-selected dir.
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	success := false
	defer func() {
		_ = f.Close()
		if !success {
			_ = os.Remove(tmp)
		}
	}()

	for _, stmt := range statements {
		if _, err := f.WriteString(stmt); err != nil {
			return fmt.Errorf("write %s: %w", tmp, err)
		}
		if !strings.HasSuffix(stmt, "\n") {
			if _, err := f.WriteString("\n"); err != nil {
				return fmt.Errorf("write newline to %s: %w", tmp, err)
			}
		}
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmp, dest, err)
	}
	success = true
	return nil
}

// dumpFilename builds the timestamped dump filename for the variant.
func (h BackupHandler) dumpFilename(t time.Time) string {
	suffix := ".dump"
	if h.Variant == BackupNeo4j {
		suffix = ".cypher"
	}
	return fmt.Sprintf("%s-%s%s", h.filePrefix(), t.Format("20060102T150405Z"), suffix)
}

// filePrefix is the per-variant retention-sweep prefix.
func (h BackupHandler) filePrefix() string {
	if h.Variant == BackupNeo4j {
		return "neo4j"
	}
	return "postgres"
}

// retention is the per-variant rolling window.
func (h BackupHandler) retention() time.Duration {
	if h.Variant == BackupNeo4j {
		return backupRetentionNeo4j
	}
	return backupRetentionPostgres
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// backupDir resolves AURA_BACKUP_DIR (default ~/.aura/backups/), expanding a leading
// ~ to the user home.
func backupDir() (string, error) {
	dir := strings.TrimSpace(os.Getenv("AURA_BACKUP_DIR"))
	if dir == "" {
		dir = filepath.Join("~", ".aura", "backups")
	}
	if strings.HasPrefix(dir, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("backup: resolve home for %q: %w", dir, err)
		}
		dir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), string(os.PathSeparator)))
	}
	return dir, nil
}

// sweepRetention deletes old Postgres .dump files and Neo4j .cypher files. It also
// prunes the legacy Neo4j .dump suffix so old artifacts age out after the model swap.
func sweepRetention(dir, prefix string, window time.Duration) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-window)
	pruned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix+"-") || !isBackupArtifact(e.Name()) {
			continue
		}
		info, infoErr := e.Info()
		if infoErr != nil || info.ModTime().After(cutoff) {
			continue
		}
		if rmErr := os.Remove(filepath.Join(dir, e.Name())); rmErr != nil {
			slog.Warn("backup retention sweep: remove failed", "file", e.Name(), "err", rmErr)
			continue
		}
		pruned++
	}
	return pruned
}

func isBackupArtifact(name string) bool {
	return strings.HasSuffix(name, ".dump") || strings.HasSuffix(name, ".cypher")
}

// MissedBackupAlert emits an alert log line when a nightly backup is still missed
// past the SC#3 window after catch-up. A zero missedSince never alerts.
func MissedBackupAlert(variant BackupVariant, missedSince, now time.Time) bool {
	if missedSince.IsZero() {
		return false
	}
	if now.Sub(missedSince) < missedBackupAlertAfter {
		return false
	}
	slog.Warn("backup missed past the alert window",
		"variant", variant, "missed_since", missedSince.UTC(), "overdue", now.Sub(missedSince))
	return true
}
