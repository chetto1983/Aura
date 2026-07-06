package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
)

// audit_api_branches_test.go covers the admin capability/audit handler branches the
// happy-path tests in audit_api_test.go leave uncovered: the DI setters, the 503/401 legs
// of the shared mutateCapability, the resolveCapabilityArg empty/invalid-body legs, the
// store-error legs of the roster + post-mutation capability read, and the wildcard-managed
// rejection. Every case drives the real handler with the existing fakeIdentityAdmin.

func TestSetAuditStoreAndIdentityAdminOffConstructor(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if s.audit != nil || s.idAdmin != nil {
		t.Fatalf("NewServer must leave audit + identity admin nil")
	}
	s.SetAuditStore(&fakeAuditReader{})
	s.SetIdentityAdmin(&fakeIdentityAdmin{caps: map[string][]string{}})
	if s.audit == nil || s.idAdmin == nil {
		t.Fatal("setters did not wire audit + identity admin")
	}
}

func TestMutateCapabilityGuards(t *testing.T) {
	t.Run("503 unwired", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", strings.NewReader(`{"capability":"x.y"}`))
		req.SetPathValue("id", testLocalID)
		rec := httptest.NewRecorder()
		(&Server{}).handleGrantCapability(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("401 without principal", func(t *testing.T) {
		s := &Server{idAdmin: &fakeIdentityAdmin{caps: map[string][]string{}}}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", strings.NewReader(`{"capability":"x.y"}`))
		req.SetPathValue("id", testLocalID)
		rec := httptest.NewRecorder()
		s.handleGrantCapability(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("wildcard-managed rejection is 400", func(t *testing.T) {
		admin := &fakeIdentityAdmin{caps: map[string][]string{}, grantErr: identity.ErrWildcardManaged}
		s := &Server{idAdmin: admin}
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", strings.NewReader(`{"capability":"*"}`)), testLocalID)
		req.SetPathValue("id", testLocalID)
		rec := httptest.NewRecorder()
		s.handleGrantCapability(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for a wildcard-managed grant", rec.Code)
		}
	})

	t.Run("revoke store error is 502", func(t *testing.T) {
		admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"governance.write"}}, revokeErr: errAGUITest}
		s := &Server{idAdmin: admin}
		req := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/admin/identities/"+testLocalID+"/capabilities/governance.write", nil), testLocalID)
		req.SetPathValue("id", testLocalID)
		req.SetPathValue("capability", "governance.write")
		rec := httptest.NewRecorder()
		s.handleRevokeCapability(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502 on a revoke store failure", rec.Code)
		}
	})
}

func TestResolveCapabilityArgBranches(t *testing.T) {
	t.Run("revoke with empty capability path is 400", func(t *testing.T) {
		s := &Server{idAdmin: &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"x"}}}}
		req := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/admin/identities/"+testLocalID+"/capabilities/", nil), testLocalID)
		req.SetPathValue("id", testLocalID)
		req.SetPathValue("capability", "   ")
		rec := httptest.NewRecorder()
		s.handleRevokeCapability(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for an empty capability", rec.Code)
		}
	})

	t.Run("grant with invalid JSON body is 400", func(t *testing.T) {
		s := &Server{idAdmin: &fakeIdentityAdmin{caps: map[string][]string{}}}
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", strings.NewReader(`{bad`)), testLocalID)
		req.SetPathValue("id", testLocalID)
		rec := httptest.NewRecorder()
		s.handleGrantCapability(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for malformed JSON", rec.Code)
		}
	})

	t.Run("grant with empty capability is 400", func(t *testing.T) {
		s := &Server{idAdmin: &fakeIdentityAdmin{caps: map[string][]string{}}}
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", strings.NewReader(`{"capability":"  "}`)), testLocalID)
		req.SetPathValue("id", testLocalID)
		rec := httptest.NewRecorder()
		s.handleGrantCapability(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 for an empty capability", rec.Code)
		}
	})
}

func TestHandleAdminIdentitiesErrorBranches(t *testing.T) {
	t.Run("503 unwired", func(t *testing.T) {
		rec := httptest.NewRecorder()
		(&Server{}).handleAdminIdentities(rec, httptest.NewRequest(http.MethodGet, "/api/admin/identities", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("502 when roster list fails", func(t *testing.T) {
		admin := &fakeIdentityAdmin{caps: map[string][]string{}, listErr: errAGUITest}
		s := &Server{idAdmin: admin}
		rec := httptest.NewRecorder()
		s.handleAdminIdentities(rec, httptest.NewRequest(http.MethodGet, "/api/admin/identities", nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502 when ListIdentities fails", rec.Code)
		}
	})
}

func TestHandleMe502WhenCapabilityReadFails(t *testing.T) {
	admin := &fakeIdentityAdmin{caps: map[string][]string{}, listErr: errAGUITest}
	s := &Server{idAdmin: admin}
	rec := httptest.NewRecorder()
	s.handleMe(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/me", nil), testLocalID))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 when the capability read fails", rec.Code)
	}
}

func TestHandleAdminAudit502WhenStoreFails(t *testing.T) {
	s := &Server{audit: &fakeAuditReader{err: errAGUITest}}
	rec := httptest.NewRecorder()
	s.handleAdminAudit(rec, httptest.NewRequest(http.MethodGet, "/api/admin/audit?identity="+testLocalID, nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 when the audit store fails", rec.Code)
	}
}
