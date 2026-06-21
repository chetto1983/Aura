package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/jackc/pgx/v5/pgxpool"
)

// serve_governance_write.go wires the composition-root concrete adapter for the Phase-29
// MCP WRITE surface (MCPW-01/02/03). The agui consumer declares the narrow MCPWriteProvider
// seam (governance_write_seam.go); mcpWriteAdapter satisfies it over the existing daemon
// primitives — LoadManagedConfig + the catalog + manager.SetServerEnv + the plan-01/task-1
// atomic WriteConfigWithAudit (temp→tx→rename + one mcp_audit row) + mcp.ProbeServer.
//
// Each method reloads the managed config from disk first (never a cached doc), so a
// concurrent CLI/operator edit is not clobbered, then writes atomically with its audit row.
// A nil pool (no DB) leaves the provider unset → the routes answer 503. Boot is best-effort
// (the SetGovernanceProviders precedent): a wiring failure never aborts daemon boot.

// mcpWriteAdapter satisfies agui.MCPWriteProvider over the managed-config path + the shared
// pool. path is captured once at boot (the managed servers.json destination); the adapter
// reloads its contents per call so each mutation is applied over the freshest config.
type mcpWriteAdapter struct {
	pool *pgxpool.Pool
	path string
}

// mcpProbeTimeout bounds the post-write live tool-count probe (parity with the read board's
// 3s per-row deadline). A hung/dead server fails only its own probe, fail-soft.
const mcpProbeTimeout = 3 * time.Second

func (a mcpWriteAdapter) InstallServer(ctx context.Context, actor string, req agui.MCPInstallRequest) (agui.MCPWriteResult, error) {
	doc, err := a.load()
	if err != nil {
		return agui.MCPWriteResult{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return agui.MCPWriteResult{}, fmt.Errorf("mcp install: server name is required")
	}
	if _, exists := doc.MCPServers[name]; exists {
		return agui.MCPWriteResult{}, agui.ErrMCPServerExists
	}

	server, cliEquiv, err := buildInstallServer(req)
	if err != nil {
		return agui.MCPWriteResult{}, err
	}
	doc.MCPServers[name] = server

	if err := mcpmanager.WriteConfigWithAudit(ctx, a.pool, a.path, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: "install", ServerName: name,
	}); err != nil {
		return agui.MCPWriteResult{}, err
	}

	probe := a.probe(ctx, name, server)
	return agui.MCPWriteResult{
		Name:          name,
		Server:        server,
		CLIEquivalent: cliEquiv,
		Destination:   a.path,
		Warnings:      placeholderWarnings(req.Recipe, server.Env),
		Probe:         probe,
	}, nil
}

func (a mcpWriteAdapter) SetServerEnv(ctx context.Context, actor, name string, submitted []string) (agui.MCPWriteResult, error) {
	doc, err := a.load()
	if err != nil {
		return agui.MCPWriteResult{}, err
	}
	if err := mcpmanager.SetServerEnv(&doc, name, submitted); err != nil {
		return agui.MCPWriteResult{}, mapManagerErr(err)
	}
	if err := mcpmanager.WriteConfigWithAudit(ctx, a.pool, a.path, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: "edit", ServerName: name,
	}); err != nil {
		return agui.MCPWriteResult{}, err
	}
	server := doc.MCPServers[name]
	return agui.MCPWriteResult{
		Name:     name,
		Server:   server,
		Warnings: placeholderWarnings(recipeOf(server), server.Env),
	}, nil
}

func (a mcpWriteAdapter) TrustApprove(ctx context.Context, actor, name, class, reason string) (agui.MCPWriteResult, error) {
	doc, err := a.load()
	if err != nil {
		return agui.MCPWriteResult{}, err
	}
	server, ok := doc.MCPServers[name]
	if !ok {
		return agui.MCPWriteResult{}, agui.ErrMCPServerNotFound
	}
	class = strings.TrimSpace(class)
	if class == "" {
		class = mcp.TrustTrustedLocal
	}
	if !mcp.IsKnownTrust(class) {
		return agui.MCPWriteResult{}, fmt.Errorf("mcp trust: unknown trust class %q", class)
	}
	// Operator-direct trust-approve (D-12): populate the today-empty ApprovedBy/At/Reason.
	server.Trust = mcp.ManagedTrust{
		Class:      class,
		ApprovedBy: actor,
		ApprovedAt: time.Now().UTC().Format(time.RFC3339),
		Reason:     reason,
	}
	doc.MCPServers[name] = server

	if err := mcpmanager.WriteConfigWithAudit(ctx, a.pool, a.path, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: "trust", ServerName: name, Reason: reason,
	}); err != nil {
		return agui.MCPWriteResult{}, err
	}
	return agui.MCPWriteResult{Name: name, Server: server, Probe: a.probe(ctx, name, server)}, nil
}

func (a mcpWriteAdapter) SetEnabled(ctx context.Context, actor, name string, enabled bool) (agui.MCPWriteResult, error) {
	doc, err := a.load()
	if err != nil {
		return agui.MCPWriteResult{}, err
	}
	server, ok := doc.MCPServers[name]
	if !ok {
		return agui.MCPWriteResult{}, agui.ErrMCPServerNotFound
	}
	enabledCopy := enabled
	server.Enabled = &enabledCopy
	doc.MCPServers[name] = server
	action := "disable"
	if enabled {
		action = "enable"
	}
	if err := mcpmanager.WriteConfigWithAudit(ctx, a.pool, a.path, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: action, ServerName: name,
	}); err != nil {
		return agui.MCPWriteResult{}, err
	}
	return agui.MCPWriteResult{Name: name, Server: server}, nil
}

func (a mcpWriteAdapter) RemoveServer(ctx context.Context, actor, name string) error {
	doc, err := a.load()
	if err != nil {
		return err
	}
	if _, ok := doc.MCPServers[name]; !ok {
		return agui.ErrMCPServerNotFound
	}
	delete(doc.MCPServers, name)
	for profile, cfg := range doc.Profiles {
		cfg.Servers = removeString(cfg.Servers, name)
		doc.Profiles[profile] = cfg
	}
	return mcpmanager.WriteConfigWithAudit(ctx, a.pool, a.path, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: "remove", ServerName: name,
	})
}

// load reads the managed config from disk per call (never a cached doc) so each mutation is
// applied over the freshest config — a concurrent CLI/operator edit is not clobbered.
func (a mcpWriteAdapter) load() (mcp.ManagedConfig, error) {
	doc, err := mcp.LoadManagedConfig(a.path)
	if err != nil {
		return mcp.ManagedConfig{}, fmt.Errorf("load managed MCP config: %w", err)
	}
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]mcp.ManagedServer{}
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]mcp.ManagedProfile{}
	}
	return doc, nil
}

// probe runs the bounded post-write live tool-count probe (fail-soft, per-row, 3s). A
// hung/dead server yields OK=false for its own result only, never blocking the response.
func (a mcpWriteAdapter) probe(ctx context.Context, name string, server mcp.ManagedServer) *mcp.ProbeResult {
	pctx, cancel := context.WithTimeout(ctx, mcpProbeTimeout)
	defer cancel()
	res := mcp.ProbeServer(pctx, name, server)
	return &res
}

// buildInstallServer resolves the install request into a ManagedServer + its CLI-equivalent
// preview. A recipe name resolves against the built-in catalog (its launch spec + trust);
// otherwise a custom stdio (command) or HTTP (url) server is built. Operator-supplied env is
// merged onto the recipe/custom base.
func buildInstallServer(req agui.MCPInstallRequest) (mcp.ManagedServer, string, error) {
	recipe := strings.TrimSpace(req.Recipe)
	if recipe != "" {
		entry, ok := mcpmanager.LookupCatalog(recipe)
		if !ok {
			return mcp.ManagedServer{}, "", fmt.Errorf("mcp install: unknown recipe %q", recipe)
		}
		server := entry.Server
		if len(req.Env) > 0 {
			server.Env = append(append([]string{}, server.Env...), req.Env...)
		}
		return server, "aura mcp install " + recipe, nil
	}

	url := strings.TrimSpace(req.URL)
	command := strings.TrimSpace(req.Command)
	if url == "" && command == "" {
		return mcp.ManagedServer{}, "", fmt.Errorf("mcp install: a custom server needs a command (stdio) or url (http)")
	}
	server := mcp.ManagedServer{
		Command: command,
		Args:    req.Args,
		Env:     req.Env,
		URL:     url,
		Type:    strings.TrimSpace(req.Type),
		Source:  "custom",
		// A custom server defaults to blocked until an explicit operator trust-approve
		// (parity with the CLI mcpAdd default; T-29-02-03 no model self-trust).
		Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked},
	}
	cli := "aura mcp add " + req.Name
	if command != "" {
		cli += " -- " + command
	}
	return server, cli, nil
}

// placeholderWarnings returns the soft warning list for a recipe's required env vars still
// holding a placeholder/empty value (MCPW-02 soft warning — informational, the save still
// succeeded). A non-recipe server (no RequiredEnv source) yields no warnings.
func placeholderWarnings(recipe string, env []string) []string {
	recipe = strings.TrimSpace(recipe)
	if recipe == "" {
		return nil
	}
	entry, ok := mcpmanager.LookupCatalog(recipe)
	if !ok || len(entry.RequiredEnv) == 0 {
		return nil
	}
	have := map[string]string{}
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			have[k] = v
		}
	}
	var warnings []string
	for _, key := range entry.RequiredEnv {
		v, present := have[key]
		if !present || v == "" || v == "${"+key+"}" {
			warnings = append(warnings, fmt.Sprintf("required env %q is still unset — the server stays blocked until it is filled", key))
		}
	}
	return warnings
}

// recipeOf returns the recipe name a server was installed from (its "recipe:<name>" source),
// or "" for a custom server — so an env edit can re-derive the RequiredEnv soft warnings.
func recipeOf(server mcp.ManagedServer) string {
	if rest, ok := strings.CutPrefix(strings.TrimSpace(server.Source), "recipe:"); ok {
		return rest
	}
	return ""
}

// removeString returns in with the first occurrence of v removed (profile membership
// cleanup on remove).
func removeString(in []string, v string) []string {
	out := in[:0:0]
	for _, s := range in {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

// mapManagerErr maps the manager-package sentinels onto the agui write sentinels so the
// handler classifies a missing server as 404 without importing the manager package.
func mapManagerErr(err error) error {
	if errors.Is(err, mcpmanager.ErrServerNotFound) {
		return agui.ErrMCPServerNotFound
	}
	return err
}

// buildMCPWriteProvider constructs the concrete MCP write provider best-effort: a nil pool
// (no DB) or an unresolvable managed-config path leaves it nil → the routes answer 503.
// Never aborts boot (the SetGovernanceProviders precedent).
func buildMCPWriteProvider(pool *pgxpool.Pool) (agui.MCPWriteProvider, error) {
	if pool == nil {
		return nil, nil
	}
	path, err := mcp.ManagedConfigPath()
	if err != nil {
		return nil, err
	}
	return mcpWriteAdapter{pool: pool, path: path}, nil
}
