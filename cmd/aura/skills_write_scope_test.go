package main

// skills_write_scope_test.go pins the CLI's write scope. Before --identity reached the write
// half, `aura skills` was asymmetric in a way that made a whole subcommand family dead:
// `list` and `info` took --identity, `create` and `update` did not, and `share` refuses a
// skill its owner does not own. So no CLI path could produce a shareable skill, and
// `aura skills share|unshare|shares` could only ever answer, as measured on a live stack:
//
//	skills: identity 1111...1111 owns no skill "ricetta-carbonara"
//	(only skills written by that identity can be shared)

import (
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

func scopeTestConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	return &config.Config{
		SkillsDir:         filepath.Join(root, "skills"),
		SkillsIdentityDir: filepath.Join(root, "identities"),
		SkillExportDir:    filepath.Join(root, "export"),
	}
}

// TestWriterForLandsInTheIdentitysOwnRoot: a scoped write must address the identity's root,
// which is the same directory skills.Writer.For gives the cockpit for its principal — the
// catalog row that `share` later looks for is written from exactly there.
func TestWriterForLandsInTheIdentitysOwnRoot(t *testing.T) {
	cfg := scopeTestConfig(t)
	const identity = "11111111-1111-4111-8111-111111111111"

	global := newSkillWriter(cfg, nil)
	scoped := writerFor(global, identity)

	want := filepath.Join(cfg.SkillsIdentityDir, identity)
	if got := scoped.ActiveDir(); got != want {
		t.Fatalf("scoped write lands in %q, want %q", got, want)
	}
	if got := scoped.Owner(); got != identity {
		t.Fatalf("scoped writer owns %q, want %q", got, identity)
	}
	// The deployment-global writer must be untouched: scoping is derived, never mutated in
	// place, or two CLI calls in one process would start writing into each other's roots.
	if got := global.ActiveDir(); got != cfg.SkillsDir {
		t.Fatalf("the global writer moved to %q", got)
	}
	if global.Owner() != "" {
		t.Fatalf("the global writer acquired an owner: %q", global.Owner())
	}
}

// TestWriterForWithoutIdentityIsUnchanged is the compatibility half: an operator who never
// passes --identity must get exactly the deployment-global behaviour they had before.
func TestWriterForWithoutIdentityIsUnchanged(t *testing.T) {
	cfg := scopeTestConfig(t)
	global := newSkillWriter(cfg, nil)

	if got := writerFor(global, ""); got != global {
		t.Fatal("an unscoped write must reuse the deployment-global writer, not derive a new one")
	}
	if got := writerFor(global, "   "); got != global {
		t.Fatal("a blank --identity must be treated as unscoped")
	}
}
