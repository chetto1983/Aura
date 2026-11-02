package chatgptplan

import (
	"context"

	"github.com/chetto1983/aura/internal/browserlogin"
)

// BrowserLogin retains ChatGPT's public flow while sharing the browser lifetime machinery.
type BrowserLogin struct {
	login      *browserlogin.Login
	authorizer browserAuthorizer
}

// NewBrowserLogin retains ChatGPT's private prefix and cancels its sessions with the root.
func NewBrowserLogin(root context.Context, service *Service, launcher browserlogin.Launcher) *BrowserLogin {
	login := browserlogin.New(root, launcher, browserlogin.Options{
		Prefix: "chatgpt-", Route: "/browser/", TTL: flowTTL, CheckOwner: checkOwner,
		Messages: browserlogin.Messages{
			AttemptRequired:  "chatgpt plan: login attempt id is required",
			ShuttingDown:     "chatgpt plan: browser login is shutting down",
			StartupTimeout:   "chatgpt plan: browser startup timed out; try signing in again",
			CleanupFailed:    "chatgpt plan: could not close the login browser; try again",
			CleanupLog:       "ChatGPT login browser cleanup failed",
			OpenFailed:       "chatgpt plan: could not open the login browser; try signing in again",
			BrowserStopped:   "chatgpt plan: the login browser stopped; try signing in again",
			CallbackRejected: "chatgpt plan: the sign-in callback was rejected; try signing in again",
			Expired:          "chatgpt plan: sign-in expired; try signing in again",
		},
	})
	return &BrowserLogin{login: login, authorizer: browserAuthorizer{service}}
}

// Start preserves auth_url on the wire while ChatGPT chooses its callback address.
func (b *BrowserLogin) Start(ctx context.Context, owner, attempt string) (Flow, error) {
	flow, err := b.login.Start(ctx, owner, attempt, browserlogin.LaunchOptions{}, b.authorizer)
	return Flow{AuthURL: flow.Route, Status: flow.Status}, err
}

// Owns protects the private session before a trusted relay reaches the browser.
func (b *BrowserLogin) Owns(ctx context.Context, owner, session string) bool {
	return b.login.Owns(ctx, owner, session)
}

// Cancel compares routes so an old popup cannot close its replacement.
func (b *BrowserLogin) Cancel(ctx context.Context, owner, route string) error {
	return b.login.Cancel(ctx, owner, route)
}

// Close joins browser cleanup before the credential adapter shuts down.
func (b *BrowserLogin) Close() error { return b.login.Close() }
