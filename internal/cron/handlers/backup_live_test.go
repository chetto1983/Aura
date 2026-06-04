//go:build backup_live

// Manual-Only live backup tier (RESEARCH Open Q2 — container-name stability).
// Gated behind its own build tag like the other operator tiers (sandbox_integration,
// cot_eval): CI never claims it, so there is no skip-as-green surface. Run with the
// docker stack up:
//
//	go test -tags backup_live -count=1 -run TestBackupDockerExecLive ./internal/cron/handlers/
package handlers

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestBackupDockerExecLive runs the REAL docker exec dump and asserts a dump
// artifact lands in AURA_BACKUP_DIR.
func TestBackupDockerExecLive(t *testing.T) {
	// The dump runs `docker exec ... pg_dump -f <dest>` INSIDE the container, so the
	// dest must be writable in the container AND readable from the host. A plain
	// t.TempDir() (host-only path) is invisible to the container; honour an explicit
	// AURA_BACKUP_DIR (a bind-mounted/volume path) when the operator sets one and only
	// fall back to TempDir for the degenerate same-namespace case (Gate-3 SC#3, 10-06).
	dir := strings.TrimSpace(os.Getenv("AURA_BACKUP_DIR"))
	if dir == "" {
		dir = t.TempDir()
		t.Setenv("AURA_BACKUP_DIR", dir)
	}

	summary, err := BackupHandler{Variant: BackupPostgres}.Run(context.Background(), Job{})
	if err != nil {
		t.Fatalf("live backup Run: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatalf("expected a dump artifact in %s, got none (summary: %s)", dir, summary)
	}
}
