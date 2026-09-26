package main

import (
	"context"
	"io"
	"regexp"
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

func (b sandboxMCPBox) StartStdio(ctx context.Context, name string, argv, env []string, stdin io.ReadCloser, stdout io.Writer) (mcp.BoxProcess, error) {
	h, err := b.router.Route(ctx)
	if err != nil {
		return nil, err
	}
	job, err := b.router.ExecStream(ctx, h, usersandbox.ExecRequest{
		Command: boxStdioCommand(name, argv),
		Dir:     "/workspace",
		Env:     env,
	}, stdin, stdout)
	if err != nil {
		return nil, err
	}
	return boxStdioProcess{ExecStreamHandle: job, touch: func() { b.router.Touch(h.IdentityID) }}, nil
}

type boxStdioProcess struct {
	*usersandbox.ExecStreamHandle
	touch func()
}

func (p boxStdioProcess) Touch() { p.touch() }

var unsafeLogNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// boxStdioCommand runs the server with its stderr in a per-server log in the box: ExecStream
// merges stderr into stdout, where one log line would break the JSON-RPC framing (measured,
// prd.md §12). No `exec`: the server stays a child of ExecStream's wrapper shell, whose trap
// removes the job's pid file when the server ends.
func boxStdioCommand(name string, argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, tools.ShellQuoteArg(arg))
	}
	logPath := "/tmp/aura-mcp-" + unsafeLogNameChars.ReplaceAllString(name, "_") + ".log"
	return strings.Join(quoted, " ") + " 2>>" + tools.ShellQuoteArg(logPath)
}
