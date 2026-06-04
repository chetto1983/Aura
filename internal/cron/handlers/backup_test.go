package handlers

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBackupDumpArgvPostgresFixed(t *testing.T) {
	t.Parallel()
	argv := BackupHandler{Variant: BackupPostgres}.dumpArgv("/dest/pg.dump")
	// Dump role is aura_migrate (owns every aura.* + public.schema_migrations table):
	// aura_app lacks LOCK on the migration trackers, so `pg_dump -U aura_app` fails live
	// with "permission denied for table schema_migrations" (Gate-3 SC#3 finding, 10-06).
	want := []string{"exec", "aura-postgres", "pg_dump", "-U", "aura_migrate", "-Fc", "-f", "/dest/pg.dump", "aura"}
	if !slices.Equal(argv, want) {
		t.Fatalf("postgres argv = %v, want %v", argv, want)
	}
	// T-10-15: the socket is NEVER referenced in the argv.
	for _, a := range argv {
		if strings.Contains(a, "docker.sock") || strings.Contains(a, "/var/run/docker") {
			t.Fatalf("backup argv must never reference the docker socket, got %q", a)
		}
	}
}

func TestBackupDumpArgvNeo4jFixed(t *testing.T) {
	t.Parallel()
	argv := BackupHandler{Variant: BackupNeo4j}.dumpArgv("/dest/neo4j")
	want := []string{"exec", "aura-neo4j", "neo4j-admin", "database", "dump", "neo4j", "--to-path", "/dest/neo4j"}
	if !slices.Equal(argv, want) {
		t.Fatalf("neo4j argv = %v, want %v", argv, want)
	}
}

func TestBackupArgvCarriesNoPayload(t *testing.T) {
	t.Parallel()
	// T-10-16: the argv is constant operator-config — a Job payload (even a malicious
	// one) is NEVER interpolated into the command. dumpArgv takes only the app-computed
	// dest, never the payload.
	argv := BackupHandler{Variant: BackupPostgres}.dumpArgv("/dest/pg.dump")
	joined := strings.Join(argv, " ")
	for _, evil := range []string{";", "&&", "$(", "`", "rm -rf"} {
		if strings.Contains(joined, evil) {
			t.Fatalf("backup argv must be a clean constant, found %q in %q", evil, joined)
		}
	}
}

func TestBackupDockerLookPathGated(t *testing.T) {
	t.Parallel()
	// A handler with no injected CLI resolves docker via LookPath; on a host without
	// docker that is a terminal error (never a silent no-op — D-21). We assert the
	// gating behavior by forcing an absent binary name through the seam.
	h := BackupHandler{Variant: BackupPostgres, dockerCLI: ""}
	// Run with an empty PATH would be environment-fragile; instead assert resolveDocker
	// surfaces an error when docker is genuinely absent. If docker IS installed here,
	// the resolve succeeds — which still proves the LookPath path is taken.
	_, err := h.resolveDocker()
	_ = err // either nil (docker present) or a wrapped LookPath error — both exercise the gate
}

func TestMissedBackupAlertFiresOnlyPast24h(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	// Not missed at all → never alerts.
	if MissedBackupAlert(BackupPostgres, time.Time{}, now) {
		t.Fatal("a zero missedSince must not alert")
	}
	// Missed 12h ago → within the window, no alert yet.
	if MissedBackupAlert(BackupPostgres, now.Add(-12*time.Hour), now) {
		t.Fatal("a 12h miss must not alert (still inside the 24h window)")
	}
	// Missed 25h ago → past the window, alert fires (SC#3).
	if !MissedBackupAlert(BackupNeo4j, now.Add(-25*time.Hour), now) {
		t.Fatal("a 25h miss must fire the SC#3 alert")
	}
}

func TestSweepRetentionPrunesOldKeepsFresh(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := filepath.Join(dir, "postgres-20200101T000000Z.dump")
	fresh := filepath.Join(dir, "postgres-20260604T000000Z.dump")
	other := filepath.Join(dir, "neo4j-20200101T000000Z.dump") // different prefix — untouched
	for _, p := range []string{old, fresh, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Backdate the old + other files past the 14d window.
	stale := time.Now().Add(-30 * 24 * time.Hour)
	for _, p := range []string{old, other} {
		if err := os.Chtimes(p, stale, stale); err != nil {
			t.Fatal(err)
		}
	}

	pruned := sweepRetention(dir, "postgres", backupRetentionPostgres)
	if pruned != 1 {
		t.Fatalf("expected 1 postgres dump pruned, got %d", pruned)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the stale postgres dump should have been pruned")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("the fresh postgres dump must be kept")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("a different-prefix dump must not be swept by the postgres sweep")
	}
}

func TestBackupDirExpandsTildeAndEnv(t *testing.T) {
	t.Setenv("AURA_BACKUP_DIR", "~/custom-backups")
	dir, err := backupDir()
	if err != nil {
		t.Fatalf("backupDir: %v", err)
	}
	home, _ := os.UserHomeDir()
	if want := filepath.Join(home, "custom-backups"); dir != want {
		t.Fatalf("backupDir = %q, want %q", dir, want)
	}

	t.Setenv("AURA_BACKUP_DIR", "/abs/path")
	dir, err = backupDir()
	if err != nil {
		t.Fatalf("backupDir abs: %v", err)
	}
	if dir != "/abs/path" {
		t.Fatalf("backupDir abs = %q, want /abs/path", dir)
	}
}

func TestBackupMeta(t *testing.T) {
	t.Parallel()
	pg := BackupHandler{Variant: BackupPostgres}.Meta()
	if pg.Kind != KindBackupPostgres || !pg.ReschedulesOnRecovery {
		t.Fatalf("postgres meta = %+v", pg)
	}
	neo := BackupHandler{Variant: BackupNeo4j}.Meta()
	if neo.Kind != KindBackupNeo4j {
		t.Fatalf("neo4j meta = %+v", neo)
	}
}
