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
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/skills"
)

// modelActor labels every model-path write/save on the D-29 audit tuple so a
// model-authored mutation is attributable (T-18-08-S). Since amendment #214 it also carries
// the OWNING identity, so the ledger answers "whose library changed" as well as "who typed
// it" — and an unscoped turn records exactly the row it recorded before.
func modelActor(ctx context.Context) skills.AuditActor {
	return skills.AuditActor{ActorID: "model", IdentityID: identityctx.IdentityID(ctx)}
}

// LoaderResolver hands back the *skills.Loader that answers for one identity ("" = the
// deployment library alone). It is the seam per-identity skills hang on (amendment #214):
// the composition root owns the identity→roots decision (skills.Layout) and this package
// stays a projection.
type LoaderResolver func(identityID string) *skills.Loader

// Loader bridges a live *skills.Loader onto the tools.skillLoader seam: it projects
// skills.Skill into the tool-local SkillMeta, renders the manifest the tool's
// Description shows, and resolves a snippet skill into its in-box by-path invocation.
//
// It resolves the loader PER CALL from the call's identity, because a skill belongs to
// somebody: answering `use` from a loader fixed at boot would hand one person's instructions
// to another person's model.
type Loader struct {
	resolve    LoaderResolver
	invalidate func()
	manCap     int
}

// NewLoader builds the loader adapter over ONE loader — the deployment-global wiring, and
// every unscoped caller (the CLI, a pool-free manifest path). manCap is the manifest byte cap
// (cfg.SkillManifestCapBytes).
func NewLoader(loader *skills.Loader, manCap int) *Loader {
	return NewIdentityLoader(func(string) *skills.Loader { return loader }, loader.Invalidate, manCap)
}

// NewIdentityLoader builds the adapter that answers per identity. invalidate expires every
// cached snapshot the resolver can hand out: a write must be visible to the next read in the
// same turn, and the writer does not know which identities have a warm loader.
func NewIdentityLoader(resolve LoaderResolver, invalidate func(), manCap int) *Loader {
	return &Loader{resolve: resolve, invalidate: invalidate, manCap: manCap}
}

// loaderFor resolves the loader for the identity carried on ctx.
func (a *Loader) loaderFor(ctx context.Context) *skills.Loader {
	return a.resolve(identityctx.IdentityID(ctx))
}

// List projects the loaded skills into the tool-local SkillMeta shape.
func (a *Loader) List(ctx context.Context) []tools.SkillMeta {
	loaded := a.loaderFor(ctx).List()
	out := make([]tools.SkillMeta, 0, len(loaded))
	for _, s := range loaded {
		out = append(out, tools.SkillMeta{Name: s.Name, Description: s.Description})
	}
	return out
}

// Body returns the named skill's markdown body.
func (a *Loader) Body(ctx context.Context, name string) (string, bool) {
	s, ok := a.loaderFor(ctx).Get(name)
	if !ok {
		return "", false
	}
	return s.Body, true
}

// Invalidate makes a completed model-path write visible to the next read action
// in the same turn. It expires EVERY identity's snapshot, not just the writer's: a write can
// change what a grantee sees, and one extra lazy re-scan is cheaper than a reader holding a
// stale answer for a TTL.
func (a *Loader) Invalidate() {
	if a.invalidate != nil {
		a.invalidate()
	}
}

// ManifestDescription renders the turn-stable, alphabetical, cap-bounded manifest
// the skill tool's Description shows (D-06/D-09).
func (a *Loader) ManifestDescription(ctx context.Context) string {
	return skills.RenderManifest(a.loaderFor(ctx).List(), a.manCap)
}

// Snippet resolves an active snippet skill into its IN-BOX by-path invocation
// (skills.SnippetSandboxPath — /skills/<name>/<name>.<ext>, the SAME root MaterializeIn lands the
// snippet at, D-10), plus the docs instructions (SKILL.md body) and the interpreter. There is one
// path because shell_exec has one place to run: the host export-dir copy still EXISTS (it is the
// materialize SOURCE), but nothing the model can call reaches it. ok=false for an absent or
// non-snippet skill (action=use then falls back to the instruction authority-frame path).
func (a *Loader) Snippet(ctx context.Context, name string) (instructions, sandboxPath, interpreter string, ok bool) {
	s, found := a.loaderFor(ctx).Get(name)
	if !found || s.Type != skills.TypeSnippet {
		return "", "", "", false
	}
	sp, interp, err := skills.SnippetInvocation(s.Name, s.Language)
	if err != nil {
		return "", "", "", false
	}
	return s.Body, sp, interp, true
}

// scopedWriter resolves the Writer that writes as the identity on ctx (amendment #214): a
// model-authored skill lands in ITS OWNER'S root, not in a root shared with everybody. An
// unscoped context resolves back to the deployment-global Writer.
func (a *Writer) scopedWriter(ctx context.Context) (*skills.Writer, error) {
	return a.w.For(identityctx.IdentityID(ctx))
}

// scopedInstaller is scopedWriter's twin for the install transport, so an installed skill
// lands in the same root an authored one does.
func (a *Writer) scopedInstaller(ctx context.Context) (*skills.Installer, error) {
	if a.installer == nil {
		return nil, nil
	}
	return a.installer.For(identityctx.IdentityID(ctx))
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
	w, err := a.scopedWriter(ctx)
	if err != nil {
		return "", err
	}
	return w.WriteMutationByName(ctx, action, name, description, body, always, modelActor(ctx))
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
	w, err := a.scopedWriter(ctx)
	if err != nil {
		return "", err
	}
	res, err := w.SaveSnippet(ctx, name, language, code, fm, modelActor(ctx))
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
	w, err := a.scopedWriter(ctx)
	if err != nil {
		return "", err
	}
	if err := w.Restore(ctx, name, skills.ApprovalCLI, modelActor(ctx)); err != nil {
		return "", err
	}
	return skills.StatusActive, nil
}

// ArchiveSnippet maps the tool's archive call onto the live Writer.Archive (SAFE tier, no
// gate), labeling the actor "model" with the cli ApprovalSource (the manual operator-source
// archive, distinct from the TTL sweep's auto source). It returns an "archived" status.
func (a *Writer) ArchiveSnippet(ctx context.Context, name string) (string, error) {
	w, err := a.scopedWriter(ctx)
	if err != nil {
		return "", err
	}
	if err := w.Archive(ctx, name, skills.ApprovalCLI, modelActor(ctx)); err != nil {
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
	installer, err := a.scopedInstaller(ctx)
	if err != nil {
		return "", err
	}
	if installer == nil {
		return "", fmt.Errorf("install %q: no installer is wired in this context", source)
	}
	info, err := installer.Install(ctx, source, modelActor(ctx))
	if err != nil {
		return "", err
	}
	return info.Name, nil
}
