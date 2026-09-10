package agui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func restartReq(principal string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/admin/restart", nil)
	if principal != "" {
		r = withPrincipal(r, principal)
	}
	return r
}

func TestHandleRestartAnswersBeforeTriggeringShutdown(t *testing.T) {
	rr := httptest.NewRecorder()
	triggers := 0
	var codeAtTrigger int
	var bodyAtTrigger string
	var flushedAtTrigger bool
	s := &Server{}
	s.SetRestartTrigger(func() {
		triggers++
		codeAtTrigger, bodyAtTrigger, flushedAtTrigger = rr.Code, rr.Body.String(), rr.Flushed
	})

	s.handleRestart(rr, restartReq("op-1"))

	if triggers != 1 {
		t.Fatalf("trigger calls = %d, want 1", triggers)
	}
	if codeAtTrigger != http.StatusAccepted || !strings.Contains(bodyAtTrigger, `"restarting":true`) || !flushedAtTrigger {
		t.Fatalf("at trigger time: code=%d body=%q flushed=%v, want the 202 written and flushed first", codeAtTrigger, bodyAtTrigger, flushedAtTrigger)
	}
}

func TestHandleRestartUnsupportedRefuses(t *testing.T) {
	rr := httptest.NewRecorder()
	(&Server{}).handleRestart(rr, restartReq("op-1"))
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rr.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got["error"] != "restart_unsupported" {
		t.Fatalf("body = %s, want {\"error\":\"restart_unsupported\"}", rr.Body.String())
	}
}

func TestHandleRestartWithoutPrincipalNeverTriggers(t *testing.T) {
	triggers := 0
	s := &Server{}
	s.SetRestartTrigger(func() { triggers++ })
	rr := httptest.NewRecorder()
	s.handleRestart(rr, restartReq(""))
	if rr.Code != http.StatusUnauthorized || triggers != 0 {
		t.Fatalf("status=%d triggers=%d, want 401 and none", rr.Code, triggers)
	}
}

// The mutation-coverage sweep already fails for an uninventoried unsafe route; this
// pins the entry by name like the other admin mutations, and proves Server.Mux answers it.
func TestRestartRouteMountedAndInventoried(t *testing.T) {
	if meta, ok := httpMutationRoutes["POST /api/admin/restart"]; !ok || meta.Normalize == "" {
		t.Fatal("POST /api/admin/restart is absent from httpMutationRoutes")
	}
	rr := httptest.NewRecorder()
	(&Server{}).Mux().ServeHTTP(rr, restartReq("op-1"))
	if rr.Code != http.StatusConflict {
		t.Fatalf("Mux POST /api/admin/restart = %d, want the unwired 409", rr.Code)
	}
}

func TestHandleListSettingsReportsRestartSupported(t *testing.T) {
	for _, supported := range []bool{false, true} {
		s := &Server{settings: &fakeSettingsStore{}}
		if supported {
			s.SetRestartTrigger(func() {})
		}
		rr := httptest.NewRecorder()
		s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
		var got map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if value, present := got["restart_supported"]; !present || value != supported {
			t.Fatalf("restart_supported = %v (present %v), want %v", value, present, supported)
		}
	}
}
