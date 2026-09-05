package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
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
// (activate/restore/archive/create/update/delete); activeDir is the active-skill root (for an
// honest post-install destination in the response).
// blocklist/bodyCapBytes are the SAME config values the Writer was built with, held here
// so Validate can dry-run the write boundary without reaching back into config.
//
// Every write leg is scoped to the ACTOR (amendment #214). The provider seam already carried
// an actor on each method because the ledger wanted it; what changes is that the actor now
// also decides WHERE the write lands, so a cockpit install by one person no longer appears in
// everybody else's agent. The fields below stay the deployment-global primitives — forActor
// derives the scoped pair per call, and an unscoped actor gets exactly them.
type skillsWriteAdapter struct {
	installer    *skills.Installer
	writer       *skills.Writer
	activeDir    string
	blocklist    []string
	bodyCapBytes int
}

// scopedSkillWrite is one actor's write surface: their Writer, their Installer, and the root
// their skills actually land in (which the install response quotes, so an operator is told
// the truth about where the tree went).
type scopedSkillWrite struct {
	writer    *skills.Writer
	installer *skills.Installer
	activeDir string
}

// forActor resolves the write surface for one actor. An actor whose identity cannot name a
// directory is an error, not a silent fallback to the shared root: writing somebody's skill
// into the house library because their id looked odd is the failure this slice removes.
func (a skillsWriteAdapter) forActor(actor string) (scopedSkillWrite, error) {
	writer, err := a.writer.For(actor)
	if err != nil {
		return scopedSkillWrite{}, err
	}
	installer, err := a.installer.For(actor)
	if err != nil {
		return scopedSkillWrite{}, err
	}
	out := scopedSkillWrite{writer: writer, installer: installer, activeDir: a.activeDir}
	if writer != a.writer {
		out.activeDir = writer.ActiveDir()
	}
	return out, nil
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
	scoped, err := a.forActor(actor)
	if err != nil {
		return agui.SkillsInstallInfo{}, err
	}
	auditActor := skills.AuditActor{ActorID: "operator", IdentityID: actor}
	info, err := scoped.installer.Install(ctx, source, auditActor)
	if err != nil {
		// An invalid structure / blocklist hit / empty source from the Installer is a
		// client-correctable input, surfaced as a safe 400.
		if errors.Is(err, skills.ErrInvalidStructure) || errors.Is(err, skills.ErrBlocklisted) {
			return agui.SkillsInstallInfo{}, fmt.Errorf("%w: %v", agui.ErrSkillInvalidInput, err)
		}
		return agui.SkillsInstallInfo{}, err
	}

	return agui.SkillsInstallInfo{
		Name:        info.Name,
		Source:      info.Source,
		ContentHash: info.ContentHash,
		Preview:     info.Preview,
		Destination: filepath.Join(scoped.activeDir, info.Name),
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
	scoped, err := a.forActor(actor)
	if err != nil {
		return err
	}
	if scoped.writer.ActiveExists(name) {
		return fmt.Errorf("%w: %q", agui.ErrSkillActiveExists, name)
	}
	return scoped.writer.Restore(ctx, name, skills.ApprovalCLI, skills.AuditActor{ActorID: "operator", IdentityID: actor})
}

// Archive de-materializes + moves active/{name} → archived + audits (SKW-03).
func (a skillsWriteAdapter) Archive(ctx context.Context, actor, name string) error {
	scoped, err := a.forActor(actor)
	if err != nil {
		return err
	}
	return scoped.writer.Archive(ctx, name, skills.ApprovalCLI, skills.AuditActor{ActorID: "operator", IdentityID: actor})
}

// Mutate wraps Writer.WriteMutationByName for create/update (SKW-01). The actor is
// labelled "operator" (not "model"), which is now purely a ledger attribution: every actor
// takes the same write path and the mutation is live when this returns.
//
// It maps the two client-correctable sentinels the way Install already did. Without that
// mapping an invalid body or a blocklist hit rendered as a sanitized 502 — an operator
// typo reported as a backend outage, with nothing they could act on.
func (a skillsWriteAdapter) Mutate(ctx context.Context, actor, action, name, description, body string, always bool) (string, error) {
	scoped, err := a.forActor(actor)
	if err != nil {
		return "", err
	}
	status, err := scoped.writer.WriteMutationByName(ctx, action, name, description, body, always,
		skills.AuditActor{ActorID: "operator", IdentityID: actor})
	if err != nil {
		if errors.Is(err, skills.ErrInvalidStructure) || errors.Is(err, skills.ErrBlocklisted) || errors.Is(err, skills.ErrInvalidName) {
			return "", fmt.Errorf("%w: %v", agui.ErrSkillInvalidInput, err)
		}
		return "", err
	}
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
	scoped, err := a.forActor(actor)
	if err != nil {
		return err
	}
	if _, err := scoped.writer.Delete(ctx, name, skills.AuditActor{ActorID: "operator", IdentityID: actor}); err != nil {
		if errors.Is(err, skills.ErrInvalidName) || errors.Is(err, skills.ErrUnknownSkill) {
			return fmt.Errorf("%w: %v", agui.ErrSkillInvalidInput, err)
		}
		return err
	}
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
func buildSkillsWriteProvider(cfg *config.Config, pool *pgxpool.Pool) agui.SkillsWriteProvider {
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
	return skillsWriteAdapter{
		installer:    installer,
		writer:       writer,
		activeDir:    cfg.SkillsDir,
		blocklist:    cfg.SkillInjectionBlocklist,
		bodyCapBytes: cfg.SkillBodyCapBytes,
	}
}
