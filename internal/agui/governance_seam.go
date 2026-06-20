package agui

import (
	"context"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/skills"
)

// governance_seam.go declares the consumer-side narrow provider interfaces for the
// Phase-28 governance boards (GOV-01/02/03) and the onboarding wizard (ONBD-01/02),
// plus their off-constructor injection setters (D-A2-02). The interfaces are declared
// HERE (the consumer) so a handler depends only on the methods it calls, never a whole
// store; the daemon composition root wires concrete providers via the setters after
// NewServer. A Server with a provider unwired answers that provider's routes 503 — the
// handlers (Plan 02 / Plan 05) nil-check the field, exactly as handleGraphSchema does.
//
// This plan (Wave 0) adds ONLY the seams: the fields, the interfaces, and the setters.
// The route registrations (registerGovernanceRoutes / registerOnboardingRoutes) and the
// handlers land in Plan 02 / Plan 05, so existing NewServer callers and tests are
// unchanged and the new routes do not yet exist on the mux.

// MCPBoardProvider is the read-only MCP-governance surface (GOV-01): the static row
// snapshot from the managed config plus a bounded, per-row live probe. ProbeServer is
// the structured live probe extracted in this plan; the handler bounds the supplied ctx
// with a per-request timeout so a hung server fails only its own row.
type MCPBoardProvider interface {
	Servers() mcp.ManagedConfig
	Probe(ctx context.Context, name string, server mcp.ManagedServer) mcp.ProbeResult
}

// SkillsBoardProvider is the read-only skills-governance surface (GOV-02): the active
// snapshot (Loader.List), the per-stage pending/archived metadata (ListStage — never a
// mounted body), and the append-only audit ledger (newest-first). The pending/archived
// reader returns metadata only, so a pending body never enters context by construction.
type SkillsBoardProvider interface {
	ActiveSkills() []skills.Skill
	StageSkills(stage string) ([]skills.StageSkill, error)
	AuditLog(ctx context.Context, filter skills.AuditFilter) ([]skills.AuditRow, error)
}

// SchedulerBoardProvider is the read-only scheduler-governance surface (GOV-03): the
// active task list and a paginated, newest-first run history per task.
type SchedulerBoardProvider interface {
	ListActiveTasks(ctx context.Context) ([]cron.Task, error)
	ListRunsForTask(ctx context.Context, taskID string, limit, offset int) ([]cron.Run, error)
}

// GovernanceProviders bundles the three read-only board providers so the composition
// root wires them in one call. Any field may be nil; the corresponding board's handler
// (Plan 02) answers 503 when its provider is unset.
type GovernanceProviders struct {
	MCP       MCPBoardProvider
	Skills    SkillsBoardProvider
	Scheduler SchedulerBoardProvider
}

// CapabilitySource is the D-06 capability-picker source: the creating operator's own
// grants (the handler filters out '*' for the checklist and re-validates the subset
// before the saga). identity.Store satisfies it.
type CapabilitySource interface {
	ListCapabilities(ctx context.Context, identityID string) ([]string, error)
}

// OnboardingService is the server-held provisioning + interview surface the onboarding
// wizard handlers (Plan 05) consume: start a session, apply one step intent to the
// server-held session, run the cross-store provisioning saga, and poll the Telegram
// link. The concrete service (session store + saga) is built in Plan 05; this plan only
// declares the seam so the field + setter exist. The capability source is embedded so
// the picker read goes through the same narrow interface.
type OnboardingService interface {
	CapabilitySource
}

// SetGovernanceProviders wires the read-only governance board providers (GOV-01/02/03).
// Set by the daemon composition root after NewServer; until set, the governance board
// routes (registered in Plan 02) answer 503. Kept off the constructor so existing
// NewServer callers/tests stay unchanged (D-A2-02 narrow seam).
func (s *Server) SetGovernanceProviders(p GovernanceProviders) { s.governance = p }

// SetOnboardingService wires the onboarding provisioning + interview service (ONBD-01/
// 02). Set by the daemon composition root after NewServer; until set, the onboarding
// routes (registered in Plan 05) answer 503. Kept off the constructor so existing
// NewServer callers/tests stay unchanged (D-A2-02 narrow seam).
func (s *Server) SetOnboardingService(svc OnboardingService) { s.onboarding = svc }
