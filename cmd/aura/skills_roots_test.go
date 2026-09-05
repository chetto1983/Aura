package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/skills"
)

// skills_roots_test.go pins the composition root's answer to "whose skills does this reader
// see" (amendment #214): the scan roots and their PRECEDENCE, the per-identity always-block,
// and the sources the box is filled from.

const (
	rootsAlice = "99999999-9999-4999-8999-999999999999"
	rootsBob   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

// rootsConfig builds a config over a temp tree with all three skill bases set.
func rootsConfig(t *testing.T) *config.Config {
	t.Helper()
	base := t.TempDir()
	return &config.Config{
		SkillsDir:             filepath.Join(base, "skills"),
		SkillsIdentityDir:     filepath.Join(base, "skills-identities"),
		SkillExportDir:        filepath.Join(base, "export"),
		SkillBodyCapBytes:     32768,
		SkillManifestCapBytes: 8192,
	}
}

// seedSkill writes a minimal valid skill into root, optionally always-on.
func seedSkill(t *testing.T, root, name, body string, always bool) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	md := "---\nname: " + name + "\ndescription: " + name + " description\ntype: instruction\n"
	if always {
		md += "always: true\n"
	}
	md += "---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
}

// TestSkillLoaderRootsOrdersGlobalLast is D-214-3 at the composition root: the Loader merges
// later-root-wins, so the deployment's root must come LAST or a personal skill would shadow
// a house one without anybody noticing.
func TestSkillLoaderRootsOrdersGlobalLast(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)

	if got := skillLoaderRoots(cfg, ""); len(got) != 1 || got[0] != cfg.SkillsDir {
		t.Fatalf("unscoped roots = %v, want only the deployment root", got)
	}
	got := skillLoaderRoots(cfg, rootsAlice)
	want := []string{filepath.Join(cfg.SkillsIdentityDir, rootsAlice), cfg.SkillsDir}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("scoped roots = %v, want %v (identity first so the deployment wins a collision)", got, want)
	}

	// Per-identity skills switched off: the identity reads the deployment library alone,
	// which is the pre-#214 behaviour byte for byte.
	off := rootsConfig(t)
	off.SkillsIdentityDir = ""
	if got := skillLoaderRoots(off, rootsAlice); len(got) != 1 || got[0] != off.SkillsDir {
		t.Fatalf("roots with no identity base = %v, want the deployment root alone", got)
	}
}

// TestSkillLoaderRootsFallsBackToTheHouseLibrary proves a crafted identity does not produce
// an empty root set (an agent with no skills at all and no line saying why) and does not
// reach another identity's directory either.
func TestSkillLoaderRootsFallsBackToTheHouseLibrary(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	got := skillLoaderRoots(cfg, "../escape")
	if len(got) != 1 || got[0] != cfg.SkillsDir {
		t.Fatalf("roots for a crafted identity = %v, want the deployment root alone", got)
	}
}

// TestIdentityLoadersAreCachedPerIdentity pins the cache: one loader per identity, reused, and
// expired for everybody on a write.
func TestIdentityLoadersAreCachedPerIdentity(t *testing.T) {
	t.Parallel()
	loaders := newIdentityLoaders(rootsConfig(t))

	alice := loaders.forIdentity(rootsAlice)
	if again := loaders.forIdentity(rootsAlice); again != alice {
		t.Fatal("a second call built a second loader — the snapshot would be thrown away every turn")
	}
	if loaders.forIdentity(rootsBob) == alice {
		t.Fatal("two identities share one loader — they would share one skill set")
	}
	// Whitespace is not an identity of its own.
	if trimmed := loaders.forIdentity("  " + rootsAlice + "  "); trimmed != alice {
		t.Fatal("a padded identity resolved to a different loader")
	}
	loaders.invalidateAll() // no panic, and every cached snapshot is expired
}

// TestAlwaysBlockIsRenderedPerIdentity is acceptance criterion 3: a deployment always-on skill
// reaches everybody, a personal one reaches only its owner.
func TestAlwaysBlockIsRenderedPerIdentity(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedSkill(t, cfg.SkillsDir, "house-rule", "HOUSE BODY", true)
	seedSkill(t, filepath.Join(cfg.SkillsIdentityDir, rootsAlice), "alice-rule", "ALICE BODY", true)

	provider := alwaysBlockProvider(cfg)
	alice := provider(identityctx.WithIdentityID(context.Background(), rootsAlice))
	bob := provider(identityctx.WithIdentityID(context.Background(), rootsBob))

	if !strings.Contains(alice, "HOUSE BODY") || !strings.Contains(alice, "ALICE BODY") {
		t.Fatalf("alice's always-block is missing a body:\n%s", alice)
	}
	if !strings.Contains(bob, "HOUSE BODY") {
		t.Fatalf("the house always-on skill did not reach bob:\n%s", bob)
	}
	if strings.Contains(bob, "ALICE BODY") {
		t.Fatalf("alice's always-on instructions were prepended to bob's turn:\n%s", bob)
	}
	// The catalogue rides the same block and is scoped the same way.
	if strings.Contains(bob, "alice-rule") {
		t.Fatalf("bob's catalogue advertises alice's skill:\n%s", bob)
	}
}

// TestAlwaysBlockEmptyWithoutSkills keeps the degenerate wiring honest: no config, or no
// skills dir, renders no block at all rather than a header with nothing under it.
func TestAlwaysBlockEmptyWithoutSkills(t *testing.T) {
	t.Parallel()
	if got := alwaysBlockProvider(nil)(context.Background()); got != "" {
		t.Errorf("nil config block = %q, want empty", got)
	}
	if got := alwaysBlockProvider(&config.Config{})(context.Background()); got != "" {
		t.Errorf("empty skills dir block = %q, want empty", got)
	}
	// A configured library with nothing in it still renders the catalogue header with its
	// "no skills" line — pre-existing behaviour (skills.RenderManifest), pinned here because
	// the per-identity provider must not have changed it: the block is byte-stable and the
	// model is told the library is empty rather than being told nothing at all.
	if got := alwaysBlockProvider(rootsConfig(t))(context.Background()); !strings.HasPrefix(got, skillCatalogueHeader) {
		t.Errorf("empty library block = %q, want the catalogue header", got)
	}
}

// TestSkillLayoutMirrorsConfig pins the one place cfg becomes a layout, including the nil
// config the pool-free paths hand it.
func TestSkillLayoutMirrorsConfig(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	got := skillLayout(cfg)
	if got.Global != cfg.SkillsDir || got.Identities != cfg.SkillsIdentityDir || got.Export != cfg.SkillExportDir {
		t.Fatalf("layout = %+v, want the three configured bases", got)
	}
	if empty := skillLayout(nil); empty != (skills.Layout{}) {
		t.Fatalf("skillLayout(nil) = %+v, want the zero layout", empty)
	}
}

// TestSandboxMaterializeSourcesScopesTheBox is the box half of acceptance criterion 2: B's box
// is filled from B's export and the deployment's, never from A's — with the deployment LAST so
// a name collision resolves the way the model's context does.
func TestSandboxMaterializeSourcesScopesTheBox(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	resolve := sandboxMaterializeSources(cfg)

	got := resolve(rootsBob)
	want := []usersandbox.MaterializeSource{
		{HostDir: filepath.Join(cfg.SkillsIdentityDir, rootsBob, ".export"), Dest: "/skills"},
		{HostDir: cfg.SkillExportDir, Dest: "/skills"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sources for bob = %+v, want %+v", got, want)
	}

	// Not "does the path mention alice" — MaterializeIn TARS each source and tarDir walks the
	// whole tree, so the question is whether alice's export sits anywhere UNDER a directory
	// bob's box is filled from. A nested identity export passes the string check and still
	// lands in bob's box one level down, which is how this leak survives review.
	alice, err := skillLayout(cfg).For(rootsAlice)
	if err != nil {
		t.Fatalf("layout for alice: %v", err)
	}
	for _, src := range got {
		rel, relErr := filepath.Rel(src.HostDir, alice.Export)
		if relErr == nil && !strings.HasPrefix(rel, "..") {
			t.Fatalf("alice's export %q lives under a source of bob's box (%q) — tarDir would carry it in", alice.Export, src.HostDir)
		}
	}

	// An unscoped box gets the deployment export once — not twice, which would clear and
	// re-copy the same tree for nothing.
	if unscoped := resolve(""); len(unscoped) != 1 || unscoped[0].HostDir != cfg.SkillExportDir {
		t.Fatalf("unscoped sources = %+v, want the deployment export alone", unscoped)
	}

	// A crafted identity falls back to the house export rather than to no /skills at all,
	// which would silently unmake every stored snippet in that box.
	if bad := resolve("../escape"); len(bad) != 1 || bad[0].HostDir != cfg.SkillExportDir {
		t.Fatalf("sources for a crafted identity = %+v, want the deployment export alone", bad)
	}

	// With no export configured there is nothing to materialize and no empty source is
	// invented for MaterializeIn to reject.
	none := sandboxMaterializeSources(&config.Config{})
	if got := none(rootsBob); len(got) != 0 {
		t.Fatalf("sources with no export dir = %+v, want none", got)
	}
}
