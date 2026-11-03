package main

import (
	"context"
	"errors"
	"net/url"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/browserlogin"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

func (m *mcpAuthService) StartSignIn(ctx context.Context, owner, name string, request agui.SignInRequest) (agui.MCPSignInResult, error) {
	if owner == "" || identityctx.IdentityID(ctx) != owner {
		return agui.MCPSignInResult{}, errors.New("sign-in needs an Aura identity")
	}
	server, err := authorizationServer(name)
	if err != nil {
		return agui.MCPSignInResult{}, err
	}
	settings, err := mcp.OAuthSettingsFromEnv(server.Env)
	if err != nil {
		return agui.MCPSignInResult{}, err
	}
	var reason string
	if m.signIn == nil {
		reason = "The sign-in desktop is unavailable; use your own browser"
	} else if !settings.RedirectIsLoopback() {
		reason = "This registered redirect uses your own browser"
	}
	var stream string
	if reason == "" {
		stream, err = signInStreamOrigin(request, m.signIn.origin, m.signIn.port)
		if err != nil {
			reason = err.Error()
		}
	}
	if reason == "" {
		_, err = m.signIn.browser.preflight(ctx)
		if errors.Is(err, errSignInImageMissing) {
			return agui.MCPSignInResult{Preparing: true, Reason: err.Error()}, nil
		}
		if err != nil {
			reason = err.Error()
		}
	}
	if reason != "" {
		return m.pasteSignIn(ctx, owner, name, reason)
	}
	flow, err := m.flows.Start(ctx, owner, name, server, settings.SignInRedirect())
	if err != nil {
		return agui.MCPSignInResult{}, err
	}
	if flow.Status != mcp.FlowAuthorizationRequired {
		return agui.MCPSignInResult{Flow: flow}, nil
	}
	authorizer := &mcpSignIn{flows: m.flows, flow: flow, owner: owner}
	started, err := m.signIn.login.Start(ctx, owner, flow.ID, browserlogin.LaunchOptions{Redirect: flow.RedirectURI, CockpitOrigin: request.Origin, StreamOrigin: stream}, authorizer)
	if err != nil {
		if ctx.Err() != nil {
			return agui.MCPSignInResult{}, ctx.Err()
		}
		return m.pasteSignIn(ctx, owner, name, "The sign-in desktop could not start; use your own browser")
	}
	result := agui.MCPSignInResult{Flow: flow}
	if started.Route != "" {
		result.SignInURL = started.Route + "?" + url.Values{"flow": {flow.ID}}.Encode()
	}
	return result, nil
}

func (m *mcpAuthService) pasteSignIn(ctx context.Context, owner, name, reason string) (agui.MCPSignInResult, error) {
	flow, err := m.StartLoopbackAuthorization(ctx, owner, name)
	return agui.MCPSignInResult{Flow: flow, Via: "paste", Reason: reason}, err
}

type mcpSignIn struct {
	flows *mcp.Flows
	flow  mcp.Flow
	owner string
}

func (a *mcpSignIn) Start(_ context.Context, owner, redirect string) (string, error) {
	if owner != a.owner || redirect != a.flow.RedirectURI {
		return "", errors.New("sign-in flow changed")
	}
	return a.flow.AuthorizationURL, nil
}

func (a *mcpSignIn) Callback(_ context.Context, query url.Values) error {
	if refused := query.Get("error"); refused != "" {
		refusal := errors.New(refused + ": " + query.Get("error_description"))
		_, err := a.flows.Fail(query.Get("state"), refusal)
		return errors.Join(refusal, err)
	}
	_, err := a.flows.Complete(query.Get("state"), query.Get("code"), query.Get("iss"))
	return err
}

func (a *mcpSignIn) Cancel(owner, _ string) {
	if owner == a.owner {
		_ = a.flows.Cancel(owner, a.flow.ID)
	}
}
func (a *mcpSignIn) Fail(owner, _, message string) {
	if owner != a.owner {
		return
	}
	u, err := url.Parse(a.flow.AuthorizationURL)
	if err == nil {
		_, _ = a.flows.Fail(u.Query().Get("state"), errors.New(message))
	}
}
