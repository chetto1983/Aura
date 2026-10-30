package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/remotetunnel"
)

func TestRemoteAccessCredentialsNeverRequestAuraRestart(t *testing.T) {
	rows := []sqlc.AuraSettings{}
	for _, key := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_TUNNEL_TOKEN"} {
		t.Setenv(key, "")
		rows = append(rows, sqlc.AuraSettings{Key: key, Value: "private-token", IsSecret: true})
	}
	s := &Server{settings: &fakeSettingsStore{rows: rows}}
	rec := httptest.NewRecorder()
	s.handleListSettings(rec, httptest.NewRequest("GET", "/api/settings", nil))
	var out settingsListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.RestartRequired || len(out.RestartKeys) != 0 {
		t.Fatalf("hot credentials request restart: %v", out.RestartKeys)
	}
	for _, item := range out.Settings {
		if item.Key == "CLOUDFLARE_API_TOKEN" || item.Key == "CLOUDFLARE_TUNNEL_TOKEN" {
			if item.Applied != appliedLive || !item.HasValue || item.Value != "" {
				t.Fatalf("incorrect hot credential metadata for %s", item.Key)
			}
		}
	}
}

type fakeRemoteAccess struct {
	calls   int
	actions []string
	err     error
	status  RemoteAccessStatus
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
func (f *fakeRemoteAccess) Action(_ context.Context, action string, _ string, _ int64, _ string) error {
	f.calls++
	f.actions = append(f.actions, action)
	return f.err
}
func (f *fakeRemoteAccess) Events(context.Context) ([]RemoteAccessEvent, error) {
	return []RemoteAccessEvent{}, f.err
}

func TestRemoteAccessStatusBodyShape(t *testing.T) {
	f := &fakeRemoteAccess{status: RemoteAccessStatus{Phase: "degraded", APITokenSet: true, Reason: "tunnel_down", Accepted: true}}
	s := &Server{idAdmin: remoteAdminCaps()}
	s.SetRemoteAccess(f)
	r := withPrincipal(httptest.NewRequest("GET", "/api/settings/remote-access", nil), "admin-1")
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"warp_hostname", "last_error", "connector", "tunnel_token_set", "acceptance_required", "last_reconciled_at"} {
		if _, ok := body[key]; ok {
			t.Errorf("removed field %q is still served", key)
		}
	}
	for key, want := range map[string]string{"abandoned": "[]", "nameservers": "[]", "reason": `"tunnel_down"`, "accepted": "true", "reachable": "false", "api_token_set": "true"} {
		if string(body[key]) != want {
			t.Errorf("%s=%s want %s", key, body[key], want)
		}
	}
}

func TestRemoteAccessErrorsAndStrictBodies(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		err        error
		code       int
		calls      int
	}{
		{"stale", `{"generation":2,"zone_name":"example.com"}`, remotetunnel.ErrStaleGeneration, 409, 1},
		{"redacted", `{"generation":2,"zone_name":"example.com"}`, errors.New("api-secret"), 502, 1},
		{"unknown", `{"zone_name":"example.com","unknown":true}`, nil, 400, 0},
		{"first step", `{"generation":2,"account_id":"account","enabled":false,"zone_name":"","public_label":""}`, nil, 202, 1},
		{"retired warp label", `{"zone_name":"example.com","warp_label":"aura-warp"}`, nil, 400, 0},
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
		{"POST", "/abandoned/dismiss", `{}`, "admin-1", 202, 1},
		{"POST", "/disable", `{}`, "admin-1", 202, 1},
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

func TestDismissAbandonedRoute(t *testing.T) {
	for _, tc := range []struct {
		actor   string
		code    int
		actions []string
	}{{"admin-1", 202, []string{"dismiss-abandoned"}}, {"member-1", 403, nil}} {
		f := &fakeRemoteAccess{}
		s := &Server{idAdmin: remoteAdminCaps()}
		s.SetRemoteAccess(f)
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, withPrincipal(httptest.NewRequest("POST", "/api/settings/remote-access/abandoned/dismiss", strings.NewReader(`{}`)), tc.actor))
		if rec.Code != tc.code || !reflect.DeepEqual(f.actions, tc.actions) {
			t.Fatalf("actor=%s status=%d actions=%v body=%s", tc.actor, rec.Code, f.actions, rec.Body.String())
		}
	}
}

func TestErrorBodiesCarryTheirReasonAndAFixedSentence(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   int
		reason string
	}{
		{remotetunnel.ErrStaleGeneration, 409, ""},
		{remotetunnel.ErrOwnershipConflict, 409, "not_owned"},
		{&cloudflareapi.APIError{Status: 403}, 400, "token_rejected"},
		{cloudflareapi.ErrTokenInactive, 400, "token_rejected"},
		{remotetunnel.ErrNoAccounts, 400, "no_accounts"},
		{remotetunnel.ErrAccountNotVisible, 400, "account_not_visible"},
		{remotetunnel.ErrDomainInvalid, 400, "domain_invalid"},
		{remotetunnel.ErrConfiguration, 400, "unknown"},
		{remotetunnel.ErrDomainElsewhere, 502, "domain_elsewhere"},
		{remotetunnel.ErrHostnameTaken, 502, "hostname_taken"},
		{remotetunnel.ErrMemberLockout, 502, "no_administrator"},
		{errors.New("secret-abc"), 502, "unknown"},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			f := &fakeRemoteAccess{err: tc.err}
			s := &Server{idAdmin: remoteAdminCaps()}
			s.SetRemoteAccess(f)
			rec := httptest.NewRecorder()
			s.Mux().ServeHTTP(rec, withPrincipal(httptest.NewRequest("POST", "/api/settings/remote-access/token/verify", strings.NewReader(`{"api_token":"candidate"}`)), "admin-1"))
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.code || body["reason"] != tc.reason || body["error"] == "" || len(body) != 2 || strings.Contains(rec.Body.String(), "secret-abc") {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestStatusNeverReturnsUpstreamText(t *testing.T) {
	f := &fakeRemoteAccess{err: errors.New("upstream said secret-abc")}
	s := &Server{idAdmin: remoteAdminCaps()}
	s.SetRemoteAccess(f)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, withPrincipal(httptest.NewRequest("GET", "/api/settings/remote-access", nil), "admin-1"))
	if rec.Code != 502 || strings.Contains(rec.Body.String(), "secret-abc") || !strings.Contains(rec.Body.String(), `"reason":"unknown"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestActionErrorsCarryTheirReason(t *testing.T) {
	f := &fakeRemoteAccess{err: remotetunnel.ErrOwnershipConflict}
	s := &Server{idAdmin: remoteAdminCaps()}
	s.SetRemoteAccess(f)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, withPrincipal(httptest.NewRequest("POST", "/api/settings/remote-access/reconcile", strings.NewReader(`{}`)), "admin-1"))
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"reason":"not_owned"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStatusAlwaysCarriesAReason(t *testing.T) {
	f := &fakeRemoteAccess{status: RemoteAccessStatus{Phase: "healthy"}}
	s := &Server{idAdmin: remoteAdminCaps()}
	s.SetRemoteAccess(f)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, withPrincipal(httptest.NewRequest("GET", "/api/settings/remote-access", nil), "admin-1"))
	if !strings.Contains(rec.Body.String(), `"reason":""`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
