//go:build sandbox_integration && db_integration

// Live by-path snippet exec E2E (SC#4): save -> activate -> materialize into the ro
// /skills mount -> exec BY PATH via the real sandbox-agent client -> output captured
// -> usage stamped. It exercises the FULL 7e snippet runtime against the live stack
// (the token + the /skills mount + the sandbox-agent container), not a fake.
//
// Requires the stack up AND the export dir wired to the container's /skills mount:
//
//	make sandbox-up                                   # /skills mount
//	export AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468
//	export AURA_SKILL_EXPORT_DIR=<host dir mounted at /skills in the container>
//	export POSTGRES_PASSWORD/AURA_DB_URL/AURA_DB_MIGRATE_URL  # migrated through 0010
//	go test -tags 'sandbox_integration db_integration' -race -run TestSnippetExec ./internal/skills/ -v
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when a required var is unset, so a
// skipped tier can never report falsely green in the pipeline.
package skills

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/sandboxagent"
	"github.com/google/uuid"
)

// TestSnippetExec is the SC#4 by-path exec proof: the snippet is saved, activated
// (materialized into the live /skills mount), resolved to its in-sandbox path, and
// run BY PATH (python3 /skills/<name>/<name>.py) through the real client. The marker
// stdout proves the file is visible + executable in-container; the usage stamp proves
// the deterministic operator stamp (D-19).
func TestSnippetExec(t *testing.T) {
	exportDir := envOrSkip(t, "AURA_SKILL_EXPORT_DIR")
	sandboxURL := envOrSkip(t, "AURA_SANDBOX_AGENT_URL")

	pool := migratedPool(t)
	ctx := context.Background()

	// sandboxagent.Client uses http.DefaultTransport, whose keep-alive readLoop/
	// writeLoop goroutines outlive the request; the package goleak gate (main_test.go)
	// would flag them. Close idle connections before verification (mirrors the tools
	// live-sandbox test cleanup).
	t.Cleanup(func() {
		if tr, ok := http.DefaultTransport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	})

	// A unique name keeps parallel/repeat runs from colliding on the shared export dir.
	name := "calc" + strings.ReplaceAll(uuid.Must(uuid.NewV7()).String()[:8], "-", "")
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:         pool,
		PendingDir:   filepath.Join(root, "pending"),
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    exportDir, // the SAME host dir the container mounts ro at /skills
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    nil,
		BodyCapBytes: 32768,
	})
	t.Cleanup(func() {
		// Best-effort: remove the snippet from the shared export dir so a later run is clean.
		_ = Dematerialize(name, exportDir)
	})

	const marker = "snippet-e2e-ok 42"
	code := "print('" + marker + "')\n"
	res, err := w.SaveSnippet(ctx, name, "python", code, Frontmatter{Description: "adds"}, AuditActor{ActorID: "cli"})
	if err != nil {
		t.Fatalf("SaveSnippet: %v", err)
	}
	if res.Status != StatusPendingApproval {
		t.Fatalf("SaveSnippet status = %q, want pending_approval", res.Status)
	}

	// Activate via the CLI source (operator approve) — materializes into the /skills mount.
	if err := w.Activate(ctx, name, ApprovalCLI, "", AuditActor{ActorID: "cli"}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	use, err := w.UseSnippet(name)
	if err != nil {
		t.Fatalf("UseSnippet: %v", err)
	}
	if !strings.HasPrefix(use.SandboxPath, "/skills/") {
		t.Fatalf("UseSnippet path = %q, want a /skills/ path", use.SandboxPath)
	}

	// Run BY PATH through the real client (interpreter + path, never the exec bit).
	client := sandboxagent.New(sandboxagent.Config{BaseURL: sandboxURL, TimeoutSec: 30})
	run, err := client.Run(ctx, sandboxagent.RunRequest{
		Command: use.Interpreter,
		Args:    []string{use.SandboxPath},
	})
	if err != nil {
		t.Fatalf("by-path exec: %v", err)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Fatalf("by-path exec exit = %v, stderr=%q", run.ExitCode, run.Stderr)
	}
	if !strings.Contains(run.Stdout, marker) {
		t.Fatalf("by-path exec stdout = %q, want marker %q", run.Stdout, marker)
	}

	// Stamp usage (the deterministic operator stamp, D-19) and assert it bumped.
	if err := w.StampUsage(name, time.Now()); err != nil {
		t.Fatalf("StampUsage: %v", err)
	}
	u, err := w.ReadUsage(name)
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if u.UseCount != 1 || u.LastUsedAt.IsZero() {
		t.Fatalf("usage sidecar after stamp = %+v, want use_count=1 + last_used_at set", u)
	}
}
