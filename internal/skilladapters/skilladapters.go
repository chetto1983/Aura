// Package skilladapters is the composition-root seam that bridges the live
// *skills.Loader / *skills.Writer onto the consumer-declared tools.skillLoader /
// tools.skillWriter interfaces the `skill` tool dispatches against. It keeps
// internal/agent/tools free of an internal/skills import (the boundary 11-02
// established) while giving the production composition root (cmd/aura) one shared
// adapter implementation. It was extracted to stop a second, drifting ~30 LOC mirror
// in the eval-parity registry (IN-04); that registry has since been deleted.
//
// The adapters satisfy the tools seams structurally (the seams are package-private
// to internal/agent/tools; an adapter only needs the matching method set). The
// actor on the write path is the model (allowBlocklisted=false is enforced inside
// the live Writer); restore/archive audit with the cli ApprovalSource.
package skilladapters

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/skills"
)

// modelActor labels every model-path write/save on the D-29 audit tuple so a
// model-authored mutation is attributable (T-18-08-S).
var modelActor = skills.AuditActor{ActorID: "model"}

// Loader bridges a live *skills.Loader onto the tools.skillLoader seam: it projects
// skills.Skill into the tool-local SkillMeta, renders the manifest the tool's
// Description shows, and resolves a snippet skill into its HOST by-path invocation
// (D-01 host-primary) under the export dir the Writer materializes into.
type Loader struct {
	loader    *skills.Loader
	manCap    int
	exportDir string // AURA_SKILL_EXPORT_DIR — the host materialization target a snippet's HOST by-path use frame points at (D-01)
}

// NewLoader builds the loader adapter. manCap is the manifest byte cap
// (cfg.SkillManifestCapBytes); exportDir is AURA_SKILL_EXPORT_DIR (cfg.SkillExportDir).
func NewLoader(loader *skills.Loader, manCap int, exportDir string) *Loader {
	return &Loader{loader: loader, manCap: manCap, exportDir: exportDir}
}

// List projects the loaded skills into the tool-local SkillMeta shape.
func (a *Loader) List() []tools.SkillMeta {
	loaded := a.loader.List()
	out := make([]tools.SkillMeta, 0, len(loaded))
	for _, s := range loaded {
		out = append(out, tools.SkillMeta{Name: s.Name, Description: s.Description})
	}
	return out
}

// Body returns the named skill's markdown body.
func (a *Loader) Body(name string) (string, bool) {
	s, ok := a.loader.Get(name)
	if !ok {
		return "", false
	}
	return s.Body, true
}

// Invalidate makes a completed model-path write visible to the next read action
// in the same turn.
func (a *Loader) Invalidate() {
	a.loader.Invalidate()
}

// ManifestDescription renders the turn-stable, alphabetical, cap-bounded manifest
// the skill tool's Description shows (D-06/D-09).
func (a *Loader) ManifestDescription() string {
	return skills.RenderManifest(a.loader.List(), a.manCap)
}

// Snippet resolves an active snippet skill into BOTH by-path invocations: the HOST export-dir path
// (D-01 host-primary, under the SAME export dir the Writer materializes into) AND the IN-BOX
// SandboxPath (skills.SnippetSandboxPath — /skills/<name>/<name>.<ext>, the SAME root MaterializeIn
// lands the snippet at under a strict profile, D-10), plus the docs instructions (SKILL.md body)
// and the interpreter. action=use renders the sandbox path under Strict(), the host path otherwise.
// ok=false for an absent or non-snippet skill (action=use then falls back to the instruction
// authority-frame path).
func (a *Loader) Snippet(name string) (instructions, hostPath, sandboxPath, interpreter string, ok bool) {
	s, found := a.loader.Get(name)
	if !found || s.Type != skills.TypeSnippet {
		return "", "", "", "", false
	}
	hp, interp, perr := skills.SnippetHostInvocation(s.Name, s.Language, a.exportDir)
	if perr != nil {
		return "", "", "", "", false
	}
	sp, _, serr := skills.SnippetInvocation(s.Name, s.Language)
	if serr != nil {
		return "", "", "", "", false
	}
	return s.Body, hp, sp, interp, true
}

// Writer bridges a live *skills.Writer onto the tools.skillWriter seam the skill
// tool's create/update/delete/save_snippet/restore/archive actions dispatch against.
// The model is the actor on this path (allowBlocklisted=false is enforced inside the
// live Writer's WriteMutation).
// installer is optional: a nil one makes action=install a clear error instead of a
// panic, the same posture a nil Writer already has on the pool-free manifest paths.
type Writer struct {
	w         *skills.Writer
	installer *skills.Installer
}

// NewWriter builds the writer adapter over the live *skills.Writer and the Installer
// that backs action=install. The installer may be nil (no install action available).
func NewWriter(w *skills.Writer, installer *skills.Installer) *Writer {
	return &Writer{w: w, installer: installer}
}

// WriteMutation maps the tool's string-keyed call onto the live Writer, labeling the
// actor "model". A blocklist/validation reject comes back as an error (the tool
// surfaces it as a self-correct, NOT a pause).
func (a *Writer) WriteMutation(ctx context.Context, action, name, description, body string, always bool) (string, error) {
	return a.w.WriteMutationByName(ctx, action, name, description, body, always, modelActor)
}

// SaveSnippet maps the tool's UNGATED save_snippet call onto the live Writer.SaveSnippet
// (D-02), labeling the actor "model" on the D-29 pending audit tuple (so a model-authored
// save is attributable, T-18-08-S). SaveSnippet still validates + runs the injection
// blocklist on the CODE + lands pending — it NEVER self-activates; the model cannot bypass
// the save-time gate. It returns the pending status string for the tool's confirmation.
func (a *Writer) SaveSnippet(ctx context.Context, name, language, code, description string, needsNetwork, needsWorkspace bool) (string, error) {
	fm := skills.Frontmatter{
		Name:           name,
		Description:    description,
		NeedsNetwork:   needsNetwork,
		NeedsWorkspace: needsWorkspace,
	}
	res, err := a.w.SaveSnippet(ctx, name, language, code, fm, modelActor)
	if err != nil {
		return "", err
	}
	return res.Status, nil
}

// Restore maps the tool's restore call onto the live Writer.Restore (the inverse of
// Archive), labeling the actor "model" with the cli ApprovalSource (the D-29 cli tuple
// the 0010 CHECK accepts — restore audits as activate/cli, no new migration). It returns
// the active status string.
func (a *Writer) Restore(ctx context.Context, name string) (string, error) {
	if err := a.w.Restore(ctx, name, skills.ApprovalCLI, modelActor); err != nil {
		return "", err
	}
	return skills.StatusActive, nil
}

// ArchiveSnippet maps the tool's archive call onto the live Writer.Archive (SAFE tier, no
// gate), labeling the actor "model" with the cli ApprovalSource (the manual operator-source
// archive, distinct from the TTL sweep's auto source). It returns an "archived" status.
func (a *Writer) ArchiveSnippet(ctx context.Context, name string) (string, error) {
	if err := a.w.Archive(ctx, name, skills.ApprovalCLI, modelActor); err != nil {
		return "", err
	}
	return "archived", nil
}

// Install maps the tool's install call onto the live Installer — the SAME fetch →
// validate → write → materialize → audit path the cockpit install button runs, with the
// actor labelled "model". It returns the installed name so the tool can name it back.
//
// Routing the model through the Installer rather than through a terminal is the whole
// point: the CLI would land the tree in its working directory, outside every loader
// root, and the model would truthfully report an install of something Aura cannot load.
func (a *Writer) Install(ctx context.Context, source string) (string, error) {
	if a.installer == nil {
		return "", fmt.Errorf("install %q: no installer is wired in this context", source)
	}
	info, err := a.installer.Install(ctx, source, modelActor)
	if err != nil {
		return "", err
	}
	return info.Name, nil
}
