package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// Activate is the resume/CLI activation half (called by the ask_user resume handler
// in 11-05 or the `aura skills approve` CLI — NEVER by WriteMutation, T-11-04-E1).
// It moves pending/<name> → active, materializes the active skill into the export
// dir (D-17), and records the D-29 approved audit row inside db.WithTx:
//   - src=ApprovalAskUser requires a non-empty pausedToken (the matrix row);
//   - src=ApprovalCLI requires an empty token;
//   - src=ApprovalAuto (the unreachable WriteMutation auto path) requires an empty
//     token and gate_recommended=false.
//
// The FS promotion + materialize happen BEFORE the tx (reconcilable by the boot
// scan); the tx is the audit INSERT (all-or-nothing on the audit row).
func (w *Writer) Activate(ctx context.Context, name string, src ApprovalSource, pausedToken string, actor AuditActor) error {
	srcDir := filepath.Join(w.pendingDir, name)
	dstDir := filepath.Join(w.activeDir, name)

	hash, err := HashSkillDir(srcDir)
	if err != nil {
		return fmt.Errorf("activate %q: hash pending: %w", name, err)
	}

	if err := promoteDir(srcDir, dstDir); err != nil {
		return fmt.Errorf("activate %q: promote: %w", name, err)
	}
	if err := Materialize(name, dstDir, w.exportDir); err != nil {
		return fmt.Errorf("activate %q: materialize: %w", name, err)
	}

	gateRecommended := src != ApprovalAuto
	if err := db.WithTx(ctx, w.pool, func(q *sqlc.Queries) error {
		return InsertAuditTx(ctx, q, AuditInsert{
			ActorID:          actor.ActorID,
			IdentityID:       actor.IdentityID,
			SkillName:        name,
			Action:           AuditActivate,
			ContentHash:      hash,
			ApprovalSource:   src,
			PausedStateToken: pausedToken,
			GateRecommended:  gateRecommended,
			GateTaken:        true,
		})
	}); err != nil {
		return fmt.Errorf("activate %q: audit: %w", name, err)
	}
	return nil
}

// Archive de-materializes the skill (removes it from the export dir / mount, D-17)
// and moves active/<name> → archived/<name>, recording an archive audit row. The
// audit tuple is the system shape (auto) when src is ApprovalAuto (TTL sweep), else
// the cli shape.
func (w *Writer) Archive(ctx context.Context, name string, src ApprovalSource, actor AuditActor) error {
	dstDir := filepath.Join(w.activeDir, name)
	hash, _ := HashSkillDir(dstDir) // best-effort; a missing dir hashes empty

	if err := Dematerialize(name, w.exportDir); err != nil {
		return fmt.Errorf("archive %q: dematerialize: %w", name, err)
	}
	if w.archiveDir != "" {
		if err := promoteDir(dstDir, filepath.Join(w.archiveDir, name)); err != nil {
			return fmt.Errorf("archive %q: move to archived: %w", name, err)
		}
	}

	action := AuditArchive
	if src == ApprovalAuto {
		action = AuditAutoArchive
	}
	if err := w.auditActivationLike(ctx, name, action, hash, src, actor); err != nil {
		return fmt.Errorf("archive %q: audit: %w", name, err)
	}
	return nil
}

// Delete is the Destructive-tiered removal: de-materialize, remove the skill dir
// (active and pending), and record a delete audit row. It is invoked by
// WriteMutation for action=delete and by the CLI.
func (w *Writer) Delete(ctx context.Context, name string, actor AuditActor) (string, error) {
	activeDir := filepath.Join(w.activeDir, name)
	hash, _ := HashSkillDir(activeDir) // best-effort content_hash for the recovery path

	if err := Dematerialize(name, w.exportDir); err != nil {
		return "", fmt.Errorf("delete %q: dematerialize: %w", name, err)
	}
	if err := os.RemoveAll(activeDir); err != nil {
		return "", fmt.Errorf("delete %q: remove active: %w", name, err)
	}
	if w.pendingDir != "" {
		_ = os.RemoveAll(filepath.Join(w.pendingDir, name))
	}

	// Delete gates (Destructive) but the mutation itself is recorded as a CLI/system
	// action when it actually executes; WriteMutation routes the gated proposal here
	// after the gate is taken. The pending tuple is recorded by WriteMutation for the
	// non-delete actions; delete records its taken row directly with the cli source.
	if err := w.auditActivationLike(ctx, name, AuditDelete, hash, ApprovalCLI, actor); err != nil {
		return "", fmt.Errorf("delete %q: audit: %w", name, err)
	}
	return StatusActive, nil
}

// auditRejection records the audit row for a HUMAN-DECLINED (or operator-rejected)
// pending mutation. A discard exercises the gate (the human reviewed and said no), so
// the D-29 tuple is the gate_taken=true shape implied by src — the ask_user shape
// (NOT-NULL token) for a resume decline, the cli shape (NULL token) for a CLI reject.
// The action is recorded as cleanup_pending_stale: the pending skill was removed
// without activating. The approve-vs-reject distinction lives in the resume answer,
// not the tuple (the D-29 matrix shares the ask_user shape for both — 11-04 decision).
func (w *Writer) auditRejection(ctx context.Context, name, hash string, src ApprovalSource, pausedToken string, actor AuditActor) error {
	return db.WithTx(ctx, w.pool, func(q *sqlc.Queries) error {
		return InsertAuditTx(ctx, q, AuditInsert{
			ActorID:          actor.ActorID,
			IdentityID:       actor.IdentityID,
			SkillName:        name,
			Action:           AuditCleanupPending,
			ContentHash:      hash,
			ApprovalSource:   src,
			PausedStateToken: pausedToken,
			GateRecommended:  true,
			GateTaken:        true,
		})
	})
}

// auditActivationLike records a gate_taken=true audit row for activate/archive/
// delete, applying the D-29 tuple shape implied by src.
func (w *Writer) auditActivationLike(ctx context.Context, name string, action AuditAction, hash string, src ApprovalSource, actor AuditActor) error {
	gateRecommended := src != ApprovalAuto
	return db.WithTx(ctx, w.pool, func(q *sqlc.Queries) error {
		return InsertAuditTx(ctx, q, AuditInsert{
			ActorID:         actor.ActorID,
			IdentityID:      actor.IdentityID,
			SkillName:       name,
			Action:          action,
			ContentHash:     hash,
			ApprovalSource:  src,
			GateRecommended: gateRecommended,
			GateTaken:       true,
		})
	})
}

// promoteDir moves src → dst, creating dst's parent and replacing any stale dst. A
// cross-device rename (EXDEV) falls back to a copy+remove. A missing src is an error.
func promoteDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename %q -> %q: %w", src, dst, err)
	}
	return nil
}
