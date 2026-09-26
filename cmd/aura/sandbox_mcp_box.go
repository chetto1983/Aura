package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

// boxInstallDir keeps, per identity, the record of each box server's completed install. It is
// on the workspace volume beside what the install put there, so the two survive a box
// recreate together and leave together when the identity is deprovisioned.
const boxInstallDir = "/workspace/.aura-mcp"

func (b sandboxMCPBox) Install(ctx context.Context, name, script string, env []string) error {
	h, err := b.router.Route(ctx)
	if err != nil {
		return err
	}
	res, err := b.router.Exec(ctx, h, usersandbox.ExecRequest{Command: boxInstallCommand(name, script), Dir: "/workspace", Env: env})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("exit %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// boxInstallCommand runs script once per distinct script: the record holds the hash of the
// script that last completed, a lock serialises concurrent starts of the same server in one
// box, and a failure writes no record and reports the tail of the install log. The script
// stays on its own lines inside the subshell, so a trailing comment cannot swallow the
// closing parenthesis.
func boxInstallCommand(name, script string) string {
	sum := sha256.Sum256([]byte(script))
	hash := hex.EncodeToString(sum[:])
	record := boxInstallDir + "/" + boxFileName(name) + ".installed"
	log := tools.ShellQuoteArg(boxLogPath(name + "-install"))
	inner := "[ \"$(cat " + tools.ShellQuoteArg(record) + " 2>/dev/null)\" = " + hash + " ] && exit 0\n" +
		"(\n" + script + "\n) >>" + log + " 2>&1 || { rc=$?; tail -n 20 " + log + " >&2; exit $rc; }\n" +
		"printf %s " + hash + " > " + tools.ShellQuoteArg(record)
	return "mkdir -p " + tools.ShellQuoteArg(boxInstallDir) +
		" && flock " + tools.ShellQuoteArg(record+".lock") + " sh -c " + tools.ShellQuoteArg(inner)
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

var unsafeFileNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// boxFileName is a server name made safe to use in a path in the box.
func boxFileName(name string) string {
	return unsafeFileNameChars.ReplaceAllString(name, "_")
}

func boxLogPath(name string) string {
	return "/tmp/aura-mcp-" + boxFileName(name) + ".log"
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
