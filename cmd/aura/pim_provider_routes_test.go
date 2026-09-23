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

// memberIdentities holds exactly the capabilities every identity gets at provisioning, so it
// passes governance.write and fails identity.create — the line between member and admin.
type memberIdentities struct{ id string }

func (m memberIdentities) GetIdentityByID(_ context.Context, id string) (agui.Identity, error) {
	if id != m.id {
		return agui.Identity{}, errWiringNotFound
	}
	return agui.Identity{ID: id, Name: "member", Kind: "user"}, nil
}

func (m memberIdentities) HasCapability(_ context.Context, id, capability string) (bool, error) {
	return id == m.id && slices.Contains(identity.UserSet(), capability), nil
}

func TestPIMProviderRoutesSplitMemberAndAdmin(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	auth := authulaTestDeps(localID, memberIdentities{id: localID})
	var hits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		_, _ = io.WriteString(w, `{}`)
	})
	handler, err := newServeHandler(aguiHandler, auth, &fakeAuthulaProvider{})
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/connect/pim/providers", nil)
	addAuthulaSession(req)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(hits) != 1 {
		t.Fatalf("member GET providers = %d (hits %v), want it to reach the handler", rec.Code, hits)
	}

	hits = nil
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/connect/pim/providers/google", strings.NewReader(`{"clientId":"c","clientSecret":"s"}`))
	addAuthulaSession(req)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || len(hits) != 0 {
		t.Fatalf("member PUT provider = %d (hits %v), want 403 before the handler", rec.Code, hits)
	}
}
