package mcp

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// A plugin's stdio server runs in its plugin root (§7.2.1's default working directory) and
// writes into its data directory. Both are in-box paths the registry carries, so the shapes it
// accepts are the containment line: a plugin root, or the workspace, and a data directory under
// the one root Aura reserves for plugin data.
func TestValidateBoxRuntimeDirs(t *testing.T) {
	t.Parallel()
	box := func(dir, data string) ManagedServer {
		return ManagedServer{Command: "uvx", Runtime: ManagedRuntime{Kind: RuntimeKindBox, Dir: dir, DataDir: data}}
	}
	for name, server := range map[string]ManagedServer{
		"plugin root":                  box("/plugins/broker-analysis", ""),
		"plugin subdir":                box("/plugins/p/bin", ""),
		"workspace":                    box("/workspace/x", ""),
		"data dir":                     box("", "/workspace/.plugin-data/broker-analysis-1a2b3c4d5e6f"),
		"cwd under the data dir":       box("/workspace/.plugin-data/p-1a2b/x", "/workspace/.plugin-data/p-1a2b"),
		"no dirs, as before the slice": box("", ""),
	} {
		if err := ValidateManagedServer("s", server); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	for name, server := range map[string]ManagedServer{
		"relative dir":        box("plugins/x", ""),
		"system dir":          box("/etc", ""),
		"escaping dir":        box("/plugins/../etc", ""),
		"the plugins root":    box("/plugins", ""),
		"data off its root":   box("", "/workspace/x"),
		"data in tmp":         box("", "/tmp/x"),
		"escaping data":       box("", "/workspace/.plugin-data/../x"),
		"the data root":       box("", "/workspace/.plugin-data"),
		"dir on a local one":  {Command: "uvx", Runtime: ManagedRuntime{Dir: "/plugins/p"}},
		"data on a local one": {Command: "uvx", Runtime: ManagedRuntime{DataDir: "/workspace/.plugin-data/p"}},
	} {
		if err := ValidateManagedServer("s", server); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestBoxSessionStartsInTheServerDirWithItsDataDir(t *testing.T) {
	box := &fakeBox{}
	server := ManagedServer{Command: "uvx", Args: []string{"x"}, Runtime: ManagedRuntime{
		Kind: RuntimeKindBox, Dir: "/plugins/p", DataDir: "/workspace/.plugin-data/p-1a2b",
	}}
	session, err := OpenSDKSession(context.Background(), "p", server, EgressPolicy{}, SessionOptions{Box: box})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = session.Close()
	waitClosed(t, box.proc(t).done, "the box process")
	if box.start.Name != "p" || box.start.Dir != "/plugins/p" || box.start.DataDir != "/workspace/.plugin-data/p-1a2b" ||
		!slices.Equal(box.start.Argv, []string{"uvx", "x"}) {
		t.Errorf("the box got %+v", box.start)
	}
}

func TestAddToProfileIsIdempotent(t *testing.T) {
	t.Parallel()
	var doc ManagedConfig
	doc.AddToProfile("", "b")
	doc.AddToProfile("", "a")
	doc.AddToProfile("", "b")
	if got := doc.Profiles[DefaultMCPProfile].Servers; !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("default profile = %v", got)
	}
	doc.AddToProfile("work", "c")
	if got := doc.Profiles["work"].Servers; !slices.Equal(got, []string{"c"}) {
		t.Errorf("work profile = %v", got)
	}
}

func TestRemoveServerLeavesEveryProfile(t *testing.T) {
	t.Parallel()
	shared := []string{"a", "b"}
	doc := ManagedConfig{
		MCPServers: map[string]ManagedServer{"a": {Command: "x"}, "b": {Command: "y"}},
		Profiles:   map[string]ManagedProfile{DefaultMCPProfile: {Servers: shared}, "work": {Servers: []string{"a"}}},
	}
	if !doc.RemoveServer("a") || doc.RemoveServer("ghost") {
		t.Fatal("RemoveServer misreported whether the server was there")
	}
	if _, ok := doc.MCPServers["a"]; ok {
		t.Error("the server is still registered")
	}
	if got := doc.Profiles[DefaultMCPProfile].Servers; !slices.Equal(got, []string{"b"}) {
		t.Errorf("default profile = %v", got)
	}
	if got := doc.Profiles["work"].Servers; len(got) != 0 {
		t.Errorf("work profile = %v", got)
	}
	if !slices.Equal(shared, []string{"a", "b"}) {
		t.Errorf("RemoveServer rewrote a slice it did not own: %v", shared)
	}
}

func TestManagedStdioConfigCarriesTheBoxDirs(t *testing.T) {
	t.Parallel()
	cfg, err := ManagedStdioConfig(ManagedServer{Command: "uvx", Runtime: ManagedRuntime{
		Kind: RuntimeKindBox, Dir: "/plugins/p", DataDir: "/workspace/.plugin-data/p",
	}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !cfg.Box || cfg.Dir != "/plugins/p" || !strings.HasSuffix(cfg.DataDir, "/p") {
		t.Errorf("config = %+v", cfg)
	}
}
