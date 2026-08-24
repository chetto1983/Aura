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
	// boot is the config read at start-up. It is the fallback, not the answer: a reload
	// that fails must not blank a board that was rendering a moment ago.
	boot mcp.ManagedConfig
}

func (a mcpBoardAdapter) Servers() mcp.ManagedConfig {
	doc, err := governanceMCPBoardConfig()
	if err != nil {
		slog.Warn("aura serve: mcp board reload failed, serving the boot config", "err", err)
		return a.boot
	}
	return doc
}

func (a mcpBoardAdapter) Probe(ctx context.Context, name string, server mcp.ManagedServer) mcp.ProbeResult {
	return probeManagedMCPServer(ctx, name, server)
}

// governanceMCPBoardConfig is the board's read, and it reads ONE thing: the managed config,
// re-read per call, plus the catalog recipes that are declared in code rather than stored.
//
// Two overlays used to sit here and both are gone, because neither was an overlay. Each
// layered a copy of the SAME managed config — taken once at boot by
// internal/config/config_mcp.go — back on top of the file the board had just re-read. A
// stale copy of your own source is not extra information, it is a cache that can only ever
// disagree, and on 2026-08-24 it disagreed in the worst direction: the file lost its server
// map, every write path correctly saw nothing, and the board went on listing servers from
// the boot copy — so the operator was told "mcp server not found" about a row they were
// looking at on screen.
func governanceMCPBoardConfig() (mcp.ManagedConfig, error) {
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return mcp.ManagedConfig{}, err
	}
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]mcp.ManagedServer{}
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
func buildGovernanceProviders(cfg *config.Config, pool *pgxpool.Pool, store agui.SchedulerBoardProvider) agui.GovernanceProviders {
	var providers agui.GovernanceProviders

	if doc, err := governanceMCPBoardConfig(); err != nil {
		// A managed-config load failure leaves the MCP board nil (503), never fatal —
		// mirrors the SetGraphView best-effort warn.
		slog.Warn("aura serve: governance mcp board unavailable", "err", err)
	} else {
		providers.MCP = mcpBoardAdapter{boot: doc}
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
