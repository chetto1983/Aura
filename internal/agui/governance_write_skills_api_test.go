package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSkillsWrite is an in-memory SkillsWriteProvider for the handler tests. It records the
// mutating calls (so a test can assert restore was NOT invoked on a collision), holds a set
// of "active" skill names (the restore-collision source), a discovery flag for the catalog,
// and an optional failWith to drive the sanitized-error path — all without a live PG/FS/npx.
type fakeSkillsWrite struct {
	activeNames     map[string]bool // names that already exist active (restore-collision source)
	discoveryOn     bool            // AURA_SKILLS_EXTERNAL_DISCOVERY analog
	failWith        error           // when set, the targeted call returns it
	calls           []string        // one entry per provider call (install/restore/archive/mutate/search)
	lastInstallName string
}

func newFakeSkillsWrite() *fakeSkillsWrite {
	return &fakeSkillsWrite{activeNames: map[string]bool{}}
}

func (f *fakeSkillsWrite) Install(_ context.Context, _ /*actor*/, source string) (SkillsInstallInfo, error) {
	f.calls = append(f.calls, "install:"+source)
	if f.failWith != nil {
		return SkillsInstallInfo{}, f.failWith
	}
	if source == "" {
		return SkillsInstallInfo{}, fmt.Errorf("%w: empty source", ErrSkillInvalidInput)
	}
	name := "demo-skill"
	f.lastInstallName = name
	// The handler must surface the install as RISKY + the FIVE-item checklist. The fake
	// mirrors the live Installer's shape, including its ACTIVE status: a fake that still
	// returned "pending_approval" while the adapter returned "active" is how the stale
	// contract stayed green for a whole phase.
	f.activeNames[name] = true
	return SkillsInstallInfo{
		Name:        name,
		Source:      source,
		ContentHash: "sha256:deadbeef",
		Preview:     "# demo",
		Destination: "/skills/active/demo-skill",
		RiskTier:    "RISKY",
		Status:      "active",
		Checklist: []SkillsCheckItem{
			{Label: "sanitized env", Passed: true},
			{Label: "SKILL.md parsed", Passed: true},
			{Label: "body within cap", Passed: true},
			{Label: "injection blocklist clean", Passed: true},
			{Label: "sanitized name/path", Passed: true},
		},
	}, nil
}

func (f *fakeSkillsWrite) Search(_ context.Context, q string) (SkillsCatalogResult, error) {
	f.calls = append(f.calls, "search:"+q)
	if f.failWith != nil {
		return SkillsCatalogResult{}, f.failWith
	}
	if !f.discoveryOn {
		return SkillsCatalogResult{Enabled: false, Query: q, Hits: []SkillsCatalogHit{}}, nil
	}
	return SkillsCatalogResult{Enabled: true, Query: q, Hits: []SkillsCatalogHit{{Source: "owner/repo", Skill: q, Installs: "12K"}}}, nil
}

func (f *fakeSkillsWrite) Restore(_ context.Context, _ /*actor*/, name string) error {
	// Mirror the concrete adapter: a name-colliding restore returns the sentinel BEFORE any
	// real Restore work — the test asserts NO "restore" call is recorded on a collision.
	if f.activeNames[name] {
		return fmt.Errorf("%w: %q", ErrSkillActiveExists, name)
	}
	f.calls = append(f.calls, "restore:"+name)
	if f.failWith != nil {
		return f.failWith
	}
	return nil
}

func (f *fakeSkillsWrite) Archive(_ context.Context, _ /*actor*/, name string) error {
	f.calls = append(f.calls, "archive:"+name)
	return f.failWith
}

func (f *fakeSkillsWrite) Mutate(_ context.Context, _ /*actor*/, action, name, _, _ string, _ bool) (string, error) {
	f.calls = append(f.calls, action+":"+name)
	if f.failWith != nil {
		return "", f.failWith
	}
	f.activeNames[name] = true
	return "active", nil
}

// Delete makes the removal OBSERVABLE, not just recorded. The previous test asserted only
// that a call had happened, which is exactly how a delete route that deleted nothing
// shipped: it recorded the call faithfully and removed nothing.
func (f *fakeSkillsWrite) Delete(_ context.Context, _ /*actor*/, name string) error {
	f.calls = append(f.calls, "delete:"+name)
	if f.failWith != nil {
		return f.failWith
	}
	delete(f.activeNames, name)
	return nil
}

// skillsWriteServer builds a Server with the fake skills write provider wired (and a nil MCP
// provider — the bundle's nil field answers 503 only for the MCP routes).
func skillsWriteServer(p SkillsWriteProvider) (*Server, *fakeSkillsWrite) {
	fake, _ := p.(*fakeSkillsWrite)
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceWriteProviders(GovernanceWriteProviders{Skills: p})
	return s, fake
}

func doSkillsWrite(t *testing.T, s *Server, method, target, body string, withPrin bool) *httptest.ResponseRecorder {
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

// TestGovernanceWriteSkillsUnwired503 asserts every skills write route answers 503 when the
// skills provider is nil (the unwired-best-effort posture, parity with the MCP routes).
func TestGovernanceWriteSkillsUnwired503(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	cases := []struct{ method, target, body string }{
		{http.MethodPost, "/api/governance/skills/install", `{"source":"owner/repo"}`},
		{http.MethodPost, "/api/governance/skills/x/restore", ""},
		{http.MethodPost, "/api/governance/skills/x/archive", ""},
		{http.MethodPost, "/api/governance/skills", `{"name":"x","body":"b"}`},
		{http.MethodPatch, "/api/governance/skills/x", `{"body":"b"}`},
		{http.MethodDelete, "/api/governance/skills/x", ""},
		{http.MethodGet, "/api/governance/skills/catalog?q=x", ""},
	}
	for _, c := range cases {
		rec := doSkillsWrite(t, s, c.method, c.target, c.body, true)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", c.method, c.target, rec.Code)
		}
	}
}

// TestGovernanceWriteSkillsUnauthorized401 asserts a write with no bound principal is 401
// (the audit actor is required even though the parent-mux capability gate normally binds it).
// The catalog GET is exempt (it writes nothing → no audit actor needed).
func TestGovernanceWriteSkillsUnauthorized401(t *testing.T) {
	s, _ := skillsWriteServer(newFakeSkillsWrite())
	rec := doSkillsWrite(t, s, http.MethodPost, "/api/governance/skills/install", `{"source":"owner/repo"}`, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestGovernanceWriteSkillsInstallRiskyChecklist asserts the install response carries the
// RISKY label, exactly FIVE checklist items (no --ignore-scripts item), source/hash/preview/
// destination, the pending status, and the operator-origin approval token (the /api/approvals
// deep-link). The staged skill is pending (Status pending_approval) — never active.
func TestGovernanceWriteSkillsInstallRiskyChecklist(t *testing.T) {
	s, _ := skillsWriteServer(newFakeSkillsWrite())
	rec := doSkillsWrite(t, s, http.MethodPost, "/api/governance/skills/install", `{"source":"owner/repo"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var info SkillsInstallInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.RiskTier != "RISKY" {
		t.Errorf("risk tier = %q, want RISKY (never safe)", info.RiskTier)
	}
	if len(info.Checklist) != 5 {
		t.Fatalf("checklist has %d items, want exactly 5 (post-D-09; no --ignore-scripts)", len(info.Checklist))
	}
	for _, c := range info.Checklist {
		if strings.Contains(strings.ToLower(c.Label), "ignore-scripts") {
			t.Errorf("checklist must NOT mention --ignore-scripts: %q", c.Label)
		}
	}
	if info.Status != "active" {
		t.Errorf("status = %q, want active — an install that returns is usable", info.Status)
	}
	if strings.Contains(rec.Body.String(), "approval_token") {
		t.Errorf("install must not carry an approval token: nothing is queued for approval: %s", rec.Body.String())
	}
	if info.ContentHash == "" || info.Preview == "" || info.Destination == "" || info.Source == "" {
		t.Errorf("install must surface source/hash/preview/destination: %+v", info)
	}
}

// TestGovernanceWriteSkillsInstallEmptySource400 asserts an empty source is a safe 400
// (ErrSkillInvalidInput), not a 502.
func TestGovernanceWriteSkillsInstallEmptySource400(t *testing.T) {
	s, _ := skillsWriteServer(newFakeSkillsWrite())
	rec := doSkillsWrite(t, s, http.MethodPost, "/api/governance/skills/install", `{"source":"  "}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty source = %d, want 400", rec.Code)
	}
}

// TestGovernanceWriteSkillsRestoreCollision409 asserts a restore whose name collides with an
// active skill returns 409 and that the provider's real Restore work was NOT invoked (the
// restore-collision landmine guard — the active skill is untouched).
func TestGovernanceWriteSkillsRestoreCollision409(t *testing.T) {
	s, fake := skillsWriteServer(newFakeSkillsWrite())
	fake.activeNames["dup"] = true

	rec := doSkillsWrite(t, s, http.MethodPost, "/api/governance/skills/dup/restore", "", true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("colliding restore = %d, want 409", rec.Code)
	}
	for _, c := range fake.calls {
		if c == "restore:dup" {
			t.Fatal("Writer.Restore must NOT run on a collision (the active skill must be untouched)")
		}
	}
}

// TestGovernanceWriteSkillsRestoreOK asserts a non-colliding restore reaches the provider and
// returns 204.
func TestGovernanceWriteSkillsRestoreOK(t *testing.T) {
	s, fake := skillsWriteServer(newFakeSkillsWrite())
	rec := doSkillsWrite(t, s, http.MethodPost, "/api/governance/skills/fresh/restore", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("restore = %d, want 204", rec.Code)
	}
	if !contains(fake.calls, "restore:fresh") {
		t.Fatalf("restore call not recorded: %v", fake.calls)
	}
}

// TestGovernanceWriteSkillsArchive asserts archive reaches the provider and returns 204.
func TestGovernanceWriteSkillsArchive(t *testing.T) {
	s, fake := skillsWriteServer(newFakeSkillsWrite())
	rec := doSkillsWrite(t, s, http.MethodPost, "/api/governance/skills/old/archive", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("archive = %d, want 204", rec.Code)
	}
	if !contains(fake.calls, "archive:old") {
		t.Fatalf("archive call not recorded: %v", fake.calls)
	}
}

// TestGovernanceWriteSkillsCreateUpdateDelete asserts create/update wrap the provider
// Mutate with the right action+name (the path {name} wins for update), and that DELETE
// goes to the dedicated Delete method, answers 204 with an EMPTY body, and actually
// removes the skill from the fake.
//
// The removal assertion is the point. The previous version of this test checked only
// that a call had been recorded — which a route that deletes nothing satisfies
// perfectly, and did, for as long as it shipped.
func TestGovernanceWriteSkillsCreateUpdateDelete(t *testing.T) {
	s, fake := skillsWriteServer(newFakeSkillsWrite())

	if rec := doSkillsWrite(t, s, http.MethodPost, "/api/governance/skills", `{"name":"made","body":"b"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("create = %d, want 200", rec.Code)
	}
	if rec := doSkillsWrite(t, s, http.MethodPatch, "/api/governance/skills/made", `{"body":"b2"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("update = %d, want 200", rec.Code)
	}
	if !fake.activeNames["made"] {
		t.Fatal("create/update must leave the skill active in the fake")
	}

	rec := doSkillsWrite(t, s, http.MethodDelete, "/api/governance/skills/made", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "" {
		t.Errorf("delete must answer with an empty body, got %q", body)
	}
	if fake.activeNames["made"] {
		t.Fatal("delete answered 204 but the skill is still there — the route must actually remove it")
	}
	for _, want := range []string{"create:made", "update:made", "delete:made"} {
		if !contains(fake.calls, want) {
			t.Errorf("missing provider call %q in %v", want, fake.calls)
		}
	}
}

// TestGovernanceWriteSkillsDeleteErrorMapping covers the delete path's two error shapes,
// which had NO coverage at all before — precisely how a delete that always failed at a
// validation check went unnoticed: a client-correctable input is a 400 naming the problem,
// and a backend failure is a sanitized 502 that leaks no DSN.
func TestGovernanceWriteSkillsDeleteErrorMapping(t *testing.T) {
	t.Run("invalid input is a 400", func(t *testing.T) {
		fake := newFakeSkillsWrite()
		fake.failWith = fmt.Errorf("%w: bad name", ErrSkillInvalidInput)
		s, _ := skillsWriteServer(fake)
		rec := doSkillsWrite(t, s, http.MethodDelete, "/api/governance/skills/nope", "", true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("delete with invalid input = %d, want 400: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("backend failure is a sanitized 502", func(t *testing.T) {
		fake := newFakeSkillsWrite()
		fake.failWith = errors.New("connect postgres://aura:topsecret@db.internal:5432 failed")
		s, _ := skillsWriteServer(fake)
		rec := doSkillsWrite(t, s, http.MethodDelete, "/api/governance/skills/nope", "", true)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("delete with a backend failure = %d, want 502", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "topsecret") || strings.Contains(rec.Body.String(), "db.internal") {
			t.Fatalf("delete error leaked a credential/host: %s", rec.Body.String())
		}
	})
}

// TestGovernanceWriteSkillsCatalogFlagGated asserts the catalog GET returns a disabled result
// (Enabled=false, toggle explicit) when discovery is off, and an enabled result with hits when
// on — no silent discovery escalation.
func TestGovernanceWriteSkillsCatalogFlagGated(t *testing.T) {
	s, fake := skillsWriteServer(newFakeSkillsWrite())

	rec := doSkillsWrite(t, s, http.MethodGet, "/api/governance/skills/catalog?q=test", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog off = %d, want 200", rec.Code)
	}
	var off SkillsCatalogResult
	if err := json.Unmarshal(rec.Body.Bytes(), &off); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if off.Enabled {
		t.Error("catalog must be disabled (toggle explicit) when discovery is off")
	}
	if len(off.Hits) != 0 {
		t.Error("disabled catalog must have no hits (no silent discovery)")
	}

	fake.discoveryOn = true
	rec = doSkillsWrite(t, s, http.MethodGet, "/api/governance/skills/catalog?q=test", "", true)
	var on SkillsCatalogResult
	if err := json.Unmarshal(rec.Body.Bytes(), &on); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !on.Enabled || len(on.Hits) == 0 {
		t.Errorf("catalog must return enabled+hits when discovery is on: %+v", on)
	}
}

// TestGovernanceWriteSkillsSanitizedError asserts a forced backend failure renders a sanitized
// 502 (not a 5xx leak) — a DSN/token-bearing error is not echoed to the wire.
func TestGovernanceWriteSkillsSanitizedError(t *testing.T) {
	fake := newFakeSkillsWrite()
	fake.failWith = errors.New("postgres://aura_app:s3cr3t@db:5432 connection refused")
	s, _ := skillsWriteServer(fake)

	rec := doSkillsWrite(t, s, http.MethodPost, "/api/governance/skills/install", `{"source":"owner/repo"}`, true)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("forced failure = %d, want 502", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "s3cr3t") {
		t.Fatalf("error body leaked a secret: %s", rec.Body.String())
	}
}

// TestGovernanceWriteSkillsMountCapabilityGate exercises the production mount shape
// (RequireCapability(handler, deps, "governance.write")) over the skills install route — the
// serve-level auth gate the Task-3 mount provides: the `*`-holding local identity reaches the
// handler (200), an identity without the grant is 403, and a request with no bound principal
// is 403. This pins the skills mount contract at the agui layer without a live PG (the real
// seeded `*` grant is covered by the db_integration capability test). It mirrors the MCP
// mount-gate test so both write surfaces are pinned to ONE gate string.
func TestGovernanceWriteSkillsMountCapabilityGate(t *testing.T) {
	t.Parallel()

	const granteeID = "00000000-0000-0000-0000-0000000000b1"
	const ungrantedID = "00000000-0000-0000-0000-0000000000b2"

	s, _ := skillsWriteServer(newFakeSkillsWrite())
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
	mount := RequireCapability(s.Mux(), deps, "governance.write")
	const target = "/api/governance/skills/install"
	body := `{"source":"owner/repo"}`

	t.Run("grantee -> handler (200)", func(t *testing.T) {
		req := withPrincipal(httptest.NewRequest(http.MethodPost, target, strings.NewReader(body)), granteeID)
		rec := httptest.NewRecorder()
		mount.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("granted install = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no governance.write -> 403", func(t *testing.T) {
		req := withPrincipal(httptest.NewRequest(http.MethodPost, target, strings.NewReader(body)), ungrantedID)
		rec := httptest.NewRecorder()
		mount.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("ungranted install = %d, want 403", rec.Code)
		}
	})

	t.Run("no principal -> 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
		rec := httptest.NewRecorder()
		mount.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("no-principal install = %d, want 403", rec.Code)
		}
	})
}

// contains is a small slice-membership helper for the call-log assertions.
func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
