package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/idroot"
)

func testSkillBase(t *testing.T) SkillRootBase {
	t.Helper()
	return SkillRootBase{SkillsDir: t.TempDir(), PyScriptsDir: t.TempDir()}
}

func nameSet(list []Skill) map[string]bool {
	out := map[string]bool{}
	for _, s := range list {
		out[s.Name] = true
	}
	return out
}

// Test 1: a per-identity tool roots user skills under $AURA_SKILLS_DIR/{id} and
// snippets under ~/.aura/pyscripts/{id}, both contained under their base.
func TestNewSkillToolForIdentityRootsUserSkills(t *testing.T) {
	base := testSkillBase(t)
	ctx := identityctx.WithIdentityID(context.Background(), "alice")
	roots, err := NewSkillToolForIdentity(ctx, base)
	if err != nil {
		t.Fatalf("NewSkillToolForIdentity: %v", err)
	}
	if want := filepath.Join(base.SkillsDir, "alice"); roots.UserSkillsDir != want {
		t.Fatalf("UserSkillsDir = %q, want %q", roots.UserSkillsDir, want)
	}
	if want := filepath.Join(base.PyScriptsDir, "alice"); roots.PyScriptsDir != want {
		t.Fatalf("PyScriptsDir = %q, want %q", roots.PyScriptsDir, want)
	}
	rel, err := filepath.Rel(base.SkillsDir, roots.UserSkillsDir)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("UserSkillsDir %q escapes base %q (rel=%q, err=%v)", roots.UserSkillsDir, base.SkillsDir, rel, err)
	}
}

// Test 1b: a traversal identity is rejected by the containment guard.
func TestNewSkillToolForIdentityRejectsTraversal(t *testing.T) {
	base := testSkillBase(t)
	for _, id := range []string{"..", "../evil", "a/b", `a\b`, "../../etc"} {
		ctx := identityctx.WithIdentityID(context.Background(), id)
		if _, err := NewSkillToolForIdentity(ctx, base); !errors.Is(err, idroot.ErrInvalidIdentity) {
			t.Fatalf("NewSkillToolForIdentity(%q) err = %v, want ErrInvalidIdentity", id, err)
		}
	}
}

// Test 2: built-ins stay shared read-only (not copied into the per-identity dir); the
// per-identity loader sees them via the shared root, and one identity never sees
// another identity's user skills.
func TestNewSkillToolForIdentitySharedBuiltinsIsolateUsers(t *testing.T) {
	base := testSkillBase(t)
	// A shared "built-in" is materialized once into the base dir (shared read-only).
	writeSkill(t, base.SkillsDir, "shared-builtin", skillMD("shared-builtin", "shared read-only"))

	alice, err := NewSkillToolForIdentity(identityctx.WithIdentityID(context.Background(), "alice"), base)
	if err != nil {
		t.Fatalf("alice roots: %v", err)
	}
	bob, err := NewSkillToolForIdentity(identityctx.WithIdentityID(context.Background(), "bob"), base)
	if err != nil {
		t.Fatalf("bob roots: %v", err)
	}

	// The built-in is NOT copied into a per-identity dir; it stays shared.
	if _, err := os.Stat(filepath.Join(alice.UserSkillsDir, "shared-builtin")); !os.IsNotExist(err) {
		t.Fatalf("shared-builtin must NOT be copied into alice's dir (stat err=%v)", err)
	}
	if alice.SharedSkillsDir != base.SkillsDir {
		t.Fatalf("SharedSkillsDir = %q, want base %q", alice.SharedSkillsDir, base.SkillsDir)
	}

	// alice installs a private skill; both see the shared built-in, only alice sees hers.
	writeSkill(t, alice.UserSkillsDir, "alice-skill", skillMD("alice-skill", "alice private"))

	aliceNames := nameSet(NewLoader(Config{Roots: alice.LoaderRoots}).List())
	if !aliceNames["shared-builtin"] || !aliceNames["alice-skill"] {
		t.Fatalf("alice loader = %v, want shared-builtin + alice-skill", aliceNames)
	}
	bobNames := nameSet(NewLoader(Config{Roots: bob.LoaderRoots}).List())
	if !bobNames["shared-builtin"] {
		t.Fatalf("bob loader = %v, want shared-builtin", bobNames)
	}
	if bobNames["alice-skill"] {
		t.Fatalf("bob loader leaked alice-skill: %v", bobNames)
	}
}

// Test 3: the local / no-principal path resolves the existing default dirs (backward
// compatible — single-user CLI keeps reading $AURA_SKILLS_DIR directly).
func TestNewSkillToolForIdentityLocalUsesBase(t *testing.T) {
	base := testSkillBase(t)
	cases := map[string]context.Context{
		"no-principal": context.Background(),
		"local":        identityctx.WithIdentityID(context.Background(), "local"),
	}
	for label, ctx := range cases {
		roots, err := NewSkillToolForIdentity(ctx, base)
		if err != nil {
			t.Fatalf("[%s] NewSkillToolForIdentity: %v", label, err)
		}
		if roots.UserSkillsDir != base.SkillsDir {
			t.Fatalf("[%s] UserSkillsDir = %q, want base %q", label, roots.UserSkillsDir, base.SkillsDir)
		}
		if roots.PyScriptsDir != base.PyScriptsDir {
			t.Fatalf("[%s] PyScriptsDir = %q, want base %q", label, roots.PyScriptsDir, base.PyScriptsDir)
		}
	}
}
