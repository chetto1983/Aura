package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/skills"
)

// serve_governance_skills_board_test.go pins the cockpit board's answer to "whose skills is
// this console showing" (amendment #218, superseding the stopgap of #216). The board is a
// person's console: their own library, the house's over the top on a name collision, and what
// other identities have shared with them — never another person's.
//
// It also pins the invariant #216 states and could not then satisfy: A CONSOLE'S WRITE LANDS
// WHERE ITS READ LOOKS. That is asserted here as a property of the two adapters' directory
// arithmetic, which needs no database; the live proof that a real install shows up on the
// right board is the db_integration tier (TestSkillsInstallActivatesDirectly).

// boardFor builds the board adapter over a config, with an optional SharedReader.
func boardFor(cfg *config.Config, shared *skills.SharedReader) skillsBoardAdapter {
	loaders := newIdentityLoaders(cfg, shared)
	return skillsBoardAdapter{loaders: loaders, layout: skillLayout(cfg)}
}

// boardNames lists the names one identity's board shows.
func boardNames(board skillsBoardAdapter, identity string) []string {
	ctx := identityctx.WithIdentityID(context.Background(), identity)
	loaded := board.ActiveSkills(ctx)
	out := make([]string, 0, len(loaded))
	for _, sk := range loaded {
		out = append(out, sk.Name)
	}
	return out
}

// has reports whether names contains name.
func has(names []string, name string) bool { return slices.Contains(names, name) }

// TestSkillsBoardShowsTheCallerTheirOwnLibrary is the core of the scoping: alice sees her own
// skill and the house's, bob sees his own and the house's, and neither sees the other's.
func TestSkillsBoardShowsTheCallerTheirOwnLibrary(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedSkill(t, cfg.SkillsDir, "house-rule", "house body", false)
	seedSkill(t, filepath.Join(cfg.SkillsIdentityDir, rootsAlice), "alice-only", "alice body", false)
	seedSkill(t, filepath.Join(cfg.SkillsIdentityDir, rootsBob), "bob-only", "bob body", false)

	board := boardFor(cfg, nil)

	alice := boardNames(board, rootsAlice)
	if !has(alice, "alice-only") || !has(alice, "house-rule") {
		t.Fatalf("alice's board = %v, want her own skill and the house's", alice)
	}
	if has(alice, "bob-only") {
		t.Fatalf("alice's board = %v, must NOT carry another person's skill", alice)
	}

	bob := boardNames(board, rootsBob)
	if !has(bob, "bob-only") || !has(bob, "house-rule") {
		t.Fatalf("bob's board = %v, want his own skill and the house's", bob)
	}
	if has(bob, "alice-only") {
		t.Fatalf("bob's board = %v, must NOT carry another person's skill", bob)
	}

	// An unscoped read is the deployment library alone, exactly as before the slice.
	unscoped := boardNames(board, "")
	if !has(unscoped, "house-rule") || has(unscoped, "alice-only") || has(unscoped, "bob-only") {
		t.Fatalf("unscoped board = %v, want the deployment library alone", unscoped)
	}
}

// TestSkillsBoardHouseWinsANameCollision is D-214-3 seen from the board: a person who writes a
// skill named like a house one gets the HOUSE body, because a skill the operator ships is
// policy and a silent shadow is how policy stops applying.
func TestSkillsBoardHouseWinsANameCollision(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedSkill(t, cfg.SkillsDir, "deploy", "HOUSE BODY", false)
	seedSkill(t, filepath.Join(cfg.SkillsIdentityDir, rootsAlice), "deploy", "ALICE BODY", false)

	board := boardFor(cfg, nil)
	body, ok := board.SkillBody(identityctx.WithIdentityID(context.Background(), rootsAlice), "deploy")
	if !ok {
		t.Fatal("the colliding name must still resolve")
	}
	if !strings.Contains(body, "HOUSE BODY") {
		t.Fatalf("body = %q, want the HOUSE one (D-214-3)", body)
	}
}

// TestSkillsBoardCarriesASharedSkill proves the third source: what somebody else has shared
// reaches the board through the SAME SharedReader join the model's loader and the box use, so
// the listing a person reads and the tree their box holds cannot disagree (amendment #217).
func TestSkillsBoardCarriesASharedSkill(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	layout := skillLayout(cfg)
	aliceRoots, err := layout.For(rootsAlice)
	if err != nil {
		t.Fatalf("layout for alice: %v", err)
	}
	// The body is read from the OWNER'S EXPORT, never from her root: the export holds exactly
	// the active skills, so an archived skill stops being readable by its grantees at the same
	// moment it stops being readable by its owner.
	seedSkill(t, aliceRoots.Export, "alice-share", "SHARED BODY", false)

	shared := sharedWith(cfg, rootsBob, skills.CatalogRow{ID: "row-1", OwnerID: rootsAlice, Name: "alice-share"})
	board := boardFor(cfg, shared)

	if names := boardNames(board, rootsBob); !has(names, "alice-share") {
		t.Fatalf("bob's board = %v, want the skill alice shared with him", names)
	}
	// Nobody else's grant, nobody else's skill: alice's own board is unaffected and carol,
	// who holds no grant, sees nothing.
	if names := boardNames(board, rootsCarol); has(names, "alice-share") {
		t.Fatalf("carol's board = %v, must not carry a grant she does not hold", names)
	}
}

// TestSkillsBoardArchiveFollowsTheWritableRoot is the archive half of the scoping. Leaving it
// global while the active list is per identity is the configuration amendment #216 measured as
// worse than either whole choice, so the directory is asserted directly — and against the
// writer's own answer, not against a second rule written here.
func TestSkillsBoardArchiveFollowsTheWritableRoot(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	board := boardFor(cfg, nil)

	if got, want := board.archiveDir(rootsAlice), filepath.Join(cfg.SkillsIdentityDir, rootsAlice, skills.StageArchived); got != want {
		t.Fatalf("alice's archive = %q, want %q", got, want)
	}
	if got, want := board.archiveDir(""), filepath.Join(cfg.SkillsDir, skills.StageArchived); got != want {
		t.Fatalf("unscoped archive = %q, want the deployment's %q", got, want)
	}
	// A crafted identity falls back to the deployment archive rather than escaping the base
	// or listing nothing with no line saying why — the same answer skillLoaderRoots gives.
	if got, want := board.archiveDir("../escape"), filepath.Join(cfg.SkillsDir, skills.StageArchived); got != want {
		t.Fatalf("crafted-identity archive = %q, want %q", got, want)
	}
}

// TestCockpitWriteLandsWhereItsBoardLooks is the invariant amendment #216 named. For every
// actor the write adapter's destination is a root the board scans, and the archive the board
// lists is the archive the actor's own writer archives into. A regression that scopes one half
// and not the other fails here without a database.
func TestCockpitWriteLandsWhereItsBoardLooks(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	board := boardFor(cfg, nil)
	write := skillsWriteAdapter{
		writer: skills.NewWriter(skills.WriterConfig{
			ActiveDir:    cfg.SkillsDir,
			ExportDir:    cfg.SkillExportDir,
			ArchiveDir:   filepath.Join(cfg.SkillsDir, skills.StageArchived),
			Layout:       skillLayout(cfg),
			BodyCapBytes: cfg.SkillBodyCapBytes,
		}),
		layout: skillLayout(cfg),
	}

	for _, actor := range []string{"", rootsAlice, rootsBob} {
		w, err := write.forActor(actor)
		if err != nil {
			t.Fatalf("forActor(%q): %v", actor, err)
		}
		landing := w.ActiveDir()
		if landing != write.activeDir(actor) {
			t.Fatalf("actor %q: reported destination %q != the writer's root %q", actor, write.activeDir(actor), landing)
		}
		if !has(skillLoaderRoots(cfg, actor), landing) {
			t.Fatalf("actor %q: writes land in %q, which the board's roots %v do not scan",
				actor, landing, skillLoaderRoots(cfg, actor))
		}
		if got, want := board.archiveDir(actor), filepath.Join(landing, skills.StageArchived); got != want {
			t.Fatalf("actor %q: board archive %q != the writer's archive %q", actor, got, want)
		}
	}
}
