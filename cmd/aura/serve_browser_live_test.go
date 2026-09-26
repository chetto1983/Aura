package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The live view drives the agent's browser, so both routes must reach the AG-UI handler through
// the parent mux AND carry agentRunCapability: a route present only on Server.Mux would answer
// the SPA shell through the real daemon while its handler test passed.
func TestBrowserLiveRoutesAreMountedBehindAgentRun(t *testing.T) {
	var hits int
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	})
	requests := []struct{ method, path string }{
		{http.MethodGet, "/api/browser/sessions/login/stream"},
		{http.MethodPost, "/api/browser/sessions/login/input"},
	}
	for _, tc := range []struct {
		name       string
		identities aguiIdentityStore
		session    bool
		wantCode   int
		wantHits   int
	}{
		{"no session cookie", wiringIdentities{id: restartTestIdentity}, false, http.StatusUnauthorized, 0},
		{"without agent.run", uncapableIdentities{id: restartTestIdentity}, true, http.StatusForbidden, 0},
		{"with agent.run", wiringIdentities{id: restartTestIdentity}, true, http.StatusNoContent, 1},
	} {
		for _, rq := range requests {
			t.Run(tc.name+" "+rq.method, func(t *testing.T) {
				hits = 0
				handler, err := newServeHandler(aguiHandler, authulaTestDeps(restartTestIdentity, tc.identities), &fakeAuthulaProvider{})
				if err != nil {
					t.Fatalf("newServeHandler: %v", err)
				}
				req := httptest.NewRequest(rq.method, rq.path, strings.NewReader(`{}`))
				req.Header.Set("Accept", "application/json")
				if tc.session {
					addAuthulaSession(req)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != tc.wantCode || hits != tc.wantHits {
					t.Fatalf("%s %s = %d with %d handler hits, want %d/%d", rq.method, rq.path, rec.Code, hits, tc.wantCode, tc.wantHits)
				}
			})
		}
	}
}
