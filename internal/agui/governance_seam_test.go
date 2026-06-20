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

// TestGovernanceSeamsAddNoRoutes proves this plan's seams do NOT register any routes on
// the mux (the governance/onboarding routes land in Plan 02 / Plan 05). A wired Server
// must still answer the new paths 404 — the handlers do not exist yet — so existing
// callers and the route table are unchanged by Wave 0.
func TestGovernanceSeamsAddNoRoutes(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceProviders(GovernanceProviders{MCP: fakeMCPBoard{}})
	s.SetOnboardingService(fakeOnboarding{})

	// The capability source interface is satisfied by identity.Store (compile-time check
	// that the seam matches the Wave-0 backend artifact).
	var _ CapabilitySource = (*identity.Store)(nil)

	for _, path := range []string{"/api/governance/mcp", "/api/onboarding/start"} {
		rec := doGraph(t, s, "GET", path, nil)
		if rec.Code != 404 {
			t.Errorf("%s: want 404 (no route registered in Wave 0), got %d", path, rec.Code)
		}
	}
}
