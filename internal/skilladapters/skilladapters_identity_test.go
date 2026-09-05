package skilladapters

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/skills"
)

// skilladapters_identity_test.go covers the seam amendment #214 added: the adapter resolves
// the loader and the writer FROM THE CALL, so one person's skills never answer another
// person's turn.

const (
	adapterAlice = "77777777-7777-4777-8777-777777777777"
	adapterBob   = "88888888-8888-4888-8888-888888888888"
)

// seedSkillTree writes a minimal valid instruction skill under root/<name>.
func seedSkillTree(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	md := "---\nname: " + name + "\ndescription: " + name + " description\ntype: instruction\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// identityAdapter builds an adapter over two private roots, one per identity, and returns it
// with a counter of how many times the invalidator fired.
func identityAdapter(t *testing.T) (*Loader, map[string]string, *int) {
	t.Helper()
	roots := map[string]string{adapterAlice: t.TempDir(), adapterBob: t.TempDir()}
	loaders := map[string]*skills.Loader{}
	for id, root := range roots {
		loaders[id] = skills.NewLoader(skills.Config{Roots: []string{root}})
	}
	empty := skills.NewLoader(skills.Config{Roots: []string{t.TempDir()}})
	invalidations := 0
	adapter := NewIdentityLoader(func(identity string) *skills.Loader {
		if l, ok := loaders[identity]; ok {
			return l
		}
		return empty
	}, func() { invalidations++ }, 4096)
	return adapter, roots, &invalidations
}

// TestLoaderAnswersFromTheCallersOwnRoot is the read boundary at the tool seam: the same
// adapter, two contexts, two answers — and neither carries the other's body.
func TestLoaderAnswersFromTheCallersOwnRoot(t *testing.T) {
	t.Parallel()
	adapter, roots, _ := identityAdapter(t)
	seedSkillTree(t, roots[adapterAlice], "alice-skill", "alice body")
	seedSkillTree(t, roots[adapterBob], "bob-skill", "bob body")

	aliceCtx := identityctx.WithIdentityID(context.Background(), adapterAlice)
	bobCtx := identityctx.WithIdentityID(context.Background(), adapterBob)

	if body, ok := adapter.Body(aliceCtx, "alice-skill"); !ok || body != "alice body\n" {
		t.Fatalf("alice reading her own skill = (%q, %v)", body, ok)
	}
	if body, ok := adapter.Body(bobCtx, "alice-skill"); ok {
		t.Fatalf("bob read alice's skill body %q — the tool seam is not scoped", body)
	}
	if names := adapter.List(bobCtx); len(names) != 1 || names[0].Name != "bob-skill" {
		t.Fatalf("bob's list = %+v, want only his own skill", names)
	}
	if man := adapter.ManifestDescription(aliceCtx); !strings.Contains(man, "alice-skill") || strings.Contains(man, "bob-skill") {
		t.Fatalf("alice's manifest = %q, want her skill and not bob's", man)
	}
	// An unscoped context answers from the resolver's unscoped loader, which here holds
	// nothing — the pre-#214 shape, not a fallback to somebody's library.
	if names := adapter.List(context.Background()); len(names) != 0 {
		t.Fatalf("unscoped list = %+v, want nothing", names)
	}
}

// TestSnippetResolvesPerIdentity pins the snippet leg of the same boundary: the in-box path
// is only handed out for a snippet the caller actually owns.
func TestSnippetResolvesPerIdentity(t *testing.T) {
	t.Parallel()
	adapter, roots, _ := identityAdapter(t)
	dir := filepath.Join(roots[adapterAlice], "alice-snip")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\nname: alice-snip\ndescription: a snippet\ntype: snippet\nlanguage: python\n---\ndocs\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	aliceCtx := identityctx.WithIdentityID(context.Background(), adapterAlice)
	if _, path, interp, ok := adapter.Snippet(aliceCtx, "alice-snip"); !ok ||
		path != skills.InSandboxSkillsRoot+"/alice-snip/alice-snip.py" || interp != "python3" {
		t.Fatalf("alice's snippet = (%q, %q, %v)", path, interp, ok)
	}
	bobCtx := identityctx.WithIdentityID(context.Background(), adapterBob)
	if _, _, _, ok := adapter.Snippet(bobCtx, "alice-snip"); ok {
		t.Fatal("bob was handed a run path for a snippet he does not own")
	}
}

// TestInvalidateReachesEveryIdentity proves a write is visible to the next read for readers
// other than the writer: the adapter cannot know who holds a warm snapshot, so it expires
// them all.
func TestInvalidateReachesEveryIdentity(t *testing.T) {
	t.Parallel()
	adapter, _, invalidations := identityAdapter(t)
	adapter.Invalidate()
	if *invalidations != 1 {
		t.Fatalf("Invalidate fired %d times, want 1", *invalidations)
	}

	// The single-loader constructor keeps its own invalidation wired, so the CLI and the
	// pool-free manifest paths behave exactly as before.
	live := skills.NewLoader(skills.Config{Roots: []string{t.TempDir()}})
	NewLoader(live, 4096).Invalidate()
}

// TestWritePathsScopeToTheCallerAndRefuseACraftedIdentity proves the write half resolves the
// same way, and that an identity which cannot name a directory is an error rather than a
// silent write into the shared library.
func TestWritePathsScopeToTheCallerAndRefuseACraftedIdentity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	layout := skills.Layout{
		Global:     filepath.Join(root, "active"),
		Identities: filepath.Join(root, "identities"),
		Export:     filepath.Join(root, "export"),
	}
	live := skills.NewWriter(skills.WriterConfig{
		ActiveDir:    layout.Global,
		ExportDir:    layout.Export,
		Layout:       layout,
		BodyCapBytes: 32768,
	})
	adapter := NewWriter(live, skills.NewInstaller(skills.InstallerConfig{Writer: live}))

	// A crafted identity: every leg refuses before it can write anywhere.
	bad := identityctx.WithIdentityID(context.Background(), "../escape")
	if _, err := adapter.WriteMutation(bad, "create", "calc", "d", "body", false); err == nil {
		t.Error("WriteMutation accepted an identity that cannot name a directory")
	}
	if _, err := adapter.SaveSnippet(bad, "calc", "python", "print(1)", "d", false, false); err == nil {
		t.Error("SaveSnippet accepted an identity that cannot name a directory")
	}
	if _, err := adapter.Restore(bad, "calc"); err == nil {
		t.Error("Restore accepted an identity that cannot name a directory")
	}
	if _, err := adapter.ArchiveSnippet(bad, "calc"); err == nil {
		t.Error("ArchiveSnippet accepted an identity that cannot name a directory")
	}
	if _, err := adapter.Install(bad, "owner/repo"); err == nil {
		t.Error("Install accepted an identity that cannot name a directory")
	}

	// A real identity resolves, and the call then fails on its OWN merits (an invalid skill
	// name) rather than on the scoping — which is how we know the scoping succeeded.
	good := identityctx.WithIdentityID(context.Background(), adapterAlice)
	_, err := adapter.WriteMutation(good, "create", "Bad_Name", "d", "body", false)
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("scoped WriteMutation = %v, want the name validation to be what refuses", err)
	}

	// With no installer wired the install leg says so instead of panicking.
	if _, err := NewWriter(live, nil).Install(good, "owner/repo"); err == nil ||
		!strings.Contains(err.Error(), "no installer") {
		t.Fatalf("install with no installer = %v, want a clear configuration error", err)
	}
}
