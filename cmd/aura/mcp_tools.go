package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

func mcpTools(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp tools <name>")
	}
	name := args[0]
	if server, ok, err := effectiveManagedMCPServer(name); err != nil {
		return err
	} else if ok {
		if len(server.ToolPolicy.Allow) == 0 && len(server.ToolPolicy.Deny) == 0 && len(server.ToolPolicy.DenyRisk) == 0 {
			server.ToolPolicy.Allow = mcpAllowlist(name)
		}
		cli, defs, err := openAndListManagedMCPTools(ctx, name, server)
		if err != nil {
			return err
		}
		defer func() { _ = cli.Close() }()
		return writeMCPToolDecisions(out, defs, server)
	}
	server, err := mcpToolPolicyServer(name)
	if err != nil {
		return err
	}
	cfg, err := effectiveMCPServer(name)
	if err != nil {
		return err
	}
	cli, defs, err := openAndListMCPTools(ctx, name, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = cli.Close() }()
	return writeMCPToolDecisions(out, defs, server)
}

func writeMCPToolDecisions(out io.Writer, defs []mcp.ToolDef, server mcp.ManagedServer) error {
	descriptions := make(map[string]string, len(defs))
	for _, d := range defs {
		descriptions[d.Name] = d.Description
	}
	decisions := mcpmanager.PolicyDecisionsForTools(defs, server)
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].ToolName < decisions[j].ToolName })
	for _, decision := range decisions {
		status := "mounted"
		if !decision.Allowed {
			status = "blocked: " + decision.BlockReason
		}
		if err := writef(out, "%s\t%s\t%s\t%s\n",
			decision.ToolName,
			strings.Join(decision.RiskLabels, ","),
			status,
			firstMCPDescriptionLine(descriptions[decision.ToolName]),
		); err != nil {
			return err
		}
	}
	return nil
}

func mcpToolPolicyServer(name string) (mcp.ManagedServer, error) {
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return mcp.ManagedServer{}, err
	}
	server := doc.MCPServers[name]
	if len(server.ToolPolicy.Allow) == 0 && len(server.ToolPolicy.Deny) == 0 && len(server.ToolPolicy.DenyRisk) == 0 {
		server.ToolPolicy.Allow = mcpAllowlist(name)
	}
	return server, nil
}

func effectiveManagedMCPServer(name string) (mcp.ManagedServer, bool, error) {
	cfg := config.LoadDB()
	if cfg.MCPServersErr != nil {
		return mcp.ManagedServer{}, false, cfg.MCPServersErr
	}
	server, ok := cfg.MCPPolicies[name]
	return server, ok, nil
}

func effectiveMCPServer(name string) (mcp.ServerConfig, error) {
	cfg := config.LoadDB()
	if cfg.MCPServersErr != nil {
		return mcp.ServerConfig{}, cfg.MCPServersErr
	}
	server, ok := cfg.MCPServers[name]
	if !ok {
		return mcp.ServerConfig{}, fmt.Errorf("MCP server %q is not configured or is disabled", name)
	}
	return server, nil
}

func openAndListMCPTools(ctx context.Context, name string, cfg mcp.ServerConfig) (*mcp.Client, []mcp.ToolDef, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cli, err := mcp.Open(ctx, name, cfg)
	if err != nil {
		return nil, nil, err
	}
	defs, err := cli.ListTools(ctx)
	if err != nil {
		_ = cli.Close()
		return nil, nil, err
	}
	return cli, defs, nil
}

func openAndListManagedMCPTools(ctx context.Context, name string, server mcp.ManagedServer) (mcp.Transport, []mcp.ToolDef, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var (
		cli mcp.Transport
		err error
	)
	if strings.TrimSpace(server.Type) == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != "" {
		cli, err = mcp.OpenServer(ctx, name, server)
	} else {
		cfg, cfgErr := mcpmanager.RuntimeLaunchConfig(name, server)
		if cfgErr != nil {
			return nil, nil, cfgErr
		}
		cli, err = mcp.Open(ctx, name, cfg)
	}
	if err != nil {
		return nil, nil, err
	}
	defs, err := cli.ListTools(ctx)
	if err != nil {
		_ = cli.Close()
		return nil, nil, err
	}
	return cli, defs, nil
}

func firstMCPDescriptionLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func toolCount(n int) string {
	if n == 1 {
		return "1 tool"
	}
	return fmt.Sprintf("%d tools", n)
}
