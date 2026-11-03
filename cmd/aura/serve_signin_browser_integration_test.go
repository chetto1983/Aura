//go:build docker_integration

package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/browserlogin"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/moby/moby/client"
)

func TestClosingTheSignInPageEndsTheFlowAndItsContainers(t *testing.T) {
	egressITDockerdOrGate(t)
	ctx, service, _, _, _ := mcpSignInFixture(t, "")
	if err := service.signIn.login.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.LoadDB()
	cfg.Profile = config.ProfileSingleUserHardened
	cfg.AuthulaSecret = strings.Repeat("a", 64)
	cfg.Sandbox.BrowserMaxActive = 2
	router := buildSandboxRouter(cfg, nil)
	browser := &sandboxSignInBrowser{router: router, image: cfg.Sandbox.SignInBrowserImage, sidecarImage: cfg.Sandbox.Image, policy: usersandbox.EgressPolicy{Floor: true}}
	login := browserlogin.New(t.Context(), browser, browserlogin.Options{})
	browser.owns, browser.cancel = login.Owns, login.Cancel
	service.signIn = &signInRuntime{login: login, browser: browser, port: 8444}
	t.Cleanup(service.Close)
	started, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "https://aura.test:8443"})
	if err != nil || started.SignInURL == "" {
		t.Fatalf("desktop start=%+v err=%v", started, err)
	}
	u, err := url.Parse(started.SignInURL)
	if err != nil {
		t.Fatal(err)
	}
	session := strings.TrimPrefix(u.Path, "/signing-in/")
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	list := func() int {
		items, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", "aura.signin="+session)})
		if err != nil {
			t.Fatal(err)
		}
		return len(items.Items)
	}
	if count := list(); count != 3 {
		t.Fatalf("sign-in containers=%d", count)
	}
	s := agui.NewServer(nil, nil, agui.ServerConfig{})
	s.SetSignInBrowser(browser)
	r := httptest.NewRequest("DELETE", "https://aura.test:8443/api/sign-in/"+session, nil)
	r = r.WithContext(identityctx.WithIdentityID(r.Context(), "alice"))
	r.Header.Set("Idempotency-Key", "close-fixture")
	w := httptest.NewRecorder()
	s.Mux().ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("close=%d %s", w.Code, w.Body)
	}
	if _, err = service.AuthorizationFlow("alice", started.ID); !errors.Is(err, mcp.ErrNoSuchFlow) {
		t.Fatalf("flow survived=%v", err)
	}
	if count := list(); count != 0 {
		t.Fatalf("sign-in containers survived=%d", count)
	}
}

func TestSignInCallbackListenerAcceptsOnlyItsBoundState(t *testing.T) {
	ctx, router, handle := liveBox(t)
	for _, redirect := range []string{
		"http://127.0.0.1:43121/aura/mcp/oauth/callback",
		"http://localhost:43121/aura/mcp/oauth/callback",
		"http://[::1]:43121/aura/mcp/oauth/callback",
		"http://127.0.0.1/aura/mcp/oauth/callback",
		"http://127.0.0.1:80/aura/mcp/oauth/callback",
		"http://localhost/aura/mcp/oauth/callback",
		"http://localhost:80/aura/mcp/oauth/callback",
		"http://[::1]/aura/mcp/oauth/callback",
		"http://[::1]:80/aura/mcp/oauth/callback",
	} {
		t.Run(redirect, func(t *testing.T) {
			browser := &sandboxSignInBrowser{router: router}
			session := &signInBrowserSession{browser: browser, session: "signin-0123456789abcdef01234567", opts: browserlogin.LaunchOptions{Redirect: redirect}}
			t.Cleanup(func() {
				if err := session.Close(); err != nil {
					t.Error(err)
				}
			})
			const state = "synthetic-state.relayed-target"
			if err := session.startCallback(ctx, usersandbox.SignInBox{Sidecar: handle}, state); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{"state=other&code=fixture-code", "state=" + state + "&state=other&code=fixture-code"} {
				probe := callbackHTTPProbe(t, ctx, router, handle, redirect+"?"+query)
				if probe != "400" {
					t.Fatalf("unmatched callback = %s", probe)
				}
			}
			query := url.Values{"state": {state}, "code": {"fixture-code"}}
			if probe := callbackHTTPProbe(t, ctx, router, handle, redirect+"?"+query.Encode()); probe != "200" {
				t.Fatalf("matched callback = %s", probe)
			}
			wait, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			got, err := session.Callback(wait)
			if err != nil || got.Get("state") != state || got.Get("code") != "fixture-code" {
				t.Fatalf("callback IPC failed: %v", err)
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func callbackHTTPProbe(t *testing.T, ctx context.Context, router *usersandbox.SandboxRouter, h usersandbox.BoxHandle, address string) string {
	t.Helper()
	res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Env: []string{"CALLBACK_PROBE=" + address}, Command: `node -e 'require("node:http").get(process.env.CALLBACK_PROBE,r=>{console.log(r.statusCode);r.resume()}).on("error",e=>{console.error(e.code,e.address);process.exit(1)})'`})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("callback HTTP probe: %v, exit %d, %s", err, res.ExitCode, res.Stderr)
	}
	return strings.TrimSpace(string(res.Stdout))
}
