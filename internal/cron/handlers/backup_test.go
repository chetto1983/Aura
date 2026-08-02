package handlers

import (
	"context"
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
	// Isolate POSTGRES_HOST so the compose-network DEFAULT is exercised: the CI
	// integration tiers export POSTGRES_HOST=127.0.0.1 (for the host-side DSN), which
	// otherwise leaks in and overrides the default this test asserts.
	t.Setenv("POSTGRES_HOST", "")
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

func TestMissedBackupAlertFiresOnlyPast24h(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	if MissedBackupAlert(BackupPostgres, time.Time{}, now) {
		t.Fatal("a zero missedSince must not alert")
	}
	if MissedBackupAlert(BackupPostgres, now.Add(-12*time.Hour), now) {
		t.Fatal("a 12h miss must not alert")
	}
	if !MissedBackupAlert(BackupPostgres, now.Add(-25*time.Hour), now) {
		t.Fatal("a 25h miss must fire the SC#3 alert")
	}
}

func TestBackupMeta(t *testing.T) {
	t.Parallel()
	pg := BackupHandler{Variant: BackupPostgres}.Meta()
	if pg.Kind != KindBackupPostgres || !pg.ReschedulesOnRecovery {
		t.Fatalf("postgres meta = %+v", pg)
	}
}
