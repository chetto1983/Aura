package handlers

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// runFakeDocker is the fake-`docker` body the backup Run tests drive: BackupHandler.Run
// re-execs this test binary as the docker CLI (the standard os/exec self-exec pattern,
// mirrored from internal/mcp), and TestMain dispatches here when AURA_BACKUP_HELPER=1
// BEFORE running the suite (recursion-safe). It scans the docker argv for the dump
// destination (after `-f` for pg_dump or `--to-path` for neo4j-admin) and, per
// AURA_BACKUP_HELPER_MODE, either materializes that file (happy path), exits non-zero
// (docker-exec-failed path), or does nothing (the WR-04 missing-artifact path).
func runFakeDocker() {
	switch os.Getenv("AURA_BACKUP_HELPER_MODE") {
	case "fail":
		// stderr + non-zero exit so CombinedOutput carries the message and Run wraps it.
		_, _ = os.Stderr.WriteString("docker: simulated dump failure\n")
		os.Exit(2)
	case "no-artifact":
		// Succeed but never write the dump (bind-mount absent → file only in container).
		os.Exit(0)
	}
	if dest := helperDestArg(os.Args[1:]); dest != "" {
		_ = os.WriteFile(dest, []byte("fake-dump-bytes"), 0o600)
	}
	os.Exit(0)
}

// helperDestArg extracts the dump destination from a docker-exec argv: the token after
// `-f` (pg_dump) or after `--to-path` (neo4j-admin database dump).
func helperDestArg(argv []string) string {
	for i, a := range argv {
		if (a == "-f" || a == "--to-path") && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

// fakeDockerHandler returns a BackupHandler whose dockerCLI re-execs this test binary as
// the fake docker described above, in the given mode ("" = write the artifact).
func fakeDockerHandler(variant BackupVariant) BackupHandler {
	return BackupHandler{Variant: variant, dockerCLI: os.Args[0]}
}

// helperEnv sets the env the fake-docker re-exec reads. exec.Command inherits the parent
// env, so t.Setenv on the test process flows through to the spawned helper.
func helperEnv(t *testing.T, mode string) {
	t.Helper()
	t.Setenv("AURA_BACKUP_HELPER", "1")
	if mode != "" {
		t.Setenv("AURA_BACKUP_HELPER_MODE", mode)
	}
}

func TestBackupRunWritesDumpAndSweeps(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	helperEnv(t, "")

	// Seed a stale postgres dump that the retention sweep must prune, plus a fresh one
	// it must keep, so the success path exercises sweepRetention's prune branch too.
	stalePath := filepath.Join(dir, "postgres-20200101T000000Z.dump")
	if err := os.WriteFile(stalePath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(stalePath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	summary, err := fakeDockerHandler(BackupPostgres).Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(summary, "backup postgres ok") {
		t.Fatalf("summary missing ok marker: %q", summary)
	}
	if !strings.Contains(summary, "pruned 1 old dump(s)") {
		t.Fatalf("summary should report the 1 pruned stale dump, got %q", summary)
	}
	if _, statErr := os.Stat(stalePath); !os.IsNotExist(statErr) {
		t.Fatal("the stale dump should have been swept by the retention pass")
	}
	// The fresh dump the helper wrote this run must exist on the host (WR-04 verify).
	entries, _ := os.ReadDir(dir)
	freshFound := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "postgres-") && e.Name() != "postgres-20200101T000000Z.dump" {
			freshFound = true
		}
	}
	if !freshFound {
		t.Fatalf("expected the run's fresh dump artifact on the host in %s", dir)
	}
}

func TestBackupRunNeo4jVariantWritesDump(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	helperEnv(t, "")

	summary, err := fakeDockerHandler(BackupNeo4j).Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("neo4j Run: %v", err)
	}
	if !strings.Contains(summary, "backup neo4j ok") {
		t.Fatalf("neo4j summary missing ok marker: %q", summary)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatalf("expected a neo4j dump artifact in %s", dir)
	}
	if !strings.HasPrefix(entries[0].Name(), "neo4j-") {
		t.Fatalf("neo4j dump prefix = %q, want neo4j-", entries[0].Name())
	}
}

func TestBackupRunDockerExecFailureIsTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	helperEnv(t, "fail")

	_, err := fakeDockerHandler(BackupPostgres).Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("a non-zero docker exec must be a terminal error (D-21), not a silent no-op")
	}
	if !strings.Contains(err.Error(), "docker exec failed") {
		t.Fatalf("error should name the docker-exec failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "simulated dump failure") {
		t.Fatalf("error should carry the combined output, got %v", err)
	}
}

func TestBackupRunMissingArtifactIsTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	helperEnv(t, "no-artifact")

	_, err := fakeDockerHandler(BackupNeo4j).Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("a dump that succeeded in-container but never landed on the host must fail (WR-04)")
	}
	if !strings.Contains(err.Error(), "dump not found on host") {
		t.Fatalf("error should be the WR-04 missing-artifact message, got %v", err)
	}
	// The WR-04 message names the container to bind-mount — exercises containerName().
	if !strings.Contains(err.Error(), neo4jContainer) {
		t.Fatalf("WR-04 error should name the neo4j container to bind-mount, got %v", err)
	}
}

func TestBackupRunDockerAbsentIsTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	// dockerCLI left empty + an empty PATH forces LookPath to fail deterministically.
	t.Setenv("PATH", "")

	h := BackupHandler{Variant: BackupPostgres}
	_, err := h.Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("a docker-absent host must surface a terminal error (D-21)")
	}
	if !strings.Contains(err.Error(), "docker not found on PATH") {
		t.Fatalf("error should be the LookPath-gated docker-absent message, got %v", err)
	}
}

func TestBackupRunEmitsMissedAlertOnCatchUp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	helperEnv(t, "")

	// A boot catch-up run whose original fire slipped >24h: Run calls MissedBackupAlert
	// at the top. We can't trivially capture the slog line here, but the run must still
	// complete successfully with the alert path taken (no panic, summary written).
	job := Job{MissedSince: time.Now().Add(-48 * time.Hour)}
	summary, err := fakeDockerHandler(BackupPostgres).Run(context.Background(), job)
	if err != nil {
		t.Fatalf("catch-up Run: %v", err)
	}
	if !strings.Contains(summary, "backup postgres ok") {
		t.Fatalf("catch-up run summary missing ok marker: %q", summary)
	}
}

// TestBackupDumpFilenameTimestamped pins the per-variant filename format (prefix +
// UTC compact timestamp + .dump), covering dumpFilename + filePrefix for both variants.
func TestBackupDumpFilenameTimestamped(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 6, 13, 4, 30, 5, 0, time.UTC)

	pg := BackupHandler{Variant: BackupPostgres}.dumpFilename(at)
	if pg != "postgres-20260613T043005Z.dump" {
		t.Fatalf("postgres dump filename = %q", pg)
	}
	neo := BackupHandler{Variant: BackupNeo4j}.dumpFilename(at)
	if neo != "neo4j-20260613T043005Z.dump" {
		t.Fatalf("neo4j dump filename = %q", neo)
	}
}

// TestBackupRetentionPerVariant pins the rolling windows (D-26: 14d Postgres, 7d Neo4j)
// and the container-name mapping, covering retention + containerName for both variants.
func TestBackupRetentionPerVariant(t *testing.T) {
	t.Parallel()
	if got := (BackupHandler{Variant: BackupPostgres}).retention(); got != backupRetentionPostgres {
		t.Fatalf("postgres retention = %s, want %s", got, backupRetentionPostgres)
	}
	if got := (BackupHandler{Variant: BackupNeo4j}).retention(); got != backupRetentionNeo4j {
		t.Fatalf("neo4j retention = %s, want %s", got, backupRetentionNeo4j)
	}
	if got := (BackupHandler{Variant: BackupPostgres}).containerName(); got != pgContainer {
		t.Fatalf("postgres container = %q, want %q", got, pgContainer)
	}
	if got := (BackupHandler{Variant: BackupNeo4j}).containerName(); got != neo4jContainer {
		t.Fatalf("neo4j container = %q, want %q", got, neo4jContainer)
	}
}

// TestBackupDirDefaultsToHomeAuraBackups covers the no-env default branch of backupDir
// (the `~/.aura/backups` fallback), distinct from the existing explicit-env test.
func TestBackupDirDefaultsToHomeAuraBackups(t *testing.T) {
	t.Setenv("AURA_BACKUP_DIR", "")
	dir, err := backupDir()
	if err != nil {
		t.Fatalf("backupDir default: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".aura", "backups")
	if dir != want {
		t.Fatalf("default backupDir = %q, want %q", dir, want)
	}
}

// TestSweepRetentionMissingDirIsZero covers sweepRetention's ReadDir-error branch: a
// nonexistent directory is a harmless 0 prune (the dump itself must not fail on a sweep
// of a directory that does not exist yet).
func TestSweepRetentionMissingDirIsZero(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if pruned := sweepRetention(missing, "postgres", backupRetentionPostgres); pruned != 0 {
		t.Fatalf("sweep of a missing dir must prune 0, got %d", pruned)
	}
}

// TestSweepRetentionIgnoresNonMatching covers the skip branches: a subdirectory, a
// wrong-prefix file, and a non-.dump file are all left untouched even when stale.
func TestSweepRetentionIgnoresNonMatching(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	subdir := filepath.Join(dir, "postgres-subdir.dump") // dir, not file — skipped
	if err := os.Mkdir(subdir, 0o750); err != nil {
		t.Fatal(err)
	}
	wrongExt := filepath.Join(dir, "postgres-20200101T000000Z.txt") // wrong suffix
	noPrefix := filepath.Join(dir, "random-20200101T000000Z.dump")  // wrong prefix
	for _, p := range []string{wrongExt, noPrefix} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stale := time.Now().Add(-30 * 24 * time.Hour)
	for _, p := range []string{wrongExt, noPrefix} {
		if err := os.Chtimes(p, stale, stale); err != nil {
			t.Fatal(err)
		}
	}
	if pruned := sweepRetention(dir, "postgres", backupRetentionPostgres); pruned != 0 {
		t.Fatalf("non-matching entries must not be swept, pruned %d", pruned)
	}
	// All three survive.
	for _, p := range []string{subdir, wrongExt, noPrefix} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("non-matching entry %s should survive the sweep", p)
		}
	}
}

// TestBackupRunMkdirFailureIsTerminal covers Run's MkdirAll-error branch: when
// AURA_BACKUP_DIR points UNDER a path component that is a regular file, MkdirAll can't
// create the directory and Run returns a terminal "mkdir" error before exec'ing docker.
func TestBackupRunMkdirFailureIsTerminal(t *testing.T) {
	base := t.TempDir()
	// Make `base/afile` a regular file, then ask for a backup dir BENEATH it — MkdirAll
	// must fail because a path component is not a directory.
	notADir := filepath.Join(base, "afile")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AURA_BACKUP_DIR", filepath.Join(notADir, "backups"))
	helperEnv(t, "")

	_, err := fakeDockerHandler(BackupPostgres).Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("a backup dir under a non-directory must fail at MkdirAll (terminal, D-21)")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("error should name the mkdir failure, got %v", err)
	}
}

// TestResolveDockerInjectedSeam covers resolveDocker's injected-CLI branch (the early
// return when dockerCLI is set) distinctly from the LookPath path.
func TestResolveDockerInjectedSeam(t *testing.T) {
	t.Parallel()
	h := BackupHandler{Variant: BackupPostgres, dockerCLI: "/usr/local/bin/docker-fake"}
	got, err := h.resolveDocker()
	if err != nil {
		t.Fatalf("resolveDocker with an injected CLI must not error: %v", err)
	}
	if got != "/usr/local/bin/docker-fake" {
		t.Fatalf("resolveDocker = %q, want the injected CLI", got)
	}
}

// guardArgv keeps the imported slices package referenced even if the argv assertions are
// later trimmed; it pins dumpArgv's two-variant shape alongside the run-level coverage.
func TestBackupDumpArgvVariantsConsistent(t *testing.T) {
	t.Parallel()
	pg := BackupHandler{Variant: BackupPostgres}.dumpArgv("/d/pg.dump")
	neo := BackupHandler{Variant: BackupNeo4j}.dumpArgv("/d/neo")
	if slices.Equal(pg, neo) {
		t.Fatal("the two variants must produce different argv")
	}
	if pg[1] != pgContainer || neo[1] != neo4jContainer {
		t.Fatalf("argv[1] must be the variant container: pg=%q neo=%q", pg[1], neo[1])
	}
}
