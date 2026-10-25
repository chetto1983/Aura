package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// sandboxMCPBox starts box-runtime MCP servers (mcp.RuntimeKindBox) in the box of the
// identity the context carries, over the same stdin-capable ExecStream the browser live view
// uses: an exec is Aura's only channel into a box.
type sandboxMCPBox struct {
	router *usersandbox.SandboxRouter
}

// newSandboxMCPBox returns nil without a router, so a host with no sandbox refuses box
// servers (mcp.ErrNoBox) instead of failing each start on a missing backend.
func newSandboxMCPBox(router *usersandbox.SandboxRouter) mcp.BoxLauncher {
	if router == nil {
		return nil
	}
	return sandboxMCPBox{router: router}
}

func (b sandboxMCPBox) StartStdio(ctx context.Context, start mcp.BoxStart, stdin io.ReadCloser, stdout io.Writer) (mcp.BoxProcess, error) {
	return b.startStdio(ctx, start, stdin, stdout, false)
}

func (b sandboxMCPBox) StartBrowserStdio(ctx context.Context, start mcp.BoxStart, stdin io.ReadCloser, stdout io.Writer) (mcp.BoxProcess, error) {
	if !slices.Equal(start.Argv, []string{"agent-browser", "mcp"}) || len(start.Env) != 0 || start.Dir != "" || start.DataDir != "" {
		return nil, errors.New("browser sandbox only admits its fixed recipe command")
	}
	return b.startStdio(ctx, start, stdin, stdout, true)
}

func (b sandboxMCPBox) startStdio(ctx context.Context, start mcp.BoxStart, stdin io.ReadCloser, stdout io.Writer, browser bool) (mcp.BoxProcess, error) {
	route := b.router.Route
	if browser {
		route = b.router.RouteBrowser
	}
	h, err := route(ctx)
	if errors.Is(err, usersandbox.ErrImageUnavailable) {
		return nil, fmt.Errorf("%w: %w", mcp.ErrBoxUnavailable, err)
	}
	if err != nil {
		return nil, err
	}
	job, err := b.router.ExecStream(ctx, h, boxExecRequest(start), stdin, stdout)
	if err != nil {
		return nil, err
	}
	// The browser box is never suspended for idleness, so its calls keep no clock.
	touch := func() {}
	if !browser {
		touch = func() { b.router.Touch(h.IdentityID) }
	}
	return boxStdioProcess{ExecStreamHandle: job, touch: touch}, nil
}

type boxStdioProcess struct {
	*usersandbox.ExecStreamHandle
	touch func()
}

func (p boxStdioProcess) Touch() { p.touch() }

var unsafeFileNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// boxFileName is a server name made safe to use in a path in the box.
func boxFileName(name string) string {
	return unsafeFileNameChars.ReplaceAllString(name, "_")
}

func boxLogPath(name string) string {
	return "/tmp/aura-mcp-" + boxFileName(name) + ".log"
}

// boxExecRequest is the exec a box server starts as. It always starts in /workspace: Docker
// does not create an exec's working directory, and a plugin server's directory may sit under
// its data directory, which only the `mkdir -p` in front of it creates.
func boxExecRequest(start mcp.BoxStart) usersandbox.ExecRequest {
	command := boxStdioCommand(start.Name, start.Argv)
	if start.Dir != "" {
		command = "cd " + tools.ShellQuoteArg(start.Dir) + " && " + command
	}
	if start.DataDir != "" {
		command = "mkdir -p " + tools.ShellQuoteArg(start.DataDir) + " && " + command
	}
	return usersandbox.ExecRequest{Command: command, Dir: "/workspace", Env: start.Env}
}

// boxStdioCommand runs the server with its stderr in a per-server log in the box: ExecStream
// merges stderr into stdout, where one log line would break the JSON-RPC framing (measured,
// prd.md §12). No `exec`: the server stays a child of ExecStream's wrapper shell, whose trap
// removes the job's pid file when the server ends.
func boxStdioCommand(name string, argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, tools.ShellQuoteArg(arg))
	}
	return strings.Join(quoted, " ") + " 2>>" + tools.ShellQuoteArg(boxLogPath(name))
}
