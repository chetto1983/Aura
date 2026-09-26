package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/mcp/mcpenv"
)

// withoutMCPInstallGuard neutralises the amendment #211 install guard for the tests that
// exercise registry bookkeeping (add/list/disable/trust/status) rather than launchability.
// Those tests declare fixture commands that were never meant to start a real MCP server, and
// before #211 an add stored whatever it was given. The guard's own behaviour is covered by
// TestPrepareAndVerify* and by internal/mcp/mcpenv.
func withoutMCPInstallGuard(t *testing.T) {
	t.Helper()
	prev := mcpInstallGuard
	mcpInstallGuard = func(_ context.Context, _ *mcpenv.Preparer, _ string, s mcp.ManagedServer, _ mcp.BoxLauncher) (mcp.ManagedServer, mcpenv.Report, *mcp.ProbeResult, error) {
		return s, mcpenv.Report{}, nil, nil
	}
	t.Cleanup(func() { mcpInstallGuard = prev })
}

// An HTTP server has no environment to prepare and keeps the post-save probe it always had,
// so the guard must leave it exactly as it found it — including not spawning anything.
func TestPrepareAndVerifyLeavesHTTPServersAlone(t *testing.T) {
	in := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.example.test"}
	out, rep, verified, err := prepareAndVerify(context.Background(), nil, "remote", in, nil)
	if err != nil {
		t.Fatalf("prepareAndVerify: %v", err)
	}
	if rep.Prepared {
		t.Fatalf("report = %#v, want nothing prepared for an HTTP server", rep)
	}
	// A nil probe is how the caller learns it must probe for itself (audit A5).
	if verified != nil {
		t.Fatalf("probe = %#v, want nil for a transport this does not verify", verified)
	}
	if out.URL != in.URL || out.Command != "" {
		t.Fatalf("server rewritten: %#v", out)
	}
}

// The whole point of #211: a stdio server that cannot complete a handshake is NOT stored. The
// error has to name the server and say it was not installed, because the failure this replaces
// reported "recv: unexpected EOF" and sent the operator looking at the transport.
func TestPrepareAndVerifyRefusesAServerThatCannotHandshake(t *testing.T) {
	in := mcp.ManagedServer{Command: "/nonexistent/aura-test-mcp-server", Args: []string{"--stdio"}}
	_, _, _, err := prepareAndVerify(context.Background(), nil, "broken", in, nil)
	if err == nil {
		t.Fatal("prepareAndVerify accepted a server that cannot start")
	}
	for _, want := range []string{"broken", "not installed", "handshake"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err %q should contain %q", err, want)
		}
	}
}

// A passthrough is a normal outcome and has to say so: silence would read as "prepared".
func TestDescribePreparationDistinguishesPassthrough(t *testing.T) {
	if got := describePreparation(mcpenv.Report{}); !strings.Contains(got, "no environment prepared") {
		t.Fatalf("passthrough description = %q", got)
	}
	got := describePreparation(mcpenv.Report{
		Prepared: true, Ecosystem: "python", Dir: "/var/lib/aura/mcp-envs/calc",
		Package: "calculator-mcp-server", Entrypoint: "/var/lib/aura/mcp-envs/calc/venv/bin/calculator-mcp-server",
	})
	for _, want := range []string{"python", "/var/lib/aura/mcp-envs/calc", "calculator-mcp-server"} {
		if !strings.Contains(got, want) {
			t.Fatalf("description %q should contain %q", got, want)
		}
	}
}

// The add path must surface the refusal rather than storing the row and reporting success.
func TestMCPAddRefusesAnUnverifiableServer(t *testing.T) {
	withMemoryMCPRegistry(t)
	var out bytes.Buffer
	err := runMCPCommand(context.Background(), nil,
		[]string{"add", "broken", "--", "/nonexistent/aura-test-mcp-server"}, &out)
	if err == nil {
		t.Fatal("mcp add stored a server that cannot start")
	}
	if doc := readMCPRegistry(t); len(doc.MCPServers) != 0 {
		t.Fatalf("registry = %#v, want the refused server absent", doc.MCPServers)
	}
	if strings.Contains(out.String(), "ok: added") {
		t.Fatalf("output claimed success: %q", out.String())
	}
}

// buildInstallServer had no test at all, and the CLI-equivalent preview it writes was edited
// while it did not (audit A7). The preview is what an operator copies into a terminal, so a
// preview that omits the arguments names a command the CLI would not run — and for a resolver
// launch the arguments carry the package itself.
func TestBuildInstallServerPreviewsTheWholeCommand(t *testing.T) {
	server, cli, err := buildInstallServer(agui.MCPInstallRequest{
		Name:    "calc",
		Command: "uvx",
		Args:    []string{"--from", "git+https://example.test/calc.git", "calc"},
	})
	if err != nil {
		t.Fatalf("buildInstallServer: %v", err)
	}
	const want = "aura mcp add calc -- uvx --from git+https://example.test/calc.git calc"
	if cli != want {
		t.Fatalf("cli = %q, want %q", cli, want)
	}
	if server.Source != "custom" || server.Command != "uvx" || len(server.Args) != 3 {
		t.Fatalf("server = %#v", server)
	}
}

// The cockpit declares a box server as `aura mcp add --box` does, and its preview says so.
func TestBuildInstallServerDeclaresABoxRuntime(t *testing.T) {
	server, cli, err := buildInstallServer(agui.MCPInstallRequest{
		Name: "fetch", Command: "uvx", Args: []string{"mcp-server-fetch==2026.8.18"},
		Runtime: mcp.RuntimeKindBox, InitTimeoutSec: 120,
	})
	if err != nil {
		t.Fatalf("buildInstallServer: %v", err)
	}
	if !mcp.IsBoxRuntime(server) || mcp.BoxInitTimeout(server) != 120*time.Second {
		t.Fatalf("server = %#v", server)
	}
	const want = "aura mcp add fetch --box --init-timeout 120 -- uvx mcp-server-fetch==2026.8.18"
	if cli != want {
		t.Fatalf("cli = %q, want %q", cli, want)
	}
	for _, req := range []agui.MCPInstallRequest{
		{Name: "memory", Recipe: "memory", Runtime: mcp.RuntimeKindBox},
		{Name: "gh", URL: "https://mcp.example.test", InitTimeoutSec: 60},
	} {
		if _, _, err := buildInstallServer(req); err == nil {
			t.Fatalf("%+v: a runtime outside a custom stdio server was accepted", req)
		}
	}
}

func TestBuildInstallServerReadsTheOtherTwoShapes(t *testing.T) {
	remote, cli, err := buildInstallServer(agui.MCPInstallRequest{Name: "gh", URL: "https://mcp.example.test", Type: mcp.ServerTypeStreamableHTTP})
	if err != nil {
		t.Fatalf("remote: %v", err)
	}
	if remote.URL != "https://mcp.example.test" || cli != "aura mcp add gh" {
		t.Fatalf("remote = %#v cli = %q", remote, cli)
	}

	recipe, cli, err := buildInstallServer(agui.MCPInstallRequest{Name: "memory", Recipe: "memory", Env: []string{"K=V"}})
	if err != nil {
		t.Fatalf("recipe: %v", err)
	}
	if cli != "aura mcp install memory" || len(recipe.Env) == 0 || recipe.Env[len(recipe.Env)-1] != "K=V" {
		t.Fatalf("recipe = %#v cli = %q", recipe, cli)
	}

	if _, _, err := buildInstallServer(agui.MCPInstallRequest{Name: "empty"}); err == nil {
		t.Fatal("a request naming neither a command nor a url was accepted")
	}
	if _, _, err := buildInstallServer(agui.MCPInstallRequest{Name: "x", Recipe: "no-such-recipe"}); err == nil {
		t.Fatal("an unknown recipe was accepted")
	}
}

// A box server's environment is the box image, so an install prepares nothing on this host
// and runs its handshake in the installing identity's box; with no box it is not installed.
func TestPrepareAndVerifyRunsABoxServerInTheBoxAndPreparesNothingHere(t *testing.T) {
	prep := &mcpenv.Preparer{Root: t.TempDir(), Run: func(context.Context, string, string, ...string) (string, error) {
		t.Fatal("a box server was prepared on the host")
		return "", nil
	}}
	in := mcp.ManagedServer{Command: "agent-browser", Args: []string{"mcp"}, Runtime: mcp.ManagedRuntime{Kind: mcp.RuntimeKindBox}}

	out, _, verified, err := prepareAndVerify(context.Background(), prep, "browser", in, sdkBox{})
	if err != nil || verified == nil || !verified.OK || verified.ToolCount != 1 {
		t.Fatalf("install in a box = %+v, %v", verified, err)
	}
	if out.Command != "agent-browser" || !slices.Equal(out.Args, in.Args) {
		t.Fatalf("the launch was rewritten: %+v", out)
	}
	if _, _, _, err := prepareAndVerify(context.Background(), prep, "browser", in, nil); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("install without a box = %v, want refused", err)
	}
}

// sdkBox answers a box start with a real one-tool SDK server over the exec's pipes.
type sdkBox struct{}

type sdkBoxProc struct {
	stdin io.ReadCloser
	done  chan struct{}
}

func (p sdkBoxProc) Wait() (int, error) { <-p.done; return 0, nil }
func (p sdkBoxProc) Kill()              { _ = p.stdin.Close() }
func (p sdkBoxProc) Touch()             {}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func (sdkBox) StartStdio(_ context.Context, _ string, _, _ []string, stdin io.ReadCloser, stdout io.Writer) (mcp.BoxProcess, error) {
	p := sdkBoxProc{stdin: stdin, done: make(chan struct{})}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "box", Version: "0"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{}, nil
		})
	go func() {
		defer close(p.done)
		if ss, err := server.Connect(context.Background(), &sdkmcp.IOTransport{Reader: stdin, Writer: nopWriteCloser{stdout}}, nil); err == nil {
			_ = ss.Wait()
		}
	}()
	return p, nil
}

// envBox records the env a box start was given, and starts nothing.
type envBox struct{ env *[]string }

func (b envBox) StartStdio(_ context.Context, _ string, _, env []string, _ io.ReadCloser, _ io.Writer) (mcp.BoxProcess, error) {
	*b.env = env
	return nil, errors.New("not started")
}

// A declaration the registry would refuse is refused before its handshake: a box server's
// verification runs in a box the agent's shell can read, so a secret refused only at the save
// had already been handed to that box.
func TestInstallRefusesAnInvalidDeclarationBeforeStartingIt(t *testing.T) {
	var seen []string
	in := mcp.ManagedServer{Command: "uvx", Args: []string{"mcp-server-fetch==2026.8.18"},
		Env: []string{"GITHUB_TOKEN=ghp_0123456789abcdef0123456789abcdef0123"}, Runtime: mcp.ManagedRuntime{Kind: mcp.RuntimeKindBox}}
	_, _, _, err := prepareAndVerify(context.Background(), nil, "fetch", in, envBox{env: &seen})
	if err == nil || !strings.Contains(err.Error(), "box servers take no secrets") {
		t.Fatalf("err = %v, want the secret refusal", err)
	}
	if seen != nil {
		t.Fatalf("the box was started with %q before the refusal", seen)
	}
}
