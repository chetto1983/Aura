package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every scheduler verb the board offers reaches the AG-UI handler, where the owner rule lives.
// Pause and resume shipped on 2026-10-07 registered on the AG-UI mux only, so the parent mux
// answered the cockpit's buttons with its own 404 (measured on the lab VM, 2026-10-10).
func TestSchedulerWriteRoutesReachTheHandler(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	var hits int
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, `{}`)
	})
	handler, err := newServeHandler(aguiHandler, authulaTestDeps(localID, memberIdentities{id: localID}), &fakeAuthulaProvider{})
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}
	const task = "/api/governance/scheduler/01a0e1f6-c8c1-7776-8b86-4432da6d3a56"
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, task + "/approve"},
		{http.MethodPost, task + "/run"},
		{http.MethodPost, task + "/pause"},
		{http.MethodPost, task + "/resume"},
		{http.MethodDelete, task},
		{http.MethodPatch, task},
	} {
		hits = 0
		req := httptest.NewRequest(route.method, route.path, nil)
		addAuthulaSession(req)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if hits != 1 {
			t.Errorf("%s %s = %d without reaching the handler", route.method, route.path, rec.Code)
		}
	}
}
