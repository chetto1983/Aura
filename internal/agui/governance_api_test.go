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
	"github.com/chetto1983/aura/internal/skills"
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

// scriptedSkillsBoard is a configurable SkillsBoardProvider: canned active/stage skills,
// the audit rows, and optional per-call errors. stageCalls records the requested stage so
// the per-stage routing can be asserted.
type scriptedSkillsBoard struct {
	active     []skills.Skill
	staged     map[string][]skills.StageSkill
	stageErr   error
	audit      []skills.AuditRow
	auditErr   error
	auditLimit int
	auditSince time.Time
}

func (b *scriptedSkillsBoard) ActiveSkills() []skills.Skill { return b.active }

func (b *scriptedSkillsBoard) StageSkills(stage string) ([]skills.StageSkill, error) {
	if b.stageErr != nil {
		return nil, b.stageErr
	}
	return b.staged[stage], nil
}

func (b *scriptedSkillsBoard) AuditLog(_ context.Context, f skills.AuditFilter) ([]skills.AuditRow, error) {
	b.auditLimit = f.Limit
	b.auditSince = f.Since
	if b.auditErr != nil {
		return nil, b.auditErr
	}
	return b.audit, nil
}

// scriptedSchedulerBoard is a configurable SchedulerBoardProvider: canned tasks + run
// history, optional errors, and recorded pagination args so the default limit/offset can
// be asserted, plus a runsReached flag proving a non-UUID id never reaches the store.
type scriptedSchedulerBoard struct {
	tasks       []cron.Task
	tasksErr    error
	runs        []cron.Run
	runsErr     error
	gotLimit    int
	gotOffset   int
	runsReached bool
}

func (b *scriptedSchedulerBoard) ListActiveTasks(context.Context) ([]cron.Task, error) {
	return b.tasks, b.tasksErr
}

func (b *scriptedSchedulerBoard) ListRunsForTask(_ context.Context, _ string, limit, offset int) ([]cron.Run, error) {
	b.runsReached = true
	b.gotLimit = limit
	b.gotOffset = offset
	return b.runs, b.runsErr
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
				Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
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
	var resp struct {
		Servers []struct {
			Name    string `json:"name"`
			EnvKeys []struct {
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

// TestGovernanceSkillsStages: the three stages list the correct sets and pending rows
// carry NO action field (T-28-02-04 — non-runnable by construction).
func TestGovernanceSkillsStages(t *testing.T) {
	board := &scriptedSkillsBoard{
		active: []skills.Skill{{Name: "active-one", Description: "a", Type: "instruction"}},
		staged: map[string][]skills.StageSkill{
			skills.StagePending:  {{Name: "pending-one", Description: "p", Type: "snippet", Language: "python", ContentHash: "abc"}},
			skills.StageArchived: {{Name: "archived-one", Description: "ar", Type: "instruction"}},
		},
	}
	s := govServer(GovernanceProviders{Skills: board})

	// Default (no ?stage) → active.
	def := doGov(t, s, http.MethodGet, "/api/governance/skills")
	if !strings.Contains(def.Body.String(), "active-one") {
		t.Fatalf("default stage must be active, got %s", def.Body.String())
	}

	pending := doGov(t, s, http.MethodGet, "/api/governance/skills?stage=pending")
	if pending.Code != http.StatusOK {
		t.Fatalf("pending status = %d, want 200", pending.Code)
	}
	if !strings.Contains(pending.Body.String(), "pending-one") {
		t.Fatalf("pending stage missing its skill: %s", pending.Body.String())
	}
	// A pending row must carry NO action/run/activate field.
	for _, banned := range []string{`"action"`, `"run"`, `"activate"`, `"runnable"`} {
		if strings.Contains(pending.Body.String(), banned) {
			t.Fatalf("pending row exposed a %s control (must be non-runnable): %s", banned, pending.Body.String())
		}
	}
	// A pending body must never be serialized.
	if strings.Contains(strings.ToLower(pending.Body.String()), `"body"`) {
		t.Fatalf("pending row leaked a body field: %s", pending.Body.String())
	}

	archived := doGov(t, s, http.MethodGet, "/api/governance/skills?stage=archived")
	if !strings.Contains(archived.Body.String(), "archived-one") {
		t.Fatalf("archived stage missing its skill: %s", archived.Body.String())
	}
}

// TestGovernanceSkillsUnknownStage: an unknown stage is a clean 400.
func TestGovernanceSkillsUnknownStage(t *testing.T) {
	s := govServer(GovernanceProviders{Skills: &scriptedSkillsBoard{}})
	rec := doGov(t, s, http.MethodGet, "/api/governance/skills?stage=bogus")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestGovernanceSkillsAuditNewestFirst: the audit endpoint returns the store rows in the
// order the store yields them (newest-first by contract) and applies the default limit.
func TestGovernanceSkillsAuditNewestFirst(t *testing.T) {
	now := time.Now().UTC()
	board := &scriptedSkillsBoard{
		audit: []skills.AuditRow{
			{ID: "newest", SkillName: "s", Action: skills.AuditAction("create"), CreatedAt: now},
			{ID: "older", SkillName: "s", Action: skills.AuditAction("update"), CreatedAt: now.Add(-time.Hour)},
		},
	}
	s := govServer(GovernanceProviders{Skills: board})
	rec := doGov(t, s, http.MethodGet, "/api/governance/skills/audit")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp struct {
		Rows []struct {
			ID string `json:"ID"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode audit body: %v", err)
	}
	if len(resp.Rows) != 2 || resp.Rows[0].ID != "newest" {
		t.Fatalf("audit rows not newest-first as the store yielded: %+v", resp.Rows)
	}
}

// TestGovernanceSchedulerOrdered: the scheduler list returns the store's tasks (ordered by
// next fire by contract) projected to JSON; an empty set → 200 {tasks: []}.
func TestGovernanceSchedulerOrdered(t *testing.T) {
	board := &scriptedSchedulerBoard{tasks: []cron.Task{
		{ID: "11111111-1111-1111-1111-111111111111", Kind: cron.TaskKind("reminder"), Status: "active"},
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
			name:   "skills-stage",
			server: govServer(GovernanceProviders{Skills: &scriptedSkillsBoard{stageErr: leak}}),
			target: "/api/governance/skills?stage=pending",
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
