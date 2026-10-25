// plugins.go mirrors the installed plugin trees into every shell box at /plugins/<name>.
//
// Unlike the /skills mirror it does not clear and rewrite on every Resolve. A plugin server runs
// with its tree as its working directory, and Resolve runs on every boxed call, so a clear would
// delete the tree under the running server (spec 2026-10-25-pack-install §4.4). Each tree has a
// marker, /plugins/.aura/<name>, holding the commit it was copied at. A tree is rewritten only
// when its marker is missing or names another commit, or when the tree itself is gone.

package usersandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/packs"
	"github.com/moby/moby/client"
)

// InBoxPluginsRoot is where the plugin trees land in a shell box.
const InBoxPluginsRoot = "/plugins"

const pluginMarkers = InBoxPluginsRoot + "/.aura"

// PluginTree is one installed plugin's kept tree on the host and the commit it was checked out at.
type PluginTree struct {
	Name    string
	HostDir string
	Version string
}

// PluginTreeResolver returns the plugin trees every shell box must carry. Plugins belong to the
// deployment, so it takes no identity. An error fails Resolve closed, like SourceResolver.
type PluginTreeResolver func(ctx context.Context) ([]PluginTree, error)

// listPluginMarkers prints one `name commit present` line per marker, where present is 1 when
// the tree is in the box. Only the first line of a marker is read.
const listPluginMarkers = `cd ` + pluginMarkers + ` 2>/dev/null || exit 0
for m in *; do
  [ -f "$m" ] || continue
  c=; read -r c < "$m"
  p=0; [ -d "` + InBoxPluginsRoot + `/$m" ] && p=1
  printf '%s %s %s\n' "$m" "$c" "$p"
done`

// syncPlugins brings the box's plugin trees in line with the resolver's answer.
func (b *DockerBackend) syncPlugins(ctx context.Context, h BoxHandle) error {
	if b.plugins == nil || h.Kind == Browser {
		return nil
	}
	want, err := b.plugins(ctx)
	if err != nil {
		return fmt.Errorf("plugin trees: %w", err)
	}
	res, err := b.Exec(ctx, h, ExecRequest{Command: listPluginMarkers})
	if err != nil {
		return fmt.Errorf("list plugin markers: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("list plugin markers exited %d: %s", res.ExitCode, res.Stderr)
	}
	commits, present := parseMarkers(string(res.Stdout))
	remove, write := planPluginSync(commits, present, want)
	return b.applyPluginSync(ctx, h, remove, write)
}

// parseMarkers reads the marker listing. The box is the model's to write in, and these names
// become box paths and `rm` arguments, so a line whose name or commit is not one Aura could
// have written is dropped.
func parseMarkers(out string) (commits map[string]string, present map[string]bool) {
	commits, present = map[string]string{}, map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || packs.ValidPluginName(f[0]) != nil || packs.ValidCommit(f[1]) != nil {
			continue
		}
		commits[f[0]] = f[1]
		present[f[0]] = f[2] == "1"
	}
	return commits, present
}

// planPluginSync removes the marked trees nobody wants any more, and writes the wanted trees
// that are absent or marked at another commit, in the resolver's order.
func planPluginSync(inBox map[string]string, present map[string]bool, want []PluginTree) (remove []string, write []PluginTree) {
	wanted := make(map[string]bool, len(want))
	for _, t := range want {
		wanted[t.Name] = true
		if inBox[t.Name] != t.Version || !present[t.Name] {
			write = append(write, t)
		}
	}
	for name := range inBox {
		if !wanted[name] {
			remove = append(remove, name)
		}
	}
	slices.Sort(remove)
	return remove, write
}

// applyPluginSync deletes the removed trees, then writes each tree to write. A tree is deleted
// with its marker before it is copied, and its marker is written only after the copy, so a copy
// that fails half way leaves no marker and is redone at the next sync.
func (b *DockerBackend) applyPluginSync(ctx context.Context, h BoxHandle, remove []string, write []PluginTree) error {
	if len(remove) > 0 {
		if err := runInBox(ctx, b.cli, h, removeArgv(pluginPaths(remove...)...)); err != nil {
			return fmt.Errorf("remove plugin trees: %w", err)
		}
	}
	staged, err := stagePluginTrees(write, b.spoolDir)
	if err != nil {
		return err
	}
	defer closeStagedPlugins(staged)
	for _, s := range staged {
		if err := runInBox(ctx, b.cli, h, removeArgv(pluginPaths(s.tree.Name)...)); err != nil {
			return fmt.Errorf("clear plugin %q: %w", s.tree.Name, err)
		}
		if _, err := b.cli.CopyToContainer(ctx, h.ContainerID, client.CopyToContainerOptions{
			DestinationPath: "/",
			Content:         s.tar.tar,
		}); err != nil {
			return fmt.Errorf("copy plugin %q: %w", s.tree.Name, err)
		}
		if err := CopyFileIn(ctx, b.cli, h, pluginMarkers+"/"+s.tree.Name, []byte(s.tree.Version+"\n"), 0o644); err != nil {
			return fmt.Errorf("mark plugin %q: %w", s.tree.Name, err)
		}
	}
	return nil
}

// pluginPaths is each named tree and its marker.
func pluginPaths(names ...string) []string {
	paths := make([]string, 0, 2*len(names))
	for _, name := range names {
		paths = append(paths, InBoxPluginsRoot+"/"+name, pluginMarkers+"/"+name)
	}
	return paths
}

type stagedPlugin struct {
	tree PluginTree
	tar  stagedSource
}

// stagePluginTrees spools every tree's tar before the first one is cleared. A tree that cannot
// be staged is skipped and logged: one faulty plugin must not cost the box its other plugins,
// or its skills and its shell.
func stagePluginTrees(trees []PluginTree, spoolDir string) ([]stagedPlugin, error) {
	if err := ensureSpoolDir(spoolDir); err != nil {
		return nil, err
	}
	staged := make([]stagedPlugin, 0, len(trees))
	for _, t := range trees {
		f, err := stagePluginTree(t, spoolDir)
		if err != nil {
			slog.Warn("plugin mirror: skipping a plugin tree that could not be staged; the rest of the box is unaffected",
				"plugin", t.Name, "host_dir", t.HostDir, "err", err)
			continue
		}
		staged = append(staged, stagedPlugin{tree: t, tar: stagedSource{hostDir: t.HostDir, dest: InBoxPluginsRoot + "/" + t.Name, tar: f}})
	}
	return staged, nil
}

func stagePluginTree(t PluginTree, spoolDir string) (*os.File, error) {
	if err := packs.ValidPluginName(t.Name); err != nil {
		return nil, err
	}
	if err := packs.ValidCommit(t.Version); err != nil {
		return nil, err
	}
	return spoolSource(MaterializeSource{HostDir: t.HostDir, Dest: InBoxPluginsRoot + "/" + t.Name}, spoolDir)
}

func closeStagedPlugins(staged []stagedPlugin) {
	for _, s := range staged {
		s.tar.close()
	}
}
