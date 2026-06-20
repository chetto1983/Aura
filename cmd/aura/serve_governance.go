package main

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	return mcp.ProbeServer(ctx, name, server)
}

// skillsBoardAdapter satisfies agui.SkillsBoardProvider over the active Loader, the Wave-0
// per-stage reader, and the audit store. ActiveSkills lists the loaded active skills;
// StageSkills reads pending/archived metadata (never a body, GOV-02 prohibition #1);
// AuditLog reads the append-only ledger newest-first.
type skillsBoardAdapter struct {
	loader     *skills.Loader
	audit      *skills.AuditStore
	pendingDir string
	archiveDir string
}

func (a skillsBoardAdapter) ActiveSkills() []skills.Skill { return a.loader.List() }

func (a skillsBoardAdapter) StageSkills(stage string) ([]skills.StageSkill, error) {
	return skills.ListStage(a.pendingDir, a.archiveDir, stage)
}

func (a skillsBoardAdapter) AuditLog(ctx context.Context, filter skills.AuditFilter) ([]skills.AuditRow, error) {
	return a.audit.List(ctx, filter)
}

// buildGovernanceProviders assembles the three read-only board providers best-effort over
// the daemon's existing seams. The MCP board reads the managed config (a load failure
// leaves the MCP board nil → 503, never fatal); the skills board reuses the SAME loader
// roots + pending/archived dirs the CLI and model path use (newSkillWriter), with the
// audit store over the shared pool; the scheduler board is the cron Store directly (it
// already satisfies the interface). A nil pool leaves the pool-backed boards unset. None
// of these aborts boot — a governance-board outage is not a daemon outage.
func buildGovernanceProviders(cfg *config.Config, pool *pgxpool.Pool, store agui.SchedulerBoardProvider) agui.GovernanceProviders {
	var providers agui.GovernanceProviders

	if doc, _, err := loadManagedMCPConfig(); err != nil {
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
			pendingDir: filepath.Join(cfg.SkillsDir, "pending"),
			archiveDir: filepath.Join(cfg.SkillsDir, "archived"),
		}
	}

	if store != nil {
		providers.Scheduler = store
	}

	return providers
}
