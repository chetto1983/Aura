//go:build db_integration

// Integration tests for the D-03 ask_user resume handler (plan 11-05). They require
// the migrations applied through 0010 (see audit_store_integration_test.go header for
// the env + invocation). No-skip-as-green: the shared envOrSkip t.Fatals under $CI.
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/scoring"
	"github.com/google/uuid"
)

func newResumeWriter(t *testing.T) (*Writer, string) {
	t.Helper()
	pool := migratedPool(t)
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:       pool,
		PendingDir: filepath.Join(root, "pending"),
		ActiveDir:  filepath.Join(root, "active"),
		ExportDir:  filepath.Join(root, "export"),
		ArchiveDir: filepath.Join(root, "archived"),
		Blocklist:  []string{"<|im_start|>"},
	})
	return w, root
}

// TestResumeAcceptActivates proves an accept resume runs Writer.Activate: the pending
// skill moves to active + materializes + records the ask_user approved audit tuple.
func TestResumeAcceptActivates(t *testing.T) {
	w, root := newResumeWriter(t)
	store := NewAuditStore(w.pool)
	h := NewResumeHandler(w)

	name := "ra-" + uuid.Must(uuid.NewV7()).String()[:8]
	fm := Frontmatter{Name: name, Description: "d", Type: TypeInstruction}
	if _, err := w.WriteMutation(t.Context(), scoring.SkillCreate, fm, "body", AuditActor{ActorID: "cli"}); err != nil {
		t.Fatalf("WriteMutation: %v", err)
	}

	token := uuid.Must(uuid.NewV7()).String()
	seedPausedState(t, w.pool, token)
	if err := h.Resume(t.Context(), ResumeAccept, name, token, AuditActor{ActorID: "user"}); err != nil {
		t.Fatalf("Resume accept: %v", err)
	}

	if _, serr := os.Stat(filepath.Join(root, "export", name, "SKILL.md")); serr != nil {
		t.Errorf("accept did not materialize the active skill: %v", serr)
	}
	rows, err := store.List(t.Context(), AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var sawActivate bool
	for _, r := range rows {
		if r.Action == AuditActivate && r.ApprovalSource == ApprovalAskUser && r.PausedStateToken == token && r.GateTaken {
			sawActivate = true
		}
	}
	if !sawActivate {
		t.Errorf("accept did not record the ask_user activate audit row: %+v", rows)
	}
}

// TestResumeDeclineDiscards proves a decline resume removes the pending skill (it
// never activates) and records the rejection audit row with the ask_user shape.
func TestResumeDeclineDiscards(t *testing.T) {
	w, root := newResumeWriter(t)
	store := NewAuditStore(w.pool)
	h := NewResumeHandler(w)

	name := "rd-" + uuid.Must(uuid.NewV7()).String()[:8]
	fm := Frontmatter{Name: name, Description: "d", Type: TypeInstruction}
	if _, err := w.WriteMutation(t.Context(), scoring.SkillCreate, fm, "body", AuditActor{ActorID: "cli"}); err != nil {
		t.Fatalf("WriteMutation: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(root, "pending", name)); serr != nil {
		t.Fatalf("pending skill not staged: %v", serr)
	}

	token := uuid.Must(uuid.NewV7()).String()
	seedPausedState(t, w.pool, token)
	if err := h.Resume(t.Context(), ResumeDecline, name, token, AuditActor{ActorID: "user"}); err != nil {
		t.Fatalf("Resume decline: %v", err)
	}

	// Pending removed; nothing materialized.
	if _, serr := os.Stat(filepath.Join(root, "pending", name)); !os.IsNotExist(serr) {
		t.Errorf("decline must remove the pending skill (stat err=%v)", serr)
	}
	if _, serr := os.Stat(filepath.Join(root, "export", name)); !os.IsNotExist(serr) {
		t.Errorf("decline must not materialize the skill (stat err=%v)", serr)
	}
	// A rejection audit row exists with the ask_user shape + the token.
	rows, err := store.List(t.Context(), AuditFilter{SkillName: name})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var sawReject bool
	for _, r := range rows {
		if r.Action == AuditCleanupPending && r.ApprovalSource == ApprovalAskUser && r.PausedStateToken == token {
			sawReject = true
		}
	}
	if !sawReject {
		t.Errorf("decline did not record the ask_user rejection audit row: %+v", rows)
	}
}
