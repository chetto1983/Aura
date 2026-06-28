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
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	authulamodels "github.com/Authula/authula/models"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/webauth"
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

type smokeAuthConfig struct {
	Provider       string `json:"provider"`
	AuthBasePath   string `json:"auth_base_path"`
	CSRFHeaderName string `json:"csrf_header_name"`
	CSRFToken      string `json:"csrf_token"`
}

func seedSmokeAuthulaOperator(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbURL := smokeEnvOrSkip(t, "AURA_DB_URL")
	email := smokeEnvOrSkip(t, "AURA_E2E_AUTHULA_EMAIL")
	password := smokeEnvOrSkip(t, "AURA_E2E_AUTHULA_PASSWORD")
	secret := smokeEnvOrSkip(t, "AURA_AUTHULA_SECRET")

	pool, err := db.Open(ctx, &db.Config{URL: dbURL})
	if err != nil {
		t.Fatalf("open smoke db: %v", err)
	}
	defer pool.Close()

	local, err := identity.New(pool).GetIdentityByName(ctx, "local")
	if err != nil {
		t.Fatalf("resolve local identity: %v", err)
	}

	dsn := strings.TrimSpace(os.Getenv("AURA_AUTHULA_DATABASE_URL"))
	if dsn == "" {
		dsn = dbURL
	}
	provider, err := webauth.New(webauth.Config{
		DSN:            dsn,
		Secret:         secret,
		TrustedOrigins: []string{"http://127.0.0.1:9080", "https://127.0.0.1:9080"},
	})
	if err != nil {
		t.Fatalf("open smoke Authula provider: %v", err)
	}
	defer func() { _ = provider.Close() }()

	core := provider.CoreServices()
	user, err := core.UserService.GetByEmail(ctx, email)
	if err != nil {
		t.Fatalf("lookup smoke Authula user: %v", err)
	}
	if user == nil {
		user, err = core.UserService.Create(ctx, email, email, true, nil, nil)
		if err != nil {
			t.Fatalf("create smoke Authula user: %v", err)
		}
	}

	hash, err := core.PasswordService.Hash(password)
	if err != nil {
		t.Fatalf("hash smoke password: %v", err)
	}
	account, err := core.AccountService.GetByUserIDAndProvider(ctx, user.ID, authulamodels.AuthProviderEmail.String())
	if err != nil {
		t.Fatalf("lookup smoke Authula account: %v", err)
	}
	if account == nil {
		if _, err := core.AccountService.Create(ctx, user.ID, email, authulamodels.AuthProviderEmail.String(), &hash); err != nil {
			t.Fatalf("create smoke Authula account: %v", err)
		}
	} else {
		account.AccountID = email
		account.Password = &hash
		if _, err := core.AccountService.Update(ctx, account); err != nil {
			t.Fatalf("update smoke Authula account: %v", err)
		}
	}
	if err := webauth.NewIdentityLinker(pool).LinkOperator(ctx, local.ID, user.ID); err != nil {
		t.Fatalf("link smoke Authula operator: %v", err)
	}
}

func smokeAuthulaLogin(t *testing.T, base string) *http.Cookie {
	t.Helper()
	email := smokeEnvOrSkip(t, "AURA_E2E_AUTHULA_EMAIL")
	password := smokeEnvOrSkip(t, "AURA_E2E_AUTHULA_PASSWORD")

	cfgReq, err := http.NewRequest(http.MethodGet, base+"/api/auth/config", nil)
	if err != nil {
		t.Fatalf("new auth config request: %v", err)
	}
	cfgResp, err := smokeClient().Do(cfgReq)
	if err != nil {
		t.Fatalf("GET /api/auth/config: %v", err)
	}
	defer func() { _ = cfgResp.Body.Close() }()
	if cfgResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/auth/config = %d, want 200", cfgResp.StatusCode)
	}
	var cfg smokeAuthConfig
	if err := json.NewDecoder(cfgResp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode auth config: %v", err)
	}
	if cfg.Provider != "authula" {
		t.Fatalf("auth config provider = %q, want authula", cfg.Provider)
	}
	if cfg.AuthBasePath == "" {
		cfg.AuthBasePath = "/auth"
	}
	if cfg.CSRFHeaderName == "" {
		cfg.CSRFHeaderName = webauth.CSRFHeaderName
	}
	if cfg.CSRFToken == "" {
		cfg.CSRFToken = cfgResp.Header.Get(cfg.CSRFHeaderName)
	}
	if cfg.CSRFToken == "" {
		t.Fatal("auth config did not return a CSRF token")
	}

	payload, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, err := http.NewRequest(http.MethodPost, base+cfg.AuthBasePath+"/email-password/sign-in", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new Authula sign-in request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(cfg.CSRFHeaderName, cfg.CSRFToken)
	req.Header.Set("Origin", base)
	for _, c := range cfgResp.Cookies() {
		req.AddCookie(c)
	}
	resp, err := smokeClient().Do(req)
	if err != nil {
		t.Fatalf("POST /auth/email-password/sign-in: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /auth/email-password/sign-in = %d, want 200\n%s", resp.StatusCode, body)
	}
	cookies := append([]*http.Cookie{}, cfgResp.Cookies()...)
	cookies = append(cookies, resp.Cookies()...)
	var signIn struct {
		TOTPRedirect bool `json:"totp_redirect"`
	}
	_ = json.Unmarshal(body, &signIn)
	if signIn.TOTPRedirect {
		cookies = append(cookies, smokeVerifyTOTP(t, base, cfg, cookies)...)
	}
	return smokeSessionCookie(t, cookies)
}

func smokeVerifyTOTP(t *testing.T, base string, cfg smokeAuthConfig, cookies []*http.Cookie) []*http.Cookie {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"code": smokeAuthulaTOTPCode(t), "trust_device": false})
	req, err := http.NewRequest(http.MethodPost, base+cfg.AuthBasePath+"/totp/verify", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new Authula TOTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(cfg.CSRFHeaderName, cfg.CSRFToken)
	req.Header.Set("Origin", base)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := smokeClient().Do(req)
	if err != nil {
		t.Fatalf("POST /auth/totp/verify: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /auth/totp/verify = %d, want 200\n%s", resp.StatusCode, body)
	}
	return resp.Cookies()
}

func smokeSessionCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, c := range cookies {
		if c.Name == webauth.SessionCookieName {
			return c
		}
	}
	t.Fatalf("a successful Authula login set no %s cookie", webauth.SessionCookieName)
	return nil
}

func smokeAuthulaTOTPCode(t *testing.T) string {
	t.Helper()
	if code := strings.TrimSpace(os.Getenv("AURA_E2E_AUTHULA_TOTP_CODE")); code != "" {
		return code
	}
	secret := smokeEnvOrSkip(t, "AURA_E2E_AUTHULA_TOTP_SECRET")
	normalized := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	normalized = strings.TrimRight(normalized, "=")
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		t.Fatalf("decode TOTP secret: %v", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		(uint32(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", bin%1000000)
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

	t.Run("non-loopback bind without Authula config fail-fasts", func(t *testing.T) {
		// The daemon validates DB/Neo4j secrets before the auth composition root, so
		// the live env must be present for the Authula config check to be what trips.
		smokeEnvOrSkip(t, "AURA_DB_URL")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// 0.0.0.0 is non-loopback; without Authula config the daemon must refuse.
		cmd := exec.CommandContext(ctx, bin, "serve", "--only=cli")
		cmd.Env = envWith(map[string]string{
			"AURA_AGUI_BIND":       "0.0.0.0:0",
			"AURA_AUTHULA_SECRET":  "",
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
		if !strings.Contains(body, "AURA_AUTHULA_SECRET") {
			t.Fatalf("guard output missing the actionable env-var hint:\n%s", body)
		}
	})

	t.Run("WEB-01/03 served binary: api 404, healthz public, shell gated", func(t *testing.T) {
		// Booting the real daemon needs the live DB (no-skip-as-green under $CI).
		smokeEnvOrSkip(t, "AURA_DB_URL")
		seedSmokeAuthulaOperator(t)
		aguiAddr := freeAddr(t)
		setupAddr := freeAddr(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "serve", "--only=cli")
		cmd.Env = envWith(map[string]string{
			"AURA_AGUI_BIND":   aguiAddr,
			"AURA_SETUP_BIND":  setupAddr,
			"AURA_SETUP_TOKEN": "smoke-setup-token",
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

		// --- Authenticated (Authula login mints a cookie) ---
		cookie := smokeAuthulaLogin(t, base)
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
