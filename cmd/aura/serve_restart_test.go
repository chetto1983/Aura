package main

import (
	"context"
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
			handler, err := newServeHandler(server.Mux(), authulaTestDeps(restartTestIdentity, adminIdentities{id: restartTestIdentity}), &fakeAuthulaProvider{})
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
