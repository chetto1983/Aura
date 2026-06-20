package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

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
	if err := writef(out, "name\tprofiles\ttrust\truntime\tstartup\tauth\terror\n"); err != nil {
		return err
	}
	for _, status := range statuses {
		if err := writef(out, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			status.Name,
			strings.Join(status.Profiles, ","),
			status.Trust,
			status.Runtime,
			status.StartupState,
			status.AuthStatus,
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
		if err := writeRuntimeCheck(ctx, out, status.Name, server); err != nil {
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

// writeRuntimeCheck renders one server's reachability line for `mcp doctor --all`
// from the structured mcp.ProbeServer result (GOV-01: one probe, two renderers — the
// CLI text here and the governance board JSON elsewhere). The message vocabulary is
// unchanged: an HTTP/streamable server reports its configured endpoint without a dial;
// a stdio server with no command reports the missing command; otherwise the probe
// dials + tools/list under ctx and a success/failure maps to "runtime ok (cmd)" /
// "runtime missing cmd". ctx bounds the dial so a hung server cannot stall the doctor.
func writeRuntimeCheck(ctx context.Context, out io.Writer, name string, server mcp.ManagedServer) error {
	if server.Type == mcp.ServerTypeStreamableHTTP || server.URL != "" {
		return writef(out, "%s: http endpoint configured\n", name)
	}
	if strings.TrimSpace(server.Command) == "" {
		return writef(out, "%s: runtime missing command\n", name)
	}
	res := mcp.ProbeServer(ctx, name, server)
	if !res.OK {
		return writef(out, "%s: runtime missing %s\n", name, server.Command)
	}
	return writef(out, "%s: runtime ok (%s)\n", name, server.Command)
}

func writeRecipeChecks(ctx context.Context, out io.Writer, name string, server mcp.ManagedServer) error {
	switch {
	case strings.HasPrefix(server.Source, "recipe:calendar"):
		// The PIM sidecar (forked calendar-mcp) manages its own OAuth accounts via
		// the token-gated admin REST API the cockpit drives; Aura sets no auth env
		// here, so the doctor reports the endpoint rather than probing env.
		endpoint := strings.TrimSpace(server.URL)
		if endpoint == "" {
			endpoint = "(no endpoint)"
		}
		return writef(out, "%s pim sidecar: accounts managed via admin API at %s\n", name, endpoint)
	case strings.HasPrefix(server.Source, "recipe:whatsapp"):
		status := probeWhatsAppBridge(ctx, mcp.ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env})
		return writef(out, "%s bridge: %s\n", name, mcpmanager.RedactSecrets(status))
	default:
		return nil
	}
}
