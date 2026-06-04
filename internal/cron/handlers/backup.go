package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Backup container names + retention (D-26). The container names are the compose
// service names (RESEARCH Open Q2: stability is operator-config — the backup tests
// are Manual-Only / t.Fatal-under-CI because of it).
const (
	pgContainer    = "aura-postgres"
	neo4jContainer = "aura-neo4j"

	backupRetentionPostgres = 14 * 24 * time.Hour // PRD 14d rolling
	backupRetentionNeo4j    = 7 * 24 * time.Hour  // PRD 7d rolling

	backupMaxDuration = 30 * time.Minute // a dump can be slow; bound it generously

	// missedBackupAlertAfter is the SC#3 window: a nightly backup still missed this
	// long after its scheduled fire emits an alert log line (the user is not watching
	// a daemon; the alert is the repudiation trail, D-20).
	missedBackupAlertAfter = 24 * time.Hour
)

// BackupVariant selects the database a BackupHandler dumps.
type BackupVariant string

// The two backup variants: a Postgres pg_dump and a Neo4j neo4j-admin dump (D-26).
const (
	BackupPostgres BackupVariant = "postgres"
	BackupNeo4j    BackupVariant = "neo4j"
)

// BackupHandler dumps a database via `docker exec` with a FIXED argv (D-26): the
// argv is operator-config constants, NEVER model output, so there is no injection
// surface (T-10-16); docker is LookPath-gated and the socket is NEVER mounted
// (T-10-15). The dump lands in AURA_BACKUP_DIR (default ~/.aura/backups/) and a
// rolling retention sweep prunes old dumps.
//
// OPERATOR PRECONDITION (WR-04): `docker exec ... pg_dump -f <dest>` writes <dest>
// INSIDE the database container's filesystem, not on the host. For the dump to
// survive on the host at AURA_BACKUP_DIR, the operator MUST bind-mount the host
// AURA_BACKUP_DIR into the postgres/neo4j containers at the SAME path. Run verifies
// the artifact exists on the host after the dump and returns a terminal error if it
// does not — so a missing bind-mount fails loudly instead of reporting a misleading
// "ok" for a file that landed only in the container's ephemeral FS.
type BackupHandler struct {
	Variant BackupVariant
	// dockerCLI is the resolved `docker` path; empty means resolve via LookPath at
	// Run time. The seam lets tests inject a fake CLI without touching PATH.
	dockerCLI string
}

// Meta declares the backup contract: a missed nightly backup DOES reschedule on
// recovery (D-18) and the wall budget is generous (a dump is I/O-bound).
func (h BackupHandler) Meta() HandlerMeta {
	kind := KindBackupPostgres
	if h.Variant == BackupNeo4j {
		kind = KindBackupNeo4j
	}
	return HandlerMeta{Kind: kind, MaxDuration: backupMaxDuration, ReschedulesOnRecovery: true}
}

// Run resolves docker (LookPath-gated), runs the fixed-argv dump into AURA_BACKUP_DIR,
// sweeps the retention window, and returns a summary capturing the dump path + exit
// status. A docker-absent host is a terminal error the dispatcher records + notifies
// (D-21) — never a silent no-op.
func (h BackupHandler) Run(ctx context.Context, job Job) (string, error) {
	// SC#3: a boot catch-up run for a backup still missed past the 24h window emits
	// an alert (D-18). job.MissedSince is non-zero only on a catch-up run; a normal
	// on-time run never alerts.
	MissedBackupAlert(h.Variant, job.MissedSince, time.Now().UTC())

	docker, err := h.resolveDocker()
	if err != nil {
		return "", err
	}

	dir, err := backupDir()
	if err != nil {
		return "", err
	}
	if mkErr := os.MkdirAll(dir, 0o750); mkErr != nil {
		return "", fmt.Errorf("backup: mkdir %s: %w", dir, mkErr)
	}

	dest := filepath.Join(dir, h.dumpFilename(time.Now().UTC()))
	args := h.dumpArgv(dest)

	runCtx, cancel := context.WithTimeout(ctx, backupMaxDuration)
	defer cancel()

	// G204: docker + the argv are operator-config constants (container names + dump
	// flags), NEVER model output (T-10-16). The socket is never mounted (T-10-15).
	cmd := exec.CommandContext(runCtx, docker, args...) //nolint:gosec
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return "", fmt.Errorf("backup %s: docker exec failed: %w: %s", h.Variant, runErr, strings.TrimSpace(string(out)))
	}

	// docker exec wrote dest inside the container; verify it actually materialized on
	// the host (the bind-mount precondition). A missing artifact is a terminal error,
	// never a misleading "ok" for a dump the host does not have (WR-04).
	if _, statErr := os.Stat(dest); statErr != nil {
		return "", fmt.Errorf("backup %s: dump not found on host at %s after docker exec "+
			"(bind-mount AURA_BACKUP_DIR into the %s container at the same path): %w",
			h.Variant, dest, h.containerName(), statErr)
	}

	swept := sweepRetention(dir, h.filePrefix(), h.retention())
	return fmt.Sprintf("backup %s ok → %s (pruned %d old dump(s))", h.Variant, dest, swept), nil
}

// containerName is the compose service the variant dumps from (for the WR-04
// missing-artifact error message).
func (h BackupHandler) containerName() string {
	if h.Variant == BackupNeo4j {
		return neo4jContainer
	}
	return pgContainer
}

// resolveDocker returns the injected CLI or LookPath-resolves `docker`. A missing
// docker binary is a terminal error (the backup cannot proceed) — surfaced, not
// swallowed (D-21).
func (h BackupHandler) resolveDocker() (string, error) {
	if h.dockerCLI != "" {
		return h.dockerCLI, nil
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		return "", fmt.Errorf("backup %s: docker not found on PATH: %w", h.Variant, err)
	}
	return docker, nil
}

// dumpArgv builds the FIXED docker-exec argv for the variant (D-26): pg_dump -Fc for
// Postgres, neo4j-admin database dump for Neo4j. dest is the in-container/host path
// the dump streams to; for Postgres -f writes inside the container's mounted volume
// space — here the redirect is via the dump file path the operator's volume maps. The
// argv carries NO interpolated payload — only the constant container + flags + the
// app-computed dest path.
func (h BackupHandler) dumpArgv(dest string) []string {
	if h.Variant == BackupNeo4j {
		return []string{"exec", neo4jContainer, "neo4j-admin", "database", "dump", "neo4j", "--to-path", dest}
	}
	return []string{"exec", pgContainer, "pg_dump", "-U", "aura_migrate", "-Fc", "-f", dest, "aura"}
}

// dumpFilename builds the timestamped dump filename for the variant.
func (h BackupHandler) dumpFilename(t time.Time) string {
	return fmt.Sprintf("%s-%s.dump", h.filePrefix(), t.Format("20060102T150405Z"))
}

// filePrefix is the per-variant retention-sweep prefix.
func (h BackupHandler) filePrefix() string {
	if h.Variant == BackupNeo4j {
		return "neo4j"
	}
	return "postgres"
}

// retention is the per-variant rolling window (D-26: 14d Postgres, 7d Neo4j).
func (h BackupHandler) retention() time.Duration {
	if h.Variant == BackupNeo4j {
		return backupRetentionNeo4j
	}
	return backupRetentionPostgres
}

// backupDir resolves AURA_BACKUP_DIR (default ~/.aura/backups/), expanding a leading
// ~ to the user home (the env-catalog default uses the ~ form).
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

// sweepRetention deletes dumps older than window in dir matching prefix, returning
// the count pruned. It is best-effort: an un-deletable file is logged and skipped
// (the dump itself already succeeded — a stuck old file must not fail the backup).
func sweepRetention(dir, prefix string, window time.Duration) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-window)
	pruned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix+"-") || !strings.HasSuffix(e.Name(), ".dump") {
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

// MissedBackupAlert emits an alert log line when a nightly backup is STILL missed
// past the SC#3 window after catch-up (missedSince is the original slipped fire,
// now is the current time). It returns true when an alert fired (so the dispatcher
// can also ride it through the Notifier, D-21). A zero missedSince (the task was not
// missed) never alerts.
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
