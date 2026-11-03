//go:build docker_integration

package main

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/browserlogin"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func TestSignInCallbackListenerAcceptsOnlyItsBoundState(t *testing.T) {
	ctx, router, handle := liveBox(t)
	for _, host := range []string{"127.0.0.1", "localhost", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			redirect := "http://" + host + ":43121/aura/mcp/oauth/callback"
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
