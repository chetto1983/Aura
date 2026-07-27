//go:build docker_integration

// fs_box_route_docker_test.go is the docker_integration tier for the three file tools that used to
// reach the HOST filesystem under every profile while fs_read/fs_write were confined to the
// per-identity box (fix plan 2.5). Against a LIVE Docker daemon it proves the two halves that only
// a running box can show: the operation really happens inside the box, and the host tree the tool
// is configured with stays invisible and unmodified.
//
// Compiled ONLY under `-tags docker_integration`; skips locally when dockerd is unreachable (the
// Windows worktree speaks npipe) and t.Fatals under $CI (no-skip-as-green).

package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/moby/moby/client"
)

const hostLeakCanary = "HOST-LEAK-CANARY-2-5"

// newRuncBoxRouter builds a STRICT router on single_user_hardened rather than reusing
// newDockerRouter's server_production. Both route identically — Strict() is true for both, so the
// code under test is the same branch — but specFor selects gVisor (runsc) only for
// server_production, and a daemon without gVisor answers "unknown or invalid runtime name: runsc"
// before a box ever exists. single_user_hardened is also the profile the appliance actually runs.
func newRuncBoxRouter(t *testing.T) *usersandbox.SandboxRouter {
	t.Helper()
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	limits := usersandbox.Resources{NanoCPUs: 1_000_000_000, MemoryBytes: 1 << 30, PidsLimit: 256}
	backend := usersandbox.NewDockerBackend(cli, dockerTestImage(), limits)
	return usersandbox.NewSandboxRouter(backend, config.ProfileSingleUserHardened, config.SandboxConfig{
		Image: dockerTestImage(), CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 256, IdleTTLSec: 1800,
	})
}

// hostTreeWithCanary builds a host directory the routed tool is pointed at. Nothing under it may
// appear in a routed result: if it does, the containment fs_read/fs_write establish is still open
// on this tool.
func hostTreeWithCanary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hostonly.go"), []byte("const secret = \""+hostLeakCanary+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestFSEditRoutedEditsInBoxNotHost proves the edit lands on the box copy and the identically-named
// host file is left byte-for-byte alone.
func TestFSEditRoutedEditsInBoxNotHost(t *testing.T) {
	skipUnlessDockerdTools(t)
	router := newRuncBoxRouter(t)
	hostDir := hostTreeWithCanary(t)
	hostTwin := filepath.Join(hostDir, "app.go")
	original := []byte("port := 8080\n")
	if err := os.WriteFile(hostTwin, original, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := ctxWith(t, "sess-dk-fsedit", "call-dk-fsedit")
	sh := &ShellExec{Router: router}
	if _, err := sh.Execute(ctx, json.RawMessage(
		`{"command":"printf 'port := 8080\\n' > /workspace/app.go"}`,
	)); err != nil {
		t.Fatalf("seed box file: %v", err)
	}

	edit := &FSEdit{WorkspaceRoot: hostDir, Router: router}
	res, err := edit.Execute(ctx, json.RawMessage(
		`{"path":"/workspace/app.go","old_string":"port := 8080","new_string":"port := 9090"}`,
	))
	if err != nil {
		t.Fatalf("routed fs_edit: %v", err)
	}
	if !strings.Contains(res.Preview, "replaced 1 occurrence") {
		t.Fatalf("routed fs_edit result = %q", res.Preview)
	}

	back, err := sh.Execute(ctx, json.RawMessage(`{"command":"cat /workspace/app.go"}`))
	if err != nil {
		t.Fatalf("read back in box: %v", err)
	}
	if !strings.Contains(back.Preview, "port := 9090") {
		t.Fatalf("edit did not land inside the box: %q", back.Preview)
	}

	after, err := os.ReadFile(hostTwin)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("routed fs_edit modified the HOST file — containment breached: %q", after)
	}
}

// TestFSGlobRoutedListsBoxTreeOnly proves the routed enumeration walks the box, and that the host
// tree the tool is configured with never appears in the results.
func TestFSGlobRoutedListsBoxTreeOnly(t *testing.T) {
	skipUnlessDockerdTools(t)
	router := newRuncBoxRouter(t)
	hostDir := hostTreeWithCanary(t)

	ctx := ctxWith(t, "sess-dk-fsglob", "call-dk-fsglob")
	sh := &ShellExec{Router: router}
	if _, err := sh.Execute(ctx, json.RawMessage(
		`{"command":"mkdir -p /workspace/sub /workspace/node_modules/pkg && : > /workspace/sub/inbox.go && : > /workspace/node_modules/pkg/skipme.go"}`,
	)); err != nil {
		t.Fatalf("seed box tree: %v", err)
	}

	glob := &FSGlob{WorkspaceRoot: hostDir, Router: router}
	res, err := glob.Execute(ctx, json.RawMessage(`{"pattern":"**/*.go"}`))
	if err != nil {
		t.Fatalf("routed fs_glob: %v", err)
	}
	if !strings.Contains(res.Preview, "sub/inbox.go") {
		t.Fatalf("routed fs_glob did not list the box tree: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "hostonly.go") {
		t.Fatalf("routed fs_glob listed a HOST file — containment breached: %q", res.Preview)
	}
	// The box enumeration must prune the same directories the host walk prunes.
	if strings.Contains(res.Preview, "skipme.go") {
		t.Fatalf("routed fs_glob did not prune node_modules: %q", res.Preview)
	}
}

// TestFSGrepRoutedSearchesBoxTreeOnly proves the routed search reads box content and cannot reach
// the host tree — the canary is present on the host and must never be reported.
func TestFSGrepRoutedSearchesBoxTreeOnly(t *testing.T) {
	skipUnlessDockerdTools(t)
	router := newRuncBoxRouter(t)
	hostDir := hostTreeWithCanary(t)

	ctx := ctxWith(t, "sess-dk-fsgrep", "call-dk-fsgrep")
	sh := &ShellExec{Router: router}
	if _, err := sh.Execute(ctx, json.RawMessage(
		`{"command":"mkdir -p /workspace/sub && printf 'alpha\\nBOXNEEDLE here\\n' > /workspace/sub/found.txt"}`,
	)); err != nil {
		t.Fatalf("seed box content: %v", err)
	}

	grep := &FSGrep{WorkspaceRoot: hostDir, Router: router}
	res, err := grep.Execute(ctx, json.RawMessage(`{"pattern":"BOXNEEDLE"}`))
	if err != nil {
		t.Fatalf("routed fs_grep: %v", err)
	}
	// Line number and relative path must match what the host branch would report.
	if !strings.Contains(res.Preview, "sub/found.txt:2: BOXNEEDLE here") {
		t.Fatalf("routed fs_grep result = %q", res.Preview)
	}

	leak, err := grep.Execute(ctx, json.RawMessage(`{"pattern":"`+hostLeakCanary+`"}`))
	if err != nil {
		t.Fatalf("routed fs_grep (canary): %v", err)
	}
	if !strings.Contains(leak.Preview, "[no matches]") {
		t.Fatalf("routed fs_grep read the HOST tree — containment breached: %q", leak.Preview)
	}
}
