package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcpoauth"
)

// oauth_handler.go attaches an authorization flow to a remote MCP mount. It is the piece
// that turns "the operator pasted a token into the config" into "this identity
// authorized this server", which is the only shape a per-identity connector can have.
//
// The SDK owns the protocol — discovery, PKCE, registration, exchange, step-up. What is
// Aura's is the same thing it always is at this boundary: which requests are allowed to
// leave, and where the credentials live.

const (
	// oauthClientName is what a consent screen shows the human. It is also the DCR
	// client_name, so it is the string a provider's app list will show forever.
	oauthClientName = "Aura"

	// defaultOAuthRedirectURL is a placeholder, not a listener. A real flow is started
	// by a caller that owns a redirect — the CLI binds a loopback port, the cockpit
	// uses its own route — and passes it in. This value exists because the SDK requires
	// a syntactically valid redirect before it will construct a handler at all, and a
	// handler with no fetcher never reaches it.
	defaultOAuthRedirectURL = "http://127.0.0.1:0/aura/mcp/oauth/callback"
)

// ErrOAuthAuthorizationRequired is what an unattended mount returns instead of hanging.
//
// A mount at boot has no human: nobody can be sent to a consent screen from a container
// start-up. The failure must therefore be a clear instruction rather than a timeout, and
// it must not be retried as if it were a transport fault. Hermes makes the same
// distinction its own way, with OAuthNonInteractiveError raised from
// _raise_if_non_interactive.
var ErrOAuthAuthorizationRequired = errors.New("mcp oauth: this identity has not authorized this server; run `aura mcp login <server>`")

// ErrOAuthEndpointNotPinned refuses to send a client secret anywhere the operator did
// not name.
//
// OAuthSettings already refuses a secret with no pinned endpoints, but that check alone
// would have been decoration: the SDK builds its oauth2.Config from the endpoints
// DISCOVERY returned (authorization_code.go:337), so a pinned MCP_OAUTH_TOKEN_URL has no
// effect on where the secret actually goes. This error is that pin becoming real, at the
// only layer that sees the request carrying the secret.
var ErrOAuthEndpointNotPinned = errors.New("mcp oauth: refusing to post client credentials to an endpoint the operator did not pin")

// ErrOAuthAuthorizationURLNotPinned refuses to send the HUMAN somewhere unpinned.
//
// The other half of the same hole, and the half that is phishing rather than credential
// theft: the consent URL also comes from discovery, so a hostile MCP server can name its
// own authorization server and the human is handed a login page the operator never
// approved. Pinning the authorization URL is only meaningful if the fetcher enforces it.
var ErrOAuthAuthorizationURLNotPinned = errors.New("mcp oauth: refusing to open an authorization URL the operator did not pin")

// GrantStore is the narrow slice of *mcpoauth.Store this package needs. Declared here as
// an interface rather than taking the concrete type so a mount can be tested without
// Postgres, and so internal/mcp keeps one direction of dependency on internal/mcpoauth.
type GrantStore interface {
	Load(ctx context.Context, serverName string) (mcpoauth.Grant, error)
	Save(ctx context.Context, grant mcpoauth.Grant) error
}

// OAuthOptions carries the per-mount authorization wiring. The zero value is usable and
// means "restore nothing, start nothing": a mount with no store and no fetcher still
// attaches a handler, so a server that turns out to need authorization answers with
// ErrOAuthAuthorizationRequired instead of a bare 401 the caller has to interpret.
type OAuthOptions struct {
	// Store persists this identity's grants. Nil disables both restore and persist,
	// which is what a diagnostic probe wants.
	Store GrantStore

	// Fetcher drives the human half of the flow. Nil means unattended: no browser can
	// be opened, so an authorization that is genuinely needed fails fast and says how
	// to fix it.
	Fetcher auth.AuthorizationCodeFetcher

	// RedirectURL is the redirect the Fetcher owns. It takes precedence over
	// MCP_OAUTH_REDIRECT_URL because the caller that runs the flow is the one that
	// knows where it can actually receive the code.
	//
	// It must be STABLE across restarts when dynamic client registration is in play:
	// the URI is registered with the authorization server, so a redirect that moves
	// invalidates the registration. Hermes caches its callback port for exactly this
	// reason (_cached_redirect_port).
	RedirectURL string
}

// oauthHandlerFor builds the handler for one mount, or nil when this server takes no
// authorization flow.
//
// httpClient must be the hardened, redirect-guarded client — WITHOUT the static
// credential wrapper. Two separate reasons, and both are load-bearing:
//   - headerRoundTripper applies MCP_HEADER_* to every request it sees, with no origin
//     check, so reusing it here would forward the operator's MCP headers to the
//     authorization server, a different origin. LibreChat builds a dedicated
//     createHardenedOAuthFetch for the OAuth leg rather than reusing the transport's.
//   - the SDK documents Config.Client as the place for SSRF protections, and the OAuth
//     leg fetches URLs NAMED BY THE SERVER — the one request set in this package whose
//     destination an attacker chooses.
func oauthHandlerFor(ctx context.Context, name string, server ManagedServer, settings OAuthSettings, httpClient *http.Client, o OAuthOptions, logger *slog.Logger) (auth.OAuthHandler, error) {
	if !UsesOAuth(server, settings) {
		return nil, nil
	}
	log := resolveLogger(logger)
	redirect := firstNonEmptyString(o.RedirectURL, settings.RedirectURL, defaultOAuthRedirectURL)

	cfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:              redirect,
		AuthorizationCodeFetcher: guardedFetcher(name, settings, o.Fetcher),
		// Aura stores refresh tokens encrypted, per identity, which is exactly the
		// capability SEP-2207 asks a client to assert before requesting offline_access.
		RequestRefreshToken: true,
		Client:              pinnedOAuthClient(httpClient, settings),
	}
	applyClientRegistration(cfg, settings, redirect)

	if o.Store != nil {
		initial, err := restoreTokenSource(ctx, name, o.Store, cfg.Client, log)
		if err != nil {
			return nil, err
		}
		cfg.InitialTokenSource = initial
		cfg.NewTokenSource = persistOnAuthorization(ctx, name, server.URL, o.Store, log)
	}
	handler, err := auth.NewAuthorizationCodeHandler(cfg)
	if err != nil {
		return nil, fmt.Errorf("mcp oauth %q: %w", name, err)
	}
	return handler, nil
}

// applyClientRegistration picks the registration method. The SDK tries CIMD, then
// pre-registration, then DCR, and requires at least one to be configured — so the
// Linear/Atlassian case is zero-config for the OPERATOR but not for Aura: the DCR
// metadata still has to be built, and building it is what makes "paste nothing and it
// works" true for the providers that self-register.
func applyClientRegistration(cfg *auth.AuthorizationCodeHandlerConfig, settings OAuthSettings, redirect string) {
	if settings.Preregistered() {
		creds := &oauthex.ClientCredentials{ClientID: settings.ClientID}
		if settings.Confidential() {
			creds.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: settings.ClientSecret}
		}
		cfg.PreregisteredClient = creds
		return
	}
	cfg.DynamicClientRegistrationConfig = &auth.DynamicClientRegistrationConfig{
		Metadata: &oauthex.ClientRegistrationMetadata{
			RedirectURIs:  []string{redirect},
			ClientName:    oauthClientName,
			GrantTypes:    []string{"authorization_code", "refresh_token"},
			ResponseTypes: []string{"code"},
			// A dynamically registered client keeps no secret it could authenticate
			// with, so it is a public client using PKCE. Claiming a secret method here
			// would make the authorization server mint one and then reject every token
			// request that did not send it.
			TokenEndpointAuthMethod: "none",
			Scope:                   strings.Join(settings.Scopes, " "),
		},
	}
}

// persistOnAuthorization is the SDK's post-exchange hook: it fires once, with the fully
// resolved config, which is the ONLY moment the discovered token endpoint and the
// possibly DCR-minted client id are visible to us. Missing it is what forces a browser
// on every restart.
//
// owner is captured from mountCtx — the context that built the handler — because the ctx
// the hook RECEIVES carries no identity and never can: the SDK builds it from
// context.Background() before invoking the hook (go-sdk v1.7.0
// auth/authorization_code.go:645), by design, so the token source outlives the request
// that triggered the 401. Measured 2026-08-24: Linear consented, the code was exchanged,
// and the grant was then discarded with "mcpoauth: no identity on context" — a completed
// human authorization thrown away because the store had nobody to scope the row to.
func persistOnAuthorization(mountCtx context.Context, name, resourceURL string, store GrantStore, logger *slog.Logger) func(context.Context, *oauth2.Config, *oauth2.Token) (oauth2.TokenSource, error) {
	owner := identityctx.IdentityID(mountCtx)
	return func(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
		rc := resolvedClientFrom(cfg)
		// Detached before the save, not after: the token source built here must outlive
		// the request that hit the 401. The identity is re-bound from owner rather than
		// inherited, for the reason above.
		detached := identityctx.WithIdentityID(detachedForRefresh(ctx), owner)
		grant, err := grantFrom(name, resourceURL, tok, rc)
		if err != nil {
			return nil, err
		}
		if err := store.Save(detached, grant); err != nil {
			// Not fatal: the human just completed a consent flow and throwing it away
			// over a storage fault would make them do it again with no explanation.
			// The session works; only its survival across a restart is lost, so it is
			// logged rather than swallowed.
			resolveLogger(logger).Warn("mcp oauth: authorization succeeded but could not be persisted",
				"server", name, "error", err)
		}
		return newPersistingTokenSource(detached, cfg.TokenSource(detached, tok), name, resourceURL, rc, store, logger, tok.AccessToken), nil
	}
}

// guardedFetcher wraps the caller's fetcher with the authorization-URL pin, and supplies
// the unattended refusal when there is no fetcher at all.
func guardedFetcher(name string, settings OAuthSettings, inner auth.AuthorizationCodeFetcher) auth.AuthorizationCodeFetcher {
	return func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		if settings.AuthorizationURL != "" && args != nil && !sameEndpoint(args.URL, settings.AuthorizationURL) {
			return nil, fmt.Errorf("%w: %s pins %s", ErrOAuthAuthorizationURLNotPinned, envOAuthAuthURL, settings.AuthorizationURL)
		}
		if inner == nil {
			return nil, fmt.Errorf("%w: %s", ErrOAuthAuthorizationRequired, name)
		}
		return inner(ctx, narrowScopes(args, settings.Scopes))
	}
}

// narrowScopes rewrites the authorization URL's `scope` to what the operator configured.
//
// It has to happen HERE because go-sdk v1.7.0 offers no earlier seam: the handler takes the
// scopes from the WWW-Authenticate challenge, and when the server sends none — Slack sends
// none, measured 2026-08-24 — it falls back to the protected-resource metadata's whole
// scopes_supported list. For Slack that is all 30 scopes, so a consent screen meant to read
// two channels asks for canvases, files, reactions and every search surface the workspace
// has. Upstream added a ScopeFilter hook for exactly this, but it is not in any release yet.
//
// Only `scope` is touched: PKCE, state and redirect_uri are the SDK's and must survive
// untouched. An empty configuration changes nothing, so discovery still decides by default.
func narrowScopes(args *auth.AuthorizationArgs, scopes []string) *auth.AuthorizationArgs {
	if args == nil || len(scopes) == 0 {
		return args
	}
	parsed, err := url.Parse(args.URL)
	if err != nil {
		return args
	}
	query := parsed.Query()
	if !query.Has("scope") {
		return args
	}
	query.Set("scope", strings.Join(scopes, " "))
	parsed.RawQuery = query.Encode()
	narrowed := *args
	narrowed.URL = parsed.String()
	return &narrowed
}

// pinnedOAuthClient enforces a pinned token endpoint at the transport.
//
// The rule is narrow on purpose: only a confidential client is restricted, and only its
// POSTs. Discovery is GET and carries no credential, so it stays free; the requests that
// carry the client secret — the code exchange and every refresh — are POSTs, and they
// may go only where the operator wrote. A public client is unrestricted because it has
// no secret to lose, which is the same line the MCP spec draws around dynamic
// registration.
func pinnedOAuthClient(base *http.Client, settings OAuthSettings) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	if !settings.Confidential() || settings.TokenURL == "" {
		return base
	}
	cloned := *base
	inner := cloned.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	cloned.Transport = &pinnedEndpointRoundTripper{base: inner, tokenURL: settings.TokenURL}
	return &cloned
}

type pinnedEndpointRoundTripper struct {
	base     http.RoundTripper
	tokenURL string
}

func (p *pinnedEndpointRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPost && !sameEndpoint(req.URL.String(), p.tokenURL) {
		return nil, fmt.Errorf("%w: %s pins %s, request went to %s",
			ErrOAuthEndpointNotPinned, envOAuthTokenURL, p.tokenURL, req.URL.Redacted())
	}
	return p.base.RoundTrip(req)
}

// sameEndpoint compares two endpoints by the parts that decide WHO answers: scheme, host
// and path. Query and fragment are excluded because the SDK legitimately appends state,
// PKCE challenge and scope to an authorization URL, and a byte comparison would reject
// every real flow.
func sameEndpoint(got, pinned string) bool {
	a, err := url.Parse(strings.TrimSpace(got))
	if err != nil {
		return false
	}
	b, err := url.Parse(strings.TrimSpace(pinned))
	if err != nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Host, b.Host) &&
		strings.TrimSuffix(a.Path, "/") == strings.TrimSuffix(b.Path, "/")
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
