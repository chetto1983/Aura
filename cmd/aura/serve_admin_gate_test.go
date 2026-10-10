package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identity"
)

// adminIdentities holds what the bootstrap admin holds: every identity's set plus
// identity.create, the capability that makes an identity an admin.
type adminIdentities struct{ id string }

func (a adminIdentities) GetIdentityByID(ctx context.Context, id string) (agui.Identity, error) {
	return memberIdentities(a).GetIdentityByID(ctx, id)
}

func (a adminIdentities) HasCapability(_ context.Context, id, capability string) (bool, error) {
	return id == a.id && (capability == identity.CapIdentityCreate || slices.Contains(identity.UserSet(), capability)), nil
}

// deploymentControlRoutes change what every identity runs on, or another identity's
// grants and budget. Every identity holds governance.write (D-01), so a member reaching
// them administers the deployment; identity.create is the gate.
var deploymentControlRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/admin/identities"},
	{http.MethodPost, "/api/admin/identities/00000000-0000-0000-0000-0000000000b2/capabilities"},
	{http.MethodDelete, "/api/admin/identities/00000000-0000-0000-0000-0000000000b2/capabilities/agent.run"},
	{http.MethodGet, "/api/admin/audit"},
	{http.MethodGet, "/api/admin/identities/00000000-0000-0000-0000-0000000000b2/credit"},
	{http.MethodPost, "/api/admin/identities/00000000-0000-0000-0000-0000000000b2/credit"},
	{http.MethodGet, "/api/admin/spend/overview"},
	{http.MethodPost, "/api/admin/restart"},
	{http.MethodPost, "/api/governance/mcp"},
	{http.MethodPatch, "/api/governance/mcp/slack/env"},
	{http.MethodPost, "/api/governance/mcp/slack/trust"},
	{http.MethodPost, "/api/governance/mcp/slack/enable"},
	{http.MethodPost, "/api/governance/mcp/slack/disable"},
	{http.MethodDelete, "/api/governance/mcp/slack"},
}

func TestDeploymentControlRoutesRequireAnAdmin(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	for _, tc := range []struct {
		name       string
		identities aguiIdentityStore
		wantHits   int
	}{
		{"member", memberIdentities{id: localID}, 0},
		{"admin", adminIdentities{id: localID}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits []string
			aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits = append(hits, r.Method+" "+r.URL.Path)
				_, _ = io.WriteString(w, `{}`)
			})
			handler, err := newServeHandler(aguiHandler, authulaTestDeps(localID, tc.identities), &fakeAuthulaProvider{})
			if err != nil {
				t.Fatalf("newServeHandler: %v", err)
			}
			for _, route := range deploymentControlRoutes {
				hits = nil
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
				addAuthulaSession(req)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if len(hits) != tc.wantHits || (tc.wantHits == 0 && rec.Code != http.StatusForbidden) {
					t.Errorf("%s %s = %d with hits %v, want %d hit(s)", route.method, route.path, rec.Code, hits, tc.wantHits)
				}
			}
		})
	}
}

// The member keeps what is theirs: their own authorization on a shared server.
func TestMemberStillAuthorizesTheirOwnMCPAccount(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	var hits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		_, _ = io.WriteString(w, `{}`)
	})
	handler, err := newServeHandler(aguiHandler, authulaTestDeps(localID, memberIdentities{id: localID}), &fakeAuthulaProvider{})
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		hits = nil
		req := httptest.NewRequest(method, "/api/governance/mcp/slack/authorization", nil)
		addAuthulaSession(req)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if len(hits) != 1 {
			t.Errorf("member %s authorization = %d with hits %v, want it to reach the handler", method, rec.Code, hits)
		}
	}
}
