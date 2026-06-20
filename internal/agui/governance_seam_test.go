package agui

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/skills"
)

// fakeMCPBoard / fakeSkillsBoard / fakeSchedulerBoard / fakeOnboarding are minimal
// implementations proving the consumer-side interfaces are satisfiable by concrete
// providers — the daemon composition root wires real ones in Plan 02 / Plan 05.
type fakeMCPBoard struct{}

func (fakeMCPBoard) Servers() mcp.ManagedConfig { return mcp.ManagedConfig{} }
func (fakeMCPBoard) Probe(context.Context, string, mcp.ManagedServer) mcp.ProbeResult {
	return mcp.ProbeResult{}
}

type fakeSkillsBoard struct{}

func (fakeSkillsBoard) ActiveSkills() []skills.Skill { return nil }
func (fakeSkillsBoard) StageSkills(string) ([]skills.StageSkill, error) {
	return nil, nil
}
func (fakeSkillsBoard) AuditLog(context.Context, skills.AuditFilter) ([]skills.AuditRow, error) {
	return nil, nil
}

type fakeSchedulerBoard struct{}

func (fakeSchedulerBoard) ListActiveTasks(context.Context) ([]cron.Task, error) { return nil, nil }
func (fakeSchedulerBoard) ListRunsForTask(context.Context, string, int, int) ([]cron.Run, error) {
	return nil, nil
}

type fakeOnboarding struct{}

func (fakeOnboarding) ListCapabilities(context.Context, string) ([]string, error) { return nil, nil }

// TestSetGovernanceProvidersOffConstructor proves SetGovernanceProviders exists, that
// NewServer leaves the providers unset (off the constructor, D-A2-02), and that the
// setter wires them.
func TestSetGovernanceProvidersOffConstructor(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})

	// Off the constructor: every board provider is nil until explicitly wired.
	if s.governance.MCP != nil || s.governance.Skills != nil || s.governance.Scheduler != nil {
		t.Fatalf("NewServer must leave governance providers nil, got %+v", s.governance)
	}

	s.SetGovernanceProviders(GovernanceProviders{
		MCP:       fakeMCPBoard{},
		Skills:    fakeSkillsBoard{},
		Scheduler: fakeSchedulerBoard{},
	})
	if s.governance.MCP == nil || s.governance.Skills == nil || s.governance.Scheduler == nil {
		t.Fatalf("SetGovernanceProviders did not wire all three providers: %+v", s.governance)
	}
}

// TestSetOnboardingServiceOffConstructor proves SetOnboardingService exists, that
// NewServer leaves it nil, and that the setter wires it.
func TestSetOnboardingServiceOffConstructor(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if s.onboarding != nil {
		t.Fatalf("NewServer must leave onboarding service nil, got %v", s.onboarding)
	}
	s.SetOnboardingService(fakeOnboarding{})
	if s.onboarding == nil {
		t.Fatal("SetOnboardingService did not wire the service")
	}
}

// TestOnboardingSeamAddsNoRoutes proves the onboarding seam does NOT register any routes
// on the mux yet (the onboarding routes land in Plan 05). A wired Server must still answer
// the onboarding path 404 — the handler does not exist yet. The governance routes ARE now
// registered by Plan 02 (registerGovernanceRoutes), so /api/governance/mcp is asserted
// non-404 here to lock in that the Wave-0 seam is consumed.
func TestOnboardingSeamAddsNoRoutes(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceProviders(GovernanceProviders{MCP: fakeMCPBoard{}})
	s.SetOnboardingService(fakeOnboarding{})

	// The capability source interface is satisfied by identity.Store (compile-time check
	// that the seam matches the Wave-0 backend artifact).
	var _ CapabilitySource = (*identity.Store)(nil)

	// Plan 05 has not registered the onboarding routes yet — still a clean 404.
	if rec := doGraph(t, s, "GET", "/api/onboarding/start", nil); rec.Code != 404 {
		t.Errorf("/api/onboarding/start: want 404 (no route registered until Plan 05), got %d", rec.Code)
	}
	// Plan 02 DID register the governance routes — a wired MCP board answers, not 404.
	if rec := doGraph(t, s, "GET", "/api/governance/mcp", nil); rec.Code == 404 {
		t.Errorf("/api/governance/mcp: want a registered handler (Plan 02), got 404")
	}
}
