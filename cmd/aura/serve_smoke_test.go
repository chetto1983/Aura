//go:build serve_smoke

// serve_smoke is the Plan 24-04 Task-4 live proof against the REAL `aura serve` binary:
// WEB-02 (a non-loopback bind without web-auth fail-fasts), WEB-01/SC1 (an API path 404s
// as a real error, never the SPA shell), and WEB-03/D-03 (the SPA shell is gated, GET
// /healthz stays public). It is build-tagged OUT of the default unit suite because it
// spawns a process + needs the live stack:
//
//	go test -tags serve_smoke ./cmd/aura/ -run TestServeSmoke -count=1
//
// No-skip-as-green: the DB-dependent subtest t.Fatals under $CI when AURA_DB_URL is
// unset (a skipped smoke must never pass green); locally it skips with a hint.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// smokeEnvOrSkip mirrors the integration-tier envOrSkip discipline: t.Fatal under $CI
// when a required var is unset (a skipped smoke must not pass green), skip locally.
func smokeEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("serve_smoke requires %s, but it is unset under CI — a skipped smoke "+
				"must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("serve_smoke requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// buildSmokeBinary compiles cmd/aura into a temp dir and returns the binary path. A
// fresh build guarantees the smoke exercises the current embed + auth boundary.
func buildSmokeBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "aura-smoke")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, "./")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build aura binary: %v\n%s", err, out)
	}
	return bin
}

// freePort reserves and releases a loopback TCP port, returning a host:port that is free
// at call time (a small TOCTOU window the daemon re-binds immediately after).
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

// envWith returns os.Environ() with the given KEY=VALUE overrides applied (replacing any
// inherited value for the same key, so a .env-sourced AURA_AGUI_BIND cannot shadow the
// per-subtest bind).
func envWith(overrides map[string]string) []string {
	out := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, overridden := overrides[k]; !overridden {
			out = append(out, kv)
		}
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

// smokeClient does not follow redirects so the test can observe the 302->/login.
func smokeClient() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func smokeGet(t *testing.T, url string, browser bool, cookies ...*http.Cookie) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", url, err)
	}
	if browser {
		req.Header.Set("Accept", "text/html")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := smokeClient().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// smokeLogin POSTs the operator passphrase to /login and returns the minted session
// cookie, proving the in-binary login works end-to-end against the live identity store.
func smokeLogin(t *testing.T, base, secret string) *http.Cookie {
	t.Helper()
	form := url.Values{"passphrase": {secret}}
	req, err := http.NewRequest(http.MethodPost, base+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := smokeClient().Do(req)
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /login = %d, want 303 (a correct passphrase mints a session)", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "__Host-aura_session" {
			return c
		}
	}
	t.Fatalf("a successful login set no __Host-aura_session cookie")
	return nil
}

func waitHealthy(t *testing.T, base string, d time.Duration, stderr fmt.Stringer) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		resp, err := smokeClient().Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("daemon did not become ready at %s/healthz within %s\n--- stderr ---\n%s", base, d, stderr.String())
}

func TestServeSmoke(t *testing.T) {
	bin := buildSmokeBinary(t)

	t.Run("WEB-02 non-loopback bind without web-auth fail-fasts", func(t *testing.T) {
		// The daemon validates the DB/Neo4j secrets before GuardWebBind, so the live env
		// must be present for the bind guard (not the secret check) to be what trips.
		smokeEnvOrSkip(t, "AURA_DB_URL")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// 0.0.0.0 is non-loopback; with no secret and no trust-proxy the guard must refuse.
		cmd := exec.CommandContext(ctx, bin, "serve", "--only=cli")
		cmd.Env = envWith(map[string]string{
			"AURA_AGUI_BIND":       "0.0.0.0:0",
			"AURA_WEB_AUTH_SECRET": "",
			"AURA_WEB_TRUST_PROXY": "false",
		})
		out, err := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected a non-zero exit (WEB-02 guard), got err=%v\n%s", err, out)
		}
		if exitErr.ExitCode() == 0 {
			t.Fatalf("expected a non-zero exit, got 0\n%s", out)
		}
		body := string(out)
		if !strings.Contains(body, "AURA_WEB_AUTH_SECRET") || !strings.Contains(strings.ToLower(body), "non-loopback") {
			t.Fatalf("guard output missing the actionable env-var hint:\n%s", body)
		}
	})

	t.Run("WEB-01/03 served binary: api 404, healthz public, shell gated", func(t *testing.T) {
		// Booting the real daemon needs the live DB (no-skip-as-green under $CI).
		smokeEnvOrSkip(t, "AURA_DB_URL")
		aguiAddr := freeAddr(t)
		setupAddr := freeAddr(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "serve", "--only=cli")
		cmd.Env = envWith(map[string]string{
			"AURA_AGUI_BIND":       aguiAddr,
			"AURA_SETUP_BIND":      setupAddr,
			"AURA_SETUP_TOKEN":     "smoke-setup-token",
			"AURA_WEB_AUTH_SECRET": "smoke-secret", // loopback bind, but a secret makes RequireAuth active
		})
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start daemon: %v", err)
		}
		t.Cleanup(func() {
			cancel()
			_ = cmd.Wait()
		})

		base := "http://" + aguiAddr
		waitHealthy(t, base, 45*time.Second, &stderr)

		// --- Unauthenticated (no cookie) ---
		// WEB-03 / D-03: GET /healthz is public.
		if code, _ := smokeGet(t, base+"/healthz", false); code != http.StatusOK {
			t.Fatalf("/healthz = %d, want 200 (public)", code)
		}
		// WEB-03 / D-03: the login page stays public so it can render.
		if code, _ := smokeGet(t, base+"/login", true); code != http.StatusOK {
			t.Fatalf("GET /login = %d, want 200 (public login page)", code)
		}
		// WEB-03: the whole origin is gated — the shell redirects a browser to /login and
		// 401s an API client; a bogus /api path is gated too (401, NOT the SPA shell — the
		// gate fires before routing so it never reveals which API routes exist).
		if code, _ := smokeGet(t, base+"/", true); code != http.StatusFound {
			t.Fatalf("GET / (browser) = %d, want 302 -> /login (gated)", code)
		}
		if code, _ := smokeGet(t, base+"/", false); code != http.StatusUnauthorized {
			t.Fatalf("GET / (api) = %d, want 401 (gated)", code)
		}
		if code, body := smokeGet(t, base+"/api/nope", false); code != http.StatusUnauthorized {
			t.Fatalf("/api/nope (no cookie) = %d, want 401 (gated)", code)
		} else if strings.Contains(body, `<div id="root"`) {
			t.Fatalf("/api/nope leaked the SPA shell")
		}

		// --- Authenticated (login mints a cookie) ---
		cookie := smokeLogin(t, base, "smoke-secret")
		// WEB-01 / SC1: an AUTHENTICATED bogus /api path 404s as a real error, never the
		// SPA shell — the gate let the principal through, then the SPA-fallback 404s it.
		if code, body := smokeGet(t, base+"/api/nope", false, cookie); code != http.StatusNotFound {
			t.Fatalf("/api/nope (authenticated) = %d, want 404 (real API error)", code)
		} else if strings.Contains(body, `<div id="root"`) {
			t.Fatalf("/api/nope (authenticated) leaked the SPA shell instead of a real 404")
		}
		// The authenticated shell renders (200) with the SPA root.
		if code, body := smokeGet(t, base+"/", true, cookie); code != http.StatusOK {
			t.Fatalf("GET / (authenticated) = %d, want 200 (the cockpit shell)", code)
		} else if !strings.Contains(body, `<div id="root"`) {
			t.Fatalf("authenticated GET / did not serve the SPA shell")
		}
	})
}
