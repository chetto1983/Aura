package agui

import (
	"bytes"
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
	return mux
}

// connectPIMServer builds an agui Server fronted by an httptest server, with the calendar sidecar
// optionally wired to a fake base URL + admin token.
func connectPIMServer(baseURL, token string) *httptest.Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if baseURL != "" {
		s.SetCalendarMCP(baseURL, token)
	}
	return httptest.NewServer(s.Mux())
}

// TestPIMGoogleStartRetriesTransient404 proves the google/start proxy retries the sidecar's
// post-create config-reload 404 (the wizard fires start IMMEDIATELY after create, and the .NET
// reloadOnChange makes the account briefly invisible) and returns the eventual 200 — the
// create→start race fix.
func TestPIMGoogleStartRetriesTransient404(t *testing.T) {
	var calls int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
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
	if got := atomic.LoadInt32(&calls); got < 3 {
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
		{"start", http.MethodGet, "/api/connect/pim/accounts/work/google/start"},
		{"logout", http.MethodPost, "/api/connect/pim/accounts/work/logout"},
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
