package browserlogin

import (
	"cmp"
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

// Session keeps navigation and its loopback callback in the same sandbox.
type Session interface {
	RedirectURI() string
	Navigate(context.Context, string) error
	Callback(context.Context) (url.Values, error)
	Close() error
}

// LaunchOptions binds origins to an attempt rather than mutable process-wide settings.
type LaunchOptions struct{ Redirect, CockpitOrigin, StreamOrigin string }

// Launcher allocates the callback session before its consent URL is known.
type Launcher interface {
	Open(context.Context, string, LaunchOptions) (Session, error)
}

// Authorizer keeps provider credentials and exchange logic outside the browser lifetime.
type Authorizer interface {
	Start(context.Context, string, string) (string, error)
	Callback(context.Context, url.Values) error
	Cancel(owner, authURL string)
	Fail(owner, authURL, message string)
}

// Flow advertises only a live route, never the provider's consent URL or callback secrets.
type Flow struct{ Route, Status string }

// Messages lets adapters retain their existing actionable errors without exposing helper output.
type Messages struct {
	AttemptRequired, ShuttingDown, StartupTimeout, CleanupFailed, CleanupLog string
	OpenFailed, BrowserStopped, CallbackRejected, Expired                    string
}

// Options separates the identities of independent login managers and their provider policy.
type Options struct {
	Prefix, Route string
	Startup, TTL  time.Duration
	CheckOwner    func(context.Context, string) error
	Messages      Messages
}

func defaultOptions(o Options) Options {
	o.Prefix, o.Route = cmp.Or(o.Prefix, "signin-"), cmp.Or(o.Route, "/signing-in/")
	if o.Startup <= 0 {
		o.Startup = 45 * time.Second
	}
	if o.TTL <= 0 {
		o.TTL = 10 * time.Minute
	}
	if o.CheckOwner == nil {
		o.CheckOwner = checkOwner
	}
	m := o.Messages
	o.Messages = Messages{
		AttemptRequired:  cmp.Or(m.AttemptRequired, "browser login: login attempt id is required"),
		ShuttingDown:     cmp.Or(m.ShuttingDown, "browser login: shutting down"),
		StartupTimeout:   cmp.Or(m.StartupTimeout, "browser login: startup timed out; try signing in again"),
		CleanupFailed:    cmp.Or(m.CleanupFailed, "browser login: could not close the login browser; try again"),
		CleanupLog:       cmp.Or(m.CleanupLog, "Login browser cleanup failed"),
		OpenFailed:       cmp.Or(m.OpenFailed, "browser login: could not open the login browser; try signing in again"),
		BrowserStopped:   cmp.Or(m.BrowserStopped, "browser login: the login browser stopped; try signing in again"),
		CallbackRejected: cmp.Or(m.CallbackRejected, "browser login: the sign-in callback was rejected; try signing in again"),
		Expired:          cmp.Or(m.Expired, "browser login: sign-in expired; try signing in again"),
	}
	return o
}

func checkOwner(ctx context.Context, owner string) error {
	if owner == "" || strings.TrimSpace(owner) != owner || (identityctx.IdentityID(ctx) != "" && identityctx.IdentityID(ctx) != owner) {
		return errors.New("browser login: an Aura identity is required")
	}
	return ctx.Err()
}
