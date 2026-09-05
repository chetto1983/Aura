package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/mcp/mcpenv"
)

// mcp_prepare.go is the install-time half of amendment #211, shared by `aura mcp add` and the
// cockpit's install handler so a server cannot be installed one way and left unprepared the
// other.
//
// Installing a stdio server used to mean storing a launch declaration. This turns it into
// three steps — prepare the environment, rewrite the launch to absolute paths inside it,
// verify it actually handshakes — and only a server that passes all three is written to the
// registry. Verification is mcp.ProbeServer, which already dials, completes initialize and
// counts tools/list; there is no second verifier here.

// installVerifyTimeout bounds the handshake an install waits for. A stdio server that needs
// longer than this to answer initialize on a PREPARED environment is not slow, it is broken:
// preparation already did the fetching, so the process is starting a local binary.
const installVerifyTimeout = 30 * time.Second

// installPrepareTimeout bounds the environment build. It is generous because this step does
// the fetching the mount no longer will — a git-hosted server is cloned and built here
// (measured 2026-09-05: 16s for calculator-mcp-server on a cold environment) — but it is
// bounded because the cockpit runs it on a request goroutine, and the daemon's HTTP server
// sets no write timeout: a resolver waiting forever on a dead index would otherwise hold that
// request open for as long as the browser stayed on the page.
const installPrepareTimeout = 5 * time.Minute

// execPreparer builds the live preparer. A nil-safe zero value (no root configured) makes
// Prepare a passthrough, which is what `aura chat` and the tests without a filesystem want.
func execPreparer(cfg *config.Config) *mcpenv.Preparer {
	if cfg == nil || strings.TrimSpace(cfg.MCPEnvDir) == "" {
		return nil
	}
	return &mcpenv.Preparer{Root: cfg.MCPEnvDir, Run: runPrepareCommand}
}

// runPrepareCommand runs one preparation step. The argv is built by mcpenv from a fixed verb
// set (uv/npm) plus the package the operator named, never from a shell string, so there is no
// interpretation step for an argument to escape through.
//
// The environment is mcp.InstallerEnv, not this process's: an installer runs the package's own
// setup code, so inheriting Aura's environment would hand it every secret Aura holds. Audit
// A1, 2026-09-05 — this used to set no Env at all, while the MOUNT of the same server was
// already narrowed to fourteen keys.
func runPrepareCommand(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed verbs (uv/npm), operator-supplied package as a separate argv entry
	cmd.Dir = dir
	cmd.Env = mcp.InstallerEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// mcpInstallGuard is the install-time prepare+verify step, as a seam. Production always uses
// prepareAndVerify; the registry-plumbing tests swap it out because what they exercise is the
// add/list/disable/trust bookkeeping, not whether a fixture command can start a real MCP
// server — the guard itself is covered by its own tests. It is a var rather than a parameter
// so the CLI dispatcher's signature stays the one every verb shares.
var mcpInstallGuard = prepareAndVerify

// prepareAndVerify resolves server into the one that should be stored. For a stdio server it
// prepares the environment, rewrites the launch, and refuses to return unless the result
// completes an MCP handshake. An HTTP server has nothing to prepare and keeps the existing
// post-save probe, so this narrows to the class that was actually broken.
func prepareAndVerify(ctx context.Context, p *mcpenv.Preparer, name string, server mcp.ManagedServer) (mcp.ManagedServer, mcpenv.Report, error) {
	if serverType, _, err := mcp.Classify(server); err != nil || serverType != mcp.ServerTypeStdio {
		return server, mcpenv.Report{}, nil
	}

	prepareCtx, cancelPrepare := context.WithTimeout(ctx, installPrepareTimeout)
	defer cancelPrepare()
	prepared, report, err := p.Prepare(prepareCtx, name, mcpenv.Launch{Command: server.Command, Args: server.Args})
	if err != nil {
		return mcp.ManagedServer{}, mcpenv.Report{}, fmt.Errorf("prepare %q: %w", name, err)
	}
	server.Command = prepared.Command
	server.Args = prepared.Args

	verifyCtx, cancel := context.WithTimeout(ctx, installVerifyTimeout)
	defer cancel()
	probe := mcp.ProbeServer(verifyCtx, name, server)
	if !probe.OK {
		return mcp.ManagedServer{}, mcpenv.Report{}, fmt.Errorf(
			"verify %q: the server did not complete an MCP handshake, so it was not installed: %s", name, probe.Err)
	}
	return server, report, nil
}

// describePreparation is the one operator-facing line an install prints about its environment.
// A passthrough says so rather than staying silent: "nothing was prepared" is information the
// operator needs to read the next failure.
func describePreparation(rep mcpenv.Report) string {
	if !rep.Prepared {
		return "no environment prepared (the command is launched as declared)"
	}
	return fmt.Sprintf("%s environment prepared in %s (%s -> %s)", rep.Ecosystem, rep.Dir, rep.Package, rep.Entrypoint)
}
