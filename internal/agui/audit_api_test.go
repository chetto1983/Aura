package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
)

// fakeAuditReader answers ListActivityForIdentity from an in-memory slice so the handler
// projection + sanitization are exercised without a DB (the UNION query is covered by the
// db_integration tier).
type fakeAuditReader struct {
	events    []AuditEvent
	err       error
	gotID     string
	gotLimit  int
	gotOffset int
	callCount int
}

func (f *fakeAuditReader) ListActivityForIdentity(_ context.Context, id string, limit, offset int) ([]AuditEvent, error) {
	f.callCount++
	f.gotID, f.gotLimit, f.gotOffset = id, limit, offset
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

// fakeIdentityAdmin answers the roster + capability-management seam from in-memory maps.
type fakeIdentityAdmin struct {
	identities []identity.Identity
	caps       map[string][]string
	granted    []string // "id|cap"
	revoked    []string // "id|cap"
	grantErr   error
	revokeErr  error
	listErr    error
}

func (f *fakeIdentityAdmin) ListIdentities(context.Context) ([]identity.Identity, error) {
	return f.identities, f.listErr
}

func (f *fakeIdentityAdmin) ListCapabilities(_ context.Context, id string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.caps[id], nil
}

func (f *fakeIdentityAdmin) GrantCapability(_ context.Context, id, cap string) error {
	if f.grantErr != nil {
		return f.grantErr
	}
	f.granted = append(f.granted, id+"|"+cap)
	f.caps[id] = append(f.caps[id], cap)
	return nil
}

func (f *fakeIdentityAdmin) RevokeCapability(_ context.Context, id, cap string) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.revoked = append(f.revoked, id+"|"+cap)
	return nil
}

// HasCapability answers the 37F-13/WEBSHARE-04 share.public mint-time gate the same way
// *identity.Store does: the '*' wildcard or an exact match in the in-memory grant map.
func (f *fakeIdentityAdmin) HasCapability(_ context.Context, id, cap string) (bool, error) {
	for _, c := range f.caps[id] {
		if c == "*" || c == cap {
			return true, nil
		}
	}
	return false, nil
}

func TestHandleMeReturnsCallerCapabilities(t *testing.T) {
	admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"*", "governance.write"}}}
	s := &Server{idAdmin: admin}
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/me", nil), testLocalID)
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out meDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.IdentityID != testLocalID {
		t.Fatalf("identity_id = %q, want %q", out.IdentityID, testLocalID)
	}
	if len(out.Capabilities) != 2 || out.Capabilities[0] != "*" {
		t.Fatalf("capabilities = %v, want [* governance.write]", out.Capabilities)
	}
}

// TestHandleMeReturnsConfiguredContextWindow covers the cockpit footer gauge bug: /api/me
// must carry the LIVE llm.Config.ContextWindow (e.g. a 128K local model), not leave the
// frontend to fall back to the DeepSeek-V4 1M default (footerMetrics.DEFAULT_CONTEXT_WINDOW).
func TestHandleMeReturnsConfiguredContextWindow(t *testing.T) {
	admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"*"}}}
	s := &Server{idAdmin: admin, contextWindow: 131072}
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/me", nil), testLocalID)
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out meDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ContextWindow != 131072 {
		t.Fatalf("context_window = %d, want 131072", out.ContextWindow)
	}
}

// TestSetContextWindowWiresServer covers the SetContextWindow setter the daemon
// composition root calls (mirrors SetAuditStore/SetIdentityAdmin) — a Server built via
// NewServer with no ContextWindow set answers 0 until wired.
func TestSetContextWindowWiresServer(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	if s.contextWindow != 0 {
		t.Fatalf("contextWindow = %d, want 0 before SetContextWindow", s.contextWindow)
	}
	s.SetContextWindow(131072)
	if s.contextWindow != 131072 {
		t.Fatalf("contextWindow = %d, want 131072 after SetContextWindow", s.contextWindow)
	}
}

func TestActiveContextWindowReadsHotRuntimeSnapshot(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	runtime := llm.NewRuntime(nil, llm.Config{ContextWindow: 81920})
	s.SetContextWindow(1_000_000)
	s.SetLLMRuntime(runtime)
	if got := s.activeContextWindow(); got != 81920 {
		t.Fatalf("active context = %d, want runtime context 81920", got)
	}
	runtime.Replace(nil, llm.Config{ContextWindow: 131072})
	if got := s.activeContextWindow(); got != 131072 {
		t.Fatalf("active context after hot swap = %d, want 131072", got)
	}
}

func TestHandleMeFallsBackToLocalWithoutPrincipal(t *testing.T) {
	admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"*"}}}
	s := &Server{idAdmin: admin}
	// No withPrincipal — the loopback-dev path (RequireAuth pass-through) must resolve local.
	rec := httptest.NewRecorder()
	s.handleMe(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out meDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.IdentityID != localIdentityID {
		t.Fatalf("identity_id = %q, want local fallback %q", out.IdentityID, localIdentityID)
	}
}

func TestHandleMe503WhenUnwired(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleMe(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleAdminAuditProjectsAndSanitizes(t *testing.T) {
	reader := &fakeAuditReader{events: []AuditEvent{
		{Source: "mcp", Action: "install", Target: "aura-memory", Detail: "postgres://user:pass@host:5432/db", CreatedAt: time.Unix(1000, 0).UTC()},
		{Source: "tool", Action: "end", Target: "shell_exec", Detail: "ok", CreatedAt: time.Unix(900, 0).UTC()},
	}}
	s := &Server{audit: reader}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?identity="+testLocalID+"&limit=10&offset=5", nil)
	rec := httptest.NewRecorder()
	s.handleAdminAudit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if reader.gotID != testLocalID || reader.gotLimit != 10 || reader.gotOffset != 5 {
		t.Fatalf("store args = (%q,%d,%d), want (%q,10,5)", reader.gotID, reader.gotLimit, reader.gotOffset, testLocalID)
	}
	var out struct {
		Events []AuditEvent `json:"events"`
		Limit  int          `json:"limit"`
		Offset int          `json:"offset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(out.Events))
	}
	if strings.Contains(out.Events[0].Detail, "pass") {
		t.Fatalf("DSN credential leaked to the wire: %q", out.Events[0].Detail)
	}
	if !strings.HasPrefix(out.Events[0].Detail, "postgres://"+redact.Placeholder) {
		t.Fatalf("detail = %q, want redacted DSN", out.Events[0].Detail)
	}
}

func TestHandleAdminAuditRejectsNonUUID(t *testing.T) {
	s := &Server{audit: &fakeAuditReader{}}
	rec := httptest.NewRecorder()
	s.handleAdminAudit(rec, httptest.NewRequest(http.MethodGet, "/api/admin/audit?identity=not-a-uuid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleAdminAuditClampsLimit(t *testing.T) {
	reader := &fakeAuditReader{}
	s := &Server{audit: reader}
	// A hostile over-max limit clamps to auditMaxLimit; a negative offset clamps to 0.
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?identity="+testLocalID+"&limit=99999&offset=-4", nil)
	s.handleAdminAudit(httptest.NewRecorder(), req)
	if reader.gotLimit != auditMaxLimit || reader.gotOffset != 0 {
		t.Fatalf("clamp = (limit %d, offset %d), want (%d, 0)", reader.gotLimit, reader.gotOffset, auditMaxLimit)
	}
}

func TestHandleAdminAudit503WhenUnwired(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleAdminAudit(rec, httptest.NewRequest(http.MethodGet, "/api/admin/audit?identity="+testLocalID, nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestGrantCapabilityCallsStoreAndReturnsCaps(t *testing.T) {
	admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"agent.run"}}}
	s := &Server{idAdmin: admin}
	body := strings.NewReader(`{"capability":"governance.write"}`)
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", body), testLocalID)
	req.SetPathValue("id", testLocalID)
	rec := httptest.NewRecorder()
	s.handleGrantCapability(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if len(admin.granted) != 1 || admin.granted[0] != testLocalID+"|governance.write" {
		t.Fatalf("granted = %v, want [local|governance.write]", admin.granted)
	}
}

func TestGrantCapabilityRejectsInvalidName(t *testing.T) {
	admin := &fakeIdentityAdmin{caps: map[string][]string{}, grantErr: identity.ErrInvalidCapability}
	s := &Server{idAdmin: admin}
	body := strings.NewReader(`{"capability":"NOT VALID"}`)
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", body), testLocalID)
	req.SetPathValue("id", testLocalID)
	rec := httptest.NewRecorder()
	s.handleGrantCapability(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGrantCapabilityRejectsNonUUIDTarget(t *testing.T) {
	s := &Server{idAdmin: &fakeIdentityAdmin{caps: map[string][]string{}}}
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/bad/capabilities", strings.NewReader(`{"capability":"x.y"}`)), testLocalID)
	req.SetPathValue("id", "bad")
	rec := httptest.NewRecorder()
	s.handleGrantCapability(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRevokeCapabilityCallsStore(t *testing.T) {
	admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"governance.write"}}}
	s := &Server{idAdmin: admin}
	req := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/admin/identities/"+testLocalID+"/capabilities/governance.write", nil), testLocalID)
	req.SetPathValue("id", testLocalID)
	req.SetPathValue("capability", "governance.write")
	rec := httptest.NewRecorder()
	s.handleRevokeCapability(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if len(admin.revoked) != 1 || admin.revoked[0] != testLocalID+"|governance.write" {
		t.Fatalf("revoked = %v, want [local|governance.write]", admin.revoked)
	}
}

func TestHandleAdminIdentitiesSanitizesNames(t *testing.T) {
	admin := &fakeIdentityAdmin{
		identities: []identity.Identity{{ID: testLocalID, Name: "token=sk-leak", Kind: "user"}},
		caps:       map[string][]string{testLocalID: {"*"}},
	}
	s := &Server{idAdmin: admin}
	rec := httptest.NewRecorder()
	s.handleAdminIdentities(rec, httptest.NewRequest(http.MethodGet, "/api/admin/identities", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "sk-leak") {
		t.Fatalf("token leaked to the wire: %s", rec.Body.String())
	}
}

// TestAdminRoutesRefuseNonAdmin is the D-03/T-36-10-E server-side fail-closed proof: the
// admin audit + grant routes, mounted behind RequireCapability(governance.write) exactly as
// serve_webui_musr.go mounts them, return 403 for an authenticated NON-admin (holds
// agent.run, not governance.write) and 200 only once the capability is granted. Hiding the
// SPA controls is cosmetic; THIS gate is the boundary.
func TestAdminRoutesRefuseNonAdmin(t *testing.T) {
	s := &Server{
		audit:   &fakeAuditReader{},
		idAdmin: &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"agent.run"}}},
	}
	const govWrite = "governance.write"

	nonAdmin := testDeps("operator-secret") // local holds only agent.run
	gated := RequireCapability(http.HandlerFunc(s.handleAdminAudit), nonAdmin, govWrite)
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/admin/audit?identity="+testLocalID, nil), testLocalID)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin audit status = %d, want 403", rec.Code)
	}

	// Same request once governance.write is granted -> the gate passes to the handler (200).
	adminDeps := testDeps("operator-secret")
	adminDeps.Identities = &fakeIdentities{
		known:        map[string]Identity{testLocalID: {ID: testLocalID, Name: "local", Kind: "user"}},
		capabilities: map[string]bool{testLocalID + "|" + govWrite: true},
	}
	gatedAdmin := RequireCapability(http.HandlerFunc(s.handleAdminAudit), adminDeps, govWrite)
	rec2 := httptest.NewRecorder()
	gatedAdmin.ServeHTTP(rec2, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/admin/audit?identity="+testLocalID, nil), testLocalID))
	if rec2.Code != http.StatusOK {
		t.Fatalf("admin audit status = %d, want 200", rec2.Code)
	}
}
