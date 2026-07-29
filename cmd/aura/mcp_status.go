package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/envutil"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// mcpStatusRow is the additive live-probe row appended to each config-derived
// mcpmanager.StatusSnapshot (D-17 "one probe, three surfaces"): the embedded
// StatusSnapshot fields describe what is DECLARED (trust/runtime/profiles), Probe
// describes what is LIVE right now. SnapshotStatus itself is unchanged/reused.
type mcpStatusRow struct {
	mcpmanager.StatusSnapshot
	Probe mcp.ProbeResult `json:"probe"`
}

func mcpStatus(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--json") { //nolint:gosec // G602 false positive: args[0] guarded by len(args)==1
		return fmt.Errorf("usage: aura mcp status [--json]")
	}
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	statuses := mcpmanager.SnapshotStatus(doc)
	rows := make([]mcpStatusRow, 0, len(statuses))
	for _, status := range statuses {
		rows = append(rows, mcpStatusRow{StatusSnapshot: status, Probe: probeStatusRow(ctx, doc, status)})
	}
	if len(args) == 1 {
		data, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("encode status: %w", err)
		}
		_, err = out.Write(append(data, '\n'))
		return err
	}
	if err := writef(out, "name\tprofiles\ttrust\truntime\tstartup\tauth\terror\tprobe\n"); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writef(out, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Name,
			strings.Join(row.Profiles, ","),
			row.Trust,
			row.Runtime,
			row.StartupState,
			row.AuthStatus,
			row.LastError,
			probeColumn(row.Probe),
		); err != nil {
			return err
		}
	}
	return nil
}

// probeStatusRow resolves the live-probe result for one status row. Disabled and
// trust-blocked servers are skipped without dialing (Rule 2: a "status" listing
// must never spawn/dial a server the operator turned off or explicitly blocked as
// a side effect of a read-only command — mirroring mcpDoctorAll's own disabled/
// blocked skip). Every other server is probed bounded by AURA_MCP_PROBE_TIMEOUT,
// isolated per server (a dead/hung server fails only its own row).
func probeStatusRow(ctx context.Context, doc mcp.ManagedConfig, status mcpmanager.StatusSnapshot) mcp.ProbeResult {
	switch status.StartupState {
	case mcpmanager.StartupDisabled:
		return mcp.ProbeResult{Name: status.Name, Detail: "skipped (disabled)"}
	case mcpmanager.StartupBlocked:
		return mcp.ProbeResult{Name: status.Name, Detail: "skipped (blocked)"}
	}
	server, ok := doc.MCPServers[status.Name]
	if !ok {
		return mcp.ProbeResult{Name: status.Name, Detail: "not configured", Err: "not configured"}
	}
	probeCtx, cancel := context.WithTimeout(ctx, resolveMCPProbeTimeout())
	defer cancel()
	return probeManagedMCPServer(probeCtx, status.Name, server)
}

// probeColumn renders a mcp.ProbeResult as one tab-safe text column.
func probeColumn(res mcp.ProbeResult) string {
	if res.OK {
		return fmt.Sprintf("ok(%d)", res.ToolCount)
	}
	if res.Err != "" {
		return "fail:" + res.Err
	}
	return res.Detail
}

// resolveMCPProbeTimeout resolves the AURA_MCP_PROBE_TIMEOUT knob (seconds, default 5) that
// bounds every live mcp.ProbeServer dial issued by writeRuntimeCheck, mcpStatus, and
// the new doctor "mcp" check (D-16/D-17) — mirroring the governance board's own
// per-request context.WithTimeout(r.Context(), deadline) shape.
func resolveMCPProbeTimeout() time.Duration {
	return time.Duration(envutil.IntDefault("AURA_MCP_PROBE_TIMEOUT", 5)) * time.Second
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
// from the structured mcp.ProbeServer result (GOV-01: one probe, multiple renderers
// — this CLI text line, the mcp status probe column, and the governance board
// JSON). BOTH stdio and streamable-HTTP servers are dialed through mcp.ProbeServer
// under a bounded context.WithTimeout(ctx, AURA_MCP_PROBE_TIMEOUT): a dead/typoed
// HTTP endpoint no longer short-circuits to a false "http endpoint configured"
// (F-046) — it reports OK=false / "runtime missing" like a dead stdio command,
// and never stalls past the deadline for ITS row. Transport is resolved via
// mcp.Classify (call-site #9), not an inline Type/URL check.
func writeRuntimeCheck(ctx context.Context, out io.Writer, name string, server mcp.ManagedServer) error {
	serverType, _, classifyErr := mcp.Classify(server)
	if classifyErr != nil {
		return writef(out, "%s: runtime missing (%v)\n", name, classifyErr)
	}
	if serverType != mcp.ServerTypeStreamableHTTP && strings.TrimSpace(server.Command) == "" {
		return writef(out, "%s: runtime missing command\n", name)
	}
	target := server.Command
	if serverType == mcp.ServerTypeStreamableHTTP {
		target = server.URL
	}
	probeCtx, cancel := context.WithTimeout(ctx, resolveMCPProbeTimeout())
	defer cancel()
	res := probeManagedMCPServer(probeCtx, name, server)
	if !res.OK {
		return writef(out, "%s: runtime missing %s\n", name, target)
	}
	return writef(out, "%s: runtime ok (%s)\n", name, target)
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
