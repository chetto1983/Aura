package agui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// connect_pim_api_test.go covers the cockpit "Connect Google Calendar" admin-proxy
// (connect_pim_api.go) against a scripted httptest fake sidecar: every route forwards to the right
// /admin path, the admin Bearer token is injected server-side (the fake asserts the Authorization
// header), the create body is passed through, the delete carries ?logout=true, the unset-sidecar
// 503, and a sanitized 502 on a dead sidecar (the host never leaks). The capability gate is covered
// by the cross-surface auth sweep (connectGatedRoutes, governance_write_auth_sweep_test).

const fakePIMToken = "test-admin-token-123"
const fakePIMIdentity = "00000000-0000-0000-0000-000000000001"

// fakePIM is a scripted aura-pim-mcp /admin REST: it records the last-seen Authorization header +
// request path/method/body so the test can assert token injection and correct forwarding.
type fakePIM struct {
	gotAuth   string
	gotPath   string
	gotMethod string
	gotQuery  string
	gotBody   string
}

func (p *fakePIM) handler() http.Handler {
	mux := http.NewServeMux()
	record := func(w http.ResponseWriter, r *http.Request, status int, body string) {
		p.gotAuth = r.Header.Get("Authorization")
		p.gotPath = r.URL.Path
		p.gotMethod = r.Method
		p.gotQuery = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		p.gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
	mux.HandleFunc("GET /admin/accounts", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusOK, `{"accounts":[{"id":"work","displayName":"Work","provider":"google","enabled":true}]}`)
	})
	mux.HandleFunc("POST /admin/accounts", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusCreated, `{"id":"work","displayName":"Work","provider":"google","enabled":true}`)
	})
	mux.HandleFunc("DELETE /admin/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusNoContent, "")
	})
	mux.HandleFunc("GET /admin/auth/{id}/google/start", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusOK, `{"authUrl":"https://accounts.google.com/o/oauth2/v2/auth?x=1","redirectUri":"http://localhost:8093/admin/auth/google/callback"}`)
	})
	mux.HandleFunc("POST /admin/accounts/{id}/logout", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusOK, `{"ok":true}`)
	})
	mux.HandleFunc("GET /admin/accounts/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusOK, `{"accountId":"work","displayName":"Work","provider":"outlook.com","enabled":true,"authFlow":null}`)
	})
	mux.HandleFunc("POST /admin/auth/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusOK, `{"accountId":"work","userCode":"ABCD-EFGH","verificationUrl":"https://microsoft.com/devicelogin","message":"enter the code","expiresIn":900}`)
	})
	mux.HandleFunc("GET /admin/auth/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusOK, `{"accountId":"work","status":"awaiting_user","message":"enter the code","userCode":"ABCD-EFGH","verificationUrl":"https://microsoft.com/devicelogin"}`)
	})
	mux.HandleFunc("POST /admin/auth/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, http.StatusOK, `{"message":"cancelled"}`)
	})
	return mux
}

type staticMCPAccessTokenProvider struct {
	token string
	err   error
}

func (p staticMCPAccessTokenProvider) AccessToken(context.Context, string) (string, error) {
	return p.token, p.err
}

// connectPIMServer builds an agui Server fronted by an httptest server, with the calendar sidecar
// optionally wired to a fake base URL + admin token.
func connectPIMServer(baseURL, token string) *httptest.Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if baseURL != "" {
		s.SetCalendarMCP(baseURL, staticMCPAccessTokenProvider{token: token})
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Mux().ServeHTTP(w, withPrincipal(r, fakePIMIdentity))
	}))
}

// TestPIMGoogleStartRetriesTransient404 proves the google/start proxy retries the sidecar's
// post-create config-reload 404 (the wizard fires start IMMEDIATELY after create, and the .NET
// reloadOnChange makes the account briefly invisible) and returns the eventual 200 — the
// create→start race fix.
func TestPIMGoogleStartRetriesTransient404(t *testing.T) {
	var calls atomic.Int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"Account not found."}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"authUrl":"https://accounts.google.com/o/oauth2/v2/auth?x=1","redirectUri":"http://localhost:8093/admin/auth/google/callback"}`)
	}))
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/pim/accounts/work/google/start")
	if err != nil {
		t.Fatalf("GET google/start: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google/start status = %d, want 200 (retried past transient 404)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "accounts.google.com") {
		t.Fatalf("google/start body not passed through after retry: %q", body)
	}
	if got := calls.Load(); got < 3 {
		t.Fatalf("expected >=3 sidecar calls (2x404 + 200), got %d", got)
	}
}

func TestPIMListAccountsForwardsWithBearer(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/pim/accounts")
	if err != nil {
		t.Fatalf("GET accounts: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
	if p.gotPath != "/admin/accounts" || p.gotMethod != http.MethodGet {
		t.Fatalf("forwarded to %s %s, want GET /admin/accounts", p.gotMethod, p.gotPath)
	}
	if p.gotAuth != "Bearer "+fakePIMToken {
		t.Fatalf("Authorization = %q, want Bearer %s", p.gotAuth, fakePIMToken)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"id":"work"`)) {
		t.Fatalf("list body not passed through: %q", body)
	}
}

func TestPIMProxyRejectsMissingPrincipalBeforeDial(t *testing.T) {
	var calls atomic.Int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer sidecar.Close()

	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetCalendarMCP(sidecar.URL, staticMCPAccessTokenProvider{token: fakePIMToken})
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/connect/pim/accounts", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing principal status = %d, want 401", rec.Code)
	}
	if calls.Load() != 0 {
		t.Fatalf("sidecar received %d calls without an authenticated principal, want 0", calls.Load())
	}
}

func TestPIMProxyRequiresAnIdentityScopedOAuthGrant(t *testing.T) {
	var calls atomic.Int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer sidecar.Close()

	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetCalendarMCP(sidecar.URL, staticMCPAccessTokenProvider{err: errors.New("no grant")})
	rec := httptest.NewRecorder()
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/connect/pim/accounts", nil), fakePIMIdentity)
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing OAuth grant status = %d, want 401", rec.Code)
	}
	if calls.Load() != 0 {
		t.Fatalf("sidecar received %d calls without an OAuth grant, want 0", calls.Load())
	}
}

func TestPIMCreateAccountForwardsBody(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	in := `{"id":"work","displayName":"Work","provider":"google","providerConfig":{"clientId":"cid","clientSecret":"sec"}}`
	resp, err := http.Post(srv.URL+"/api/connect/pim/accounts", "application/json", strings.NewReader(in))
	if err != nil {
		t.Fatalf("POST accounts: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	if p.gotPath != "/admin/accounts" || p.gotMethod != http.MethodPost {
		t.Fatalf("forwarded to %s %s, want POST /admin/accounts", p.gotMethod, p.gotPath)
	}
	if p.gotAuth != "Bearer "+fakePIMToken {
		t.Fatalf("Authorization = %q, want Bearer %s", p.gotAuth, fakePIMToken)
	}
	if !strings.Contains(p.gotBody, `"clientSecret":"sec"`) {
		t.Fatalf("create body not forwarded: %q", p.gotBody)
	}
}

func TestPIMDeleteAccountCarriesLogoutQuery(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/connect/pim/accounts/work", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE account: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}
	if p.gotPath != "/admin/accounts/work" || p.gotMethod != http.MethodDelete {
		t.Fatalf("forwarded to %s %s, want DELETE /admin/accounts/work", p.gotMethod, p.gotPath)
	}
	if p.gotQuery != "logout=true" {
		t.Fatalf("delete query = %q, want logout=true", p.gotQuery)
	}
	if p.gotAuth != "Bearer "+fakePIMToken {
		t.Fatalf("Authorization = %q, want Bearer %s", p.gotAuth, fakePIMToken)
	}
}

func TestPIMGoogleStartPassthrough(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/pim/accounts/work/google/start")
	if err != nil {
		t.Fatalf("GET google/start: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google/start status = %d, want 200", resp.StatusCode)
	}
	if p.gotPath != "/admin/auth/work/google/start" {
		t.Fatalf("forwarded path = %q, want /admin/auth/work/google/start", p.gotPath)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"authUrl"`)) || !bytes.Contains(body, []byte(`"redirectUri"`)) {
		t.Fatalf("google/start body not passed through: %q", body)
	}
}

func TestPIMLogoutForwardsPOST(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/connect/pim/accounts/work/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", resp.StatusCode)
	}
	if p.gotPath != "/admin/accounts/work/logout" || p.gotMethod != http.MethodPost {
		t.Fatalf("forwarded to %s %s, want POST /admin/accounts/work/logout", p.gotMethod, p.gotPath)
	}
}

// TestPIMAccountStatusForwards: GET …/{id}/status forwards to /admin/accounts/{id}/status and passes
// the linked-state body (authFlow:null) through — the per-account badge for every provider.
func TestPIMAccountStatusForwards(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/pim/accounts/work/status")
	if err != nil {
		t.Fatalf("GET account status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if p.gotPath != "/admin/accounts/work/status" || p.gotMethod != http.MethodGet {
		t.Fatalf("forwarded to %s %s, want GET /admin/accounts/work/status", p.gotMethod, p.gotPath)
	}
	if p.gotAuth != "Bearer "+fakePIMToken {
		t.Fatalf("Authorization = %q, want Bearer %s", p.gotAuth, fakePIMToken)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"authFlow":null`)) {
		t.Fatalf("account-status body not passed through: %q", body)
	}
}

// TestPIMDeviceStartForwardsPOST: POST …/{id}/auth/start forwards to /admin/auth/{id}/start (the
// Microsoft/Outlook device-code start) with the Bearer token, passing the {userCode,verificationUrl}
// body through so the wizard can render the device-code panel.
func TestPIMDeviceStartForwardsPOST(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/connect/pim/accounts/work/auth/start", "application/json", nil)
	if err != nil {
		t.Fatalf("POST auth/start: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth/start status = %d, want 200", resp.StatusCode)
	}
	if p.gotPath != "/admin/auth/work/start" || p.gotMethod != http.MethodPost {
		t.Fatalf("forwarded to %s %s, want POST /admin/auth/work/start", p.gotMethod, p.gotPath)
	}
	if p.gotAuth != "Bearer "+fakePIMToken {
		t.Fatalf("Authorization = %q, want Bearer %s", p.gotAuth, fakePIMToken)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"userCode":"ABCD-EFGH"`)) {
		t.Fatalf("device-start body not passed through: %q", body)
	}
}

// TestPIMAuthStatusForwards: GET …/{id}/auth/status forwards to /admin/auth/{id}/status so the wizard
// can poll a pending device-code flow.
func TestPIMAuthStatusForwards(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/pim/accounts/work/auth/status")
	if err != nil {
		t.Fatalf("GET auth/status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth/status = %d, want 200", resp.StatusCode)
	}
	if p.gotPath != "/admin/auth/work/status" || p.gotMethod != http.MethodGet {
		t.Fatalf("forwarded to %s %s, want GET /admin/auth/work/status", p.gotMethod, p.gotPath)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"status":"awaiting_user"`)) {
		t.Fatalf("auth-status body not passed through: %q", body)
	}
}

// TestPIMAuthCancelForwards: POST …/{id}/auth/cancel forwards to /admin/auth/{id}/cancel.
func TestPIMAuthCancelForwards(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/connect/pim/accounts/work/auth/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("POST auth/cancel: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth/cancel status = %d, want 200", resp.StatusCode)
	}
	if p.gotPath != "/admin/auth/work/cancel" || p.gotMethod != http.MethodPost {
		t.Fatalf("forwarded to %s %s, want POST /admin/auth/work/cancel", p.gotMethod, p.gotPath)
	}
}

// TestPIMTokenNeverLeaksToClient: the admin Bearer token is injected server-side and must NOT
// appear in any forwarded response body (the sidecar echoes no secrets, and we never write it).
func TestPIMTokenNeverLeaksToClient(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/pim/accounts")
	if err != nil {
		t.Fatalf("GET accounts: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if bytes.Contains(body, []byte(fakePIMToken)) {
		t.Fatalf("response leaked the admin token: %q", body)
	}
}

// TestPIMUnwired503: no sidecar wired → every calendar connect route answers 503 (graceful).
func TestPIMUnwired503(t *testing.T) {
	srv := connectPIMServer("", "")
	defer srv.Close()

	for _, tc := range []struct {
		name, method, path string
	}{
		{"list", http.MethodGet, "/api/connect/pim/accounts"},
		{"create", http.MethodPost, "/api/connect/pim/accounts"},
		{"delete", http.MethodDelete, "/api/connect/pim/accounts/work"},
		{"accountStatus", http.MethodGet, "/api/connect/pim/accounts/work/status"},
		{"start", http.MethodGet, "/api/connect/pim/accounts/work/google/start"},
		{"logout", http.MethodPost, "/api/connect/pim/accounts/work/logout"},
		{"deviceStart", http.MethodPost, "/api/connect/pim/accounts/work/auth/start"},
		{"authStatus", http.MethodGet, "/api/connect/pim/accounts/work/auth/status"},
		{"authCancel", http.MethodPost, "/api/connect/pim/accounts/work/auth/cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, srv.URL+tc.path, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("unwired %s = %d, want 503", tc.path, resp.StatusCode)
			}
		})
	}
}

// TestPIMNon2xxPassthrough: a sidecar 409 (duplicate id) / 400 (validation) is passed through with
// its status + body so the wizard can surface the inline error.
func TestPIMNon2xxPassthrough(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/accounts", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"account exists"}`)
	})
	sidecar := httptest.NewServer(mux)
	defer sidecar.Close()
	srv := connectPIMServer(sidecar.URL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/connect/pim/accounts", "application/json", strings.NewReader(`{"id":"dup"}`))
	if err != nil {
		t.Fatalf("POST accounts: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("dup status = %d, want 409", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"error":"account exists"`)) {
		t.Fatalf("409 body not passed through: %q", body)
	}
}

// TestPIMSidecarUnreachable502: a wired-but-dead sidecar → a sanitized 502 (the host never leaks).
func TestPIMSidecarUnreachable502(t *testing.T) {
	dead := httptest.NewServer(http.NewServeMux())
	deadURL := dead.URL
	dead.Close()

	srv := connectPIMServer(deadURL, fakePIMToken)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/connect/pim/accounts")
	if err != nil {
		t.Fatalf("GET accounts: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("dead-sidecar status = %d, want 502", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if bytes.Contains(body, []byte("127.0.0.1")) || bytes.Contains(body, []byte(fakePIMToken)) {
		t.Fatalf("502 body leaked host/token: %q", body)
	}
}
