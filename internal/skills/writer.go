package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Writer is the durable, auditable, gate-aware skill write primitive (Slice 7c).
// It is the seam 11-05 (governance/ask_user resume), 11-06 (installer), and 11-07
// (snippets) consume. A model-authored mutation lands in `pending/<name>/` on disk
// and records ONE audit row (the D-29 pending tuple) inside db.WithTx; it never
// self-activates (T-11-04-E1 — Activate is a SEPARATE method only the resume
// handler / CLI call). Materialization to the export dir happens ONLY on activation
// (D-17). The status strings mirror the scheduler's pending_approval contract.
type Writer struct {
	pool       *pgxpool.Pool
	pendingDir string // <root>/pending — where gated mutations land before approval
	activeDir  string // <root> active root the loader scans (pending/active live as siblings)
	exportDir  string // AURA_SKILL_EXPORT_DIR — the /skills ro-mount source (D-17)
	archiveDir string // <root>/archived — de-materialized, retained skills

	blocklist    []string
	bodyCapBytes int
}

// WriterConfig configures a Writer. PendingDir/ActiveDir/ArchiveDir default to
// <ActiveDir>/pending and <ActiveDir>/archived siblings when only ActiveDir is set
// is NOT done here — the caller (composition root) supplies explicit paths so the
// loader's scan roots and the writer's dirs stay in agreement.
type WriterConfig struct {
	Pool         *pgxpool.Pool
	PendingDir   string
	ActiveDir    string
	ExportDir    string
	ArchiveDir   string
	Blocklist    []string
	BodyCapBytes int
}

// NewWriter builds a Writer from cfg.
func NewWriter(cfg WriterConfig) *Writer {
	return &Writer{
		pool:         cfg.Pool,
		pendingDir:   cfg.PendingDir,
		activeDir:    cfg.ActiveDir,
		exportDir:    cfg.ExportDir,
		archiveDir:   cfg.ArchiveDir,
		blocklist:    cfg.Blocklist,
		bodyCapBytes: cfg.BodyCapBytes,
	}
}

// Status constants returned by WriteMutation, mirroring the scheduler's contract.
const (
	StatusPendingApproval = "pending_approval"
	StatusActive          = "active"
)

// AuditActor carries the identity attribution for an audit row (D-05): ActorID is
// the free-form actor label (e.g. "model", "cli", a swarm worker id); IdentityID is
// the owning identity ("" → "local").
type AuditActor struct {
	ActorID    string
	IdentityID string
}

// WriteMutation is the model-facing create/update/install/delete entry point. It:
//  1. computes the tier via scoring.ComputeSkillTier (create/update/install→Risky,
//     delete→Destructive) and gate via scoring.GateRecommended;
//  2. validates the frontmatter+body at the write boundary (ValidateForWrite,
//     allowBlocklisted=false — model paths NEVER bypass the blocklist, T-11-03-E1);
//  3. computes the canonical content_hash (D-23 recovery path);
//  4. writes the skill into pending/<name>/ atomically (temp dir + rename) BEFORE
//     the tx (the orphan is reconciled by the boot scan, mirroring the conversations
//     sidecar-spill-before-tx pattern, T-11-04-I1);
//  5. records ONE audit row — the D-29 pending tuple (NULL,NULL,true,false) — inside
//     db.WithTx (the tx is the audit INSERT; the FS write is reconcilable).
//
// It returns StatusPendingApproval whenever the gate fires (always, for the four
// gated actions). It NEVER activates or materializes — that is Activate's job.
func (w *Writer) WriteMutation(ctx context.Context, action scoring.SkillAction, fm Frontmatter, body string, actor AuditActor) (status string, err error) {
	tier := scoring.ComputeSkillTier(action, body)
	gate := scoring.GateRecommended(tier)

	if err := ValidateForWrite(fm, body, w.blocklist, w.bodyCapBytes, false); err != nil {
		return "", fmt.Errorf("write mutation %q: %w", fm.Name, err)
	}

	hash := HashSkillFiles(map[string][]byte{"SKILL.md": skillFileBytes(fm, body)})

	if action == scoring.SkillDelete {
		// Delete is a de-materialize + archive + audit, not a pending write.
		return w.Delete(ctx, fm.Name, actor)
	}

	if err := w.writePending(fm.Name, fm, body); err != nil {
		return "", fmt.Errorf("write mutation %q: %w", fm.Name, err)
	}

	if !gate {
		// No gated action reaches here today (all four gate). Kept for the auto path:
		// promote + materialize + audit the activation tuple. Unreachable in v1.
		if aerr := w.Activate(ctx, fm.Name, ApprovalAuto, "", actor); aerr != nil {
			return "", aerr
		}
		return StatusActive, nil
	}

	// The D-29 pending tuple: gate recommended, gate not yet taken.
	if err := db.WithTx(ctx, w.pool, func(q *sqlc.Queries) error {
		return InsertAuditTx(ctx, q, AuditInsert{
			ActorID:         actor.ActorID,
			IdentityID:      actor.IdentityID,
			SkillName:       fm.Name,
			Action:          auditActionFor(action),
			ContentHash:     hash,
			ApprovalSource:  ApprovalNone,
			GateRecommended: true,
			GateTaken:       false,
		})
	}); err != nil {
		return "", fmt.Errorf("write mutation %q: audit: %w", fm.Name, err)
	}
	return StatusPendingApproval, nil
}

// WriteInstallPending lands an installed skill (a possibly-multi-file tree already
// staged symlink-stripped + always-stripped on disk by the Installer) into pending/
// <name>/ and records the D-29 pending audit tuple with action=install and the
// installer's precomputed canonical hash. It is the installer's pending+audit sink
// (11-06): the install is a gated mutation that NEVER self-activates (D-03) —
// activation is the operator / ask_user resume path. Unlike WriteMutation (single
// SKILL.md), this promotes the whole staged tree so bundled files travel into pending.
func (w *Writer) WriteInstallPending(ctx context.Context, fm Frontmatter, body, stagedDir, hash string, actor AuditActor) (string, error) {
	if w.pendingDir == "" {
		return "", fmt.Errorf("install pending: pending dir not configured")
	}
	if err := os.MkdirAll(w.pendingDir, 0o750); err != nil {
		return "", fmt.Errorf("install pending: mkdir pending root: %w", err)
	}
	dst := filepath.Join(w.pendingDir, fm.Name)
	// Promote the staged tree into pending/<name>/ atomically: copy into a sibling
	// temp dir (symlink-stripped) then rename, mirroring writePending's crash safety.
	tmp, err := os.MkdirTemp(w.pendingDir, "."+fm.Name+"-tmp-*")
	if err != nil {
		return "", fmt.Errorf("install pending: mkdir temp: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tmp)
		}
	}()
	if err := copyTreeNoSymlinks(stagedDir, tmp); err != nil {
		return "", fmt.Errorf("install pending: copy staged tree: %w", err)
	}
	if err := os.RemoveAll(dst); err != nil {
		return "", fmt.Errorf("install pending: clear stale pending %q: %w", fm.Name, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", fmt.Errorf("install pending: rename into place: %w", err)
	}
	committed = true

	if err := db.WithTx(ctx, w.pool, func(q *sqlc.Queries) error {
		return InsertAuditTx(ctx, q, AuditInsert{
			ActorID:         actor.ActorID,
			IdentityID:      actor.IdentityID,
			SkillName:       fm.Name,
			Action:          AuditInstall,
			ContentHash:     hash,
			ApprovalSource:  ApprovalNone,
			GateRecommended: true,
			GateTaken:       false,
		})
	}); err != nil {
		return "", fmt.Errorf("install pending %q: audit: %w", fm.Name, err)
	}
	return StatusPendingApproval, nil
}

// WriteMutationByName is the string-keyed entry point the CLI and the model-tool
// adapter share: it maps the action name + plain frontmatter fields onto the typed
// WriteMutation. The actor labels the caller (the CLI passes "cli", the tool adapter
// passes "model"). It is a thin convenience over WriteMutation so callers outside
// internal/skills do not have to construct a Frontmatter / scoring.SkillAction.
func (w *Writer) WriteMutationByName(ctx context.Context, action, name, description, body string, always bool, actor AuditActor) (string, error) {
	fm := Frontmatter{Name: name, Description: description, Type: TypeInstruction, Always: always}
	return w.WriteMutation(ctx, scoring.SkillAction(action), fm, body, actor)
}

// WriteMutationCLI is the operator-authored convenience used by `aura skills
// create|update`: it labels the actor "cli" and stages the mutation as pending.
func (w *Writer) WriteMutationCLI(ctx context.Context, action, name, description, body string, always bool) (string, error) {
	return w.WriteMutationByName(ctx, action, name, description, body, always, AuditActor{ActorID: "cli"})
}

// writePending materializes the skill into pending/<name>/SKILL.md atomically: it
// writes into a sibling temp dir then renames it into place, so a crash mid-write
// never leaves a half-written pending skill the loader could read.
func (w *Writer) writePending(name string, fm Frontmatter, body string) error {
	if w.pendingDir == "" {
		return fmt.Errorf("pending dir not configured")
	}
	if err := os.MkdirAll(w.pendingDir, 0o750); err != nil {
		return fmt.Errorf("mkdir pending root: %w", err)
	}
	tmp, err := os.MkdirTemp(w.pendingDir, "."+name+"-tmp-*")
	if err != nil {
		return fmt.Errorf("mkdir temp: %w", err)
	}
	// On any failure, best-effort clean the temp dir.
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tmp)
		}
	}()

	if err := os.WriteFile(filepath.Join(tmp, "SKILL.md"), skillFileBytes(fm, body), 0o600); err != nil {
		return fmt.Errorf("write SKILL.md: %w", err)
	}

	final := filepath.Join(w.pendingDir, name)
	if err := os.RemoveAll(final); err != nil { // replace any stale pending of the same name
		return fmt.Errorf("clear stale pending %q: %w", name, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("rename pending into place: %w", err)
	}
	committed = true
	return nil
}

// skillFileBytes renders a SKILL.md from frontmatter + body. The frontmatter is the
// minimal name/description/type/always set the writer controls; richer fields ride
// through unchanged on update paths the installer/CLI handle.
func skillFileBytes(fm Frontmatter, body string) []byte {
	var b []byte
	b = append(b, "---\n"...)
	b = append(b, "name: "+fm.Name+"\n"...)
	b = append(b, "description: "+yamlScalar(fm.Description)+"\n"...)
	t := fm.Type
	if t == "" {
		t = TypeInstruction
	}
	b = append(b, "type: "+t+"\n"...)
	// A snippet's language is load-bearing (UseSnippet resolves the by-path interpreter
	// + extension from it, D-20), so it must round-trip into the materialized SKILL.md.
	if t == TypeSnippet && fm.Language != "" {
		b = append(b, "language: "+fm.Language+"\n"...)
	}
	if fm.Always {
		b = append(b, "always: true\n"...)
	}
	b = append(b, "---\n"...)
	b = append(b, body...)
	return b
}

// yamlScalar double-quotes a description that needs it (contains a colon, a leading
// special char, or a quote), escaping inner quotes — the goccy parser round-trips it
// (spike 006). A plain description is emitted bare.
func yamlScalar(s string) string {
	needsQuote := false
	for _, r := range s {
		if r == ':' || r == '"' || r == '\n' || r == '#' || r == '\'' {
			needsQuote = true
			break
		}
	}
	if s == "" || s[0] == ' ' || s[0] == '!' || s[0] == '&' || s[0] == '*' {
		needsQuote = true
	}
	if !needsQuote {
		return s
	}
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		if s[i] == '"' || s[i] == '\\' {
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	out = append(out, '"')
	return string(out)
}

// auditActionFor maps a scoring.SkillAction to the audit action enum.
func auditActionFor(a scoring.SkillAction) AuditAction {
	switch a {
	case scoring.SkillCreate:
		return AuditCreate
	case scoring.SkillUpdate:
		return AuditUpdate
	case scoring.SkillInstall:
		return AuditInstall
	case scoring.SkillDelete:
		return AuditDelete
	default:
		return AuditUpdate
	}
}
