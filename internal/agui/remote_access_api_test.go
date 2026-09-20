package agui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/remotetunnel"
)

type fakeRemoteAccess struct {
	calls  int
	err    error
	status RemoteAccessStatus
}

func remoteAdminCaps() *fakeIdentityAdmin {
	return &fakeIdentityAdmin{caps: map[string][]string{"admin-1": {"identity.create", "governance.write"}}}
}
func (f *fakeRemoteAccess) Status(context.Context) (RemoteAccessStatus, error) {
	return f.status, f.err
}
func (f *fakeRemoteAccess) Verify(context.Context, string) ([]cloudflareapi.Account, error) {
	f.calls++
	return []cloudflareapi.Account{}, f.err
}
func (f *fakeRemoteAccess) Configure(context.Context, RemoteAccessConfiguration, string) error {
	f.calls++
	return f.err
}
func (f *fakeRemoteAccess) Action(context.Context, string, string, int64, string) error {
	f.calls++
	return f.err
}
func (f *fakeRemoteAccess) Events(context.Context) ([]RemoteAccessEvent, error) {
	return []RemoteAccessEvent{}, f.err
}

func TestRemoteAccessStatusNeverReturnsSecrets(t *testing.T) {
	f := &fakeRemoteAccess{status: RemoteAccessStatus{Phase: "degraded", APITokenSet: true, TunnelTokenSet: true, LastError: "api-secret tunnel-secret"}}
	s := &Server{idAdmin: remoteAdminCaps()}
	s.SetRemoteAccess(f)
	r := withPrincipal(httptest.NewRequest("GET", "/api/settings/remote-access", nil), "admin-1")
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, r)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRemoteAccessErrorsAndStrictBodies(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		err        error
		code       int
		calls      int
	}{
		{"locked", `{"generation":2,"zone_name":"example.com","account_id":"account","public_label":"aura","warp_label":"aura-warp"}`, remotetunnel.ErrAddressLocked, 409, 1},
		{"stale", `{"generation":2,"zone_name":"example.com"}`, remotetunnel.ErrStaleGeneration, 409, 1},
		{"redacted", `{"generation":2,"zone_name":"example.com"}`, errors.New("api-secret"), 502, 1},
		{"unknown", `{"zone_name":"example.com","unknown":true}`, nil, 400, 0},
		{"no domain", `{"generation":2}`, nil, 202, 0},
		{"trailing", `{} {}`, nil, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRemoteAccess{err: tc.err}
			s := &Server{idAdmin: remoteAdminCaps()}
			s.SetRemoteAccess(f)
			rec := httptest.NewRecorder()
			r := withPrincipal(httptest.NewRequest("PUT", "/api/settings/remote-access", strings.NewReader(tc.body)), "admin-1")
			s.Mux().ServeHTTP(rec, r)
			if rec.Code != tc.code || f.calls != tc.calls || strings.Contains(rec.Body.String(), "api-secret") {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, f.calls, rec.Body.String())
			}
			if tc.name == "locked" && !strings.Contains(rec.Body.String(), "delete") {
				t.Fatal("missing delete-first guidance")
			}
		})
	}
}

func TestRemoteAccessAcceptanceRequiresTunnelIngress(t *testing.T) {
	for _, tc := range []struct {
		marker, host string
		code         int
	}{
		{"", "aura.example.com", 403}, {"external", "aura.example.com", 403}, {"tunnel", "wrong.example.com", 409}, {"tunnel", "aura.example.com", 202},
	} {
		t.Run(tc.marker+tc.host, func(t *testing.T) {
			f := &fakeRemoteAccess{status: RemoteAccessStatus{PublicHostname: "aura.example.com", Generation: 2}}
			s := &Server{idAdmin: remoteAdminCaps()}
			s.SetRemoteAccess(f)
			r := withPrincipal(httptest.NewRequest("POST", "https://"+tc.host+"/api/settings/remote-access/accept-external", strings.NewReader(`{"generation":2}`)), "admin-1")
			r.Header.Set("X-Aura-Remote-Ingress", tc.marker)
			rec := httptest.NewRecorder()
			s.Mux().ServeHTTP(rec, r)
			if rec.Code != tc.code {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRemoteAccessWriteRequiresAdmin(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		f := &fakeRemoteAccess{}
		s := &Server{idAdmin: remoteAdminCaps()}
		s.SetRemoteAccess(f)
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, withPrincipal(httptest.NewRequest(method, "/api/settings/remote-access", strings.NewReader(`{}`)), "member-1"))
		if rec.Code != 403 || f.calls != 0 {
			t.Fatalf("method=%s status=%d calls=%d", method, rec.Code, f.calls)
		}
	}
}

func TestRemoteAccessEndpointContracts(t *testing.T) {
	for _, tc := range []struct {
		method, path, body, actor string
		code                      int
		calls                     int
	}{
		{"GET", "", "", "", 401, 0},
		{"GET", "/events", "", "admin-1", 200, 0},
		{"POST", "/token/verify", `{"api_token":"candidate"}`, "admin-1", 200, 1},
		{"POST", "/token/verify", `{"api_token":""}`, "admin-1", 400, 0},
		{"POST", "/token/refresh", `{}`, "admin-1", 202, 1},
		{"POST", "/reconcile", `{}`, "admin-1", 202, 1},
		{"POST", "/disable", `{}`, "admin-1", 202, 1},
		{"POST", "/rotate-token", `{}`, "admin-1", 409, 0},
		{"DELETE", "", `{"hostname":"aura.example.com"}`, "admin-1", 202, 1},
		{"PUT", "", `{"zone_name":"example.com"}`, "admin-1", 202, 1},
		{"POST", "/disable", `{"unknown":true}`, "admin-1", 400, 0},
	} {
		t.Run(tc.method+tc.path+tc.actor+tc.body, func(t *testing.T) {
			f := &fakeRemoteAccess{}
			s := &Server{idAdmin: remoteAdminCaps()}
			s.SetRemoteAccess(f)
			r := httptest.NewRequest(tc.method, "/api/settings/remote-access"+tc.path, strings.NewReader(tc.body))
			if tc.actor != "" {
				r = withPrincipal(r, tc.actor)
			}
			rec := httptest.NewRecorder()
			s.Mux().ServeHTTP(rec, r)
			if rec.Code != tc.code || f.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, f.calls, rec.Body.String())
			}
		})
	}
}

func TestRemoteAccessMutationReplayDoesNotRepeatWrite(t *testing.T) {
	f := &fakeRemoteAccess{}
	registry := &memoryHTTPRegistry{}
	caps := remoteAdminCaps()
	caps.caps[testLocalID] = caps.caps["admin-1"]
	s := &Server{idAdmin: caps, operations: registry}
	s.SetRemoteAccess(f)
	handler := s.Mux()
	for range 2 {
		r := withPrincipal(httptest.NewRequest("PUT", "/api/settings/remote-access", strings.NewReader(`{"zone_name":"example.com","api_token":"candidate"}`)), testLocalID)
		r.Header.Set("Idempotency-Key", "same-request")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != 202 || strings.Contains(rec.Body.String(), "candidate") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	if f.calls != 1 {
		t.Fatalf("writes=%d", f.calls)
	}
}

func TestRemoteAccessFailuresStaySecretSafe(t *testing.T) {
	for _, path := range []string{"", "/events", "/token/verify", "/disable"} {
		f := &fakeRemoteAccess{err: errors.New("api-secret")}
		s := &Server{idAdmin: remoteAdminCaps()}
		s.SetRemoteAccess(f)
		method := "GET"
		if path == "/token/verify" || path == "/disable" {
			method = "POST"
		}
		body := `{}`
		if path == "/token/verify" {
			body = `{"api_token":"candidate"}`
		}
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, withPrincipal(httptest.NewRequest(method, "/api/settings/remote-access"+path, strings.NewReader(body)), "admin-1"))
		if rec.Code != 502 || strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("path=%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}
