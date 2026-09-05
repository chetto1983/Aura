package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/mcp/mcpenv"
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

// mcpWriteAdapter satisfies agui.MCPWriteProvider over the Postgres registry + the shared
// pool. It holds no path and caches no document: every method reloads the registry, so a
// mutation is always applied over the freshest servers.
type mcpWriteAdapter struct {
	pool *pgxpool.Pool
	// live mounts and unmounts against the running registry, so an install is usable and
	// a remove stops being offered without a restart. Nil under `aura chat`, where there
	// is no long-lived registry to keep in step.
	live *liveMCPMount
	// prep materialises a stdio server's environment at install (#211). Nil is a
	// passthrough, which keeps a deployment with no configured root working exactly as
	// before rather than failing every install.
	prep *mcpenv.Preparer
}

// mcpRegistryDestination is what the install preview names as the place the server lands.
// It used to be a filesystem path, which was only ever meaningful to somebody with a shell
// inside the container; the registry is a table, and saying so is both shorter and true.
const mcpRegistryDestination = "postgres: aura.mcp_server"

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
	// Amendment #211: prepare and verify BEFORE the save. This handler used to save first and
	// probe after, fail-soft, so a stdio server that could not start was stored and merely
	// annotated — the operator read "installed" and got a row that would never mount.
	// The preparation report is not projected: the panel closes on success, so the only thing
	// an operator needs from here is the REFUSAL, which travels as the 502's reason. The CLI,
	// which stays open, prints it.
	server, _, err = prepareAndVerify(ctx, a.prep, name, server)
	if err != nil {
		return agui.MCPWriteResult{}, err
	}
	doc.MCPServers[name] = server
	joinActiveProfile(&doc, name)

	if err := a.save(ctx, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: "install", ServerName: name,
	}); err != nil {
		return agui.MCPWriteResult{}, err
	}

	a.live.Mount(ctx, name, server)
	probe := a.probe(ctx, name, server)
	return agui.MCPWriteResult{
		Name:          name,
		Server:        server,
		CLIEquivalent: cliEquiv,
		Destination:   mcpRegistryDestination,
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
	if err := a.save(ctx, doc, mcpmanager.MCPAuditInsert{
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
	class, reason, err = validateTrustClassReason(class, reason)
	if err != nil {
		return agui.MCPWriteResult{}, err
	}
	// Operator-direct trust-approve (D-12): populate the today-empty ApprovedBy/At/Reason.
	server.Trust = mcp.ManagedTrust{
		Class:      class,
		ApprovedBy: actor,
		ApprovedAt: time.Now().UTC().Format(time.RFC3339),
		Reason:     reason,
	}
	doc.MCPServers[name] = server

	if err := a.save(ctx, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: "trust", ServerName: name, Reason: reason,
	}); err != nil {
		return agui.MCPWriteResult{}, err
	}
	return agui.MCPWriteResult{Name: name, Server: server, Probe: a.probe(ctx, name, server)}, nil
}

// validateTrustClassReason is the single source of truth for MCPH-03/D-13's trust-approve
// validation: a known trust class AND a non-empty reason are both required. Both TrustApprove
// (the web + operator-direct path) and the CLI `aura mcp trust` (mcp_profile.go) call this SAME
// helper, so the CLI can never reintroduce F-038 by reaching a silent empty-class default
// (Pitfall #5) — there is no other route to mutate Trust. It returns the trimmed class/reason.
func validateTrustClassReason(class, reason string) (string, string, error) {
	class = strings.TrimSpace(class)
	reason = strings.TrimSpace(reason)
	if class == "" || reason == "" || !mcp.IsKnownTrust(class) {
		return "", "", fmt.Errorf("mcp trust: a known trust class and a non-empty reason are required")
	}
	return class, reason, nil
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
	if err := a.save(ctx, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: action, ServerName: name,
	}); err != nil {
		return agui.MCPWriteResult{}, err
	}
	if enabled {
		a.live.Mount(ctx, name, server)
	} else {
		a.live.Unmount(name)
	}
	return agui.MCPWriteResult{Name: name, Server: server}, nil
}

// joinActiveProfile puts a freshly installed server into the profile that is actually in
// force. Without it install was the only asymmetric mutation on this adapter: RemoveServer
// takes a name OUT of every profile, but nothing ever put one IN, so a server installed from
// the cockpit was written to the registry, listed on the board — and absent from
// ProfileServerNames, which is what decides the runnable set. It could not be reached by the
// agent and its authorization endpoint answered "not configured or is disabled", with the
// board showing it installed the whole time. Measured on the live stack, 2026-08-24.
func joinActiveProfile(doc *mcp.ManagedConfig, name string) {
	profile := doc.ActiveProfileName()
	if doc.Profiles == nil {
		doc.Profiles = map[string]mcp.ManagedProfile{}
	}
	cfg := doc.Profiles[profile]
	if slices.Contains(cfg.Servers, name) {
		return
	}
	cfg.Servers = append(cfg.Servers, name)
	doc.Profiles[profile] = cfg
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
	if err := a.save(ctx, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: "remove", ServerName: name,
	}); err != nil {
		return err
	}
	// A removed server must stop being offered to the model now, not at the next restart.
	// The operator reported the mirror of this bug on 2026-08-24: Remove reported success
	// and the row stayed on screen.
	a.live.Unmount(name)
	return nil
}

// save applies doc to the registry and records its audit row in one transaction, so a
// governance mutation is never applied without its ledger entry (MCPH-07).
func (a mcpWriteAdapter) save(ctx context.Context, doc mcp.ManagedConfig, in mcpmanager.MCPAuditInsert) error {
	return saveManagedMCPConfig(ctx, a.pool, doc, in)
}

// load reads the registry per call (never a cached doc) so each mutation is applied over
// the freshest config — a concurrent CLI/operator edit is not clobbered.
func (a mcpWriteAdapter) load() (mcp.ManagedConfig, error) {
	doc, err := loadManagedMCPConfig()
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
	res := probeManagedMCPServer(pctx, name, server)
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
		// No trust class: Classify resolves one from the transport. Installing IS the
		// authorization — this route is operator-authenticated and capability-gated
		// (governance.write), so the human who reached it already made the decision a
		// trust-approve would have asked for a second time.
	}
	cli := "aura mcp add " + req.Name
	if command != "" {
		// The arguments belong in the preview: without them it names a command the CLI would
		// not run, and for a resolver launch they carry the package itself.
		cli += " -- " + strings.Join(append([]string{command}, req.Args...), " ")
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
// (no DB) leaves it nil → the routes answer 503. Never aborts boot (the
// SetGovernanceProviders precedent).
func buildMCPWriteProvider(pool *pgxpool.Pool, live *liveMCPMount, prep *mcpenv.Preparer) (agui.MCPWriteProvider, error) {
	if pool == nil {
		return nil, nil
	}
	return mcpWriteAdapter{pool: pool, live: live, prep: prep}, nil
}
