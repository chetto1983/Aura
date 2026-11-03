package main

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/browserlogin"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

type mcpSignInTestLauncher struct {
	opens   atomic.Int32
	options chan browserlogin.LaunchOptions
}

func (l *mcpSignInTestLauncher) Open(_ context.Context, _ string, opts browserlogin.LaunchOptions) (browserlogin.Session, error) {
	l.opens.Add(1)
	l.options <- opts
	return mcpSignInTestSession{opts.Redirect}, nil
}

type mcpSignInTestSession struct{ redirect string }

func (s mcpSignInTestSession) RedirectURI() string                  { return s.redirect }
func (mcpSignInTestSession) Navigate(context.Context, string) error { return nil }
func (mcpSignInTestSession) Callback(ctx context.Context) (url.Values, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (mcpSignInTestSession) Close() error { return nil }

func mcpSignInFixture(t *testing.T, redirect string) (context.Context, *mcpAuthService, *mcpSignInTestLauncher, *signInPreflightBackend, <-chan string) {
	t.Helper()
	withMemoryMCPRegistry(t)
	var env []string
	if redirect != "" {
		env = []string{"MCP_OAUTH_REDIRECT_URL=" + redirect, "MCP_OAUTH_CLIENT_ID=registered-fixture"}
	}
	seedMCPRegistry(t, mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{"canva": {Type: mcp.ServerTypeStreamableHTTP, URL: "https://provider.example/mcp", Env: env}}})
	seen := make(chan string, 4)
	store := &authorizationStateStore{}
	flows, err := mcp.NewFlows(store, func(ctx context.Context, _ string, _ mcp.ManagedServer, opts mcp.SessionOptions) (io.Closer, error) {
		seen <- opts.OAuth.RedirectURL
		_, err := opts.OAuth.Fetcher(ctx, &auth.AuthorizationArgs{URL: "https://provider.example/authorize?state=fixture-state"})
		return nil, err
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(flows.Close)
	launcher := &mcpSignInTestLauncher{options: make(chan browserlogin.LaunchOptions, 4)}
	login := browserlogin.New(t.Context(), launcher, browserlogin.Options{})
	t.Cleanup(func() {
		if err := login.Close(); err != nil {
			t.Error(err)
		}
	})
	backend := &signInPreflightBackend{present: true, df: "Avail\n9000000000\n"}
	browser := &sandboxSignInBrowser{router: usersandbox.NewSandboxRouter(backend, config.SandboxConfig{}), image: "desktop:fixture", sidecarImage: "sidecar:fixture"}
	service := &mcpAuthService{store: store, flows: flows, signIn: &signInRuntime{login: login, browser: browser, port: 8444}}
	return identityctx.WithIdentityID(t.Context(), "alice"), service, launcher, backend, seen
}

func TestStartAuthorizationGoesThroughTheContainer(t *testing.T) {
	ctx, service, launcher, _, seen := mcpSignInFixture(t, "")
	result, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "https://aura.test"})
	if err != nil {
		t.Fatal(err)
	}
	if <-seen != mcp.LoopbackCallback || !strings.HasPrefix(result.SignInURL, "/signing-in/signin-") || !strings.Contains(result.SignInURL, "?flow=") {
		t.Fatalf("start=%+v", result)
	}
	opts := <-launcher.options
	if opts.Redirect != mcp.LoopbackCallback || opts.CockpitOrigin != "https://aura.test" || opts.StreamOrigin != "https://aura.test:8444" {
		t.Fatalf("launch=%+v", opts)
	}
}

func TestAPreRegisteredClientKeepsItsRedirect(t *testing.T) {
	ctx, service, launcher, _, seen := mcpSignInFixture(t, "http://127.0.0.1:9100/cb")
	if _, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "https://aura.test"}); err != nil {
		t.Fatal(err)
	}
	if <-seen != "http://127.0.0.1:9100/cb" || (<-launcher.options).Redirect != "http://127.0.0.1:9100/cb" {
		t.Fatal("registered redirect changed")
	}
}

func TestAnHTTPSRedirectGoesToThePasteBack(t *testing.T) {
	ctx, service, launcher, _, seen := mcpSignInFixture(t, "https://registered.example/cb")
	result, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "https://aura.test"})
	if err != nil || result.Via != "paste" || result.SignInURL != "" || launcher.opens.Load() != 0 || <-seen != "https://registered.example/cb" {
		t.Fatalf("paste=%+v err=%v", result, err)
	}
}

func TestANonSecureCockpitGoesToThePasteBack(t *testing.T) {
	ctx, service, launcher, _, _ := mcpSignInFixture(t, "")
	result, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "http://192.168.1.5"})
	if err != nil || result.Via != "paste" || launcher.opens.Load() != 0 {
		t.Fatalf("paste=%+v err=%v", result, err)
	}
}

func TestACrossSchemeDesktopOriginGoesToThePasteBack(t *testing.T) {
	for _, tc := range []struct {
		desktop string
		paste   bool
	}{{"", true}, {"http://127.0.0.1:8444", false}} {
		t.Run(tc.desktop, func(t *testing.T) {
			ctx, service, launcher, _, _ := mcpSignInFixture(t, "")
			service.signIn.origin = tc.desktop
			result, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "http://127.0.0.1:8080"})
			if err != nil || (result.Via == "paste") != tc.paste || (launcher.opens.Load() == 0) != tc.paste {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestASeparateHostOrTunnelGoesToThePasteBack(t *testing.T) {
	for _, tc := range []struct {
		desktop string
		remote  bool
	}{{"https://views.aura.test", false}, {"", true}} {
		ctx, service, launcher, _, _ := mcpSignInFixture(t, "")
		service.signIn.origin = tc.desktop
		result, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "https://aura.test", Remote: tc.remote})
		if err != nil || result.Via != "paste" || launcher.opens.Load() != 0 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}

func TestAShortDiskAnswersWithTheReasonAndThePasteBack(t *testing.T) {
	ctx, service, launcher, backend, _ := mcpSignInFixture(t, "")
	backend.present = false
	backend.df = "Avail\n1\n"
	result, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "https://aura.test"})
	if err != nil || result.Via != "paste" || !strings.Contains(result.Reason, "8 GiB") || launcher.opens.Load() != 0 || len(backend.admitted) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAMissingImagePreparesBeforeStartingAnyConsent(t *testing.T) {
	ctx, service, launcher, backend, seen := mcpSignInFixture(t, "")
	backend.present = false
	result, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "https://aura.test"})
	if err != nil || !result.Preparing || result.ID != "" || launcher.opens.Load() != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	select {
	case <-seen:
		t.Fatal("consent started before image approval")
	default:
	}
}

func TestAFailedReauthorizationKeepsTheStoredGrant(t *testing.T) {
	ctx, service, _, _, _ := mcpSignInFixture(t, "")
	store := service.store.(*authorizationStateStore)
	store.grant.RefreshToken = "old-healthy-refresh"
	result, err := service.StartSignIn(ctx, "alice", "canva", agui.SignInRequest{Origin: "https://aura.test"})
	if err != nil {
		t.Fatal(err)
	}
	route, _ := url.Parse(result.SignInURL)
	if err = service.signIn.login.Cancel(ctx, "alice", route.Path); err != nil {
		t.Fatal(err)
	}
	grant, err := store.Load(ctx, "canva")
	if err != nil || grant.RefreshToken != "old-healthy-refresh" {
		t.Fatal("cancel overwrote the stored grant")
	}
	if _, err = service.flows.Status("alice", result.ID); !errors.Is(err, mcp.ErrNoSuchFlow) {
		t.Fatalf("cancelled flow still active: %v", err)
	}
}

func TestMCPAuthorizerReportsAProviderRefusal(t *testing.T) {
	ctx, service, _, _, _ := mcpSignInFixture(t, "")
	flow, err := service.StartLoopbackAuthorization(ctx, "alice", "canva")
	if err != nil {
		t.Fatal(err)
	}
	a := &mcpSignIn{flows: service.flows, flow: flow, owner: "alice"}
	err = a.Callback(ctx, url.Values{"state": {"fixture-state"}, "error": {"access_denied"}, "error_description": {"fixture refusal"}})
	if err == nil {
		t.Fatal("provider refusal was reported as successful consent")
	}
	state, statusErr := service.flows.Status("alice", flow.ID)
	if statusErr != nil || state.Status != mcp.FlowError || !strings.Contains(state.Error, "access_denied") {
		t.Fatalf("state=%+v err=%v", state, statusErr)
	}
}
