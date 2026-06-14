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

func TestPostgresDumpArgvTargetsNetworkHost(t *testing.T) {
	t.Parallel()
	req := postgresDumpRequest{
		Dest:     "/backups/postgres.dump",
		Host:     "postgres",
		Port:     "5432",
		User:     "aura_migrate",
		Password: "secret",
		Database: "aura",
	}
	argv := req.argv()
	want := []string{"-h", "postgres", "-p", "5432", "-U", "aura_migrate", "-Fc", "-f", "/backups/postgres.dump", "-d", "aura"}
	if !slices.Equal(argv, want) {
		t.Fatalf("postgres argv = %v, want %v", argv, want)
	}
	for _, arg := range argv {
		if arg == "docker" || arg == "exec" || strings.Contains(arg, "docker.sock") || strings.Contains(arg, "/var/run/docker") {
			t.Fatalf("postgres backup argv must stay socketless, got %q in %v", arg, argv)
		}
	}
	if strings.Contains(strings.Join(argv, " "), req.Password) {
		t.Fatalf("postgres password must be passed through PGPASSWORD, not argv: %v", argv)
	}
}

func TestPostgresDumpRequestFromURL(t *testing.T) {
	t.Parallel()
	req, err := postgresDumpRequestFromURL(
		"/backups/postgres.dump",
		"postgres://aura_migrate:s3cret@postgres:5432/aura?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("postgresDumpRequestFromURL: %v", err)
	}
	if req.Host != "postgres" || req.Port != "5432" || req.User != "aura_migrate" || req.Password != "s3cret" || req.Database != "aura" {
		t.Fatalf("parsed request = %+v", req)
	}
}

func TestPostgresDumpRequestFromURLFallbacks(t *testing.T) {
	t.Setenv("POSTGRES_PASSWORD", "env-secret")
	t.Setenv("AURA_DB_MIGRATE_ROLE", "env_migrate")
	req, err := postgresDumpRequestFromURL(
		"/backups/postgres.dump",
		"postgres://postgres/aura?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("postgresDumpRequestFromURL fallback: %v", err)
	}
	if req.Port != "5432" || req.User != "env_migrate" || req.Password != "env-secret" {
		t.Fatalf("fallback request = %+v", req)
	}
}

func TestPostgresDumpRequestFromURLRejectsMissingHost(t *testing.T) {
	t.Setenv("POSTGRES_PASSWORD", "env-secret")
	_, err := postgresDumpRequestFromURL("/backups/postgres.dump", "postgres:///aura")
	if err == nil {
		t.Fatal("missing host must fail")
	}
	if !strings.Contains(err.Error(), "missing host") {
		t.Fatalf("error should name missing host, got %v", err)
	}
}

func TestPostgresDumpRequestFromEnvDefaultsToComposeNetwork(t *testing.T) {
	t.Setenv("AURA_DB_MIGRATE_URL", "")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	req, err := postgresDumpRequestFromEnv("/backups/postgres.dump")
	if err != nil {
		t.Fatalf("postgresDumpRequestFromEnv: %v", err)
	}
	if req.Host != "postgres" {
		t.Fatalf("default postgres host = %q, want postgres", req.Host)
	}
	if !slices.Contains(req.argv(), "postgres") {
		t.Fatalf("argv should include compose network host postgres: %v", req.argv())
	}
}

func TestDefaultPostgresDumperMissingBinary(t *testing.T) {
	t.Setenv("PATH", "")
	req := postgresDumpRequest{
		Dest:     "/tmp/postgres.dump",
		Host:     "postgres",
		Port:     "5432",
		User:     "aura_migrate",
		Password: "secret",
		Database: "aura",
	}
	err := defaultPostgresDumper(context.Background(), req)
	if err == nil {
		t.Fatal("missing pg_dump binary must fail")
	}
}

func TestPostgresDumpArgvCarriesNoPayload(t *testing.T) {
	t.Parallel()
	req := postgresDumpRequest{Dest: "/dest/pg.dump", Host: "postgres", Port: "5432", User: "aura_migrate", Password: "pw", Database: "aura"}
	joined := strings.Join(req.argv(), " ")
	for _, evil := range []string{";", "&&", "$(", "`", "rm -rf"} {
		if strings.Contains(joined, evil) {
			t.Fatalf("backup argv must be a clean fixed shape, found %q in %q", evil, joined)
		}
	}
}

func TestNeo4jDumpRequestFromEnvRequiresPassword(t *testing.T) {
	t.Setenv("NEO4J_PASSWORD", "")
	_, err := neo4jDumpRequestFromEnv("/backups/neo4j.cypher")
	if err == nil {
		t.Fatal("missing Neo4j password must fail")
	}
	if !strings.Contains(err.Error(), "missing password") {
		t.Fatalf("error should name missing password, got %v", err)
	}
}

func TestNeo4jDumpRequestFromEnv(t *testing.T) {
	t.Setenv("AURA_NEO4J_BOLT_URL", "bolt://neo4j:7687")
	t.Setenv("NEO4J_USER", "neo4j")
	t.Setenv("NEO4J_PASSWORD", "graph-secret")
	t.Setenv("AURA_NEO4J_DATABASE", "neo4j")

	req, err := neo4jDumpRequestFromEnv("/backups/neo4j.cypher")
	if err != nil {
		t.Fatalf("neo4jDumpRequestFromEnv: %v", err)
	}
	if req.BoltURL != "bolt://neo4j:7687" || req.User != "neo4j" || req.Password != "graph-secret" || req.Database != "neo4j" {
		t.Fatalf("neo4j request = %+v", req)
	}
}

func TestDefaultNeo4jDumperRejectsBadURL(t *testing.T) {
	req := neo4jDumpRequest{
		Dest:     filepathForTest(t, "neo4j.cypher"),
		BoltURL:  "://bad",
		User:     "neo4j",
		Password: "secret",
		Database: "neo4j",
	}
	err := defaultNeo4jDumper(context.Background(), req)
	if err == nil {
		t.Fatal("bad Neo4j URL must fail")
	}
}

func TestDefaultNeo4jDumperConnectionFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req := neo4jDumpRequest{
		Dest:     filepathForTest(t, "neo4j.cypher"),
		BoltURL:  "bolt://127.0.0.1:1",
		User:     "neo4j",
		Password: "secret",
		Database: "neo4j",
	}
	err := defaultNeo4jDumper(ctx, req)
	if err == nil {
		t.Fatal("unreachable Neo4j must fail")
	}
	if !strings.Contains(err.Error(), "run apoc export") {
		t.Fatalf("error should name the APOC run failure, got %v", err)
	}
}

func TestNeo4jExportQueryStreamsCypherShellStatements(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"apoc.export.cypher.all(null",
		"streamStatements: true",
		"format: 'cypher-shell'",
		"cypherStatements",
	} {
		if !strings.Contains(neo4jExportCypherAll, want) {
			t.Fatalf("neo4j export query missing %q:\n%s", want, neo4jExportCypherAll)
		}
	}
}

func TestWriteNeo4jCypherFileWritesStatements(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "neo4j.cypher")
	if err := writeNeo4jCypherFile(dest, []string{"CREATE (:One)", "CREATE (:Two)\n"}); err != nil {
		t.Fatalf("writeNeo4jCypherFile: %v", err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read cypher file: %v", err)
	}
	if got, want := string(b), "CREATE (:One)\nCREATE (:Two)\n"; got != want {
		t.Fatalf("cypher file = %q, want %q", got, want)
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary cypher file should be gone, stat err=%v", err)
	}
}

func TestWriteNeo4jCypherFileAllowsEmptyExport(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "empty.cypher")
	if err := writeNeo4jCypherFile(dest, nil); err != nil {
		t.Fatalf("write empty cypher file: %v", err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read empty cypher file: %v", err)
	}
	if len(b) != 0 {
		t.Fatalf("empty export file length = %d, want 0", len(b))
	}
}

func TestWriteNeo4jCypherFileCreateFailure(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeNeo4jCypherFile(filepath.Join(notADir, "neo4j.cypher"), []string{"CREATE (:One)"})
	if err == nil {
		t.Fatal("writing beneath a non-directory must fail")
	}
	if !strings.Contains(err.Error(), "create") {
		t.Fatalf("error should name create failure, got %v", err)
	}
}

func filepathForTest(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func TestMissedBackupAlertFiresOnlyPast24h(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	if MissedBackupAlert(BackupPostgres, time.Time{}, now) {
		t.Fatal("a zero missedSince must not alert")
	}
	if MissedBackupAlert(BackupPostgres, now.Add(-12*time.Hour), now) {
		t.Fatal("a 12h miss must not alert")
	}
	if !MissedBackupAlert(BackupNeo4j, now.Add(-25*time.Hour), now) {
		t.Fatal("a 25h miss must fire the SC#3 alert")
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
