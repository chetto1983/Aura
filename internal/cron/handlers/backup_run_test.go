package handlers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setBackupEnv(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("AURA_BACKUP_DIR", dir)
	t.Setenv("AURA_DB_MIGRATE_URL", "postgres://aura_migrate:pg-secret@postgres:5432/aura?sslmode=disable")
	t.Setenv("AURA_NEO4J_BOLT_URL", "bolt://neo4j:7687")
	t.Setenv("NEO4J_USER", "neo4j")
	t.Setenv("NEO4J_PASSWORD", "graph-secret")
	t.Setenv("AURA_NEO4J_DATABASE", "neo4j")
}

func fakePostgresHandler(fn func(context.Context, postgresDumpRequest) error) BackupHandler {
	return BackupHandler{Variant: BackupPostgres, pgDumper: fn}
}

func fakeNeo4jHandler(fn func(context.Context, neo4jDumpRequest) error) BackupHandler {
	return BackupHandler{Variant: BackupNeo4j, neo4jDumper: fn}
}

func writePostgresDump(_ context.Context, req postgresDumpRequest) error {
	return os.WriteFile(req.Dest, []byte("fake-postgres-dump"), 0o600)
}

func writeNeo4jDump(_ context.Context, req neo4jDumpRequest) error {
	return os.WriteFile(req.Dest, []byte("CREATE (:AuraBackup);\n"), 0o600)
}

func TestBackupRunWritesPostgresDumpAndSweeps(t *testing.T) {
	dir := t.TempDir()
	setBackupEnv(t, dir)

	stalePath := filepath.Join(dir, "postgres-20200101T000000Z.dump")
	if err := os.WriteFile(stalePath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(stalePath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	var gotReq postgresDumpRequest
	h := fakePostgresHandler(func(ctx context.Context, req postgresDumpRequest) error {
		gotReq = req
		return writePostgresDump(ctx, req)
	})
	summary, err := h.Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotReq.Host != "postgres" {
		t.Fatalf("pg_dump host = %q, want postgres", gotReq.Host)
	}
	joined := strings.Join(gotReq.argv(), " ")
	if !strings.Contains(joined, "-h postgres") {
		t.Fatalf("pg_dump argv should target the compose network host: %v", gotReq.argv())
	}
	if strings.Contains(joined, "docker") || strings.Contains(joined, "exec") {
		t.Fatalf("pg_dump argv must contain no docker exec path: %v", gotReq.argv())
	}
	if !strings.Contains(summary, "backup postgres ok") || !strings.Contains(summary, "pruned 1 old dump(s)") {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if _, statErr := os.Stat(stalePath); !os.IsNotExist(statErr) {
		t.Fatal("the stale dump should have been swept by the retention pass")
	}
	if !containsFileWith(dir, "postgres-", ".dump") {
		t.Fatalf("expected a fresh postgres dump in %s", dir)
	}
}

func TestBackupRunNeo4jVariantWritesCypherDump(t *testing.T) {
	dir := t.TempDir()
	setBackupEnv(t, dir)

	var gotReq neo4jDumpRequest
	h := fakeNeo4jHandler(func(ctx context.Context, req neo4jDumpRequest) error {
		gotReq = req
		return writeNeo4jDump(ctx, req)
	})
	summary, err := h.Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("neo4j Run: %v", err)
	}
	if gotReq.BoltURL != "bolt://neo4j:7687" {
		t.Fatalf("neo4j bolt URL = %q, want bolt://neo4j:7687", gotReq.BoltURL)
	}
	if !strings.Contains(summary, "backup neo4j ok") {
		t.Fatalf("neo4j summary missing ok marker: %q", summary)
	}
	if !containsFileWith(dir, "neo4j-", ".cypher") {
		t.Fatalf("expected a neo4j .cypher artifact in %s", dir)
	}
}

func TestBackupRunDumpFailureIsTerminal(t *testing.T) {
	dir := t.TempDir()
	setBackupEnv(t, dir)

	_, err := fakePostgresHandler(func(context.Context, postgresDumpRequest) error {
		return errors.New("simulated pg_dump failure")
	}).Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("a dump failure must be terminal")
	}
	if !strings.Contains(err.Error(), "pg_dump failed") || !strings.Contains(err.Error(), "simulated pg_dump failure") {
		t.Fatalf("error should carry the pg_dump failure, got %v", err)
	}
}

func TestBackupRunNeo4jDumpFailureIsTerminal(t *testing.T) {
	dir := t.TempDir()
	setBackupEnv(t, dir)

	_, err := fakeNeo4jHandler(func(context.Context, neo4jDumpRequest) error {
		return errors.New("simulated apoc failure")
	}).Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("a Neo4j dump failure must be terminal")
	}
	if !strings.Contains(err.Error(), "APOC export failed") || !strings.Contains(err.Error(), "simulated apoc failure") {
		t.Fatalf("error should carry the APOC failure, got %v", err)
	}
}

func TestBackupRunMissingArtifactIsTerminal(t *testing.T) {
	dir := t.TempDir()
	setBackupEnv(t, dir)

	_, err := fakeNeo4jHandler(func(context.Context, neo4jDumpRequest) error {
		return nil
	}).Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("a dump that never lands in AURA_BACKUP_DIR must fail")
	}
	if !strings.Contains(err.Error(), "dump not found at") {
		t.Fatalf("error should name the missing artifact, got %v", err)
	}
}

func TestBackupRunMissingConfigIsTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	t.Setenv("AURA_DB_MIGRATE_URL", "")
	t.Setenv("POSTGRES_PASSWORD", "")

	_, err := fakePostgresHandler(writePostgresDump).Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("missing Postgres dump config must fail")
	}
	if !strings.Contains(err.Error(), "missing password") {
		t.Fatalf("error should name missing password, got %v", err)
	}
}

func TestBackupRunNeo4jMissingConfigIsTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	t.Setenv("NEO4J_PASSWORD", "")

	_, err := fakeNeo4jHandler(writeNeo4jDump).Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("missing Neo4j dump config must fail")
	}
	if !strings.Contains(err.Error(), "missing password") {
		t.Fatalf("error should name missing password, got %v", err)
	}
}

func TestBackupRunEmitsMissedAlertOnCatchUp(t *testing.T) {
	dir := t.TempDir()
	setBackupEnv(t, dir)

	job := Job{MissedSince: time.Now().Add(-48 * time.Hour)}
	summary, err := fakePostgresHandler(writePostgresDump).Run(context.Background(), job)
	if err != nil {
		t.Fatalf("catch-up Run: %v", err)
	}
	if !strings.Contains(summary, "backup postgres ok") {
		t.Fatalf("catch-up run summary missing ok marker: %q", summary)
	}
}

func TestBackupDumpFilenameTimestamped(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 6, 13, 4, 30, 5, 0, time.UTC)

	pg := BackupHandler{Variant: BackupPostgres}.dumpFilename(at)
	if pg != "postgres-20260613T043005Z.dump" {
		t.Fatalf("postgres dump filename = %q", pg)
	}
	neo := BackupHandler{Variant: BackupNeo4j}.dumpFilename(at)
	if neo != "neo4j-20260613T043005Z.cypher" {
		t.Fatalf("neo4j dump filename = %q", neo)
	}
}

func TestBackupRetentionPerVariant(t *testing.T) {
	t.Parallel()
	if got := (BackupHandler{Variant: BackupPostgres}).retention(); got != backupRetentionPostgres {
		t.Fatalf("postgres retention = %s, want %s", got, backupRetentionPostgres)
	}
	if got := (BackupHandler{Variant: BackupNeo4j}).retention(); got != backupRetentionNeo4j {
		t.Fatalf("neo4j retention = %s, want %s", got, backupRetentionNeo4j)
	}
}

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

func TestSweepRetentionPrunesOldKeepsFresh(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := filepath.Join(dir, "postgres-20200101T000000Z.dump")
	fresh := filepath.Join(dir, "postgres-20260604T000000Z.dump")
	other := filepath.Join(dir, "neo4j-20200101T000000Z.cypher")
	for _, p := range []string{old, fresh, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
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

func TestSweepRetentionPrunesLegacyDumpAndCypherSuffixes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	oldDump := filepath.Join(dir, "neo4j-20200101T000000Z.dump")
	oldCypher := filepath.Join(dir, "neo4j-20200102T000000Z.cypher")
	for _, p := range []string{oldDump, oldCypher} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, time.Now().Add(-30*24*time.Hour), time.Now().Add(-30*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if pruned := sweepRetention(dir, "neo4j", backupRetentionNeo4j); pruned != 2 {
		t.Fatalf("neo4j sweep should prune legacy .dump and current .cypher, got %d", pruned)
	}
}

func TestSweepRetentionMissingDirIsZero(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if pruned := sweepRetention(missing, "postgres", backupRetentionPostgres); pruned != 0 {
		t.Fatalf("sweep of a missing dir must prune 0, got %d", pruned)
	}
}

func TestSweepRetentionIgnoresNonMatching(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	subdir := filepath.Join(dir, "postgres-subdir.dump")
	if err := os.Mkdir(subdir, 0o750); err != nil {
		t.Fatal(err)
	}
	wrongExt := filepath.Join(dir, "postgres-20200101T000000Z.txt")
	noPrefix := filepath.Join(dir, "random-20200101T000000Z.dump")
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
	for _, p := range []string{subdir, wrongExt, noPrefix} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("non-matching entry %s should survive the sweep", p)
		}
	}
}

func TestBackupRunMkdirFailureIsTerminal(t *testing.T) {
	base := t.TempDir()
	notADir := filepath.Join(base, "afile")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AURA_BACKUP_DIR", filepath.Join(notADir, "backups"))

	_, err := fakePostgresHandler(writePostgresDump).Run(context.Background(), Job{})
	if err == nil {
		t.Fatal("a backup dir under a non-directory must fail at MkdirAll")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("error should name the mkdir failure, got %v", err)
	}
}

func containsFileWith(dir, prefix, suffix string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), suffix) {
			return true
		}
	}
	return false
}
