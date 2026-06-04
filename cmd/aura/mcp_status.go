package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

var mcpLookPath = exec.LookPath

func mcpStatus(args []string, out io.Writer) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--json") { //nolint:gosec // G602 false positive: args[0] guarded by len(args)==1
		return fmt.Errorf("usage: aura mcp status [--json]")
	}
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	statuses := mcpmanager.SnapshotStatus(doc)
	if len(args) == 1 {
		data, err := json.Marshal(statuses)
		if err != nil {
			return fmt.Errorf("encode status: %w", err)
		}
		_, err = out.Write(append(data, '\n'))
		return err
	}
	if err := writef(out, "name\tprofiles\ttrust\truntime\tstartup\tauth\tmounted\tblocked\terror\n"); err != nil {
		return err
	}
	for _, status := range statuses {
		if err := writef(out, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			status.Name,
			strings.Join(status.Profiles, ","),
			status.Trust,
			status.Runtime,
			status.StartupState,
			status.AuthStatus,
			status.MountedToolCount,
			status.BlockedToolCount,
			status.LastError,
		); err != nil {
			return err
		}
	}
	return nil
}

func mcpDoctorAll(ctx context.Context, out io.Writer) error {
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	for _, status := range mcpmanager.SnapshotStatus(doc) {
		server := doc.MCPServers[status.Name]
		if err := writef(out, "%s: %s trust=%s runtime=%s\n", status.Name, status.StartupState, status.Trust, status.Runtime); err != nil {
			return err
		}
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		if status.Trust == mcp.TrustBlocked {
			continue
		}
		if err := writeRuntimeCheck(out, status.Name, server); err != nil {
			return err
		}
		if err := writeRecipeChecks(ctx, out, status.Name, server); err != nil {
			return err
		}
	}
	return nil
}

func mcpLogs(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp logs <name>")
	}
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if _, ok := doc.MCPServers[args[0]]; !ok {
		return fmt.Errorf("MCP server %q not found in managed config", args[0])
	}
	return writef(out, "%s logs: no captured log tail yet; run doctor for live diagnostics\n", args[0])
}

func writeRuntimeCheck(out io.Writer, name string, server mcp.ManagedServer) error {
	if server.Type == mcp.ServerTypeStreamableHTTP || server.URL != "" {
		return writef(out, "%s: http endpoint configured\n", name)
	}
	if strings.TrimSpace(server.Command) == "" {
		return writef(out, "%s: runtime missing command\n", name)
	}
	if _, err := mcpLookPath(server.Command); err != nil {
		return writef(out, "%s: runtime missing %s\n", name, server.Command)
	}
	return writef(out, "%s: runtime ok (%s)\n", name, server.Command)
}

func writeRecipeChecks(ctx context.Context, out io.Writer, name string, server mcp.ManagedServer) error {
	switch {
	case strings.HasPrefix(server.Source, "recipe:mail"):
		return writeMailChecks(out, name, server)
	case strings.HasPrefix(server.Source, "recipe:calendar"):
		if envValue(server.Env, "AURA_CALENDAR_MODE") == "fixture" {
			return writef(out, "%s fixture: ready\n", name)
		}
		return writef(out, "%s live auth: not configured\n", name)
	case strings.HasPrefix(server.Source, "recipe:whatsapp"):
		status := probeWhatsAppBridge(ctx, mcp.ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env})
		return writef(out, "%s bridge: %s\n", name, mcpmanager.RedactSecrets(status))
	default:
		return nil
	}
}

func writeMailChecks(out io.Writer, name string, server mcp.ManagedServer) error {
	for _, key := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "SMTP_FROM"} {
		value := envValue(server.Env, key)
		if value == "" {
			if err := writef(out, "%s env %s=missing\n", name, key); err != nil {
				return err
			}
			continue
		}
		rendered := key + "=" + value
		if key == "SMTP_PASS" {
			rendered = mcpmanager.RedactSecrets(rendered)
		}
		if err := writef(out, "%s env %s\n", name, rendered); err != nil {
			return err
		}
	}
	return nil
}

func envValue(env []string, key string) string {
	for _, entry := range env {
		gotKey, value, ok := strings.Cut(entry, "=")
		if ok && gotKey == key {
			return value
		}
	}
	return ""
}
