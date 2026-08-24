package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/jackc/pgx/v5/pgxpool"
)

const mcpUsage = "usage: aura mcp {recipes [--json]|install <recipe> [name]|add <name> [--env KEY=VALUE] [--disabled] [--trust local] -- <command> [args...]|profile ...|trust <name> --reason <text> [--class <class>]|list|doctor <name>|tools <name>|console [--addr host:port]|enable <name>|disable <name>|remove <name>|login <name>|logout <name>|authorizations}"

// mcpMutatingSubcommands is the set of top-level `aura mcp` CLI verbs that mutate the
// managed config (D-12): these route through the audited WriteConfigWithAudit and need a
// live *pgxpool.Pool. Every other subcommand (recipes/status/list/logs/doctor/tools/
// console) is read-only and stays pool-free.
var mcpMutatingSubcommands = map[string]bool{
	"add": true, "install": true, "trust": true,
	"enable": true, "disable": true, "remove": true,
}

// mcpOAuthSubcommands need a live *pgxpool.Pool for the per-identity grant store but do
// NOT touch the managed config, so they stay out of mcpMutatingSubcommands and away from
// WriteConfigWithAudit: what they change is one identity's own credentials, which the
// config audit trail has no business recording.
var mcpOAuthSubcommands = map[string]bool{
	"login": true, "logout": true, "authorizations": true,
}

// mcpProfileMutatingSubcommands mirrors the same split one level down, inside
// `aura mcp profile <verb>`: create/use/add/remove mutate; list stays read-only.
var mcpProfileMutatingSubcommands = map[string]bool{
	"create": true, "use": true, "add": true, "remove": true,
}

// mcpCommandNeedsPool reports whether args (the tokens after "aura mcp") name a mutating
// subcommand — main.go's dispatch must open a *pgxpool.Pool for it (D-12); a read-only
// subcommand stays pool-free.
func mcpCommandNeedsPool(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "profile" {
		return len(args) > 1 && mcpProfileMutatingSubcommands[args[1]]
	}
	return mcpMutatingSubcommands[args[0]] || mcpOAuthSubcommands[args[0]]
}

func runMCP(ctx context.Context, pool *pgxpool.Pool, args []string) {
	if err := runMCPCommand(ctx, pool, args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, mcpUsage)
		os.Exit(1)
	}
}

func runMCPCommand(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(mcpUsage)
	}
	switch args[0] {
	case "recipes":
		return mcpRecipes(args[1:], out)
	case "install":
		return mcpInstall(ctx, pool, args[1:], out)
	case "add":
		return mcpAdd(ctx, pool, args[1:], out)
	case "profile":
		return mcpProfile(ctx, pool, args[1:], out)
	case "trust":
		return mcpTrust(ctx, pool, args[1:], out)
	case "status":
		return mcpStatus(ctx, args[1:], out)
	case "logs":
		return mcpLogs(args[1:], out)
	case "list":
		return mcpList(out)
	case "doctor":
		return mcpDoctor(ctx, args[1:], out)
	case "tools":
		return mcpTools(ctx, args[1:], out)
	case "console":
		return mcpConsole(args[1:], out)
	case "enable":
		return mcpSetEnabled(ctx, pool, args[1:], true, out)
	case "disable":
		return mcpSetEnabled(ctx, pool, args[1:], false, out)
	case "remove":
		return mcpRemove(ctx, pool, args[1:], out)
	case "login":
		return mcpLogin(ctx, pool, args[1:], out)
	case "logout":
		return mcpLogout(ctx, pool, args[1:], out)
	case "authorizations":
		return mcpAuthorizations(ctx, pool, args[1:], out)
	default:
		return fmt.Errorf("unknown mcp subcommand %q", args[0])
	}
}

func mcpRecipes(args []string, out io.Writer) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--json") { //nolint:gosec // G602 false positive: args[0] guarded by len(args)==1
		return fmt.Errorf("usage: aura mcp recipes [--json]")
	}
	catalog := mcpmanager.BuiltInCatalog()
	if len(args) == 1 {
		data, err := json.Marshal(catalog)
		if err != nil {
			return fmt.Errorf("encode recipes: %w", err)
		}
		_, err = out.Write(append(data, '\n'))
		return err
	}
	if err := writef(out, "name\ttrust\truntime\tsource\tsummary\n"); err != nil {
		return err
	}
	for _, entry := range catalog {
		if err := writef(out, "%s\t%s\t%s\t%s\t%s\n", entry.Name, entry.TrustClass, entry.Runtime, entry.Source, entry.Summary); err != nil {
			return err
		}
	}
	return nil
}

func mcpInstall(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: aura mcp install <recipe> [name]")
	}
	recipeName := args[0]
	recipe, ok := mcpmanager.LookupCatalog(recipeName)
	if !ok {
		return fmt.Errorf("unknown MCP recipe %q", recipeName)
	}
	name := recipeName
	if len(args) == 2 {
		name = strings.TrimSpace(args[1])
	}
	doc, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if _, exists := doc.MCPServers[name]; exists {
		return fmt.Errorf("MCP server %q already exists", name)
	}
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]mcp.ManagedServer{}
	}
	doc.MCPServers[name] = recipe.Server
	ensureProfileMembership(&doc, doc.ActiveProfileName(), name)
	if err := mcpWriteManagedConfig(ctx, pool, doc, "install", name, ""); err != nil {
		return err
	}
	return writef(out, "ok: installed %s\n", name)
}

func mcpAdd(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	// Only guard against an empty arg vector (so args[0] below is safe). The real
	// invariant — a non-empty name AND a non-empty command after "--" — is enforced
	// precisely by the empty-name and len(commandParts)==0 checks below; a brittle
	// `len(args) < 3` pre-check implied a different, contradictory contract (WR-06).
	if len(args) == 0 {
		return fmt.Errorf("usage: aura mcp add <name> [--env KEY=VALUE] [--disabled] -- <command> [args...]")
	}
	name := strings.TrimSpace(args[0])
	if name == "" {
		return fmt.Errorf("MCP server name cannot be empty")
	}
	env := []string{}
	enabled := true
	trustClass := mcp.TrustBlocked
	pendingEnv := false
	pendingTrust := false
	inCommand := false
	commandParts := []string{}
	for _, arg := range args[1:] {
		if inCommand {
			commandParts = append(commandParts, arg)
			continue
		}
		if pendingEnv {
			if !strings.Contains(arg, "=") {
				return fmt.Errorf("--env value %q must be KEY=VALUE", arg)
			}
			env = append(env, arg)
			pendingEnv = false
			continue
		}
		if pendingTrust {
			if arg != "local" {
				return fmt.Errorf("--trust value %q must be local", arg)
			}
			trustClass = mcp.TrustTrustedLocal
			pendingTrust = false
			continue
		}
		switch arg {
		case "--":
			inCommand = true
		case "--env":
			pendingEnv = true
		case "--trust":
			pendingTrust = true
		case "--disabled":
			enabled = false
		default:
			return fmt.Errorf("unknown mcp add option %q", arg)
		}
	}
	if pendingEnv {
		return fmt.Errorf("--env requires KEY=VALUE")
	}
	if pendingTrust {
		return fmt.Errorf("--trust requires local")
	}
	if len(commandParts) == 0 {
		return fmt.Errorf("usage: aura mcp add <name> [--env KEY=VALUE] [--disabled] -- <command> [args...]")
	}
	command, commandArgs := splitCommandParts(commandParts)
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
	doc.MCPServers[name] = mcp.ManagedServer{
		Command: command,
		Args:    commandArgs,
		Env:     env,
		Enabled: new(enabled),
		Source:  "manual",
		Trust:   mcp.ManagedTrust{Class: trustClass},
	}
	ensureProfileMembership(&doc, doc.ActiveProfileName(), name)
	if err := mcpWriteManagedConfig(ctx, pool, doc, "add", name, ""); err != nil {
		return err
	}
	return writef(out, "ok: added %s\n", name)
}

func mcpList(out io.Writer) error {
	doc, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if len(doc.MCPServers) == 0 {
		return writeln(out, "no MCP servers configured")
	}
	names := sortedManagedNames(doc)
	for _, name := range names {
		s := doc.MCPServers[name]
		status := "enabled"
		if s.Enabled != nil && !*s.Enabled {
			status = "disabled"
		}
		if err := writef(out, "%s\t%s\t%s\n", name, status, renderMCPCommand(mcp.ServerConfig{Command: s.Command, Args: s.Args})); err != nil {
			return err
		}
	}
	return nil
}

func mcpDoctor(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 1 && args[0] == "--all" { //nolint:gosec // G602 false positive: args[0] guarded by len(args)==1
		return mcpDoctorAll(ctx, out)
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp doctor <name>|--all")
	}
	name := args[0]
	doc, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if _, ok := doc.MCPServers[name]; ok && doc.NormalizedTrust(name) == mcp.TrustBlocked {
		return writef(out, "%s blocked: trust approval required before launch\n", name)
	}
	if server, ok, err := effectiveManagedMCPServer(name); err != nil {
		return err
	} else if ok {
		cli, defs, err := openAndListManagedMCPTools(ctx, name, server)
		if err != nil {
			return err
		}
		defer func() { _ = cli.Close() }()
		if err := writef(out, "ok: %s started; %s\n", name, toolCount(len(defs))); err != nil {
			return err
		}
		if name == "whatsapp" {
			cfg := mcp.ServerConfig{}
			if strings.TrimSpace(server.Type) != mcp.ServerTypeStreamableHTTP && strings.TrimSpace(server.URL) == "" {
				var err error
				cfg, err = mcpmanager.RuntimeLaunchConfig(name, server)
				if err != nil {
					return err
				}
			}
			return writeWhatsAppBridgeHealth(ctx, out, cfg)
		}
		return nil
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
	if err := writef(out, "ok: %s started; %s\n", name, toolCount(len(defs))); err != nil {
		return err
	}
	if name == "whatsapp" {
		return writeWhatsAppBridgeHealth(ctx, out, cfg)
	}
	return nil
}

func mcpSetEnabled(ctx context.Context, pool *pgxpool.Pool, args []string, enabled bool, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp enable|disable <name>")
	}
	name := args[0]
	doc, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	s, ok := doc.MCPServers[name]
	if !ok {
		return fmt.Errorf("MCP server %q not found in managed config", name)
	}
	s.Enabled = new(enabled)
	doc.MCPServers[name] = s
	action := "disable"
	state := "disabled"
	if enabled {
		action = "enable"
		state = "enabled"
	}
	if err := mcpWriteManagedConfig(ctx, pool, doc, action, name, ""); err != nil {
		return err
	}
	return writef(out, "ok: %s %s\n", state, name)
}

func mcpRemove(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp remove <name>")
	}
	name := args[0]
	doc, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if _, ok := doc.MCPServers[name]; !ok {
		return fmt.Errorf("MCP server %q not found in managed config", name)
	}
	delete(doc.MCPServers, name)
	if err := mcpWriteManagedConfig(ctx, pool, doc, "remove", name, ""); err != nil {
		return err
	}
	return writef(out, "ok: removed %s\n", name)
}

func writeWhatsAppBridgeHealth(ctx context.Context, out io.Writer, cfg mcp.ServerConfig) error {
	status := probeWhatsAppBridge(ctx, cfg)
	return writef(out, "whatsapp bridge: %s\n", status)
}

func probeWhatsAppBridge(ctx context.Context, cfg mcp.ServerConfig) string {
	baseURL, overridden := whatsAppBridgeBaseURL()
	if !overridden && isWSLCommand(cfg.Command) {
		status, err := runWhatsAppBridgeWSLProbe(ctx, cfg)
		if err != nil {
			return fmt.Sprintf("REST :8080 in WSL unreachable (%v)", err)
		}
		if status == http.StatusMethodNotAllowed {
			return "REST :8080 in WSL reachable (GET /api/send -> 405)"
		}
		return fmt.Sprintf("REST :8080 in WSL unexpected status (GET /api/send -> %d)", status)
	}
	return probeWhatsAppBridgeHTTP(ctx, baseURL)
}

func whatsAppBridgeBaseURL() (string, bool) {
	if v := strings.TrimSpace(os.Getenv("AURA_MCP_WHATSAPP_BRIDGE_URL")); v != "" {
		return strings.TrimRight(v, "/"), true
	}
	return mcpmanager.WhatsAppBridgeBaseURL(), false
}

func probeWhatsAppBridgeHTTP(ctx context.Context, baseURL string) string {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := strings.TrimRight(baseURL, "/") + "/api/status"
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil) //nolint:gosec // operator-owned doctor URL; default is local loopback
	if err != nil {
		return fmt.Sprintf("REST %s invalid (%v)", baseURL, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("REST %s unreachable (%v)", baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("REST %s unexpected status (GET /api/status -> %d)", baseURL, resp.StatusCode)
	}
	var body struct {
		State       string `json:"state"`
		Paired      bool   `json:"paired"`
		QRAvailable bool   `json:"qr_available"`
		JID         string `json:"jid"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&body); err != nil {
		return fmt.Sprintf("REST %s reachable but status payload invalid (%v)", baseURL, err)
	}
	state := strings.TrimSpace(body.State)
	if state == "" {
		state = "unknown"
	}
	parts := []string{
		"state=" + state,
		fmt.Sprintf("paired=%t", body.Paired),
		fmt.Sprintf("qr_available=%t", body.QRAvailable),
	}
	if body.JID != "" {
		parts = append(parts, "jid="+body.JID)
	}
	return fmt.Sprintf("REST %s reachable (GET /api/status -> %s)", baseURL, strings.Join(parts, ", "))
}

var runWhatsAppBridgeWSLProbe = func(ctx context.Context, cfg mcp.ServerConfig) (int, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	args := append(wslProbePrefixArgs(cfg.Args), "-e", "bash", "-lc", wslHTTPProbeScript)
	cmd := exec.CommandContext(probeCtx, cfg.Command, args...) //nolint:gosec // operator-owned MCP config; doctor reuses the configured WSL executable.
	out, err := cmd.CombinedOutput()
	if err != nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return 0, fmt.Errorf("%w: %s", err, s)
		}
		return 0, err
	}
	return parseHTTPStatusLine(string(out))
}

const wslHTTPProbeScript = `exec 3<>/dev/tcp/127.0.0.1/8080 || exit 111
printf 'GET /api/send HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n' >&3
IFS=$'\r' read -r line <&3
printf '%s\n' "$line"`

func isWSLCommand(command string) bool {
	base := path.Base(strings.ReplaceAll(strings.TrimSpace(command), "\\", "/"))
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	return base == "wsl"
}

func wslProbePrefixArgs(args []string) []string {
	for i, arg := range args {
		if arg == "-e" || arg == "--exec" {
			return append([]string(nil), args[:i]...)
		}
	}
	return nil
}

func parseHTTPStatusLine(raw string) (int, error) {
	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return 0, fmt.Errorf("missing HTTP status line in %q", strings.TrimSpace(raw))
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("decode HTTP status line %q: %w", strings.TrimSpace(raw), err)
	}
	return status, nil
}

func sortedManagedNames(doc mcp.ManagedConfig) []string {
	names := make([]string, 0, len(doc.MCPServers))
	for name := range doc.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func renderMCPCommand(cfg mcp.ServerConfig) string {
	parts := append([]string{cfg.Command}, cfg.Args...)
	return strings.Join(parts, " ")
}

func splitCommandParts(parts []string) (string, []string) {
	command := ""
	args := []string{}
	for i, part := range parts {
		if i == 0 {
			command = part
			continue
		}
		args = append(args, part)
	}
	return command, args
}

func writef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

func writeln(w io.Writer, args ...any) error {
	_, err := fmt.Fprintln(w, args...)
	return err
}
