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

var governanceVisibleContainerRecipes = []string{"calendar", "whatsapp"}

// serve_governance.go wires the composition-root adapters for the Phase-28 read-only
// governance boards (GOV-01/02/03). The agui consumer declares the narrow board
// interfaces (governance_seam.go); these tiny adapters satisfy them over the existing
// daemon seams — the managed MCP config + structured probe, the skills active loader +
// per-stage reader + audit store, and the cron Store. Each is built best-effort by
// buildGovernanceProviders: a provider that cannot be constructed is left nil so its
// board answers 503, NEVER aborting daemon boot (the SetGraphView precedent).

// mcpBoardAdapter satisfies agui.MCPBoardProvider over the managed MCP config and the
// structured per-server probe. Servers re-derives the config on every read; Probe delegates
// to mcp.ProbeServer under the handler's bounded ctx so a hung/dead server fails only its
// own row.
//
// The config was once captured at boot, which was defensible while the board was read-only.
// It stopped being defensible when MCPW-01 gave the cockpit an Install button: the write
// lands in servers.json, the board kept rendering the boot snapshot, and the server the
// operator had just added was invisible until the daemon restarted — measured 2026-08-24
// with a real Slack install, present on disk and absent from the list. Every CLI path
// (mcp.go, mcp_profile.go, mcp_status.go) already re-loads per invocation; this is the
// board joining them.
type mcpBoardAdapter struct {
	// live answers "is this mounted right now". Nil under a composition with no running
	// registry, where the honest answer is "not mounted here".
	live *liveMCPMount
}

// Mounted reports the live registry's answer, not the config's.
func (a mcpBoardAdapter) Mounted(name string) bool { return a.live.Mounted(name) }

func (a mcpBoardAdapter) Servers() mcp.ManagedConfig {
	doc, err := governanceMCPBoardConfig()
	if err != nil {
		// An empty board, not a stale one. This used to fall back to a snapshot taken at
		// boot, and that fallback is the bug the operator hit on 2026-08-24: the board went
		// on listing servers the write path could no longer find, so Remove answered "mcp
		// server not found" about a row they were looking at on screen. A board that shows
		// nothing is a visible symptom; a board that disagrees with every write is not.
		slog.Error("aura serve: mcp board read failed", "err", err)
		return mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{}}
	}
	return doc
}

func (a mcpBoardAdapter) Probe(ctx context.Context, name string, server mcp.ManagedServer) mcp.ProbeResult {
	return probeManagedMCPServer(ctx, name, server)
}

// governanceMCPBoardConfig is the board's read, and it answers from exactly what the mount
// resolves: the registry plus the catalog recipes that are on by default.
//
// Sharing mcpRuntimeSet with the mount is the point. The board used to compose its own
// answer — the managed config, plus a copy of that same config taken at boot, plus its own
// separate injection of the default-on recipes — and three renderings of one fact can only
// ever disagree. On 2026-08-24 they did: the registry lost its server map, every write path
// correctly saw nothing, and the board kept listing servers from the boot copy.
//
// Policies, not just installed servers: a default-on recipe like memory has no registry row
// until someone customizes it, and leaving it off the board is how memory disappeared from
// the cockpit while mounting perfectly well at every boot.
func governanceMCPBoardConfig() (mcp.ManagedConfig, error) {
	doc, err := loadManagedMCPConfig()
	if err != nil {
		return mcp.ManagedConfig{}, err
	}
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]mcp.ManagedServer{}
	}
	_, policies, err := mcpRuntimeSet()
	if err != nil {
		return mcp.ManagedConfig{}, err
	}
	for name, server := range policies {
		if _, installed := doc.MCPServers[name]; !installed {
			doc.MCPServers[name] = server
		}
	}
	addContainerRecipeRows(&doc)
	return doc, nil
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
func buildGovernanceProviders(cfg *config.Config, pool *pgxpool.Pool, store agui.SchedulerBoardProvider, live *liveMCPMount) agui.GovernanceProviders {
	var providers agui.GovernanceProviders

	// The board reads per request, so there is nothing to capture here — and nothing to
	// go stale. It is wired unconditionally: a registry that is briefly unreachable is a
	// board that answers empty and recovers, not a route that answers 503 until restart.
	providers.MCP = mcpBoardAdapter{live: live}

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
