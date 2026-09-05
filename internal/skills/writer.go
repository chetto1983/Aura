package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/scoring"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Writer is the durable, auditable skill write primitive (Slice 7c). It is the seam
// 11-06 (installer) and 11-07 (snippets) consume, and the sink behind the model tool,
// the CLI and the cockpit.
//
// Amendment #97: a mutation TAKES EFFECT WHEN IT IS WRITTEN. The skill lands in the
// active root the loader scans AND is materialized into the export dir (D-17) in the
// same call — there is no staging stage, no approval step, and nothing that reports
// success while doing nothing. What remains as control is the write boundary
// (ValidateForWrite, allowBlocklisted=false), the loader's own blocklist scan, and
// the append-only audit ledger — which is why writeActive commits the audit row
// BEFORE the skill becomes visible.
type Writer struct {
	pool       *pgxpool.Pool
	activeDir  string // <root> active root the loader scans (archived/.staging live as siblings)
	exportDir  string // AURA_SKILL_EXPORT_DIR — the /skills mount source (D-17)
	archiveDir string // <root>/archived — de-materialized, retained skills

	// layout is the deployment's three skill bases (amendment #214). It is what For
	// derives a per-identity Writer from; a zero Layout leaves For a no-op and every write
	// deployment-global, which is exactly the pre-#214 behaviour.
	layout Layout
	// owner is the identity this Writer writes AS: set by For, empty on the global Writer.
	// It decides two things — which roots the files land in, and whether the write carries
	// a catalog row (migration 0118).
	owner string

	blocklist    []string
	bodyCapBytes int
}

// WriterConfig configures a Writer. The caller (composition root) supplies explicit
// paths — ArchiveDir is NOT derived from ActiveDir here — so the loader's scan roots
// and the writer's dirs stay in agreement.
type WriterConfig struct {
	Pool       *pgxpool.Pool
	ActiveDir  string
	ExportDir  string
	ArchiveDir string
	// Layout carries the per-identity bases For needs (amendment #214). Leaving it zero
	// keeps this Writer deployment-global and For an identity-agnostic no-op.
	Layout       Layout
	Blocklist    []string
	BodyCapBytes int
}

// NewWriter builds a Writer from cfg.
func NewWriter(cfg WriterConfig) *Writer {
	return &Writer{
		pool:         cfg.Pool,
		activeDir:    cfg.ActiveDir,
		exportDir:    cfg.ExportDir,
		archiveDir:   cfg.ArchiveDir,
		layout:       cfg.Layout,
		blocklist:    cfg.Blocklist,
		bodyCapBytes: cfg.BodyCapBytes,
	}
}

// StatusActive is what every write path returns. After amendment #97 there is no
// second status to distinguish: a skill that was written is live.
const StatusActive = "active"

// stagingDirName is the sibling directory writeActive stages into before renaming a
// skill into place. The leading dot puts it outside the skill-name grammar
// (^[a-z0-9-]{1,64}$), so neither the loader nor the TTL sweep — both of which run
// every candidate through SanitizeName — can mistake it for a skill.
const stagingDirName = ".staging"

// AuditActor carries the identity attribution for an audit row (D-05): ActorID is
// the free-form actor label (e.g. "model", "cli", a swarm worker id); IdentityID is
// the owning identity ("" → "local").
type AuditActor struct {
	ActorID    string
	IdentityID string
}

// ActorModel is the actor label the in-loop skill tool uses (mirrored by the
// cmd/aura skillWriterAdapter). After amendment #97 it no longer selects a code path
// — every actor writes the same way — but it stays the ledger's answer to WHO changed
// a skill, which is the only place that answer now survives.
const ActorModel = "model"

// WriteMutation is the create/update/delete entry point the model tool, the CLI and
// the cockpit share. It validates the frontmatter + body at the write boundary
// (allowBlocklisted=false — no actor bypasses the blocklist, T-11-03-E1), computes the
// canonical content_hash (D-23 recovery path), and lands the skill active +
// materialized + audited in one call (writeActive).
//
// Delete is routed FIRST, before the validator. It carries only a name — the caller
// synthesises an empty frontmatter around it — so running it through a description
// check made the cockpit's DELETE route fail on a field a delete does not have,
// answering 502 without deleting anything or recording a row. Delete self-guards the
// only input it has (SanitizeName, inside Writer.Delete).
func (w *Writer) WriteMutation(ctx context.Context, action scoring.SkillAction, fm Frontmatter, body string, actor AuditActor) (status string, err error) {
	if action == scoring.SkillDelete {
		return w.Delete(ctx, fm.Name, actor)
	}
	if err := ValidateForWrite(fm, body, w.blocklist, w.bodyCapBytes, false); err != nil {
		return "", fmt.Errorf("write mutation %q: %w", fm.Name, err)
	}

	files := map[string][]byte{"SKILL.md": skillFileBytes(fm, body)}
	hash := HashSkillFiles(files)
	audit := actorAudit(fm.Name, auditActionFor(action), hash, actor)
	cat := w.catalogUpsertOp(fm.Name, fm.Description, fm.Always, hash)
	if err := w.writeActive(ctx, fm.Name, writeFilesInto(files), audit, cat); err != nil {
		return "", fmt.Errorf("write mutation %q: %w", fm.Name, err)
	}
	return StatusActive, nil
}

// WriteInstall lands an installed skill — a possibly-multi-file tree the Installer has
// already staged symlink-stripped and always-stripped on disk — as ACTIVE, recording
// the install audit row with the installer's precomputed canonical hash. Unlike
// WriteMutation (a single SKILL.md) it promotes the WHOLE staged tree, so bundled
// scripts and assets travel with the skill.
func (w *Writer) WriteInstall(ctx context.Context, fm Frontmatter, stagedDir, hash string, actor AuditActor) (string, error) {
	fill := func(dir string) error {
		if err := copyTreeNoSymlinks(stagedDir, dir); err != nil {
			return fmt.Errorf("copy staged tree: %w", err)
		}
		return nil
	}
	cat := w.catalogUpsertOp(fm.Name, fm.Description, fm.Always, hash)
	if err := w.writeActive(ctx, fm.Name, fill, actorAudit(fm.Name, AuditInstall, hash, actor), cat); err != nil {
		return "", fmt.Errorf("install %q: %w", fm.Name, err)
	}
	return StatusActive, nil
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
// create|update`: it labels the actor "cli" and writes the mutation live.
func (w *Writer) WriteMutationCLI(ctx context.Context, action, name, description, body string, always bool) (string, error) {
	return w.WriteMutationByName(ctx, action, name, description, body, always, AuditActor{ActorID: "cli"})
}

// writeActive lands a skill as ACTIVE and records its audit row. The ORDER of the
// four steps is the safety story of amendment #97, and it is deliberate:
//
//	stage under <active>/.staging → commit the audit tx → rename into place → materialize
//
// Before #97 the pre-tx write went to pending/, where an orphan was invisible and
// harmless. The identical write now lands in the root the loader scans and reaches the
// model's manifest within one cache TTL, while the append-only ledger is the ONLY
// surviving record of who changed what. So the tx commits FIRST: a crash after it
// leaves an audit row for a skill that does not exist — detectable and harmless —
// where the reverse leaves a live, model-visible, unrecorded mutation.
//
// fill populates the staging dir: an explicit file map for create/update/snippet, a
// symlink-stripped tree copy for install. cat is the owner's catalog change (amendment
// #214), committed in the SAME transaction as the audit row and nil on the global Writer.
func (w *Writer) writeActive(ctx context.Context, name string, fill func(dir string) error, audit AuditInsert, cat catalogOp) error {
	// The name chokepoint (D-30) runs BEFORE the name is joined into a path that
	// promoteDir and Materialize both turn into an os.RemoveAll.
	if err := SanitizeName(name, name); err != nil {
		return err
	}
	if w.activeDir == "" {
		return fmt.Errorf("active dir not configured")
	}
	staging := filepath.Join(w.activeDir, stagingDirName)
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return fmt.Errorf("mkdir staging root: %w", err)
	}
	tmp, err := os.MkdirTemp(staging, "skill-*")
	if err != nil {
		return fmt.Errorf("mkdir staging dir: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tmp)
		}
	}()

	if err := fill(tmp); err != nil {
		return err
	}
	if err := w.commitLedger(ctx, audit, cat); err != nil {
		return fmt.Errorf("audit: %w", err)
	}

	dst := filepath.Join(w.activeDir, name)
	if err := promoteDir(tmp, dst); err != nil {
		return fmt.Errorf("rename into the active root: %w", err)
	}
	committed = true
	if err := Materialize(name, dst, w.exportDir); err != nil {
		return fmt.Errorf("materialize: %w", err)
	}
	return nil
}

// writeFilesInto returns a writeActive fill that writes an explicit file map. The file
// names are writer-controlled (SKILL.md, <name>.<ext>) over a SanitizeName-validated
// staging dir.
func writeFilesInto(files map[string][]byte) func(dir string) error {
	return func(dir string) error {
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				return fmt.Errorf("write %s: %w", name, err)
			}
		}
		return nil
	}
}

// actorAudit builds the audit row every ACTOR-initiated write records.
//
// Two constants here are load-bearing and must not be "corrected":
//
//   - approval_source no longer means how a mutation was approved — after #97 nothing
//     is approved. It distinguishes an actor-initiated write ('cli') from a system
//     sweep ('auto'). WHO acted is actor_id, which still carries ActorModel for a
//     model-authored mutation; without that distinction the ledger would read as if an
//     operator had made every change.
//   - gate_recommended stays true although no gate is recommended any more: the 0010
//     skill_audit_d29_coherence CHECK admits no ('cli',NULL,false,true) tuple, so
//     writing false raises SQLSTATE 23514 at runtime — under db_integration only, never
//     at compile time. Writer.Delete has always written exactly this tuple.
func actorAudit(name string, action AuditAction, hash string, actor AuditActor) AuditInsert {
	return AuditInsert{
		ActorID:         actor.ActorID,
		IdentityID:      actor.IdentityID,
		SkillName:       name,
		Action:          action,
		ContentHash:     hash,
		ApprovalSource:  ApprovalCLI,
		GateRecommended: true,
		GateTaken:       true,
	}
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
