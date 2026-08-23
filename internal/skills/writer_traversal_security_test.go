package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writer_traversal_security_test.go answers HANDOFF §2.3, the unverified half of D-106.
//
// `go/command-injection` on internal/mcp/sdkclient.go:169 is an ACCEPTED critical: the MCP
// launch command is not validated, on the premise that only the operator ever writes the
// registry it comes from. Measured on the live deployment 2026-08-23, that registry is
// /var/lib/aura/mcp/servers.json (mode 0600) and the skills root is its SIBLING at
// /var/lib/aura/skills — so "../mcp/servers.json" is one relative hop, and the aura process
// runs as root and can write it. The premise therefore rests entirely on whether any
// agent-reachable writer can be steered out of the skills root.
//
// These tests aim every such writer at that exact path and assert two things, because one is
// not enough: the call is REFUSED with ErrInvalidName, and the sibling file is byte-identical
// afterwards. Asserting only the error would pass even if a rejected call had already written,
// renamed or removed something on its way to the refusal.
//
// The layout is production's, not a convenient one: <root>/skills next to <root>/mcp, the
// registry holding real-shaped content.

// traversalTargets is every shape that names the sibling registry, or leaves the skills root
// at all. `..%2Fmcp` and the backslash forms are here because a name can arrive from an HTTP
// handler or a Windows-authored upstream tree, and a grammar that only rejects "/" is a
// grammar that rejects one spelling of the idea.
var traversalTargets = []string{
	"../mcp",
	"../mcp/servers",
	"../../mcp/servers.json",
	"..",
	"../",
	"./../mcp",
	"..%2Fmcp",
	`..\mcp`,
	`..\..\mcp\servers.json`,
	"/var/lib/aura/mcp/servers",
	"servers.json",         // right basename, wrong grammar (dot) — must still be refused
	"../mcp/servers.json/", // trailing separator
	"a/../../mcp",
	strings.Repeat("../", 8) + "mcp",
}

// siblingLayout builds <root>/skills (the writer's roots) beside <root>/mcp/servers.json and
// returns the writer, the root, and a func that fails the test unless the registry is exactly
// as it was.
func siblingLayout(t *testing.T) (*Writer, string, func()) {
	t.Helper()
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills")
	mcpDir := filepath.Join(root, "mcp")
	if err := os.MkdirAll(mcpDir, 0o750); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(mcpDir, "servers.json")
	const content = `{"mcpServers":{"memory":{"command":"aura","args":["memory","serve"]}}}`
	if err := os.WriteFile(registry, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewWriter(WriterConfig{
		Pool:         nil, // a refusal must land BEFORE the audit tx; a nil pool proves it did
		ActiveDir:    filepath.Join(skillsRoot, "active"),
		ExportDir:    filepath.Join(skillsRoot, "export"),
		ArchiveDir:   filepath.Join(skillsRoot, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})
	assertRegistryIntact := func() {
		t.Helper()
		got, err := os.ReadFile(registry) // #nosec G304 -- test-owned temp path
		if err != nil {
			t.Fatalf("the MCP registry is gone after a rejected write: %v", err)
		}
		if string(got) != content {
			t.Fatalf("the MCP registry changed after a rejected write:\n got %q\nwant %q", got, content)
		}
		if _, err := os.Stat(mcpDir); err != nil {
			t.Fatalf("the MCP directory is gone after a rejected write: %v", err)
		}
	}
	return w, root, assertRegistryIntact
}

// TestSkillWritersCannotReachTheSiblingMCPRegistry drives every agent-reachable write verb at
// the registry and requires a refusal from each. These are the verbs `skill_manage` exposes to
// the model (create, update, save_snippet, install) plus the lifecycle verbs whose joined path
// feeds an os.RemoveAll (archive, restore, delete) or an os.WriteFile (set-always) — the ones
// where a traversal would not merely write a file but destroy the registry.
func TestSkillWritersCannotReachTheSiblingMCPRegistry(t *testing.T) {
	t.Parallel()
	w, _, assertRegistryIntact := siblingLayout(t)
	ctx := t.Context()
	actor := AuditActor{ActorID: "model"}

	for _, bad := range traversalTargets {
		t.Run(bad, func(t *testing.T) {
			// Each verb is named so a failure says WHICH writer escaped, not merely that one did.
			verbs := map[string]func() error{
				"create": func() error {
					_, err := w.WriteMutationByName(ctx, "create", bad, "d", "body", false, actor)
					return err
				},
				"update": func() error {
					_, err := w.WriteMutationByName(ctx, "update", bad, "d", "body", false, actor)
					return err
				},
				"save_snippet": func() error {
					_, err := w.SaveSnippet(ctx, bad, "python", "print(1)", Frontmatter{Description: "d"}, actor)
					return err
				},
				"install": func() error {
					// The staged dir is irrelevant: the name must be refused before it is joined.
					_, err := w.WriteInstall(ctx, Frontmatter{Name: bad, Description: "d"}, t.TempDir(), "hash", actor)
					return err
				},
				"archive": func() error { return w.Archive(ctx, bad, ApprovalCLI, actor) },
				"restore": func() error { return w.Restore(ctx, bad, ApprovalCLI, actor) },
				"delete": func() error {
					_, err := w.Delete(ctx, bad, actor)
					return err
				},
				"set_always": func() error { return w.SetAlways(ctx, bad, true, actor) },
				"use_snippet": func() error {
					_, err := w.UseSnippet(bad)
					return err
				},
			}
			for verb, call := range verbs {
				if err := call(); !errors.Is(err, ErrInvalidName) {
					t.Errorf("%s(%q) = %v, want ErrInvalidName — this writer escapes the skills root", verb, bad, err)
				}
			}
			assertRegistryIntact()
		})
	}
}

// TestSanitizeNameRejectsEverySeparatorSpelling pins the grammar itself rather than one
// caller's use of it. ^[a-z0-9-]{1,64}$ admits no dot and no separator, so every traversal
// spelling dies on the charset — but a future relaxation "just to allow dots in names" would
// re-open the sibling hop, and this is the test that would notice.
func TestSanitizeNameRejectsEverySeparatorSpelling(t *testing.T) {
	t.Parallel()
	for _, bad := range traversalTargets {
		if err := SanitizeName(bad, bad); !errors.Is(err, ErrInvalidName) {
			t.Errorf("SanitizeName(%q) = %v, want ErrInvalidName", bad, err)
		}
	}
	// And the positive control: a legal name still passes, so the test above is not green
	// merely because everything is rejected.
	if err := SanitizeName("call-prep", "call-prep"); err != nil {
		t.Fatalf("SanitizeName on a legal name = %v, want nil", err)
	}
}

// TestInstalledTreeCannotEscapeTheSkillRoot covers the OTHER install vector: the name is fine,
// but the fetched upstream tree carries entries aimed out of the skill directory. Two shapes
// are exercised against the real materializer — a symlink pointing at the sibling registry,
// and a nested directory whose contents would land on it if any component were resolved.
//
// A symlink is the sharp one: copyTreeNoSymlinks Lstats and SKIPS it, so the registry is
// neither read into the skill nor written through. If that strip were ever replaced by a
// follow, this test fails with the registry's own bytes sitting inside the skill.
func TestInstalledTreeCannotEscapeTheSkillRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	registry := filepath.Join(root, "mcp", "servers.json")
	if err := os.MkdirAll(filepath.Dir(registry), 0o750); err != nil {
		t.Fatal(err)
	}
	const secret = `{"mcpServers":{"memory":{"command":"aura"}}}`
	if err := os.WriteFile(registry, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	staged := filepath.Join(root, "staged")
	if err := os.MkdirAll(staged, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "SKILL.md"), []byte("---\nname: probe\n---\nbody"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(registry, filepath.Join(staged, "leaked.json")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	if err := os.Symlink(filepath.Dir(registry), filepath.Join(staged, "mcp")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := copyTreeNoSymlinks(staged, dst); err != nil {
		t.Fatalf("copyTreeNoSymlinks: %v", err)
	}

	for _, leaked := range []string{"leaked.json", filepath.Join("mcp", "servers.json")} {
		if _, err := os.Lstat(filepath.Join(dst, leaked)); err == nil {
			t.Errorf("the installed tree carried %q out of the staging dir — the symlink strip is gone", leaked)
		}
	}
	// The copy must still have done its job, or the assertion above passes vacuously.
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Fatalf("the regular file was not copied: %v", err)
	}
	after, err := os.ReadFile(registry) // #nosec G304 -- test-owned temp path
	if err != nil || string(after) != secret {
		t.Fatalf("the registry was disturbed by an install copy: %v", err)
	}
}
