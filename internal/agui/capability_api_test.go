package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capability_api_test.go proves T-02-03's mitigation at the transport boundary (D-02,
// RBAC-06): identity.CanGrantThroughAPI/CanRevokeThroughAPI refuse the two administrative
// capability names for EVERY caller before mutateCapability reaches the store — asserted on
// the fake identityAdmin, so a handler that called the store and then discarded the error
// would still fail these tests. audit_api_test.go already covers the non-refused path
// (TestGrantCapabilityCallsStoreAndReturnsCaps / TestRevokeCapabilityCallsStore); this file
// covers only the refusal branch Task 2a added.

func TestAdminCapabilityGrantRefusesEscalation(t *testing.T) {
	t.Parallel()

	t.Run("identity.create refused, store never called", func(t *testing.T) {
		t.Parallel()
		admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"governance.write"}}}
		s := &Server{idAdmin: admin}
		body := strings.NewReader(`{"capability":"identity.create"}`)
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", body), testLocalID)
		req.SetPathValue("id", testLocalID)
		rec := httptest.NewRecorder()
		s.handleGrantCapability(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
		}
		if len(admin.granted) != 0 {
			t.Fatalf("granted = %v, want none — the store must never be reached on refusal", admin.granted)
		}
	})

	t.Run("identity.delete refused, store never called", func(t *testing.T) {
		t.Parallel()
		admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"governance.write"}}}
		s := &Server{idAdmin: admin}
		body := strings.NewReader(`{"capability":"identity.delete"}`)
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", body), testLocalID)
		req.SetPathValue("id", testLocalID)
		rec := httptest.NewRecorder()
		s.handleGrantCapability(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
		}
		if len(admin.granted) != 0 {
			t.Fatalf("granted = %v, want none — the store must never be reached on refusal", admin.granted)
		}
	})

	// The guard is caller-independent (D-02): a caller who already holds identity.create is
	// refused identically when granting it to a DIFFERENT identity. "No path at all" means
	// holding the capability yourself buys nothing.
	t.Run("caller already holding identity.create is still refused", func(t *testing.T) {
		t.Parallel()
		callerID := "11111111-1111-4111-8111-111111111111"
		targetID := "22222222-2222-4222-8222-222222222222"
		admin := &fakeIdentityAdmin{caps: map[string][]string{
			callerID: {"identity.create", "governance.write"},
			targetID: {"agent.run"},
		}}
		s := &Server{idAdmin: admin}
		body := strings.NewReader(`{"capability":"identity.create"}`)
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+targetID+"/capabilities", body), callerID)
		req.SetPathValue("id", targetID)
		rec := httptest.NewRecorder()
		s.handleGrantCapability(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
		}
		if len(admin.granted) != 0 {
			t.Fatalf("granted = %v, want none — holding identity.create must not open a path to grant it", admin.granted)
		}
	})
}

func TestAdminCapabilityRevokeRefusesAdministrative(t *testing.T) {
	t.Parallel()

	for _, cap := range []string{"identity.create", "identity.delete"} {
		t.Run(cap, func(t *testing.T) {
			t.Parallel()
			admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {cap, "governance.write"}}}
			s := &Server{idAdmin: admin}
			req := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/admin/identities/"+testLocalID+"/capabilities/"+cap, nil), testLocalID)
			req.SetPathValue("id", testLocalID)
			req.SetPathValue("capability", cap)
			rec := httptest.NewRecorder()
			s.handleRevokeCapability(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
			}
			if len(admin.revoked) != 0 {
				t.Fatalf("revoked = %v, want none — the store must never be reached on refusal", admin.revoked)
			}
		})
	}
}

// TestAdminCapabilityGrantAllowsUserSet proves the guard has not blocked everything: a
// non-administrative capability still reaches the store and succeeds.
func TestAdminCapabilityGrantAllowsUserSet(t *testing.T) {
	t.Parallel()
	admin := &fakeIdentityAdmin{caps: map[string][]string{testLocalID: {"agent.run"}}}
	s := &Server{idAdmin: admin}
	body := strings.NewReader(`{"capability":"share.public"}`)
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/capabilities", body), testLocalID)
	req.SetPathValue("id", testLocalID)
	rec := httptest.NewRecorder()
	s.handleGrantCapability(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if len(admin.granted) != 1 || admin.granted[0] != testLocalID+"|share.public" {
		t.Fatalf("granted = %v, want [%s|share.public]", admin.granted, testLocalID)
	}
}
