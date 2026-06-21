package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

const govWriteActorID = "00000000-0000-0000-0000-0000000000aa"

// fakeMCPWrite is an in-memory MCPWriteProvider for the handler tests: it holds a
// ManagedConfig + an audit-action log so the tests can assert duplicate rejection (409),
// secret preservation (no value on the wire), trust-field population, idempotent toggles,
// double-remove 404, and one-action-per-call — all without a live PG/FS. It reuses the
// REAL manager.SetServerEnv so the four-state secret-preserve merge is exercised end to end.
type fakeMCPWrite struct {
	doc       mcp.ManagedConfig
	audit     []string // one entry per named action (the one-row-per-action contract)
	failWith  error    // when set, every call returns it (sanitized-error path)
	lastTrust mcp.ManagedTrust
}

func newFakeMCPWrite() *fakeMCPWrite {
	return &fakeMCPWrite{doc: mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{}}}
}

func (f *fakeMCPWrite) InstallServer(_ context.Context, _ string, req MCPInstallRequest) (MCPWriteResult, error) {
	if f.failWith != nil {
		return MCPWriteResult{}, f.failWith
	}
	if _, exists := f.doc.MCPServers[req.Name]; exists {
		return MCPWriteResult{}, ErrMCPServerExists
	}
	srv := mcp.ManagedServer{Command: req.Command, Args: req.Args, URL: req.URL, Type: req.Type, Env: req.Env}
	f.doc.MCPServers[req.Name] = srv
	f.audit = append(f.audit, "install:"+req.Name)
	return MCPWriteResult{
		Name:          req.Name,
		Server:        srv,
		CLIEquivalent: "aura mcp add " + req.Name,
		Destination:   "/home/op/.aura/mcp/servers.json",
		Probe:         &mcp.ProbeResult{Name: req.Name, OK: true, ToolCount: 3},
	}, nil
}

func (f *fakeMCPWrite) SetServerEnv(_ context.Context, _ string, name string, submitted []string) (MCPWriteResult, error) {
	if f.failWith != nil {
		return MCPWriteResult{}, f.failWith
	}
	if err := mcpmanager.SetServerEnv(&f.doc, name, submitted); err != nil {
		if errors.Is(err, mcpmanager.ErrServerNotFound) {
			return MCPWriteResult{}, ErrMCPServerNotFound
		}
		return MCPWriteResult{}, err
	}
	f.audit = append(f.audit, "edit:"+name)
	srv := f.doc.MCPServers[name]
	var warnings []string
	if _, ok := envValueTest(srv.Env, "REQUIRED_TOKEN"); ok {
		if v, _ := envValueTest(srv.Env, "REQUIRED_TOKEN"); v == "${REQUIRED_TOKEN}" || v == "" {
			warnings = append(warnings, "REQUIRED_TOKEN is still a placeholder")
		}
	}
	return MCPWriteResult{Name: name, Server: srv, Warnings: warnings}, nil
}

func (f *fakeMCPWrite) TrustApprove(_ context.Context, actor, name, class, reason string) (MCPWriteResult, error) {
	if f.failWith != nil {
		return MCPWriteResult{}, f.failWith
	}
	srv, ok := f.doc.MCPServers[name]
	if !ok {
		return MCPWriteResult{}, ErrMCPServerNotFound
	}
	srv.Trust = mcp.ManagedTrust{Class: class, ApprovedBy: actor, ApprovedAt: "2026-06-21T00:00:00Z", Reason: reason}
	f.doc.MCPServers[name] = srv
	f.lastTrust = srv.Trust
	f.audit = append(f.audit, "trust:"+name)
	return MCPWriteResult{Name: name, Server: srv, Probe: &mcp.ProbeResult{Name: name, OK: true, ToolCount: 5}}, nil
}

func (f *fakeMCPWrite) SetEnabled(_ context.Context, _ string, name string, enabled bool) (MCPWriteResult, error) {
	if f.failWith != nil {
		return MCPWriteResult{}, f.failWith
	}
	srv, ok := f.doc.MCPServers[name]
	if !ok {
		return MCPWriteResult{}, ErrMCPServerNotFound
	}
	srv.Enabled = &enabled
	f.doc.MCPServers[name] = srv
	action := "disable"
	if enabled {
		action = "enable"
	}
	f.audit = append(f.audit, action+":"+name)
	return MCPWriteResult{Name: name, Server: srv}, nil
}

func (f *fakeMCPWrite) RemoveServer(_ context.Context, _ string, name string) error {
	if f.failWith != nil {
		return f.failWith
	}
	if _, ok := f.doc.MCPServers[name]; !ok {
		return ErrMCPServerNotFound
	}
	delete(f.doc.MCPServers, name)
	f.audit = append(f.audit, "remove:"+name)
	return nil
}

func envValueTest(env []string, key string) (string, bool) {
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if ok && k == key {
			return v, true
		}
	}
	return "", false
}

// govWriteServer builds a Server with the write provider wired and a fast probe timeout.
func govWriteServer(p MCPWriteProvider) (*Server, *fakeMCPWrite) {
	fake, _ := p.(*fakeMCPWrite)
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceWriteProviders(GovernanceWriteProviders{MCP: p})
	return s, fake
}

func doGovWrite(t *testing.T, s *Server, method, target, body string, withPrin bool) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if withPrin {
		r = withPrincipal(r, govWriteActorID)
	}
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, r)
	return rec
}

// TestGovernanceWriteUnwired503 asserts every MCP write route answers 503 when the write
// provider is nil (the unwired-best-effort posture, parity with the read board).
func TestGovernanceWriteUnwired503(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	cases := []struct {
		method, target, body string
	}{
		{http.MethodPost, "/api/governance/mcp", `{"name":"x","command":"x"}`},
		{http.MethodPatch, "/api/governance/mcp/x/env", `{"env":[]}`},
		{http.MethodPost, "/api/governance/mcp/x/trust", `{"class":"trusted_local"}`},
		{http.MethodPost, "/api/governance/mcp/x/enable", ""},
		{http.MethodPost, "/api/governance/mcp/x/disable", ""},
		{http.MethodDelete, "/api/governance/mcp/x", ""},
	}
	for _, c := range cases {
		rec := doGovWrite(t, s, c.method, c.target, c.body, true)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", c.method, c.target, rec.Code)
		}
	}
}

// TestGovernanceWriteUnauthorized401 asserts a write with no bound principal is 401 (the
// audit actor is required even though the parent-mux capability gate normally binds it).
func TestGovernanceWriteUnauthorized401(t *testing.T) {
	s, _ := govWriteServer(newFakeMCPWrite())
	rec := doGovWrite(t, s, http.MethodPost, "/api/governance/mcp", `{"name":"x","command":"x"}`, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestGovernanceWriteInstallDuplicate409 asserts a first install succeeds and previews the
// CLI-equiv + destination + probe, while a duplicate name returns 409 with NO second entry.
func TestGovernanceWriteInstallDuplicate409(t *testing.T) {
	s, fake := govWriteServer(newFakeMCPWrite())

	rec := doGovWrite(t, s, http.MethodPost, "/api/governance/mcp", `{"name":"calc","command":"calc-bin"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("first install status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp mcpWriteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CLIEquivalent == "" || resp.Destination == "" {
		t.Errorf("install must preview CLI-equiv + destination, got %+v", resp)
	}
	if resp.Probe == nil || resp.Probe.ToolCount != 3 {
		t.Errorf("install must re-probe for the live tool count, got %+v", resp.Probe)
	}

	dup := doGovWrite(t, s, http.MethodPost, "/api/governance/mcp", `{"name":"calc","command":"calc-bin"}`, true)
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate install status = %d, want 409", dup.Code)
	}
	if got := countAction(fake.audit, "install:calc"); got != 1 {
		t.Errorf("duplicate install must write no second entry, install audit count = %d, want 1", got)
	}
}

// TestGovernanceWriteEnvEditNoSecretOnWire is the MCPW-02 / Pitfall-4 belt: a PATCH .../env
// response carries ONLY key-only redacted chips (no value), and submitting the redacted
// ${KEY} placeholder leaves the stored secret intact (never the placeholder text).
func TestGovernanceWriteEnvEditNoSecretOnWire(t *testing.T) {
	s, fake := govWriteServer(newFakeMCPWrite())
	fake.doc.MCPServers["srv"] = mcp.ManagedServer{
		Command: "srv-bin",
		Env:     []string{"OPENAI_API_KEY=sk-realsecret", "PUBLIC_FLAG=on"},
	}

	body := `{"env":["OPENAI_API_KEY=${OPENAI_API_KEY}","PUBLIC_FLAG=off"]}`
	rec := doGovWrite(t, s, http.MethodPatch, "/api/governance/mcp/srv/env", body, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("env edit status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-realsecret") {
		t.Fatalf("env-edit response leaked the secret value: %q", rec.Body.String())
	}

	var resp mcpWriteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var sawKey, redacted bool
	for _, chip := range resp.EnvKeys {
		if chip.Key == "OPENAI_API_KEY" {
			sawKey, redacted = true, chip.Redacted
		}
	}
	if !sawKey || !redacted {
		t.Errorf("env chips must include OPENAI_API_KEY as redacted, got %+v", resp.EnvKeys)
	}

	// The stored secret survived (placeholder = unchanged), the non-secret edit took effect.
	if v, _ := envValueTest(fake.doc.MCPServers["srv"].Env, "OPENAI_API_KEY"); v != "sk-realsecret" {
		t.Errorf("stored secret = %q, want sk-realsecret (placeholder must preserve, never overwrite)", v)
	}
	if v, _ := envValueTest(fake.doc.MCPServers["srv"].Env, "PUBLIC_FLAG"); v != "off" {
		t.Errorf("non-secret edit = %q, want off", v)
	}
}

// TestGovernanceWriteEnvEditMissingServer404 asserts a PATCH against an unknown server is a
// clean 404.
func TestGovernanceWriteEnvEditMissingServer404(t *testing.T) {
	s, _ := govWriteServer(newFakeMCPWrite())
	rec := doGovWrite(t, s, http.MethodPatch, "/api/governance/mcp/ghost/env", `{"env":[]}`, true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestGovernanceWriteEnvEditSoftWarning asserts a still-placeholder required var raises a
// soft warning but the save still succeeds (200) — informational, never a blocker.
func TestGovernanceWriteEnvEditSoftWarning(t *testing.T) {
	s, fake := govWriteServer(newFakeMCPWrite())
	fake.doc.MCPServers["srv"] = mcp.ManagedServer{Command: "srv-bin", Env: []string{"REQUIRED_TOKEN=${REQUIRED_TOKEN}"}}

	rec := doGovWrite(t, s, http.MethodPatch, "/api/governance/mcp/srv/env", `{"env":["REQUIRED_TOKEN=${REQUIRED_TOKEN}"]}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (save still succeeds): %s", rec.Code, rec.Body.String())
	}
	var resp mcpWriteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Warnings) == 0 {
		t.Error("a still-placeholder required var must raise a soft warning")
	}
}

// TestGovernanceWriteTrustPopulatesFields asserts trust-approve populates the today-empty
// ApprovedBy/ApprovedAt/Reason, writes its audit row, and re-probes the tool count.
func TestGovernanceWriteTrustPopulatesFields(t *testing.T) {
	s, fake := govWriteServer(newFakeMCPWrite())
	fake.doc.MCPServers["srv"] = mcp.ManagedServer{Command: "srv-bin"}

	rec := doGovWrite(t, s, http.MethodPost, "/api/governance/mcp/srv/trust",
		`{"class":"trusted_local","reason":"operator vetted the publisher"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fake.lastTrust.ApprovedBy != govWriteActorID || fake.lastTrust.ApprovedAt == "" || fake.lastTrust.Reason == "" {
		t.Fatalf("trust must populate ApprovedBy/At/Reason, got %+v", fake.lastTrust)
	}
	if countAction(fake.audit, "trust:srv") != 1 {
		t.Errorf("trust must write exactly one audit row, got %d", countAction(fake.audit, "trust:srv"))
	}
	var resp mcpWriteResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Probe == nil || resp.Probe.ToolCount != 5 {
		t.Errorf("trust must re-probe for the live tool count, got %+v", resp.Probe)
	}
}

// TestGovernanceWriteEnableDisableIdempotent asserts each toggle is one named action and
// returns 200.
func TestGovernanceWriteEnableDisableIdempotent(t *testing.T) {
	s, fake := govWriteServer(newFakeMCPWrite())
	fake.doc.MCPServers["srv"] = mcp.ManagedServer{Command: "srv-bin"}

	for _, verb := range []string{"disable", "enable", "enable"} {
		rec := doGovWrite(t, s, http.MethodPost, "/api/governance/mcp/srv/"+verb, "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200: %s", verb, rec.Code, rec.Body.String())
		}
	}
	if got := countAction(fake.audit, "enable:srv"); got != 2 {
		t.Errorf("enable audit count = %d, want 2 (one per call)", got)
	}
	if got := countAction(fake.audit, "disable:srv"); got != 1 {
		t.Errorf("disable audit count = %d, want 1", got)
	}
}

// TestGovernanceWriteDoubleRemove404 asserts a first DELETE removes (204) and a second is a
// clean 404 with at most one remove audit row.
func TestGovernanceWriteDoubleRemove404(t *testing.T) {
	s, fake := govWriteServer(newFakeMCPWrite())
	fake.doc.MCPServers["srv"] = mcp.ManagedServer{Command: "srv-bin"}

	rec := doGovWrite(t, s, http.MethodDelete, "/api/governance/mcp/srv", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("first remove status = %d, want 204", rec.Code)
	}
	again := doGovWrite(t, s, http.MethodDelete, "/api/governance/mcp/srv", "", true)
	if again.Code != http.StatusNotFound {
		t.Fatalf("second remove status = %d, want 404", again.Code)
	}
	if got := countAction(fake.audit, "remove:srv"); got != 1 {
		t.Errorf("remove audit count = %d, want 1 (≤1 row)", got)
	}
}

// TestGovernanceWriteSanitizedError asserts a backend failure renders a sanitized 502 with
// no secret/stack leak.
func TestGovernanceWriteSanitizedError(t *testing.T) {
	fake := newFakeMCPWrite()
	fake.failWith = errors.New("write servers.json: token=sk-leakedsecret host=db.internal")
	s, _ := govWriteServer(fake)

	rec := doGovWrite(t, s, http.MethodPost, "/api/governance/mcp", `{"name":"x","command":"x"}`, true)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "sk-leakedsecret") {
		t.Fatalf("sanitized error leaked a secret: %q", rec.Body.String())
	}
}

// TestGovernanceWriteByNameOrder asserts the install fixture renders servers in a
// deterministic by-name order after edits (the ordering edge, MCPW-02) — proven over the
// fake's resulting config.
func TestGovernanceWriteByNameOrder(t *testing.T) {
	s, fake := govWriteServer(newFakeMCPWrite())
	for _, name := range []string{"zeta", "alpha", "mid"} {
		rec := doGovWrite(t, s, http.MethodPost, "/api/governance/mcp", `{"name":"`+name+`","command":"bin"}`, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("install %s: %d", name, rec.Code)
		}
	}
	names := make([]string, 0, len(fake.doc.MCPServers))
	for n := range fake.doc.MCPServers {
		names = append(names, n)
	}
	if !sort.StringsAreSorted(names) {
		sort.Strings(names) // a map is unordered; the deterministic order is the sorted projection the handler renders
	}
	want := []string{"alpha", "mid", "zeta"}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("by-name order = %v, want %v", names, want)
		}
	}
}

// TestGovernanceWriteMountCapabilityGate exercises the production mount shape
// (RequireCapability(handler, deps, "governance.write")) over the install route: the
// `*`-holding local identity reaches the handler (200), an identity without the grant is
// 403, and a request with no bound principal is 403. This pins the Task-3 mount contract at
// the agui layer without a live PG (the capability check uses the fake identity store; the
// real seeded `*` grant is covered by the db_integration capability test).
func TestGovernanceWriteMountCapabilityGate(t *testing.T) {
	t.Parallel()

	const granteeID = "00000000-0000-0000-0000-0000000000ab"
	const ungrantedID = "00000000-0000-0000-0000-0000000000ac"

	s, _ := govWriteServer(newFakeMCPWrite())
	deps := AuthDeps{
		Secret:           "operator-secret",
		SigningKey:       deriveSigningKey("operator-secret"),
		SecretConfigured: true,
		LocalIdentityID:  granteeID,
		Identities: &fakeIdentities{
			known: map[string]Identity{
				granteeID:   {ID: granteeID, Name: "local", Kind: "user"},
				ungrantedID: {ID: ungrantedID, Name: "other", Kind: "user"},
			},
			capabilities: map[string]bool{granteeID + "|governance.write": true},
		},
	}
	// The production mount: RequireCapability fronts the AG-UI handler for the install route.
	mount := RequireCapability(s.Mux(), deps, "governance.write")

	body := `{"name":"calc","command":"calc-bin"}`

	t.Run("grantee -> handler (200)", func(t *testing.T) {
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/governance/mcp", strings.NewReader(body)), granteeID)
		rec := httptest.NewRecorder()
		mount.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("granted install = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no governance.write -> 403", func(t *testing.T) {
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/governance/mcp", strings.NewReader(body)), ungrantedID)
		rec := httptest.NewRecorder()
		mount.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("ungranted install = %d, want 403", rec.Code)
		}
	})

	t.Run("no principal -> 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/governance/mcp", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mount.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("no-principal install = %d, want 403", rec.Code)
		}
	})
}

// countAction counts entries equal to want in the fake audit log.
func countAction(log []string, want string) int {
	n := 0
	for _, e := range log {
		if e == want {
			n++
		}
	}
	return n
}
