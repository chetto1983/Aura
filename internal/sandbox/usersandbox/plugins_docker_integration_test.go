//go:build docker_integration

package usersandbox

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/moby/moby/client"
)

// pluginTrees is a resolver whose answer a test changes between Resolves.
type pluginTrees struct {
	mu    sync.Mutex
	trees []PluginTree
}

func (p *pluginTrees) set(trees ...PluginTree) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trees = trees
}

func (p *pluginTrees) resolve(context.Context) ([]PluginTree, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.trees), nil
}

func boxPathExists(t *testing.T, cli *client.Client, containerID, boxPath string) bool {
	t.Helper()
	_, code := rawExec(t, cli, containerID, []string{"test", "-e", boxPath})
	return code == 0
}

func TestPluginTreeIsRewrittenOnlyWhenItsCommitChanges(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	ctx := context.Background()
	v1, v2 := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(v1, "bin", "run.sh"), "v1\n")
	writeFixture(t, filepath.Join(v2, "bin", "run.sh"), "v2\n")
	trees := &pluginTrees{}
	trees.set(PluginTree{Name: "p", HostDir: v1, Version: commitOf('1')})
	backend := NewDockerBackend(cli, testBoxImage(), testLimits(), WithPluginTrees(trees.resolve))
	spec := SandboxSpec{IdentityID: uniqueIdentity(t, "plug")}
	resolve := func() BoxHandle {
		t.Helper()
		h, err := backend.Resolve(ctx, spec)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		return h
	}

	h := resolve()
	t.Cleanup(func() { _ = backend.Stop(context.Background(), h) })
	assertBoxFile(t, cli, h.ContainerID, "/plugins/p/bin/run.sh", "v1")

	if _, code := rawExec(t, cli, h.ContainerID, []string{"touch", "/plugins/p/.sentinel"}); code != 0 {
		t.Fatalf("touch sentinel: exit %d", code)
	}
	resolve()
	if !boxPathExists(t, cli, h.ContainerID, "/plugins/p/.sentinel") {
		t.Fatal("an unchanged tree was rewritten: the sentinel is gone")
	}

	trees.set(PluginTree{Name: "p", HostDir: v2, Version: commitOf('2')})
	resolve()
	if boxPathExists(t, cli, h.ContainerID, "/plugins/p/.sentinel") {
		t.Fatal("a tree at a new commit kept the old copy's files")
	}
	assertBoxFile(t, cli, h.ContainerID, "/plugins/p/bin/run.sh", "v2")

	if _, code := rawExec(t, cli, h.ContainerID, []string{"rm", "-rf", "/plugins/p"}); code != 0 {
		t.Fatalf("rm tree: exit %d", code)
	}
	resolve()
	assertBoxFile(t, cli, h.ContainerID, "/plugins/p/bin/run.sh", "v2")

	trees.set()
	resolve()
	for _, gone := range []string{"/plugins/p", "/plugins/.aura/p"} {
		if boxPathExists(t, cli, h.ContainerID, gone) {
			t.Errorf("%s survived the plugin's removal", gone)
		}
	}
}

// Before the plugin mirror, materializeInputs returned as soon as the skill sources were empty.
func TestPluginSyncRunsWithoutSkillSources(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	ctx := context.Background()
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "plugin.json"), "{}\n")
	trees := &pluginTrees{}
	trees.set(PluginTree{Name: "p", HostDir: root, Version: commitOf('1')})
	backend := NewDockerBackend(cli, testBoxImage(), testLimits(),
		WithMaterializeSources(func(context.Context, string) ([]MaterializeSource, error) { return nil, nil }),
		WithPluginTrees(trees.resolve))
	h, err := backend.Resolve(ctx, SandboxSpec{IdentityID: uniqueIdentity(t, "plugsrc")})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), h) })
	assertBoxFile(t, cli, h.ContainerID, "/plugins/p/plugin.json", "{}")
}

// A plugin server runs in its root, the tree keeps its modes, and PLUGIN_DATA lives on the
// workspace volume, so it outlives the box container.
func TestPluginProcessSeesItsRootAndKeepsItsData(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	ctx := context.Background()
	root := t.TempDir()
	script := filepath.Join(root, "bin", "run.sh")
	writeFixture(t, script, "#!/bin/sh\nprintf '%s %s %s\\n' \"$PLUGIN_ROOT\" \"$PLUGIN_DATA\" \"$(pwd)\" > \"$PLUGIN_DATA/out\"\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	trees := &pluginTrees{}
	trees.set(PluginTree{Name: "p", HostDir: root, Version: commitOf('1')})
	backend := NewDockerBackend(cli, testBoxImage(), testLimits(), WithPluginTrees(trees.resolve))
	spec := SandboxSpec{IdentityID: uniqueIdentity(t, "plugdata")}
	h, err := backend.Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	last := h
	t.Cleanup(func() { _ = backend.Stop(context.Background(), last) })

	const data = "/workspace/.plugin-data/p-1a2b3c4d5e6f"
	res, err := backend.Exec(ctx, h, ExecRequest{
		Command: "mkdir -p '" + data + "' && cd '/plugins/p' && ./bin/run.sh",
		Dir:     "/workspace",
		Env:     []string{"PLUGIN_ROOT=/plugins/p", "PLUGIN_DATA=" + data},
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("run plugin: err %v, exit %d, stderr %q", err, res.ExitCode, res.Stderr)
	}

	if _, err := cli.ContainerRemove(ctx, h.ContainerID, client.ContainerRemoveOptions{Force: true}); err != nil {
		t.Fatalf("remove box container: %v", err)
	}
	if last, err = backend.Resolve(ctx, spec); err != nil {
		t.Fatalf("resolve after removal: %v", err)
	}
	if last.ContainerID == h.ContainerID {
		t.Fatal("the box was not re-created")
	}
	assertBoxFile(t, cli, last.ContainerID, data+"/out", "/plugins/p "+data+" /plugins/p")
}
