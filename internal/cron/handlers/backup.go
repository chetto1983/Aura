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
)

const (
	backupRetentionPostgres = 14 * 24 * time.Hour

	backupMaxDuration = 30 * time.Minute

	// missedBackupAlertAfter is the SC#3 window: a nightly backup still missed this
	// long after its scheduled fire emits an alert log line.
	missedBackupAlertAfter = 24 * time.Hour
)

// BackupVariant selects the database a BackupHandler dumps.
//
// Postgres is the only one. ArcadeDB is NOT backed up by anything: memory lives in
// one database per identity (internal/arcadedb/tenant.go) and no handler dumps them.
type BackupVariant string

// BackupPostgres selects the Postgres pg_dump backup.
const BackupPostgres BackupVariant = "postgres"

type pgDumper func(context.Context, postgresDumpRequest) error

// BackupHandler dumps a database from inside the Aura box without a Docker socket.
//
// It runs network pg_dump against the migrate-role DSN and promotes a completed
// sibling partial into AURA_BACKUP_DIR atomically.
type BackupHandler struct {
	Variant  BackupVariant
	pgDumper pgDumper
}

// Meta declares the backup contract: a missed nightly backup reschedules on
// recovery and the wall budget is generous because dumps are I/O-bound.
func (h BackupHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindBackupPostgres, MaxDuration: backupMaxDuration, ReschedulesOnRecovery: true}
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
	partial, err := createBackupPartial(dir, filepath.Base(dest))
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(partial) }()
	runCtx, cancel := context.WithTimeout(ctx, backupMaxDuration)
	defer cancel()

	if err := h.dump(runCtx, partial); err != nil {
		return "", err
	}
	if _, statErr := os.Stat(partial); statErr != nil {
		return "", fmt.Errorf("backup %s: dump not found at %s after backup: %w", h.Variant, partial, statErr)
	}
	if err := os.Rename(partial, dest); err != nil {
		return "", fmt.Errorf("backup %s: promote partial artifact: %w", h.Variant, err)
	}

	swept := sweepRetention(dir, h.filePrefix(), h.retention())
	return fmt.Sprintf("backup %s ok -> %s (pruned %d old dump(s))", h.Variant, dest, swept), nil
}

func createBackupPartial(dir, finalName string) (string, error) {
	file, err := os.CreateTemp(dir, "."+finalName+"-*.partial")
	if err != nil {
		return "", fmt.Errorf("backup: create partial artifact: %w", err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("backup: close partial artifact: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return "", fmt.Errorf("backup: prepare partial artifact: %w", err)
	}
	return name, nil
}

func (h BackupHandler) dump(ctx context.Context, dest string) error {
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

// dumpFilename builds the timestamped dump filename for the variant.
func (h BackupHandler) dumpFilename(t time.Time) string {
	return fmt.Sprintf("%s-%s.dump", h.filePrefix(), t.Format("20060102T150405Z"))
}

// filePrefix is the per-variant retention-sweep prefix.
func (h BackupHandler) filePrefix() string {
	return "postgres"
}

// retention is the per-variant rolling window.
func (h BackupHandler) retention() time.Duration {
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

// sweepRetention deletes old Postgres .dump files. It also matches the retired
// .cypher suffix so exports left in AURA_BACKUP_DIR by an earlier deployment age
// out instead of being stranded there forever by a prefix nothing writes any more.
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
