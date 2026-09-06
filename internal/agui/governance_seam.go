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
	// Mounted reports whether this server's tools are in the live registry right now.
	//
	// Without it the board could only derive a startup state from enabled/trust, so every
	// server that was neither disabled nor blocked rendered as "unknown" — which the
	// cockpit shows as down. On 2026-08-24 that meant five servers reading as down while
	// all five were mounted and answering, Linear with 53 tools.
	Mounted(name string) bool
}

// SkillsBoardProvider is the read-only skills-governance surface (GOV-02): the active
// snapshot (Loader.List), the per-stage archived metadata (ListStage — never a mounted
// body), and the append-only audit ledger (newest-first). The archived reader returns
// metadata only, so an unmounted body never enters context by construction.
//
// SkillBody resolves an active skill's body from the SAME loader snapshot ActiveSkills
// lists (37D / WEBSKILL-02): one source of truth for the composer pinned-skill application
// (handleRun Mechanism A). Because both delegate to one Loader.Get/List snapshot, every
// name ActiveSkills returns is resolvable via SkillBody (list ⊆ resolvable — Pitfall 2). An
// unknown name → ("", false), which handleRun treats as a no-op (never a path read).
//
// ALL THREE READS TAKE A CONTEXT, and the identity on it is what they answer for. A skill
// is executable instruction, so "the active skills" has no deployment-wide answer once
// skills belong to somebody (amendment #214): the board, the composer picker and the run
// pin must each show the caller their own library overlaid on the house's, plus what has
// been shared with them, and never another person's. Scoping only two of the three would
// leave the archive global — the half-and-half configuration amendment #216 measured as
// worse than either whole choice — so ArchivedSkills carries the context too even though
// it reads a directory rather than a snapshot.
type SkillsBoardProvider interface {
	ActiveSkills(ctx context.Context) []skills.Skill
	SkillBody(ctx context.Context, name string) (string, bool)
	ArchivedSkills(ctx context.Context) ([]skills.StageSkill, error)
	AuditLog(ctx context.Context, filter skills.AuditFilter) ([]skills.AuditRow, error)
	// WritableRoot is the directory a write from this caller lands in — the same one
	// skills.Writer.For(identity) addresses. The board needs it to answer a question the
	// row list alone cannot: WHICH of the listed skills this person may act on. The house
	// library and another identity's share are both listed and both unwritable, and before
	// this the cockpit offered Archive/Delete on them anyway, so the verb reached a name
	// the actor's root does not hold and the operator was told the backend had failed.
	//
	// It is used to compute one boolean per row and is NEVER serialized: a host path is
	// not the cockpit's business, only the answer derived from it is.
	WritableRoot(ctx context.Context) string
}

// SchedulerBoardProvider is the scheduler-governance surface (GOV-03): the manageable
// task list (active + pending_approval), a paginated newest-first run history per task,
// and the operator management verbs (approve / run-now / cancel / reschedule). GetTask is
// the pre-mutation read the write handlers use to enforce the system-kind guard
// (IsUserManageableKind) before any state change. cron.Store satisfies the whole surface.
type SchedulerBoardProvider interface {
	ListManageableTasks(ctx context.Context) ([]cron.Task, error)
	ListRunsForTask(ctx context.Context, taskID string, limit, offset int) ([]cron.Run, error)
	GetTask(ctx context.Context, id string) (cron.Task, error)
	ApproveTask(ctx context.Context, id string) error
	RunTaskNow(ctx context.Context, id string) error
	CancelTask(ctx context.Context, id string) error
	UpdateTask(ctx context.Context, id string, p cron.UpdateTaskParams) error
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

// OnboardingService is the server-held provisioning + seed surface the onboarding
// wizard handlers (Plan 05) consume: start a session (returns the D-06 capability picker
// options = the creator's grants minus '*'), run the ordered cross-store provisioning
// saga at the final confirm (RESEARCH §Hard Problem 1 — orphan-free on abandonment
// because the saga ONLY runs here), write the current identity's typed profile seed, and
// poll the Telegram link over PendingConsumed (REST, not SSE — D-03). The concrete
// service (the goroutine-free TTL session store + the saga) is built in
// onboarding_session.go and wired at the composition root via SetOnboardingService; a
// Server with it unwired answers the onboarding routes 503. The handlers depend only on
// these methods (D-A2-02 narrow seam), never the concrete service.
type OnboardingService interface {
	// StartSession mints a server-held onboarding session keyed by an opaque token for
	// the creating operator and returns the capability picker options (the creator's
	// grants with '*' excluded — ONBD-01a / D-06).
	StartSession(ctx context.Context, creatorIdentityID string) (OnboardingStart, error)
	// Provision runs the ordered cross-store saga (Leg B Authula -> Leg A aura tx ->
	// recovery challenge -> Leg C Telegram mint -> one immutable audit row) with per-leg
	// compensation + the three-way no-escalation re-validation, and returns the Telegram deep-link + server-rendered QR
	// (ONBD-01a/01b). The password is write-only (hashed immediately, never echoed/logged).
	Provision(ctx context.Context, requesterIdentityID, token string, in OnboardingProvisionRequest) (OnboardingProvisionResponse, error)
	// TelegramStatus reports whether the minted onboarding token has been consumed (the
	// user scanned the deep-link and linked Telegram) via a REST poll over PendingConsumed
	// (ONBD-01b / R6).
	TelegramStatus(ctx context.Context, requesterIdentityID, token string) (OnboardingTelegramStatus, error)
	// CreateTelegramLink mints a current-identity Telegram deep-link + QR without
	// re-submitting the profile seed. It returns a session token that can be polled
	// through TelegramStatus.
	CreateTelegramLink(ctx context.Context, requesterIdentityID string) (OnboardingTelegramLink, error)
	// SubmitProfile is the ONE deterministic seed write (Amendment #95): the whole typed
	// form arrives in a single stateless call, is mapped to entity/fact/preference writes
	// with NO LLM anywhere on the path, and mints the Telegram link the completion screen
	// polls. An entirely blank seed records only the skip.
	SubmitProfile(ctx context.Context, requesterIdentityID string, seed OnboardingSeed) (OnboardingProfileComplete, error)
}

// SetGovernanceProviders wires the read-only governance board providers (GOV-01/02/03).
// Set by the daemon composition root after NewServer; until set, the governance board
// routes (registered in Plan 02) answer 503. Kept off the constructor so existing
// NewServer callers/tests stay unchanged (D-A2-02 narrow seam).
func (s *Server) SetGovernanceProviders(p GovernanceProviders) { s.governance = p }

// SetOnboardingService wires the onboarding provisioning + seed service (ONBD-01/
// 02). Set by the daemon composition root after NewServer; until set, the onboarding
// routes (registered in Plan 05) answer 503. Kept off the constructor so existing
// NewServer callers/tests stay unchanged (D-A2-02 narrow seam).
func (s *Server) SetOnboardingService(svc OnboardingService) { s.onboarding = svc }
