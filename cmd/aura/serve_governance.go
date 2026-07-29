package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/jackc/pgx/v5/pgxpool"
)

var governanceVisibleContainerRecipes = []string{"calculator", "calendar", "whatsapp"}

// serve_governance.go wires the composition-root adapters for the Phase-28 read-only
// governance boards (GOV-01/02/03). The agui consumer declares the narrow board
// interfaces (governance_seam.go); these tiny adapters satisfy them over the existing
// daemon seams — the managed MCP config + structured probe, the skills active loader +
// per-stage reader + audit store, and the cron Store. Each is built best-effort by
// buildGovernanceProviders: a provider that cannot be constructed is left nil so its
// board answers 503, NEVER aborting daemon boot (the SetGraphView precedent).

// mcpBoardAdapter satisfies agui.MCPBoardProvider over the loaded managed MCP config and
// the structured per-server probe. Servers returns the snapshot config the handler renders
// (static, by-name); Probe delegates to mcp.ProbeServer under the handler's bounded ctx so
// a hung/dead server fails only its own row. The config is captured once at boot (the
// board reflects the config the daemon started with — a config reload is out of scope for
// the read board).
type mcpBoardAdapter struct {
	doc mcp.ManagedConfig
}

func (a mcpBoardAdapter) Servers() mcp.ManagedConfig { return a.doc }

func (a mcpBoardAdapter) Probe(ctx context.Context, name string, server mcp.ManagedServer) mcp.ProbeResult {
	return probeManagedMCPServer(ctx, name, server)
}

func governanceMCPBoardConfig(cfg *config.Config) (mcp.ManagedConfig, error) {
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return mcp.ManagedConfig{}, err
	}
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]mcp.ManagedServer{}
	}
	if cfg != nil {
		addMCPPolicyRows(&doc, cfg.MCPPolicies)
		addLegacyMCPRows(&doc, cfg.MCPServers)
	}
	addContainerRecipeRows(&doc)
	return doc, nil
}

func addMCPPolicyRows(doc *mcp.ManagedConfig, policies map[string]mcp.ManagedServer) {
	for name, server := range policies {
		if _, exists := doc.MCPServers[name]; exists {
			continue
		}
		doc.MCPServers[name] = server
	}
}

func addLegacyMCPRows(doc *mcp.ManagedConfig, servers map[string]mcp.ServerConfig) {
	for name, server := range servers {
		if _, exists := doc.MCPServers[name]; exists {
			continue
		}
		doc.MCPServers[name] = mcp.ManagedServer{
			Command: server.Command,
			Args:    server.Args,
			Env:     server.Env,
			Source:  "env:AURA_MCP_SERVERS_JSON",
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
		}
	}
}

func addContainerRecipeRows(doc *mcp.ManagedConfig) {
	if os.Getenv("AURA_IN_CONTAINER") != "1" {
		return
	}
	for _, name := range governanceVisibleContainerRecipes {
		if _, exists := doc.MCPServers[name]; exists {
			continue
		}
		recipe, ok := mcpmanager.LookupCatalog(name)
		if !ok {
			continue
		}
		doc.MCPServers[name] = recipe.Server
	}
}

// skillsBoardAdapter satisfies agui.SkillsBoardProvider over the active Loader, the
// archived-stage reader, and the audit store. ActiveSkills lists the loaded active skills;
// ArchivedSkills reads archived metadata (never a body, GOV-02 prohibition #1);
// AuditLog reads the append-only ledger newest-first.
type skillsBoardAdapter struct {
	loader     *skills.Loader
	audit      *skills.AuditStore
	archiveDir string
}

func (a skillsBoardAdapter) ActiveSkills() []skills.Skill { return a.loader.List() }

// SkillBody resolves a skill body from the SAME loader snapshot ActiveSkills lists (37D /
// WEBSKILL-02): one source of truth for the composer pinned-skill application. loader.Get
// re-reads the cached snapshot List reads, so every listed name resolves (list ⊆ resolvable,
// Pitfall 2); an unknown name → ("", false). The name is a snapshot KEY, never a path.
func (a skillsBoardAdapter) SkillBody(name string) (string, bool) {
	sk, ok := a.loader.Get(name)
	return sk.Body, ok
}

func (a skillsBoardAdapter) ArchivedSkills() ([]skills.StageSkill, error) {
	return skills.ListArchived(a.archiveDir)
}

func (a skillsBoardAdapter) AuditLog(ctx context.Context, filter skills.AuditFilter) ([]skills.AuditRow, error) {
	return a.audit.List(ctx, filter)
}

// buildGovernanceProviders assembles the three read-only board providers best-effort over
// the daemon's existing seams. The MCP board reads the managed config (a load failure
// leaves the MCP board nil → 503, never fatal); the skills board reuses the SAME loader
// roots + archive dir the CLI and model path use (newSkillWriter), with the
// audit store over the shared pool; the scheduler board is the cron Store directly (it
// already satisfies the interface). A nil pool leaves the pool-backed boards unset. None
// of these aborts boot — a governance-board outage is not a daemon outage.
func buildGovernanceProviders(cfg *config.Config, pool *pgxpool.Pool, store agui.SchedulerBoardProvider) agui.GovernanceProviders {
	var providers agui.GovernanceProviders

	if doc, err := governanceMCPBoardConfig(cfg); err != nil {
		// A managed-config load failure leaves the MCP board nil (503), never fatal —
		// mirrors the SetGraphView best-effort warn.
		slog.Warn("aura serve: governance mcp board unavailable", "err", err)
	} else {
		providers.MCP = mcpBoardAdapter{doc: doc}
	}

	if pool != nil {
		providers.Skills = skillsBoardAdapter{
			loader:     skills.NewLoader(skills.Config{Roots: skillLoaderRoots(cfg), BodyCapBytes: cfg.SkillBodyCapBytes, Blocklist: cfg.SkillInjectionBlocklist}),
			audit:      skills.NewAuditStore(pool),
			archiveDir: filepath.Join(cfg.SkillsDir, "archived"),
		}
	}

	if store != nil {
		providers.Scheduler = store
	}

	return providers
}
