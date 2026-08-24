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
func (fakeMCPBoard) Mounted(string) bool        { return false }
func (fakeMCPBoard) Probe(context.Context, string, mcp.ManagedServer) mcp.ProbeResult {
	return mcp.ProbeResult{}
}

type fakeSkillsBoard struct{}

func (fakeSkillsBoard) ActiveSkills() []skills.Skill { return nil }
func (fakeSkillsBoard) SkillBody(string) (string, bool) {
	return "", false
}
func (fakeSkillsBoard) ArchivedSkills() ([]skills.StageSkill, error) {
	return nil, nil
}
func (fakeSkillsBoard) AuditLog(context.Context, skills.AuditFilter) ([]skills.AuditRow, error) {
	return nil, nil
}

type fakeSchedulerBoard struct{}

func (fakeSchedulerBoard) ListManageableTasks(context.Context) ([]cron.Task, error) { return nil, nil }
func (fakeSchedulerBoard) ListRunsForTask(context.Context, string, int, int) ([]cron.Run, error) {
	return nil, nil
}
func (fakeSchedulerBoard) GetTask(context.Context, string) (cron.Task, error) {
	return cron.Task{}, nil
}
func (fakeSchedulerBoard) ApproveTask(context.Context, string) error { return nil }
func (fakeSchedulerBoard) RunTaskNow(context.Context, string) error  { return nil }
func (fakeSchedulerBoard) CancelTask(context.Context, string) error  { return nil }
func (fakeSchedulerBoard) UpdateTask(context.Context, string, cron.UpdateTaskParams) error {
	return nil
}

// fakeOnboarding is the scriptable OnboardingService fake shared by the seam test and the
// onboarding-handler tests (onboarding_api_test.go). It satisfies the full interface
// (StartSession/Provision/TelegramStatus/CreateTelegramLink/SubmitProfile + the embedded
// CapabilitySource) and records the routed token/requester/seed so the thin handlers can
// be asserted.
type fakeOnboarding struct {
	start        OnboardingStart
	startErr     error
	gotToken     string
	gotRequester string
	gotSeed      OnboardingSeed
	provResp     OnboardingProvisionResponse
	provErr      error
	statusResp   OnboardingTelegramStatus
	statusErr    error
	linkResp     OnboardingTelegramLink
	linkErr      error
	profileErr   error
	profileDone  OnboardingProfileComplete
}

func (f *fakeOnboarding) StartSession(context.Context, string) (OnboardingStart, error) {
	return f.start, f.startErr
}

func (f *fakeOnboarding) Provision(_ context.Context, requester, token string, _ OnboardingProvisionRequest) (OnboardingProvisionResponse, error) {
	f.gotRequester = requester
	f.gotToken = token
	return f.provResp, f.provErr
}

func (f *fakeOnboarding) TelegramStatus(_ context.Context, requester, token string) (OnboardingTelegramStatus, error) {
	f.gotRequester = requester
	f.gotToken = token
	return f.statusResp, f.statusErr
}

func (f *fakeOnboarding) CreateTelegramLink(_ context.Context, requester string) (OnboardingTelegramLink, error) {
	f.gotRequester = requester
	return f.linkResp, f.linkErr
}

func (f *fakeOnboarding) SubmitProfile(_ context.Context, requester string, seed OnboardingSeed) (OnboardingProfileComplete, error) {
	f.gotRequester = requester
	f.gotSeed = seed
	return f.profileDone, f.profileErr
}

func (f *fakeOnboarding) ListCapabilities(context.Context, string) ([]string, error) {
	return nil, nil
}

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
	s.SetOnboardingService(&fakeOnboarding{})
	if s.onboarding == nil {
		t.Fatal("SetOnboardingService did not wire the service")
	}
}

// TestGovernanceAndOnboardingRoutesRegistered proves both the Plan-02 governance routes
// and the Plan-05 onboarding routes are now registered on the mux (the Wave-0 seam test's
// "no onboarding routes yet" assertion is superseded by Plan 05, which registers them via
// registerOnboardingRoutes). A wired Server answers them (never 404); the onboarding
// routes are POST, so a GET probe gets 405 (method-not-allowed) — proving the path IS
// registered, not absent.
func TestGovernanceAndOnboardingRoutesRegistered(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceProviders(GovernanceProviders{MCP: fakeMCPBoard{}})
	s.SetOnboardingService(&fakeOnboarding{})

	// The capability source interface is satisfied by identity.Store (compile-time check
	// that the seam matches the Wave-0 backend artifact).
	var _ CapabilitySource = (*identity.Store)(nil)

	// Plan 05 registered POST /api/onboarding/start — a GET probe gets 405, NOT 404, so
	// the route now exists (the Wave-0 placeholder 404 is superseded).
	if rec := doGraph(t, s, "GET", "/api/onboarding/start", nil); rec.Code == 404 {
		t.Errorf("/api/onboarding/start: want a registered handler (Plan 05), got 404")
	}
	// Plan 02 DID register the governance routes — a wired MCP board answers, not 404.
	if rec := doGraph(t, s, "GET", "/api/governance/mcp", nil); rec.Code == 404 {
		t.Errorf("/api/governance/mcp: want a registered handler (Plan 02), got 404")
	}
}
