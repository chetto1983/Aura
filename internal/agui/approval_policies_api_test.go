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

	"github.com/chetto1983/aura/internal/approvalgrants"
	"github.com/chetto1983/aura/internal/approvalpolicies"
)

const (
	policyOwner = "11111111-1111-1111-1111-111111111111"
	policyOther = "22222222-2222-2222-2222-222222222222"
)

// fakePolicyStore keys rows by identity so a test can prove the routes never cross owners.
type fakePolicyStore struct {
	rows   map[string][]approvalpolicies.Row
	setBy  string
	setErr error
}

func newFakePolicyStore() *fakePolicyStore {
	return &fakePolicyStore{rows: map[string][]approvalpolicies.Row{}}
}

func (f *fakePolicyStore) Set(_ context.Context, id, tool, action string, p approvalpolicies.Policy, setBy string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.setBy = setBy
	f.rows[id] = append(f.rows[id], approvalpolicies.Row{Tool: tool, Action: action, Policy: p, SetAt: time.Unix(0, 0), SetBy: setBy})
	return nil
}

func (f *fakePolicyStore) List(_ context.Context, id string) ([]approvalpolicies.Row, error) {
	return f.rows[id], nil
}

func (f *fakePolicyStore) Clear(_ context.Context, id, tool, action string) (bool, error) {
	for i, r := range f.rows[id] {
		if r.Tool == tool && r.Action == action {
			f.rows[id] = append(f.rows[id][:i], f.rows[id][i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// fakeRevoker records the grant revokes an ask triggers.
type fakeRevoker struct{ revoked []string }

func (f *fakeRevoker) List(context.Context, string) ([]approvalgrants.Grant, error) { return nil, nil }

func (f *fakeRevoker) Revoke(_ context.Context, id, tool, action string) (bool, error) {
	f.revoked = append(f.revoked, id+"|"+tool+"|"+action)
	return true, nil
}

func policyRequest(t *testing.T, s *Server, method, path, body, identity string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = withPrincipal(req, identity)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

func TestApprovalPolicyRoutesAnswer503Unwired(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/approvals/policies", ""},
		{http.MethodPut, "/api/approvals/policies", `{"tool":"x","policy":"ask"}`},
		{http.MethodPost, "/api/approvals/policies/clear", `{"tool":"x"}`},
	} {
		if rec := policyRequest(t, s, c.method, c.path, c.body, policyOwner); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d, want 503", c.method, c.path, rec.Code)
		}
	}
}

func TestApprovalPolicySetListClearIsOwnerScoped(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	store := newFakePolicyStore()
	s.SetApprovalPolicyStore(store)

	rec := policyRequest(t, s, http.MethodPut, "/api/approvals/policies", `{"tool":"calendar","action":"send_email","policy":"deny"}`, policyOwner)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	if store.setBy != policyOwner {
		t.Errorf("set_by = %q, want the principal", store.setBy)
	}

	rec = policyRequest(t, s, http.MethodGet, "/api/approvals/policies", "", policyOwner)
	var items []approvalPolicyItem
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("GET body: %v", err)
	}
	if len(items) != 1 || items[0].Subject != "calendar send_email" || items[0].Policy != "deny" {
		t.Fatalf("owner list = %+v", items)
	}
	rec = policyRequest(t, s, http.MethodGet, "/api/approvals/policies", "", policyOther)
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("another principal sees %s", rec.Body)
	}

	rec = policyRequest(t, s, http.MethodPost, "/api/approvals/policies/clear", `{"tool":"calendar","action":"send_email"}`, policyOther)
	if !strings.Contains(rec.Body.String(), `"cleared":false`) {
		t.Fatalf("another principal cleared the owner's policy: %s", rec.Body)
	}
	rec = policyRequest(t, s, http.MethodPost, "/api/approvals/policies/clear", `{"tool":"calendar","action":"send_email"}`, policyOwner)
	if !strings.Contains(rec.Body.String(), `"cleared":true`) {
		t.Fatalf("owner clear = %s", rec.Body)
	}
}

func TestApprovalPolicyAskRevokesTheStandingGrant(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	s.SetApprovalPolicyStore(newFakePolicyStore())
	grants := &fakeRevoker{}
	s.SetApprovalGrantStore(grants)

	rec := policyRequest(t, s, http.MethodPut, "/api/approvals/policies", `{"tool":"shell_exec","policy":"ask"}`, policyOwner)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revoked_grant":true`) {
		t.Fatalf("PUT ask = %d %s", rec.Code, rec.Body)
	}
	if len(grants.revoked) != 1 || grants.revoked[0] != policyOwner+"|shell_exec|" {
		t.Fatalf("revoked = %v, want the owner's shell_exec grant", grants.revoked)
	}
	policyRequest(t, s, http.MethodPut, "/api/approvals/policies", `{"tool":"shell_exec","policy":"deny"}`, policyOwner)
	if len(grants.revoked) != 1 {
		t.Fatalf("a deny revoked a grant: %v", grants.revoked)
	}
}

func TestApprovalPolicyRefusesBadBodies(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	store := newFakePolicyStore()
	s.SetApprovalPolicyStore(store)
	for _, c := range []struct{ method, path, body, want string }{
		{http.MethodPut, "/api/approvals/policies", `{"tool":"x","policy":"maybe"}`, "invalid_policy"},
		{http.MethodPut, "/api/approvals/policies", `{"policy":"ask"}`, "tool is required"},
		{http.MethodPut, "/api/approvals/policies", `{`, "invalid JSON"},
		{http.MethodPost, "/api/approvals/policies/clear", `{}`, "tool is required"},
		{http.MethodPost, "/api/approvals/policies/clear", `{`, "invalid JSON"},
	} {
		rec := policyRequest(t, s, c.method, c.path, c.body, policyOwner)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s %s %s = %d %s, want 400 %q", c.method, c.path, c.body, rec.Code, rec.Body, c.want)
		}
	}
	store.setErr = errors.New("dial tcp: postgres://aura_app:supersecret@db/aura refused")
	rec := policyRequest(t, s, http.MethodPut, "/api/approvals/policies", `{"tool":"x","policy":"ask"}`, policyOwner)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "supersecret") {
		t.Fatalf("store error = %d %s, want a redacted 500", rec.Code, rec.Body)
	}
}
