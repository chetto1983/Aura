package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/mcpoauth"
)

// mcp_login.go is the human half of the authorization flow. Everything else in the OAuth
// path runs unattended and fails with ErrOAuthAuthorizationRequired precisely because
// nobody can answer a consent screen from a container start-up; this is the command that
// answers it once, so every later mount reads the stored grant instead.
//
// The flow is driven by OPENING A REAL SESSION rather than by calling the SDK's
// authorization code directly. That is deliberate: the 401 that triggers authorization is
// the server's own, the token that comes back is proven against the server that issued
// it, and a login that reports success has actually completed a handshake. A flow driven
// in isolation can only prove that an authorization server said yes.

const (
	// mcpOAuthCallbackPath is where the redirect lands when Aura chooses the address
	// itself. The one-shot listener accepts any path, so this is a label for the human
	// reading the URL rather than a route.
	mcpOAuthCallbackPath = "/aura/mcp/oauth/callback"

	// mcpLoginTimeout bounds the whole flow, including the time a human spends in a
	// browser. Long enough to find a password manager, short enough that a forgotten
	// terminal does not hold a listener open forever.
	mcpLoginTimeout = 5 * time.Minute
)

func mcpLogin(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: aura mcp login <server>")
	}
	name := args[0]
	server, ok, err := effectiveManagedMCPServer(name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("MCP server %q is not configured or is disabled", name)
	}
	settings, err := mcp.OAuthSettingsFromEnv(server.Env)
	if err != nil {
		return err
	}
	if !mcp.UsesOAuth(server, settings) {
		return fmt.Errorf("%q takes no authorization flow: %s", name, noOAuthReason(server, settings))
	}
	ctx, store, err := mcpOAuthStore(ctx, pool)
	if err != nil {
		return err
	}
	flow, err := newLoopbackFlow(out, settings.RedirectURL)
	if err != nil {
		return err
	}
	defer func() { _ = flow.Close() }()

	ctx, cancel := context.WithTimeout(ctx, mcpLoginTimeout)
	defer cancel()

	opts := cliSessionOptions()
	opts.OAuth = mcp.OAuthOptions{Store: store, Fetcher: flow.fetch, RedirectURL: flow.redirect}
	session, err := mcp.OpenSDKSession(ctx, name, server, runtimeMCPEgressPolicy(server), opts)
	if err != nil {
		return fmt.Errorf("authorizing %q: %w", name, err)
	}
	defer func() { _ = session.Close() }()

	// The session opening is not proof on its own: a server that needs no authorization
	// connects happily and no flow ever runs. Reporting "authorized" there would be a
	// lie the operator only discovers when a second identity cannot reach the server.
	if _, err := store.Load(ctx, name); errors.Is(err, mcpoauth.ErrNoGrant) {
		return writef(out, "%s connected without asking for authorization — nothing was stored\n", name)
	} else if err != nil {
		return err
	}
	return writef(out, "authorized %s for this identity\n", name)
}

// noOAuthReason explains a refusal in the operator's terms. "Takes no authorization flow"
// on its own sends someone reading config files for the reason we already know.
func noOAuthReason(server mcp.ManagedServer, settings mcp.OAuthSettings) string {
	switch {
	case settings.Disabled:
		return "MCP_OAUTH_DISABLED is set for it"
	case strings.TrimSpace(server.URL) == "" || strings.TrimSpace(server.Command) != "":
		return "it is a local (stdio) server, and there is no HTTP request to authorize"
	default:
		return "it already carries a static MCP_BEARER_TOKEN, which takes precedence"
	}
}

func mcpLogout(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: aura mcp logout <server>")
	}
	name := args[0]
	ctx, store, err := mcpOAuthStore(ctx, pool)
	if err != nil {
		return err
	}
	removed, err := store.Delete(ctx, name)
	if err != nil {
		return err
	}
	if !removed {
		return writef(out, "no stored authorization for %s\n", name)
	}
	return writef(out, "removed the stored authorization for %s\n", name)
}

// mcpAuthorizations lists what this identity has authorized. It is also the escape hatch
// for a stale dynamic client registration: a provider that starts answering invalid_client
// is fixed by `aura mcp logout` followed by `aura mcp login`, and knowing what is stored is
// the first step.
func mcpAuthorizations(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: aura mcp authorizations")
	}
	ctx, store, err := mcpOAuthStore(ctx, pool)
	if err != nil {
		return err
	}
	entries, err := store.List(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return writef(out, "this identity has authorized no MCP servers\n")
	}
	if err := writef(out, "name\tresource\taccess token expires\tupdated\n"); err != nil {
		return err
	}
	for _, e := range entries {
		expiry := "never advertised"
		if !e.ExpiresAt.IsZero() {
			expiry = e.ExpiresAt.UTC().Format(time.RFC3339)
		}
		if err := writef(out, "%s\t%s\t%s\t%s\n", e.ServerName, e.ResourceURL, expiry, e.UpdatedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

// mcpOAuthStore opens the grant store and puts the operator's identity on the context.
// Both halves are required: the store scopes every row by the identity Postgres reads
// from app.current_identity, so a call without one is refused rather than silently
// reading somebody else's grant.
func mcpOAuthStore(ctx context.Context, pool *pgxpool.Pool) (context.Context, *mcpoauth.Store, error) {
	if pool == nil {
		return nil, nil, errors.New("this command needs Postgres")
	}
	cfg := config.LoadDB()
	store, err := mcpoauth.NewStore(pool, cfg.AuthulaSecret)
	if err != nil {
		return nil, nil, err
	}
	identityID, err := identityctx.OperatorIdentity(ctx, pool)
	if err != nil {
		return nil, nil, err
	}
	return identityctx.WithIdentityID(ctx, identityID), store, nil
}

// loopbackFlow receives the authorization redirect on a listener bound to loopback.
//
// The listener is bound BEFORE the flow starts, not when the redirect arrives, because
// its port is part of the authorization request: with dynamic registration it is
// registered as the client's redirect URI, and a port chosen later would not be the one
// the authorization server sends the human back to.
type loopbackFlow struct {
	listener net.Listener
	redirect string
	out      io.Writer
}

// newLoopbackFlow binds the address the redirect will land on.
//
// An operator-configured MCP_OAUTH_REDIRECT_URL is used VERBATIM and its port is bound
// as given: with a pre-registered client (Slack, GitHub) the redirect URI has to match
// the one registered in the provider's app settings byte for byte, so choosing our own
// would fail at the provider with an unhelpful error. With nothing configured the port is
// ephemeral, which is correct for dynamic registration — the URI is registered during
// this very flow, so any free port is as good as another.
func newLoopbackFlow(out io.Writer, configured string) (*loopbackFlow, error) {
	addr := "127.0.0.1:0"
	if strings.TrimSpace(configured) != "" {
		u, err := url.Parse(strings.TrimSpace(configured))
		if err != nil {
			return nil, fmt.Errorf("MCP_OAUTH_REDIRECT_URL is not a URL: %w", err)
		}
		if u.Port() == "" {
			return nil, fmt.Errorf("MCP_OAUTH_REDIRECT_URL needs an explicit port to receive the redirect on, got %q", configured)
		}
		host := u.Hostname()
		if host == "" {
			host = "127.0.0.1"
		}
		addr = net.JoinHostPort(host, u.Port())
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("cannot receive the authorization redirect on %s: %w", addr, err)
	}
	redirect := strings.TrimSpace(configured)
	if redirect == "" {
		redirect = "http://" + listener.Addr().String() + mcpOAuthCallbackPath
	}
	return &loopbackFlow{listener: listener, redirect: redirect, out: out}, nil
}

func (f *loopbackFlow) Close() error { return f.listener.Close() }

// fetch prints the consent URL and waits for the redirect.
//
// It prints rather than opening a browser: Aura runs in a container, where launching one
// either fails or opens it on the wrong machine. The State and Iss values are relayed
// EXACTLY as received and never invented — the SDK generated the state and compares it
// (authorization_code.go:594), so a fetcher that dropped it would turn CSRF protection
// into a "state mismatch" bug report.
func (f *loopbackFlow) fetch(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	results := make(chan *auth.AuthorizationResult, 1)
	failures := make(chan error, 1)

	server := &http.Server{
		Handler:           http.HandlerFunc(f.handleCallback(results, failures)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = server.Serve(f.listener) }()
	defer func() { _ = server.Close() }()

	if err := writef(f.out, "\nopen this URL to authorize, then come back:\n\n%s\n\nwaiting for the redirect on %s ...\n", args.URL, f.redirect); err != nil {
		return nil, err
	}
	select {
	case result := <-results:
		return result, nil
	case err := <-failures:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("gave up waiting for the authorization redirect: %w", ctx.Err())
	}
}

func (f *loopbackFlow) handleCallback(results chan<- *auth.AuthorizationResult, failures chan<- error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if denied := query.Get("error"); denied != "" {
			http.Error(w, "authorization was refused; the terminal has the details", http.StatusBadRequest)
			failures <- fmt.Errorf("the authorization server refused: %s %s", denied, query.Get("error_description"))
			return
		}
		code := query.Get("code")
		if code == "" {
			// A browser fetching /favicon.ico must not be mistaken for a failed
			// authorization, so an unrecognised request is answered and ignored.
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "Aura is authorized. You can close this tab.\n")
		results <- &auth.AuthorizationResult{
			Code:  code,
			State: query.Get("state"),
			Iss:   query.Get("iss"),
		}
	}
}
