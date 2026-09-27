package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
)

const mcpAddUsage = "usage: aura mcp add <name> [--env KEY=VALUE] [--disabled] " +
	"(--url URL | [--trust local] [--box [--init-timeout SECONDS]] -- <command> [args...])"

func mcpAdd(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	name, server, err := parseMCPAddArgs(args)
	if err != nil {
		return err
	}
	doc, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]mcp.ManagedServer{}
	}
	if _, exists := doc.MCPServers[name]; exists {
		return fmt.Errorf("MCP server %q already exists", name)
	}
	cfg := config.LoadDB()
	var launcher mcp.BoxLauncher
	if mcp.IsBoxRuntime(server) {
		ctx, launcher = operatorBoxLauncher(ctx, cfg, pool)
	}

	// Amendment #211: an add is an install. Prepare the environment, rewrite the launch into
	// it, and refuse to store a server that cannot complete a handshake — the declaration
	// this used to write was only ever a promise that something would resolve at mount. A box
	// server completes that handshake in the operator's own box.
	prepared, report, _, err := mcpInstallGuard(ctx, execPreparer(cfg), name, server, launcher)
	if err != nil {
		return err
	}

	doc.MCPServers[name] = prepared
	ensureProfileMembership(&doc, doc.ActiveProfileName(), name)
	if err := mcpWriteManagedConfig(ctx, pool, doc, "add", name, ""); err != nil {
		return err
	}
	if err := writef(out, "%s\n", describePreparation(report)); err != nil {
		return err
	}
	return writef(out, "ok: added %s\n", name)
}

// parseMCPAddArgs reads `mcp add` into the server it declares: a stdio command after "--", or a
// streamable-HTTP server with --url. Either is stored blocked unless --trust says otherwise.
func parseMCPAddArgs(args []string) (string, mcp.ManagedServer, error) {
	if len(args) == 0 {
		return "", mcp.ManagedServer{}, errors.New(mcpAddUsage)
	}
	name := strings.TrimSpace(args[0])
	if name == "" {
		return "", mcp.ManagedServer{}, fmt.Errorf("MCP server name cannot be empty")
	}
	server := mcp.ManagedServer{
		Env:     []string{},
		Enabled: new(true),
		Source:  "manual",
		Trust:   mcp.ManagedTrust{Class: mcp.TrustBlocked},
	}
	rest := args[1:]
	for len(rest) > 0 {
		flag := rest[0]
		rest = rest[1:]
		switch flag {
		case "--":
			if len(rest) == 0 {
				return "", mcp.ManagedServer{}, errors.New(mcpAddUsage)
			}
			server.Command, server.Args = rest[0], append([]string{}, rest[1:]...)
			rest = nil
			continue
		case "--disabled":
			server.Enabled = new(false)
			continue
		case "--box":
			server.Runtime.Kind = mcp.RuntimeKindBox
			continue
		case "--env", "--trust", "--init-timeout", "--url":
		default:
			return "", mcp.ManagedServer{}, fmt.Errorf("unknown mcp add option %q", flag)
		}
		if len(rest) == 0 {
			return "", mcp.ManagedServer{}, fmt.Errorf("%s requires %s", flag, mcpAddValueName[flag])
		}
		value := rest[0]
		rest = rest[1:]
		if err := setMCPAddValue(&server, flag, value); err != nil {
			return "", mcp.ManagedServer{}, err
		}
	}
	return name, server, checkMCPAddShape(server)
}

var mcpAddValueName = map[string]string{
	"--env":          "KEY=VALUE",
	"--trust":        "local",
	"--init-timeout": "a number of seconds",
	"--url":          "a URL",
}

func setMCPAddValue(server *mcp.ManagedServer, flag, value string) error {
	switch flag {
	case "--env":
		if !strings.Contains(value, "=") {
			return fmt.Errorf("--env value %q must be KEY=VALUE", value)
		}
		server.Env = append(server.Env, value)
	case "--trust":
		if value != "local" {
			return fmt.Errorf("--trust value %q must be local", value)
		}
		server.Trust.Class = mcp.TrustTrustedLocal
	case "--init-timeout":
		sec, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("--init-timeout value %q must be a number of seconds", value)
		}
		server.Runtime.InitTimeoutSec = sec
	case "--url":
		server.URL = strings.TrimSpace(value)
		server.Type = mcp.ServerTypeStreamableHTTP
	}
	return nil
}

// checkMCPAddShape refuses a mix of the two shapes. The stdio-only flags would otherwise be
// stored on an HTTP server, where nothing reads them.
func checkMCPAddShape(server mcp.ManagedServer) error {
	switch {
	case server.URL == "" && server.Command == "":
		return errors.New(mcpAddUsage)
	case server.URL != "" && server.Command != "":
		return fmt.Errorf("mcp add: --url and a command after -- declare two different servers")
	case server.URL != "" && (server.Runtime.Kind != "" || server.Runtime.InitTimeoutSec != 0 || server.Trust.Class != mcp.TrustBlocked):
		return fmt.Errorf("mcp add: --trust, --box and --init-timeout apply to a stdio server, not to --url")
	}
	return nil
}
