package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/jackc/pgx/v5/pgxpool"
)

// serve_governance_write_skills.go wires the composition-root concrete adapter for the
// Phase-29 SKILLS WRITE surface (SKW-01/02/03). The agui consumer declares the narrow
// SkillsWriteProvider seam (governance_write_seam.go); skillsWriteAdapter satisfies it over
// the EXISTING Phase-11 primitives — the live Writer (the lifecycle sink) + the Task-1
// Installer (the npx-skills fetch→validate→stage transport).
//
// Install posture (Claude-Code parity, operator directive 2026-06-21; amendment #97): a cockpit
// install ACTIVATES directly — no approval pause, no staging ceremony, no two-step. The Writer
// itself lands the fetched tree active + materialized + audited, so the adapter has no promotion
// step of its own to perform. The security keep is intrinsic and invisible: the loader-level
// injection blocklist + the five write-boundary validations run on every body, and the container
// is the blast boundary. There is no model/agent path here — the route is operator-only behind
// RequireCapability(governance.write).
//
// SKW-03 restore-collision guard: the restore handler maps the provider's ErrSkillActiveExists
// sentinel to 409 — the provider stat'd active/{name} and returned the sentinel BEFORE
// Writer.Restore (which does an os.RemoveAll that would silently overwrite an active skill).
// The parent-mux mount behind RequireCapability(governance.write) is cmd/aura/serve_webui.go's
// job; every wire error passes through sanitizeErr (no leak).

// skillsWriteAdapter satisfies agui.SkillsWriteProvider over the live Phase-11 primitives.
// installer fetches + validates + stages; writer is the lifecycle sink
// (activate/restore/archive/create/update/delete); layout resolves the actor's own roots (for
// the scoped writer and for an honest post-install destination in the response).
// blocklist/bodyCapBytes are the SAME config values the Writer was built with, held here
// so Validate can dry-run the write boundary without reaching back into config.
//
// Every write leg acts on the ACTOR'S OWN library, and that is only sound because the board
// beside it now reads the same one. It was per-actor once before and run 1790 said out loud
// why not: the board read the deployment library, so an install vanished from the console
// that had just made it — a write landing where the read cannot see it is worse than either
// choice alone (amendment #216). The answer was never "write to the house library", it was
// "give the board an identity", and that is what this slice does: skillsBoardAdapter resolves
// its loader and its archive from the identity on the request, so the invariant #216 states —
// a console's write lands where its read looks — holds with BOTH halves scoped (#218 A).
//
// The actor is threaded through every method for a second, independent reason: it is what the
// audit ledger records. WHO did it and WHERE it landed are now the same identity, but they
// stay separate arguments because they answer different questions.
//
// A consequence to state rather than discover: the cockpit no longer edits the house library.
// A skill the deployment publishes is house policy, and the console of one person is not
// where policy is rewritten — `aura skills` with no --identity is (amendment #214, D-214-3).
type skillsWriteAdapter struct {
	installer    *skills.Installer
	writer       *skills.Writer
	layout       skills.Layout
	blocklist    []string
	bodyCapBytes int
	// capabilities answers whether the actor may write the HOUSE library. Nil means "nobody
	// does", which is the answer every caller got before this field existed.
	capabilities capabilityChecker
	// invalidate expires every cached loader snapshot the BOARD reads, so a completed write
	// is on the board of the person who made it when the call returns instead of up to a
	// snapshot TTL later. Nil in a composition with no loaders (the CLI-shaped tests).
	invalidate func()
}

// forActor resolves the Writer that writes AS the actor, into the actor's own root. An
// unscoped actor, or a deployment with no per-identity base configured, resolves back to the
// deployment-global Writer — which is exactly the pre-#214 behaviour.
func (a skillsWriteAdapter) forActor(actor string) (*skills.Writer, error) {
	return a.writer.For(actor)
}

// writerFor resolves the Writer a verb aimed at THIS NAME must use: the actor's own root when
// it holds the name, and the HOUSE root when it does not and the actor holds governance.write.
//
// Resolving by name rather than by actor alone is what makes the board's `owned` flag honest.
// The board lists the house library beside the caller's own, and #214 pointed every write at
// the caller's root, so "archive that row" reached a name their root does not hold as a matter
// of course; the operator was told their own library had no such skill about a row on screen.
//
// The fallback is deliberately narrow. It requires BOTH the capability and the house actually
// holding the name, and it never widens to a third root, so a share stays unwritable and a
// typo still fails in the caller's own library rather than silently addressing the
// deployment's.
func (a skillsWriteAdapter) writerFor(ctx context.Context, actor, name string) (*skills.Writer, error) {
	return a.rootHolding(ctx, actor, name, (*skills.Writer).ActiveExists)
}

// archiveWriterFor is writerFor for the verbs that address the ARCHIVE rather than the active
// root — Restore. Archive and Restore are two halves of one button, so resolving them by
// different rules is how an operator sends a house skill to the house archive and then cannot
// bring it back: the skill is not lost, but it is reachable from nowhere, which is worse than
// the disabled button this whole change replaced.
func (a skillsWriteAdapter) archiveWriterFor(ctx context.Context, actor, name string) (*skills.Writer, error) {
	return a.rootHolding(ctx, actor, name, (*skills.Writer).ArchivedExists)
}

// rootHolding resolves the Writer whose root actually HOLDS this name, per the supplied
// predicate: the actor's own root when it holds it, and the HOUSE root when it does not and the
// actor holds governance.write.
//
// Resolving by name rather than by actor alone is what makes the board's `owned` flag honest.
// The board lists the house library beside the caller's own, and #214 pointed every write at
// the caller's root, so "archive that row" reached a name their root does not hold as a matter
// of course; the operator was told their own library had no such skill about a row on screen.
//
// The fallback is deliberately narrow. It requires BOTH the capability and the house actually
// holding the name, and it never widens to a third root, so a share stays unwritable and a typo
// still fails in the caller's own library rather than silently addressing the deployment's.
func (a skillsWriteAdapter) rootHolding(
	ctx context.Context, actor, name string, holds func(*skills.Writer, string) bool,
) (*skills.Writer, error) {
	own, err := a.forActor(actor)
	if err != nil {
		return nil, err
	}
	if holds(own, name) || !holds(a.writer, name) {
		return own, nil
	}
	if !a.mayWriteHouse(ctx, actor) {
		return own, nil
	}
	return a.writer, nil
}

// mayWriteHouse answers whether this actor may address the deployment's own library. It fails
// closed and says so once: a write that cannot prove the permission stays in the caller's own
// root, where it fails with the sentinel that names their library rather than reaching into
// the house's.
func (a skillsWriteAdapter) mayWriteHouse(ctx context.Context, actor string) bool {
	if a.capabilities == nil {
		return false
	}
	allowed, err := a.capabilities.HasCapability(ctx, actor, governanceWriteCapability)
	if err != nil {
		slog.Warn("skills write: capability lookup failed, keeping the verb in the caller's root",
			"identity_id", actor, "capability", governanceWriteCapability, "err", err)
		return false
	}
	return allowed
}

// installerForActor is forActor's twin for the fetch transport, so an installed tree lands in
// the same root an authored one does.
func (a skillsWriteAdapter) installerForActor(actor string) (*skills.Installer, error) {
	if a.installer == nil {
		return nil, fmt.Errorf("%w: no installer is wired", agui.ErrSkillInvalidInput)
	}
	return a.installer.For(actor)
}

// done expires the shared loader snapshots after a write that changed the library.
func (a skillsWriteAdapter) done() {
	if a.invalidate != nil {
		a.invalidate()
	}
}

// Install fetches the skill through the Task-1 Installer (npx skills add → validate → write),
// which since amendment #97 lands it active + materialized + audited in one call. It returns
// the SkillsInstallInfo (source/hash/preview, the active destination, status "active", the five
// validation checks that ran). An empty source is rejected with ErrSkillInvalidInput → 400; an
// invalid structure / blocklist hit is a client-correctable 400.
func (a skillsWriteAdapter) Install(ctx context.Context, actor, source string) (agui.SkillsInstallInfo, error) {
	if source == "" {
		return agui.SkillsInstallInfo{}, fmt.Errorf("%w: install source is empty", agui.ErrSkillInvalidInput)
	}
	installer, err := a.installerForActor(actor)
	if err != nil {
		return agui.SkillsInstallInfo{}, err
	}
	auditActor := skills.AuditActor{ActorID: "operator", IdentityID: actor}
	info, err := installer.Install(ctx, source, auditActor)
	if err != nil {
		// An invalid structure / blocklist hit / empty source from the Installer is a
		// client-correctable input, surfaced as a safe 400.
		if errors.Is(err, skills.ErrInvalidStructure) || errors.Is(err, skills.ErrBlocklisted) {
			return agui.SkillsInstallInfo{}, fmt.Errorf("%w: %v", agui.ErrSkillInvalidInput, err)
		}
		return agui.SkillsInstallInfo{}, err
	}

	a.done()
	return agui.SkillsInstallInfo{
		Name:        info.Name,
		Source:      info.Source,
		ContentHash: info.ContentHash,
		Preview:     info.Preview,
		Destination: filepath.Join(a.activeDir(actor), info.Name),
		RiskTier:    info.RiskTier,
		Status:      "active",
		Checklist:   toSkillsChecklist(info.Checklist),
	}, nil
}

// Search runs the catalog query (external discovery is on by default; an explicit
// AURA_SKILLS_EXTERNAL_DISCOVERY=false opt-out disables the network fetch and returns a
// disabled result with the toggle state explicit).
func (a skillsWriteAdapter) Search(ctx context.Context, q string) (agui.SkillsCatalogResult, error) {
	// Search reaches no root at all — it is a catalog query — so it stays on the shared
	// installer rather than paying a per-actor resolution for a network fetch.
	res, err := a.installer.Search(ctx, q)
	if err != nil {
		return agui.SkillsCatalogResult{}, err
	}
	hits := make([]agui.SkillsCatalogHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		hits = append(hits, agui.SkillsCatalogHit{Source: h.Source, Skill: h.Skill, Installs: h.Installs})
	}
	return agui.SkillsCatalogResult{Enabled: res.Enabled, Query: res.Query, Hits: hits}, nil
}

// Restore guards the restore-collision landmine (SKW-03): it returns ErrSkillActiveExists
// (→ 409) BEFORE Writer.Restore (which does an os.RemoveAll on active/{name}) when an active
// skill of the same name exists; otherwise it restores + re-materializes + audits.
func (a skillsWriteAdapter) Restore(ctx context.Context, actor, name string) error {
	w, err := a.archiveWriterFor(ctx, actor, name)
	if err != nil {
		return err
	}
	if w.ActiveExists(name) {
		return fmt.Errorf("%w: %q", agui.ErrSkillActiveExists, name)
	}
	if err := w.Restore(ctx, name, skills.ApprovalCLI, skills.AuditActor{ActorID: "operator", IdentityID: actor}); err != nil {
		return clientSkillError(err)
	}
	a.done()
	return nil
}

// Archive de-materializes + moves active/{name} → archived + audits (SKW-03).
func (a skillsWriteAdapter) Archive(ctx context.Context, actor, name string) error {
	if err := refuseBuiltin("archive", name); err != nil {
		return err
	}
	w, err := a.writerFor(ctx, actor, name)
	if err != nil {
		return err
	}
	if err := w.Archive(ctx, name, skills.ApprovalCLI, skills.AuditActor{ActorID: "operator", IdentityID: actor}); err != nil {
		return clientSkillError(err)
	}
	a.done()
	return nil
}

// refuseBuiltin rejects a lifecycle verb aimed at one of Aura's own skills, and it is what
// keeps the board and the verb saying the same thing: the board stopped offering Archive and
// Delete on a builtin, so performing one here would put back the split between what a console
// shows and what its write does.
//
// The refusal is honest rather than protective. MaterializeBuiltins rewrites a builtin at the
// next boot whenever the on-disk bytes differ from the embedded ones, so the verb does not fail
// to remove the skill — it removes it until the next restart, which is a change that undoes
// itself and an audit row that describes something no longer true. ErrSkillInvalidInput so the
// route answers 400: this is a mistake to see, not an outage.
func refuseBuiltin(verb, name string) error {
	if !skills.IsBuiltin(name) {
		return nil
	}
	return fmt.Errorf("%w: %q is one of Aura's own skills and the next boot restores it; %s is not available for it",
		agui.ErrSkillInvalidInput, name, verb)
}

// clientSkillError re-labels the lifecycle failures the CALLER can act on — a name this
// actor's root does not hold, a name the grammar refuses — so the route answers 400 instead of
// the sanitized 502 the default arm produces.
//
// It became load-bearing when the cockpit went back to writing into the ACTOR'S root (#218 A).
// The board still lists the HOUSE library beside the actor's own (D-214-3), so "archive that
// row" now reaches a name the actor's own root does not hold as a matter of course, and
// Writer.Archive reports that as a failed rename rather than as ErrUnknownSkill. Handing an
// operator a gateway error for something they typed is the exact miscategorisation Mutate's own
// comment already refuses.
//
// It does NOT give the operator a way to manage the house library from the cockpit — nothing
// here does, and that gap is #218's declared consequence, not this helper's to close.
func clientSkillError(err error) error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, skills.ErrUnknownSkill) || errors.Is(err, skills.ErrInvalidName) {
		return fmt.Errorf("%w: %v", agui.ErrSkillInvalidInput, err)
	}
	return err
}

// Mutate wraps Writer.WriteMutationByName for create/update (SKW-01). The actor is
// labelled "operator" (not "model"), which is now purely a ledger attribution: every actor
// takes the same write path and the mutation is live when this returns.
//
// It maps the two client-correctable sentinels the way Install already did. Without that
// mapping an invalid body or a blocklist hit rendered as a sanitized 502 — an operator
// typo reported as a backend outage, with nothing they could act on.
func (a skillsWriteAdapter) Mutate(ctx context.Context, actor, action, name, description, body string, always bool) (string, error) {
	w, err := a.forActor(actor)
	if err != nil {
		return "", err
	}
	status, err := w.WriteMutationByName(ctx, action, name, description, body, always,
		skills.AuditActor{ActorID: "operator", IdentityID: actor})
	if err != nil {
		if errors.Is(err, skills.ErrInvalidStructure) || errors.Is(err, skills.ErrBlocklisted) || errors.Is(err, skills.ErrInvalidName) {
			return "", fmt.Errorf("%w: %v", agui.ErrSkillInvalidInput, err)
		}
		return "", err
	}
	a.done()
	return status, nil
}

// Validate dry-runs the write boundary for the cockpit editor. It calls the SAME
// skills.ValidateForWrite the Writer calls, with the SAME frontmatter shape
// WriteMutationByName builds (type instruction — the cockpit editor authors instruction
// skills; a snippet needs a language and goes through SaveSnippet), and the SAME
// config-supplied blocklist and body cap. Nothing is written and no audit row is cut.
//
// allowBlocklisted is false here for the same reason it is false for the model: the D-27
// operator override belongs to the CLI, where the gate has already shown the operator the
// matched sequence. An editor that offered "save anyway" would be that override without
// the gate.
func (a skillsWriteAdapter) Validate(name, description, body string, always bool) agui.SkillsValidation {
	fm := skills.Frontmatter{Name: name, Description: description, Type: skills.TypeInstruction, Always: always}
	err := skills.ValidateForWrite(fm, body, a.blocklist, a.bodyCapBytes, false)
	if err == nil {
		return agui.SkillsValidation{OK: true}
	}
	// The message goes out verbatim, unlike a backend/FS error: these are authored to be
	// read by an operator (they name the limit, the measured size, the matched sequence
	// and its offset) and are computed from the submitted draft alone — no path, no DSN,
	// no secret can reach them.
	return agui.SkillsValidation{
		Field:   skills.FieldForWriteError(err),
		Message: err.Error(),
	}
}

// Delete removes the skill: de-materialized from the /skills mount, gone from the active
// root, one audit row. Writer.Delete does the real work behind its SanitizeName
// chokepoint; its status return is discarded because the route answers 204 with no body.
func (a skillsWriteAdapter) Delete(ctx context.Context, actor, name string) error {
	if err := refuseBuiltin("delete", name); err != nil {
		return err
	}
	w, err := a.writerFor(ctx, actor, name)
	if err != nil {
		return err
	}
	if _, err := w.Delete(ctx, name, skills.AuditActor{ActorID: "operator", IdentityID: actor}); err != nil {
		return clientSkillError(err)
	}
	a.done()
	return nil
}

// toSkillsChecklist projects the skills-package CheckItem list onto the agui wire type.
func toSkillsChecklist(in []skills.CheckItem) []agui.SkillsCheckItem {
	out := make([]agui.SkillsCheckItem, 0, len(in))
	for _, c := range in {
		out = append(out, agui.SkillsCheckItem{Label: c.Label, Passed: c.Passed})
	}
	return out
}

// buildSkillsWriteProvider constructs the concrete skills write provider best-effort: a nil
// pool (no DB) or a missing skills dir leaves it nil → the routes answer 503. Never aborts
// boot (the SetGovernanceProviders precedent). It reuses the SAME newSkillWriter wiring the
// CLI + model path use, so the cockpit operates on one set of dirs.
func buildSkillsWriteProvider(cfg *config.Config, pool *pgxpool.Pool, loaders *identityLoaders) agui.SkillsWriteProvider {
	if cfg == nil || pool == nil || cfg.SkillsDir == "" {
		return nil
	}
	writer := newSkillWriter(cfg, pool)
	installer := skills.NewInstaller(skills.InstallerConfig{
		Writer:       writer,
		Blocklist:    cfg.SkillInjectionBlocklist,
		BodyCapBytes: cfg.SkillBodyCapBytes,
		// The clone + --copy work tree must land on a spacious, exec-capable volume, never the
		// hardened 64M noexec /tmp tmpfs — the run dir is the transient-artifact volume.
		WorkDir: cfg.RunDir,
	})
	adapter := skillsWriteAdapter{
		installer:    installer,
		writer:       writer,
		layout:       skillLayout(cfg),
		blocklist:    cfg.SkillInjectionBlocklist,
		bodyCapBytes: cfg.SkillBodyCapBytes,
		// The same store agui.RequireCapability asks at the route mount, so what the board
		// offers and what the verb reaches are one fact read twice, not two rules that drift.
		capabilities: identity.New(pool),
	}
	if loaders != nil {
		adapter.invalidate = loaders.invalidateAll
	}
	return adapter
}

// activeDir is where this actor's write landed, quoted back in the install response so an
// operator is told the truth about the destination rather than the deployment's own root. It
// is resolved from the SAME layout skills.Writer.For uses, so the two cannot disagree.
func (a skillsWriteAdapter) activeDir(actor string) string {
	roots, err := a.layout.For(actor)
	if err != nil {
		return a.layout.Global
	}
	return roots.WritableRoot()
}
