package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// symlinkOrSkip makes a symlink, skipping only where the OS refuses it (Windows without
// developer mode). Under CI the refusal is a failure: a containment test that never runs
// there is a falsely green job.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("symlink: %v", err)
		}
		t.Skipf("symlinks unavailable here: %v", err)
	}
}

func TestAgentPluginsContainment(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "skills", "x", "SKILL.md"), "---\nname: x\ndescription: d\n---\nbody")
	write(t, filepath.Join(outside, "SKILL.md"), "---\nname: y\ndescription: d\n---\nbody")
	write(t, filepath.Join(outside, "plugin.json"), minimalAgentManifest)
	write(t, filepath.Join(outside, "mcp.json"), oneServer(`{"type": "stdio", "command": "uvx"}`))

	t.Run("skills resolving outside", func(t *testing.T) {
		t.Parallel()
		root := agentPlugin(t, minimalAgentManifest, nil)
		symlinkOrSkip(t, filepath.Join(outside, "skills"), filepath.Join(root, "skills"))
		pack, err := readOne(t, root)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(pack.Skills) != 0 || len(pack.SkippedSkills) != 1 || !strings.Contains(pack.SkippedSkills[0].Reason, "resolves outside the plugin root") {
			t.Errorf("skills = %v, skipped = %+v", pack.Skills, pack.SkippedSkills)
		}
	})
	t.Run("skills as a regular file", func(t *testing.T) {
		t.Parallel()
		pack, err := readOne(t, agentPlugin(t, minimalAgentManifest, map[string]string{"skills": "not a dir"}))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(pack.SkippedSkills) != 1 || !strings.Contains(pack.SkippedSkills[0].Reason, "not a directory") {
			t.Errorf("skipped = %+v (§6.2)", pack.SkippedSkills)
		}
	})
	t.Run("one SKILL.md resolving outside", func(t *testing.T) {
		t.Parallel()
		root := agentPlugin(t, minimalAgentManifest, map[string]string{
			"skills/good/SKILL.md": "---\nname: good\ndescription: d\n---\nbody",
		})
		symlinkOrSkip(t, filepath.Join(outside, "SKILL.md"), filepath.Join(root, "skills", "bad", "SKILL.md"))
		pack, err := readOne(t, root)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(pack.Skills) != 1 || pack.Skills[0] != "good" || len(pack.SkippedSkills) != 1 || pack.SkippedSkills[0].Name != "bad" {
			t.Errorf("skills = %v, skipped = %+v", pack.Skills, pack.SkippedSkills)
		}
	})
	t.Run("plugin.json resolving outside", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		symlinkOrSkip(t, filepath.Join(outside, "plugin.json"), filepath.Join(root, "plugin.json"))
		if _, err := ReadTree(root, Ref{Source: "o/r"}); err == nil || !strings.Contains(err.Error(), "outside the plugin root") {
			t.Errorf("err = %v, want the plugin rejected (§4.1)", err)
		}
	})
	t.Run("mcp.json resolving outside", func(t *testing.T) {
		t.Parallel()
		root := agentPlugin(t, minimalAgentManifest, nil)
		symlinkOrSkip(t, filepath.Join(outside, "mcp.json"), filepath.Join(root, "mcp.json"))
		pack, err := readOne(t, root)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !strings.Contains(pack.MCPDisabled, "outside the plugin root") || len(pack.Servers) != 0 {
			t.Errorf("MCPDisabled = %q, servers = %+v", pack.MCPDisabled, pack.Servers)
		}
	})
	t.Run("a ./ command through an escaping directory symlink", func(t *testing.T) {
		t.Parallel()
		root := agentPlugin(t, minimalAgentManifest, map[string]string{
			"mcp.json": oneServer(`{"type": "stdio", "command": "./bin/x"}`),
		})
		symlinkOrSkip(t, outside, filepath.Join(root, "bin"))
		pack, err := readOne(t, root)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(pack.Servers) != 0 || len(pack.SkippedServers) != 1 {
			t.Errorf("servers = %+v, skipped = %+v", pack.Servers, pack.SkippedServers)
		}
	})
}

func TestWithinRootAcceptsAnInternalPathThatDoesNotExistYet(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got, err := withinRoot(root, "./data/later")
	if err != nil {
		t.Fatalf("withinRoot: %v", err)
	}
	real, _ := filepath.EvalSymlinks(root)
	if got != filepath.Join(real, "data", "later") {
		t.Errorf("got %q", got)
	}
	if _, err := withinRoot(root, "./../x"); err == nil {
		t.Error("an escape was accepted")
	}
}
