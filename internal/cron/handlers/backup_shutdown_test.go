package handlers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestBackupFailureNeverPromotesPartialArtifact(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	handler := BackupHandler{
		Variant: BackupPostgres,
		pgDumper: func(_ context.Context, req postgresDumpRequest) error {
			if err := os.WriteFile(req.Dest, []byte("partial"), 0o600); err != nil {
				t.Fatal(err)
			}
			return errors.New("dump interrupted")
		},
	}

	if _, err := handler.Run(t.Context(), Job{}); err == nil {
		t.Fatal("Run returned nil for an interrupted dump")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed backup left publishable or partial artifacts: %v", entryNames(entries))
	}
}

func TestBackupCancellationNeverPromotesPartialArtifact(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AURA_BACKUP_DIR", dir)
	t.Setenv("AURA_DB_MIGRATE_URL", "postgres://aura_migrate:test@postgres:5432/aura?sslmode=disable")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	handler := BackupHandler{
		Variant: BackupPostgres,
		pgDumper: func(ctx context.Context, req postgresDumpRequest) error {
			if err := os.WriteFile(req.Dest, []byte("partial"), 0o600); err != nil {
				t.Fatal(err)
			}
			return ctx.Err()
		},
	}

	if _, err := handler.Run(ctx, Job{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want cancellation", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("canceled backup left publishable or partial artifacts: %v", entryNames(entries))
	}
}

func TestSchedulerUnitStopBudgetExceedsLongestBackup(t *testing.T) {
	unit, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "aura-scheduler.service"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^TimeoutStopSec=(\d+)$`).FindSubmatch(unit)
	if len(match) != 2 {
		t.Fatalf("aura-scheduler.service has no numeric TimeoutStopSec: %s", unit)
	}
	seconds, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	if seconds <= int(backupMaxDuration.Seconds()) {
		t.Fatalf("TimeoutStopSec=%d must exceed backup max duration %.0fs", seconds, backupMaxDuration.Seconds())
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
