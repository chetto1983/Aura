package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A member repairs their own mailbox: discovery, verification and the provider-config change
// all sit behind governance.write, which every identity holds.
func TestPIMAccountRoutesReachAMember(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	auth := authulaTestDeps(localID, memberIdentities{id: localID})
	var hits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		_, _ = io.WriteString(w, `{}`)
	})
	handler, err := newServeHandler(aguiHandler, auth, &fakeAuthulaProvider{}, "")
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}

	for _, route := range []struct{ method, target, body string }{
		{http.MethodPost, "/api/connect/pim/discover", `{"email":"someone@example.com"}`},
		{http.MethodPost, "/api/connect/pim/accounts/work/verify", ""},
		{http.MethodPatch, "/api/connect/pim/accounts/work/provider-config", `{"providerConfig":{"password":"p"}}`},
	} {
		hits = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(route.method, route.target, strings.NewReader(route.body))
		addAuthulaSession(req)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || len(hits) != 1 {
			t.Fatalf("member %s %s = %d (hits %v), want it to reach the handler", route.method, route.target, rec.Code, hits)
		}
	}
}
