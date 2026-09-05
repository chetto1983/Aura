package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrUnknownSkill reports a lifecycle verb aimed at a name the addressed root does not hold.
// It is a sentinel because the caller has to tell it from a real failure: a cockpit or CLI
// delete of a name that is not there is the operator's mistake to see, not an outage.
var ErrUnknownSkill = errors.New("skills: no active skill by that name")

// Archive de-materializes the skill (removes it from the export dir / mount, D-17)
// and moves active/<name> → archived/<name>, recording an archive audit row. The
// audit tuple is the system shape (auto) when src is ApprovalAuto (TTL sweep), else
// the cli shape.
func (w *Writer) Archive(ctx context.Context, name string, src ApprovalSource, actor AuditActor) error {
	// Validate the name grammar BEFORE joining it into a path (chokepoint, D-30): a
	// name that passes ^[a-z0-9-]{1,64}$ cannot contain a path separator or "..". The
	// model fully controls this name on the action=archive path and it reaches
	// os.RemoveAll — without this guard a "../" name deletes a tree outside exportDir.
	if err := SanitizeName(name, name); err != nil {
		return fmt.Errorf("archive %q: %w", name, err)
	}
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
	// The catalog row survives an archive on purpose: archiving changes a skill's
	// lifecycle, not who owns it, so the owner keeps the row (and its shares) to restore
	// into. Only delete collects them.
	if err := w.auditActivationLike(ctx, name, action, hash, src, actor, nil); err != nil {
		return fmt.Errorf("archive %q: audit: %w", name, err)
	}
	return nil
}

// ActiveExists reports whether an active skill of the given name is present (an active/
// <name>/SKILL.md on disk). It is the restore-collision chokepoint (SKW-03): the cockpit
// restore handler calls it and 409s BEFORE Writer.Restore (which does an os.RemoveAll on
// the active dir that would SILENTLY overwrite the active skill). A name that fails the
// grammar is treated as "not present" — Restore self-guards the grammar again, so a bad
// name never reaches a path join here. The check is read-only (an os.Stat, no mutation).
func (w *Writer) ActiveExists(name string) bool {
	if err := SanitizeName(name, name); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(w.activeDir, name, "SKILL.md"))
	return err == nil
}

// Restore is the inverse of Archive (RESEARCH Pattern 1): it promotes
// archived/<name> → active, re-materializes the snippet into the export dir (so the
// loader + the D-01 host path see it again), flips the usage sidecar back to "active",
// and records an audit row. It is the recovery path for an over-eager archive (manual
// or TTL-swept) — action=restore in the skill tool, or the operator CLI.
//
// ACTION-CONSTANT DECISION (load-bearing): the 0010 `action` CHECK does NOT list
// 'restore', and this phase is forbidden from adding a snippet migration (D-19 / the
// forbidden-to-create list). A restore IS a re-activation, so it is recorded as the
// EXISTING AuditActivate constant with the cli ApprovalCLI source — the D-29 cli tuple
// (NULL token, gate_recommended=true, gate_taken=true) is already accepted by both the
// action CHECK and the coherence CHECK. There is deliberately NO AuditRestore constant
// (it would fail the live action CHECK). A future reader: restore audits as
// 'activate'/'cli', not a missing 'restore' action.
func (w *Writer) Restore(ctx context.Context, name string, src ApprovalSource, actor AuditActor) error {
	// Validate the name grammar BEFORE joining it into a path (chokepoint, D-30): a
	// name that passes ^[a-z0-9-]{1,64}$ cannot contain a path separator or "..".
	if err := SanitizeName(name, name); err != nil {
		return fmt.Errorf("restore %q: %w", name, err)
	}
	if w.archiveDir == "" {
		return fmt.Errorf("restore %q: archive dir not configured", name)
	}
	srcDir := filepath.Join(w.archiveDir, name)
	dstDir := filepath.Join(w.activeDir, name)

	hash, _ := HashSkillDir(srcDir) // best-effort; hash the archived tree before the move

	if err := promoteDir(srcDir, dstDir); err != nil {
		return fmt.Errorf("restore %q: promote archived->active: %w", name, err)
	}
	if err := Materialize(name, dstDir, w.exportDir); err != nil {
		return fmt.Errorf("restore %q: materialize: %w", name, err)
	}
	if err := w.SetUsageStatus(name, "active"); err != nil {
		return fmt.Errorf("restore %q: usage status: %w", name, err)
	}
	if err := w.auditActivationLike(ctx, name, AuditActivate, hash, src, actor, nil); err != nil {
		return fmt.Errorf("restore %q: audit: %w", name, err)
	}
	return nil
}

// Delete is the Destructive-tiered removal: de-materialize (the /skills mount), remove
// the active skill dir, and record ONE delete audit row. It is what WriteMutation
// routes action=delete to, and what the CLI and the cockpit call.
func (w *Writer) Delete(ctx context.Context, name string, actor AuditActor) (string, error) {
	// Validate the name grammar BEFORE joining it into a path (chokepoint, D-30): the
	// only live caller (WriteMutation) sanitizes upstream, but this self-guards the
	// method so a future caller cannot reach os.RemoveAll with a traversal name.
	if err := SanitizeName(name, name); err != nil {
		return "", fmt.Errorf("delete %q: %w", name, err)
	}
	// A delete of a skill this Writer's root does not hold is an ERROR, not a no-op that
	// audits. os.RemoveAll succeeds on an absent path, so without this the ledger would gain
	// a delete row for a skill still sitting in another root, still in every agent's context —
	// and the caller would be told it was removed. The scoped Writer made that reachable:
	// since #214 the root a delete addresses is the actor's, while the listing an operator
	// clicked from may be the deployment's. Restore already fails this way (promoteDir on an
	// absent source); delete was the one verb that did not.
	if !w.ActiveExists(name) {
		return "", fmt.Errorf("delete %q: %w", name, ErrUnknownSkill)
	}
	activeDir := filepath.Join(w.activeDir, name)
	hash, _ := HashSkillDir(activeDir) // best-effort content_hash for the recovery path

	if err := Dematerialize(name, w.exportDir); err != nil {
		return "", fmt.Errorf("delete %q: dematerialize: %w", name, err)
	}
	if err := os.RemoveAll(activeDir); err != nil {
		return "", fmt.Errorf("delete %q: remove active: %w", name, err)
	}

	// Delete is the one verb that also takes the ownership record with it, and with the
	// record go every grant standing on it (the 0118 trigger) — a share must never outlive
	// the thing it shared.
	if err := w.auditActivationLike(ctx, name, AuditDelete, hash, ApprovalCLI, actor, w.catalogDeleteOp(name)); err != nil {
		return "", fmt.Errorf("delete %q: audit: %w", name, err)
	}
	return StatusActive, nil
}

// auditActivationLike records a gate_taken=true audit row for activate/archive/
// delete, applying the D-29 tuple shape implied by src. cat is the owner's catalog change
// (amendment #214), committed in the same transaction; nil leaves the catalog alone.
func (w *Writer) auditActivationLike(ctx context.Context, name string, action AuditAction, hash string, src ApprovalSource, actor AuditActor, cat catalogOp) error {
	gateRecommended := src != ApprovalAuto
	return w.commitLedger(ctx, AuditInsert{
		ActorID:         actor.ActorID,
		IdentityID:      actor.IdentityID,
		SkillName:       name,
		Action:          action,
		ContentHash:     hash,
		ApprovalSource:  src,
		GateRecommended: gateRecommended,
		GateTaken:       true,
	}, cat)
}

// SetAlways re-enables (or disables) the always:true flag on an ACTIVE skill (D-10):
// the operator-only `aura skills always <name>` path that re-enables an always-on flag
// the installer stripped unconditionally. It rewrites the active SKILL.md frontmatter,
// re-materializes the skill into the export dir, and records an update audit row with
// the cli source (gate_taken). It NEVER touches a pending/archived skill — only an
// active one (the loader's always-block reads the active root).
func (w *Writer) SetAlways(ctx context.Context, name string, always bool, actor AuditActor) error {
	// Validate the name grammar BEFORE joining it into a path (chokepoint, D-30): a
	// name that passes ^[a-z0-9-]{1,64}$ cannot contain a path separator or "..".
	if err := SanitizeName(name, name); err != nil {
		return fmt.Errorf("set always %q: %w", name, err)
	}
	activeDir := filepath.Join(w.activeDir, name)
	raw, err := os.ReadFile(filepath.Join(activeDir, "SKILL.md")) // #nosec G304 -- activeDir = activeRoot/<SanitizeName-validated name>, operator-controlled
	if err != nil {
		return fmt.Errorf("set always %q: read active SKILL.md: %w", name, err)
	}
	fm, body, perr := parseFrontmatter(raw)
	if perr != nil {
		return fmt.Errorf("set always %q: parse: %w", name, perr)
	}
	fm.Always = always
	if err := os.WriteFile(filepath.Join(activeDir, "SKILL.md"), skillFileBytes(fm, body), 0o600); err != nil { // #nosec G703 -- activeDir = activeRoot/<SanitizeName-validated name>, operator-controlled root
		return fmt.Errorf("set always %q: rewrite SKILL.md: %w", name, err)
	}
	if err := Materialize(name, activeDir, w.exportDir); err != nil {
		return fmt.Errorf("set always %q: materialize: %w", name, err)
	}
	hash := HashSkillFiles(map[string][]byte{"SKILL.md": skillFileBytes(fm, body)})
	// always: is the column the always-on block is looked up by (migration 0118), so the
	// row has to learn the new value here or the index would answer with the old one.
	cat := w.catalogUpsertOp(name, fm.Description, always, hash)
	if err := w.auditActivationLike(ctx, name, AuditUpdate, hash, ApprovalCLI, actor, cat); err != nil {
		return fmt.Errorf("set always %q: audit: %w", name, err)
	}
	return nil
}

// promoteDir moves src → dst, creating dst's parent and replacing any stale dst. src
// and dst are always co-located under the skills root, so a single rename suffices (no
// cross-device fallback is needed). A missing src surfaces as the rename error.
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
