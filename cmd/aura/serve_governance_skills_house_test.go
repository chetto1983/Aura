package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/skills"
)

// serve_governance_skills_house_test.go pins who may act on the HOUSE library.
//
// #214 scoped every cockpit write to the caller's own root and pointed at `aura skills` with
// no --identity as the place house policy is edited instead. That CLI never grew an `archive`
// verb, so from 2026-09-06 a house skill was archivable from no surface at all — measured on
// the operator's own deployment, where every skill they had ever installed predates the
// per-identity roots, sits in the house root, and lost its buttons the moment the image was
// rebuilt.
//
// The capability is the boundary, not the directory: governance.write already means "may
// change shared deployment configuration" (skill_manage.go says so in those words) and the
// cockpit's write routes are mounted behind it. A tenant without it sees the house library
// exactly as #214 left it — listed, runnable, not theirs to touch.

// stubCapabilities is the identity store's one answer, stated rather than granted through a
// database: identity -> holds governance.write.
type stubCapabilities map[string]bool

func (s stubCapabilities) HasCapability(_ context.Context, identityID, capability string) (bool, error) {
	if capability != governanceWriteCapability {
		return false, nil
	}
	return s[identityID], nil
}

// failingCapabilities is the store that cannot answer, so the fail-closed branch is asserted
// on a real error rather than on a missing grant.
type failingCapabilities struct{}

func (failingCapabilities) HasCapability(context.Context, string, string) (bool, error) {
	return false, errors.New("capability store unreachable")
}

// TestSkillsBoardOffersTheHouseVerbsToAnOperator is the regression this file exists for: an
// identity holding governance.write gets the deployment root as a second writable root, so the
// board can offer Archive/Delete on house policy instead of rendering dead buttons.
func TestSkillsBoardOffersTheHouseVerbsToAnOperator(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedSkill(t, cfg.SkillsDir, "house-rule", "house body", false)

	board := boardFor(cfg, nil)
	board.capabilities = stubCapabilities{rootsAlice: true}

	operator := identityctx.WithIdentityID(context.Background(), rootsAlice)
	if got := board.WritableHouseRoot(operator); got != cfg.SkillsDir {
		t.Fatalf("operator's writable house root = %q, want the deployment root %q", got, cfg.SkillsDir)
	}
}

// TestSkillsBoardKeepsTheHouseFromATenant is the other half, and the reason this is a
// capability check rather than a widening: a person without governance.write must see the
// house library exactly as #214 left it.
func TestSkillsBoardKeepsTheHouseFromATenant(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedSkill(t, cfg.SkillsDir, "house-rule", "house body", false)

	board := boardFor(cfg, nil)
	board.capabilities = stubCapabilities{rootsAlice: true}

	tenant := identityctx.WithIdentityID(context.Background(), rootsBob)
	if got := board.WritableHouseRoot(tenant); got != "" {
		t.Fatalf("a tenant without governance.write got house root %q, want none", got)
	}
}

// TestSkillsBoardFailsClosedOnACapabilityError pins the direction of the failure: a governance
// read that cannot PROVE the permission must not grant it. Without this a store outage would
// hand every caller the house verbs.
func TestSkillsBoardFailsClosedOnACapabilityError(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	board := boardFor(cfg, nil)
	board.capabilities = failingCapabilities{}

	ctx := identityctx.WithIdentityID(context.Background(), rootsAlice)
	if got := board.WritableHouseRoot(ctx); got != "" {
		t.Fatalf("house root on a store error = %q, want none", got)
	}
}

// houseWriteAdapter builds the cockpit's write adapter over a temp tree. The pool is nil on
// purpose: choosing WHICH root a verb lands in is directory arithmetic, and a database would
// only make the assertion slower and less clear. The audit write that does need one is proven
// by the db_integration tier.
func houseWriteAdapter(cfg *config.Config, caps capabilityChecker) skillsWriteAdapter {
	return skillsWriteAdapter{
		writer:       newSkillWriter(cfg, nil),
		layout:       skillLayout(cfg),
		capabilities: caps,
	}
}

// TestSkillsWriteReachesTheHouseForAnOperator is the other half of the regression, and the
// half that matters: the board can mark the row actionable all it likes, but if the verb still
// resolves to the caller's own root then Archive answers "no active skill by that name" and the
// enabled button is worse than the disabled one was.
func TestSkillsWriteReachesTheHouseForAnOperator(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedSkill(t, cfg.SkillsDir, "house-rule", "house body", false)
	seedSkill(t, filepath.Join(cfg.SkillsIdentityDir, rootsAlice), "alice-only", "alice body", false)

	adapter := houseWriteAdapter(cfg, stubCapabilities{rootsAlice: true})

	own, err := adapter.writerFor(context.Background(), rootsAlice, "alice-only")
	if err != nil {
		t.Fatalf("writerFor own skill: %v", err)
	}
	if got, want := own.ActiveDir(), filepath.Join(cfg.SkillsIdentityDir, rootsAlice); got != want {
		t.Fatalf("own skill lands in %q, want the caller's own root %q", got, want)
	}

	house, err := adapter.writerFor(context.Background(), rootsAlice, "house-rule")
	if err != nil {
		t.Fatalf("writerFor house skill: %v", err)
	}
	if got := house.ActiveDir(); got != cfg.SkillsDir {
		t.Fatalf("house skill lands in %q, want the deployment root %q", got, cfg.SkillsDir)
	}
}

// TestSkillsWriteKeepsATenantOutOfTheHouse pins the fence. A tenant's verb must stay in their
// own root even for a name only the house holds, so it fails with the sentinel that names
// their library rather than reaching into the deployment's.
func TestSkillsWriteKeepsATenantOutOfTheHouse(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedSkill(t, cfg.SkillsDir, "house-rule", "house body", false)

	adapter := houseWriteAdapter(cfg, stubCapabilities{rootsAlice: true})

	w, err := adapter.writerFor(context.Background(), rootsBob, "house-rule")
	if err != nil {
		t.Fatalf("writerFor: %v", err)
	}
	if got, want := w.ActiveDir(), filepath.Join(cfg.SkillsIdentityDir, rootsBob); got != want {
		t.Fatalf("a tenant's verb landed in %q, want their own root %q", got, want)
	}
}

// TestSkillsWriteRefusesABuiltin keeps the board and the verb saying the same thing. The board
// stopped offering Archive/Delete on a builtin; if the verb still performed one, the split
// between what a console shows and what its write does would be back — which is the entire
// class of defect this file exists to close. The refusal is a client error, not a 502: asking
// to archive product code is a mistake to see, not an outage.
func TestSkillsWriteRefusesABuiltin(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	builtin := skills.BuiltinNames()[0]
	seedSkill(t, cfg.SkillsDir, builtin, "builtin body", false)

	adapter := houseWriteAdapter(cfg, stubCapabilities{rootsAlice: true})
	ctx := context.Background()

	if err := adapter.Archive(ctx, rootsAlice, builtin); err == nil {
		t.Fatalf("Archive(%q) succeeded: the next boot rewrites it, so the verb undoes itself", builtin)
	}
	if err := adapter.Delete(ctx, rootsAlice, builtin); err == nil {
		t.Fatalf("Delete(%q) succeeded: the next boot rewrites it, so the verb undoes itself", builtin)
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillsDir, builtin, "SKILL.md")); err != nil {
		t.Fatalf("the refused verbs must leave the builtin in place: %v", err)
	}
}

// seedArchivedSkill writes a minimal archived skill under root/archived.
func seedArchivedSkill(t *testing.T, root, name string) {
	t.Helper()
	seedSkill(t, filepath.Join(root, skills.StageArchived), name, "archived body", false)
}

// TestSkillsRestoreReachesTheHouseForAnOperator is Archive's other half. A verb that can send
// a house skill to the house archive and then cannot bring it back has not restored anything —
// it has lost the skill, which is worse than the disabled button it replaced.
func TestSkillsRestoreReachesTheHouseForAnOperator(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedArchivedSkill(t, cfg.SkillsDir, "house-parked")
	seedArchivedSkill(t, filepath.Join(cfg.SkillsIdentityDir, rootsAlice), "alice-parked")

	adapter := houseWriteAdapter(cfg, stubCapabilities{rootsAlice: true})

	own, err := adapter.archiveWriterFor(context.Background(), rootsAlice, "alice-parked")
	if err != nil {
		t.Fatalf("archiveWriterFor own: %v", err)
	}
	if got, want := own.ActiveDir(), filepath.Join(cfg.SkillsIdentityDir, rootsAlice); got != want {
		t.Fatalf("own restore lands in %q, want %q", got, want)
	}

	house, err := adapter.archiveWriterFor(context.Background(), rootsAlice, "house-parked")
	if err != nil {
		t.Fatalf("archiveWriterFor house: %v", err)
	}
	if got := house.ActiveDir(); got != cfg.SkillsDir {
		t.Fatalf("house restore lands in %q, want the deployment root %q", got, cfg.SkillsDir)
	}
}

// TestSkillsBoardListsTheHouseArchiveForAnOperator closes the third side of the triangle: the
// archive LISTING must show what Archive produced, or an operator archives house policy and
// watches it vanish from a board that can no longer offer Restore.
func TestSkillsBoardListsTheHouseArchiveForAnOperator(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedArchivedSkill(t, cfg.SkillsDir, "house-parked")
	seedArchivedSkill(t, filepath.Join(cfg.SkillsIdentityDir, rootsAlice), "alice-parked")

	board := boardFor(cfg, nil)
	board.capabilities = stubCapabilities{rootsAlice: true}

	staged, err := board.ArchivedSkills(identityctx.WithIdentityID(context.Background(), rootsAlice))
	if err != nil {
		t.Fatalf("ArchivedSkills: %v", err)
	}
	names := make(map[string]bool, len(staged))
	for _, sk := range staged {
		names[sk.Name] = true
	}
	if !names["alice-parked"] || !names["house-parked"] {
		t.Fatalf("an operator's archive = %v, want their own AND the house's", names)
	}
}

// TestSkillsBoardKeepsTheHouseArchiveFromATenant is the fence on the listing: a tenant sees
// their own archive and nothing else, exactly as before.
func TestSkillsBoardKeepsTheHouseArchiveFromATenant(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedArchivedSkill(t, cfg.SkillsDir, "house-parked")
	seedArchivedSkill(t, filepath.Join(cfg.SkillsIdentityDir, rootsBob), "bob-parked")

	board := boardFor(cfg, nil)
	board.capabilities = stubCapabilities{rootsAlice: true}

	staged, err := board.ArchivedSkills(identityctx.WithIdentityID(context.Background(), rootsBob))
	if err != nil {
		t.Fatalf("ArchivedSkills: %v", err)
	}
	for _, sk := range staged {
		if sk.Name == "house-parked" {
			t.Fatalf("a tenant's archive carries the house's: %+v", staged)
		}
	}
}

// TestSkillsBoardWithoutACapabilitySourceKeepsThePreviousAnswer covers the composition that
// wires no store at all (the CLI-shaped tests, and any future caller that omits it): the board
// must behave exactly as it did before this file existed rather than defaulting to open.
func TestSkillsBoardWithoutACapabilitySourceKeepsThePreviousAnswer(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	board := boardFor(cfg, nil)

	ctx := identityctx.WithIdentityID(context.Background(), rootsAlice)
	if got := board.WritableHouseRoot(ctx); got != "" {
		t.Fatalf("house root with no capability source = %q, want none", got)
	}
}
