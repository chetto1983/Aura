package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
)

const restartTestIdentity = "00000000-0000-0000-0000-000000000001"

func postRestart(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil)
	addAuthulaSession(req)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// Restarting the daemon is a settings-class write: it must carry the same
// governance.write gate as PUT /api/settings/{key}, never RequireAuth alone.
func TestAdminRestartRouteRequiresGovernanceWrite(t *testing.T) {
	var hits int
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"restarting":true}`)
	})
	for _, tc := range []struct {
		name       string
		identities aguiIdentityStore
		wantCode   int
		wantHits   int
	}{
		{"without governance.write", uncapableIdentities{id: restartTestIdentity}, http.StatusForbidden, 0},
		{"with governance.write", wiringIdentities{id: restartTestIdentity}, http.StatusAccepted, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits = 0
			handler, err := newServeHandler(aguiHandler, authulaTestDeps(restartTestIdentity, tc.identities), &fakeAuthulaProvider{})
			if err != nil {
				t.Fatalf("newServeHandler: %v", err)
			}
			if rec := postRestart(t, handler); rec.Code != tc.wantCode || hits != tc.wantHits {
				t.Fatalf("POST /api/admin/restart = %d with %d handler hits, want %d/%d", rec.Code, hits, tc.wantCode, tc.wantHits)
			}
		})
	}
}

// A shutdown is only a restart when a supervisor brings the process back: the image
// sets AURA_IN_CONTAINER=1 for compose's restart policy. Anywhere else the route must
// refuse and leave the daemon running.
func TestWireRestartTriggerOnlyInsideTheContainer(t *testing.T) {
	for _, tc := range []struct {
		name         string
		inContainer  string
		wantCode     int
		wantShutdown bool
	}{
		{"inside the container", "1", http.StatusAccepted, true},
		{"outside a container", "", http.StatusConflict, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AURA_IN_CONTAINER", tc.inContainer)
			lifecycle, requestShutdown := context.WithCancel(context.Background())
			defer requestShutdown()
			server := agui.NewServer(nil, nil, agui.ServerConfig{})
			wireRestartTrigger(server, requestShutdown)
			handler, err := newServeHandler(server.Mux(), authulaTestDeps(restartTestIdentity, wiringIdentities{id: restartTestIdentity}), &fakeAuthulaProvider{})
			if err != nil {
				t.Fatalf("newServeHandler: %v", err)
			}

			rec := postRestart(t, handler)

			if rec.Code != tc.wantCode {
				t.Fatalf("POST /api/admin/restart = %d (%s), want %d", rec.Code, rec.Body.String(), tc.wantCode)
			}
			if shutdown := lifecycle.Err() != nil; shutdown != tc.wantShutdown {
				t.Fatalf("shutdown requested = %v, want %v", shutdown, tc.wantShutdown)
			}
		})
	}
}
