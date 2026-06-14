//go:build db_integration && backup_live

// Manual live backup tier. Run with the stack reachable from the test host and
// pg_dump on PATH:
//
//	go test -tags "db_integration backup_live" -count=1 -run TestBackupNetworkPostgresLive ./internal/cron/handlers/
package handlers

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestBackupNetworkPostgresLive(t *testing.T) {
	requireLiveBackupEnv(t, "AURA_DB_MIGRATE_URL")
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)

	summary, err := BackupHandler{Variant: BackupPostgres}.Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("live network backup Run: %v", err)
	}
	if !containsFileWith(dir, "postgres-", ".dump") {
		t.Fatalf("expected a dump artifact in %s, got none (summary: %s)", dir, summary)
	}
}

func requireLiveBackupEnv(t *testing.T, key string) string {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(key))
	if v != "" {
		return v
	}
	msg := key + " unset for live backup test"
	if os.Getenv("CI") != "" {
		t.Fatal(msg)
	}
	t.Skip(msg)
	return ""
}
