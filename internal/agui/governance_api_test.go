package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/mcp"
)

// scriptedMCPBoard is a configurable MCPBoardProvider for the governance-api tests: it
// returns a canned ManagedConfig and a per-name probe func, recording every probe target
// so the configured-servers-only assertion can prove no dial occurs for an unknown name.
type scriptedMCPBoard struct {
	doc    mcp.ManagedConfig
	probe  func(ctx context.Context, name string, server mcp.ManagedServer) mcp.ProbeResult
	probed []string
}

func (b *scriptedMCPBoard) Servers() mcp.ManagedConfig { return b.doc }

func (b *scriptedMCPBoard) Probe(ctx context.Context, name string, server mcp.ManagedServer) mcp.ProbeResult {
	b.probed = append(b.probed, name)
	if b.probe != nil {
		return b.probe(ctx, name, server)
	}
	return mcp.ProbeResult{Name: name, OK: true}
}

// govServer builds a Server with the supplied governance providers wired (any may be nil)
// and a short probe timeout so the deadline-honoring probe path resolves fast.
func govServer(p GovernanceProviders) *Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceProviders(p)
	s.probeTimeout = 50 * time.Millisecond
	return s
}

func doGov(t *testing.T, s *Server, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

// boolPtr returns a pointer to b (for ManagedServer.Enabled).
func boolPtr(b bool) *bool { return &b }

// secretMCPDoc is a two-server config: a "zeta" server with a secret env value and a
// "alpha" server, so the by-name ordering (alpha before zeta) and the no-secret-value
// assertions can both run off one fixture.
func secretMCPDoc() mcp.ManagedConfig {
	return mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{
			"work": {Servers: []string{"alpha"}},
		},
		MCPServers: map[string]mcp.ManagedServer{
			"zeta": {
				Command: "zeta-bin",
				Enabled: boolPtr(true),
				Env:     []string{"OPENAI_API_KEY=sk-supersecretvalue", "PUBLIC_FLAG=on"},
				Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
			},
			"alpha": {
				Command: "alpha-bin",
				Enabled: boolPtr(true),
				Env:     []string{"PLAIN_SETTING=1"},
				Source:  "recipe:alpha",
				Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
				Runtime: mcp.ManagedRuntime{
					Network: []string{"api.alpha.test"},
				},
			},
		},
	}
}

// TestGovernanceMCPNoSecretAndOrdering: GET /api/governance/mcp returns rows by-name and
// NEVER serializes a raw env VALUE (only redacted KEY chips) — the T-28-02-01 control.
func TestGovernanceMCPNoSecretAndOrdering(t *testing.T) {
	s := govServer(GovernanceProviders{MCP: &scriptedMCPBoard{doc: secretMCPDoc()}})
	rec := doGov(t, s, http.MethodGet, "/api/governance/mcp")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	// The raw secret value must not appear anywhere in the body.
	if strings.Contains(rec.Body.String(), "sk-supersecretvalue") {
		t.Fatalf("MCP list leaked a raw env secret value: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"networkAllowlist":null`) {
		t.Fatalf("MCP list encoded an empty network allowlist as null: %s", rec.Body.String())
	}
	var resp struct {
		Servers []struct {
			Name             string   `json:"name"`
			Source           string   `json:"source"`
			RiskPolicy       string   `json:"riskPolicy"`
			Profiles         []string `json:"profiles"`
			NetworkAllowlist []string `json:"networkAllowlist"`
			EnvKeys          []struct {
				Key      string `json:"key"`
				Redacted bool   `json:"redacted"`
			} `json:"envKeys"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(resp.Servers) != 2 {
		t.Fatalf("want 2 servers, got %d", len(resp.Servers))
	}
	// By-name order: alpha before zeta.
	if resp.Servers[0].Name != "alpha" || resp.Servers[1].Name != "zeta" {
		t.Fatalf("rows not by-name ordered: %s then %s", resp.Servers[0].Name, resp.Servers[1].Name)
	}
	if resp.Servers[0].Source != "recipe:alpha" {
		t.Fatalf("alpha source = %q, want recipe:alpha", resp.Servers[0].Source)
	}
	if resp.Servers[0].RiskPolicy != mcp.TrustTrustedLocal {
		t.Fatalf("alpha riskPolicy = %q, want %q", resp.Servers[0].RiskPolicy, mcp.TrustTrustedLocal)
	}
	if len(resp.Servers[0].Profiles) != 1 || resp.Servers[0].Profiles[0] != "work" {
		t.Fatalf("alpha profiles = %#v, want [work]", resp.Servers[0].Profiles)
	}
	if len(resp.Servers[0].NetworkAllowlist) != 1 || resp.Servers[0].NetworkAllowlist[0] != "api.alpha.test" {
		t.Fatalf("alpha networkAllowlist = %#v, want [api.alpha.test]", resp.Servers[0].NetworkAllowlist)
	}
	// zeta's secret KEY is rendered as a redacted chip; its value is absent.
	var sawSecretKey, secretFlagged bool
	for _, chip := range resp.Servers[1].EnvKeys {
		if chip.Key == "OPENAI_API_KEY" {
			sawSecretKey = true
			secretFlagged = chip.Redacted
		}
	}
	if !sawSecretKey {
		t.Fatalf("the secret env KEY name (chip) was not rendered: %+v", resp.Servers[1].EnvKeys)
	}
	if !secretFlagged {
		t.Fatalf("the secret env KEY was not flagged redacted")
	}
}

// TestGovernanceMCPEmpty: zero servers → 200 {servers: []} (the board still renders).
func TestGovernanceMCPEmpty(t *testing.T) {
	s := govServer(GovernanceProviders{MCP: &scriptedMCPBoard{doc: mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{}}}})
	rec := doGov(t, s, http.MethodGet, "/api/governance/mcp")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"servers":[]`) {
		t.Fatalf("empty MCP list must be a safe empty array, got %s", rec.Body.String())
	}
}

// TestMCPProbeIsolation: a hung server's probe resolves to a timed-out (ok=false) result
// under the deadline for ITS row only — it never blocks a sibling (separate requests).
func TestMCPProbeIsolation(t *testing.T) {
	board := &scriptedMCPBoard{
		doc: mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
			"hung":    {Command: "hung-bin", Enabled: boolPtr(true)},
			"healthy": {Command: "healthy-bin", Enabled: boolPtr(true)},
		}},
		probe: func(ctx context.Context, name string, _ mcp.ManagedServer) mcp.ProbeResult {
			if name == "hung" {
				<-ctx.Done() // honor the handler's per-request deadline
				return mcp.ProbeResult{Name: name, OK: false, Err: "timed out"}
			}
			return mcp.ProbeResult{Name: name, OK: true, ToolCount: 3}
		},
	}
	s := govServer(GovernanceProviders{MCP: board})

	start := time.Now()
	hung := doGov(t, s, http.MethodGet, "/api/governance/mcp/hung/probe")
	elapsed := time.Since(start)
	if hung.Code != http.StatusOK {
		t.Fatalf("hung probe status = %d, want 200 (isolated, not a 5xx)", hung.Code)
	}
	if elapsed > time.Second {
		t.Fatalf("hung probe took %v — the per-request deadline did not bound it", elapsed)
	}
	var hres mcp.ProbeResult
	if err := json.Unmarshal(hung.Body.Bytes(), &hres); err != nil {
		t.Fatalf("decode hung result: %v", err)
	}
	if hres.OK {
		t.Fatalf("hung server probe must resolve ok=false, got %+v", hres)
	}
	// The sibling probe is a separate request, unaffected by the hung one.
	healthy := doGov(t, s, http.MethodGet, "/api/governance/mcp/healthy/probe")
	var heres mcp.ProbeResult
	if err := json.Unmarshal(healthy.Body.Bytes(), &heres); err != nil {
		t.Fatalf("decode healthy result: %v", err)
	}
	if !heres.OK || heres.ToolCount != 3 {
		t.Fatalf("sibling probe must succeed independently, got %+v", heres)
	}
}

// TestMCPProbeConfiguredOnly: an unknown {name} is a 404 and NO probe is dialed
// (Prohibition #5 — configured-servers-only).
func TestMCPProbeConfiguredOnly(t *testing.T) {
	board := &scriptedMCPBoard{doc: secretMCPDoc()}
	s := govServer(GovernanceProviders{MCP: board})
	rec := doGov(t, s, http.MethodGet, "/api/governance/mcp/ghost/probe")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown name status = %d, want 404", rec.Code)
	}
	if len(board.probed) != 0 {
		t.Fatalf("an unknown name must not dial any probe, probed=%v", board.probed)
	}
}

// TestMCPProbeErrorSanitized: a probe whose error embeds a DSN/credential is surfaced
// sanitized (no raw secret/host leak), still 200 + ok=false (isolated).
func TestMCPProbeErrorSanitized(t *testing.T) {
	board := &scriptedMCPBoard{
		doc: secretMCPDoc(),
		probe: func(context.Context, string, mcp.ManagedServer) mcp.ProbeResult {
			return mcp.ProbeResult{Name: "alpha", OK: false, Err: "dial postgres://u:p@10.0.0.9:5432 refused"}
		},
	}
	s := govServer(GovernanceProviders{MCP: board})
	rec := doGov(t, s, http.MethodGet, "/api/governance/mcp/alpha/probe")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.9") || strings.Contains(rec.Body.String(), ":p@") {
		t.Fatalf("probe error leaked a credential/host: %s", rec.Body.String())
	}
}

// TestGovernanceSchedulerOrdered: the scheduler list returns the store's tasks (ordered by
// next fire by contract) projected to JSON; an empty set → 200 {tasks: []}.
func TestGovernanceSchedulerOrdered(t *testing.T) {
	board := &scriptedSchedulerBoard{tasks: []cron.Task{
		{
			ID:                   "11111111-1111-1111-1111-111111111111",
			Kind:                 cron.TaskKind("reminder"),
			Status:               "active",
			Payload:              []byte("secret prompt payload"),
			IdentityID:           "secret-identity",
			OriginConversationID: "secret-conversation",
		},
	}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodGet, "/api/governance/scheduler")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "11111111-1111-1111-1111-111111111111") {
		t.Fatalf("scheduler list missing its task: %s", rec.Body.String())
	}

	// Empty set → safe empty array.
	for _, banned := range []string{"Payload", "secret prompt payload", "IdentityID", "secret-identity", "OriginConversationID", "secret-conversation"} {
		if strings.Contains(rec.Body.String(), banned) {
			t.Fatalf("scheduler task DTO leaked %q: %s", banned, rec.Body.String())
		}
	}

	empty := govServer(GovernanceProviders{Scheduler: &scriptedSchedulerBoard{}})
	erec := doGov(t, empty, http.MethodGet, "/api/governance/scheduler")
	if !strings.Contains(erec.Body.String(), `"tasks":[]`) {
		t.Fatalf("empty scheduler list must be a safe empty array, got %s", erec.Body.String())
	}
}

// TestGovernanceSchedulerRunsNonUUID404: a non-UUID {id} is a 404 BEFORE the store call
// (R3 — no store round-trip on a malformed id).
func TestGovernanceSchedulerRunsNonUUID404(t *testing.T) {
	board := &scriptedSchedulerBoard{}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodGet, "/api/governance/scheduler/not-a-uuid/runs")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if board.runsReached {
		t.Fatalf("a non-UUID id reached the store (must 404 pre-store)")
	}
}

// TestGovernanceSchedulerRunsDefaultPagination: a valid id with no ?limit applies the
// default limit 25 / offset 0; no runs → 200 {runs: []}.
func TestGovernanceSchedulerRunsDefaultPagination(t *testing.T) {
	board := &scriptedSchedulerBoard{}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodGet, "/api/governance/scheduler/22222222-2222-2222-2222-222222222222/runs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if board.gotLimit != 25 || board.gotOffset != 0 {
		t.Fatalf("default pagination = limit %d offset %d, want 25/0", board.gotLimit, board.gotOffset)
	}
	if !strings.Contains(rec.Body.String(), `"runs":[]`) {
		t.Fatalf("no-runs must be a safe empty array, got %s", rec.Body.String())
	}
}

// TestGovernanceSchedulerRunsExplicitPagination: explicit ?limit/?offset flow to the store.
func TestGovernanceSchedulerRunsExplicitPagination(t *testing.T) {
	board := &scriptedSchedulerBoard{}
	s := govServer(GovernanceProviders{Scheduler: board})
	doGov(t, s, http.MethodGet, "/api/governance/scheduler/22222222-2222-2222-2222-222222222222/runs?limit=5&offset=10")
	if board.gotLimit != 5 || board.gotOffset != 10 {
		t.Fatalf("explicit pagination = limit %d offset %d, want 5/10", board.gotLimit, board.gotOffset)
	}
}

// TestGovernanceSchedulerRunsSafeDTO proves run history omits resume tokens and sanitizes
// operator-visible text fields before they reach the board.
func TestGovernanceSchedulerRunsSafeDTO(t *testing.T) {
	heartbeat := time.Date(2026, 6, 20, 12, 30, 0, 0, time.UTC)
	board := &scriptedSchedulerBoard{runs: []cron.Run{{
		ID:               "run-1",
		TaskID:           "22222222-2222-2222-2222-222222222222",
		Status:           "failed",
		Summary:          "summary",
		LastHeartbeatAt:  heartbeat,
		LastError:        "connect postgres://aura:topsecret@db.internal:5432 failed",
		PausedStateToken: "33333333-3333-3333-3333-333333333333",
	}}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodGet, "/api/governance/scheduler/22222222-2222-2222-2222-222222222222/runs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "LastHeartbeatAt") || !strings.Contains(rec.Body.String(), "2026-06-20T12:30:00Z") {
		t.Fatalf("scheduler run DTO must include safe heartbeat, got %s", rec.Body.String())
	}
	for _, banned := range []string{"PausedStateToken", "33333333-3333-3333-3333-333333333333", "topsecret", "db.internal"} {
		if strings.Contains(rec.Body.String(), banned) {
			t.Fatalf("scheduler run DTO leaked %q: %s", banned, rec.Body.String())
		}
	}
}

// TestGovernanceNilProvider503: a Server with the governance providers unwired answers
// every board route 503 (the unwired-seam path; referenced by Task 2). NewServer +
// SetGovernanceProviders(nil providers) must boot without the providers on the constructor.
func TestGovernanceNilProvider503(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceProviders(GovernanceProviders{}) // all nil

	for _, target := range []string{
		"/api/governance/mcp",
		"/api/governance/mcp/x/probe",
		"/api/governance/skills",
		"/api/governance/skills/audit",
		"/api/governance/scheduler",
		"/api/governance/scheduler/33333333-3333-3333-3333-333333333333/runs",
	} {
		rec := doGov(t, s, http.MethodGet, target)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with unwired provider: status = %d, want 503", target, rec.Code)
		}
	}
}

// TestGovernanceBackendErrorSanitized: a backend failure mid-request is a sanitized 502
// (no stack/DSN/secret leak) for each board read that calls the store.
func TestGovernanceBackendErrorSanitized(t *testing.T) {
	leak := errors.New("connect postgres://aura:topsecret@db.internal:5432 failed")
	cases := []struct {
		name   string
		server *Server
		target string
	}{
		{
			name:   "skills-audit",
			server: govServer(GovernanceProviders{Skills: &scriptedSkillsBoard{auditErr: leak}}),
			target: "/api/governance/skills/audit",
		},
		{
			// stage=archived, NOT a retired stage name: a 400 would short-circuit before
			// the provider is reached and this leak assertion would silently pass on an
			// error the handler never rendered.
			name:   "skills-stage",
			server: govServer(GovernanceProviders{Skills: &scriptedSkillsBoard{stageErr: leak}}),
			target: "/api/governance/skills?stage=archived",
		},
		{
			name:   "scheduler-list",
			server: govServer(GovernanceProviders{Scheduler: &scriptedSchedulerBoard{tasksErr: leak}}),
			target: "/api/governance/scheduler",
		},
		{
			name:   "scheduler-runs",
			server: govServer(GovernanceProviders{Scheduler: &scriptedSchedulerBoard{runsErr: leak}}),
			target: "/api/governance/scheduler/44444444-4444-4444-4444-444444444444/runs",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doGov(t, tc.server, http.MethodGet, tc.target)
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "topsecret") || strings.Contains(rec.Body.String(), "db.internal") {
				t.Fatalf("backend error leaked credentials/host: %s", rec.Body.String())
			}
		})
	}
}

// TestGovernanceAuthGate401: the governance reads inherit RequireAuth from the parent-mux
// wrap — an unauthenticated (no session cookie) API request to a governance route is 401
// when a secret is configured (the whole-origin gate), proving the read surface is not
// public. With no SessionValidator wired and no cookie, validateSession fails and the
// non-HTML request gets a clean 401 (not a login redirect).
func TestGovernanceAuthGate401(t *testing.T) {
	s := govServer(GovernanceProviders{MCP: &scriptedMCPBoard{doc: secretMCPDoc()}})
	// Wrap the agui mux exactly as the daemon does: RequireAuth with a configured secret.
	gated := RequireAuth(s.Mux(), AuthDeps{SecretConfigured: true})
	req := httptest.NewRequest(http.MethodGet, "/api/governance/mcp", nil)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated governance read = %d, want 401", rec.Code)
	}
}
