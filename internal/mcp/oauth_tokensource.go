package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcpoauth"
)

// oauth_tokensource.go is the half that makes an authorization SURVIVE. Without it the
// grant table would fill up and never be read: the SDK's handler keeps its token in
// memory, so every restart would send the human back to a browser.
//
// The shape here is not a guess. Both reference implementations were read first
// (2026-08-24) and both persist THREE things, not one:
//
//   - Hermes (tools/mcp_oauth.py, HermesTokenStorage) writes <server>.json,
//     <server>.client.json AND <server>.meta.json, and says why in its own comment:
//     "The MCP SDK keeps discovered OAuthMetadata (token endpoint URL, etc.) in memory
//     only. […] Without this, cold-start refresh requests fall back to the SDK's guessed
//     {server_url}/token which returns 404 on most real providers and forces a full
//     browser re-authorization."
//   - LibreChat (packages/api/src/mcp/oauth/tokens.ts) stores the tokens, the client
//     info, and an OAuthStoredClientMetadata that extends the server metadata with the
//     "canonical OAuth resource indicator used when the authorization code was
//     exchanged".
//
// So a stored access token alone is worthless for a cold start: the endpoint it must be
// refreshed against has to be stored with it. resolvedClient is that third thing, and
// Grant.ClientInfo is where it goes — already encrypted, because a dynamically
// registered client carries a client_secret and is therefore itself a credential.

// oauthTokenLeeway refreshes a token slightly before it dies so a call in flight is not
// the thing that discovers the expiry. It matches the leeway Grant.Expired documents.
const oauthTokenLeeway = 60 * time.Second

// resolvedClient is the OAuth state Aura persists beside the tokens: what the client
// turned out to be, and where it must talk. Every field is one the SDK resolved at
// authorization time and would otherwise forget.
//
// The json tags are the RFC 8414 metadata names rather than Go-shaped ones so a stored
// blob stays readable next to a provider's own discovery document during triage.
type resolvedClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	// AuthorizationEndpoint is stored for triage and for a re-authorization that wants
	// to skip discovery; the refresh path needs only TokenEndpoint.
	AuthorizationEndpoint string `json:"authorization_endpoint,omitempty"`
	TokenEndpoint         string `json:"token_endpoint"`
	// AuthStyle records whether this server took its client credentials in the POST
	// body or in a Basic header. The SDK resolves it from the server's advertised
	// token_endpoint_auth_methods_supported; re-guessing it on a cold start costs a
	// failed refresh against every provider that only accepts one of the two.
	AuthStyle   int      `json:"auth_style"`
	RedirectURI string   `json:"redirect_uri,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

// resolvedClientFrom reads the config the SDK resolved. cfg is never nil: its only
// caller is the SDK's post-exchange hook, which passes the config it just built. A nil
// guard here would turn a contract violation into a grant with no token endpoint —
// storable, unrefreshable, and only noticed on the next cold start.
func resolvedClientFrom(cfg *oauth2.Config) resolvedClient {
	return resolvedClient{
		ClientID:              cfg.ClientID,
		ClientSecret:          cfg.ClientSecret,
		AuthorizationEndpoint: cfg.Endpoint.AuthURL,
		TokenEndpoint:         cfg.Endpoint.TokenURL,
		AuthStyle:             int(cfg.Endpoint.AuthStyle),
		RedirectURI:           cfg.RedirectURL,
		Scopes:                cfg.Scopes,
	}
}

// oauth2Config rebuilds the config a refresh needs. AuthURL is left as stored: a refresh
// never visits it, and a cold start that DOES need a browser goes through the SDK's
// discovery again rather than trusting a blob.
func (rc resolvedClient) oauth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     rc.ClientID,
		ClientSecret: rc.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   rc.AuthorizationEndpoint,
			TokenURL:  rc.TokenEndpoint,
			AuthStyle: oauth2.AuthStyle(rc.AuthStyle),
		},
		RedirectURL: rc.RedirectURI,
		Scopes:      rc.Scopes,
	}
}

// encode seals nothing itself: the bytes it returns go into Grant.ClientInfo, which
// mcpoauth.Store.Save encrypts with AES-256-GCM under the identity's derived key before
// it reaches a column. Serializing a client_secret is the POINT — a dynamically
// registered client is a credential, and LibreChat encrypts the same registration result
// as its third ciphertext for the same reason.
func (rc resolvedClient) encode() ([]byte, error) {
	//nolint:gosec // G117: the secret is marshaled to be SEALED by mcpoauth, never stored or logged in the clear.
	blob, err := json.Marshal(rc)
	if err != nil {
		return nil, fmt.Errorf("mcp oauth: encode resolved client: %w", err)
	}
	return blob, nil
}

func decodeResolvedClient(blob []byte) (resolvedClient, error) {
	var rc resolvedClient
	if len(blob) == 0 {
		return rc, nil
	}
	if err := json.Unmarshal(blob, &rc); err != nil {
		return resolvedClient{}, fmt.Errorf("mcp oauth: decode resolved client: %w", err)
	}
	return rc, nil
}

// grantFrom folds a token and the client that obtained it into one storable grant.
//
// resourceURL is the MCP server's own validated URL, which is what RFC 9728 makes the
// canonical resource indicator for that server. Storing it is LibreChat's habit and it
// is what a later slice needs if a provider turns out to require the resource parameter
// on refresh — see the comment on restoreTokenSource for why nothing sends it today.
func grantFrom(serverName, resourceURL string, tok *oauth2.Token, rc resolvedClient) (mcpoauth.Grant, error) {
	blob, err := rc.encode()
	if err != nil {
		return mcpoauth.Grant{}, err
	}
	grant := mcpoauth.Grant{
		ServerName:   serverName,
		ResourceURL:  resourceURL,
		ClientInfo:   blob,
		Scopes:       rc.Scopes,
		TokenType:    tok.TokenType,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		// ABSOLUTE, and the reason the column is a timestamptz: OAuth hands out a
		// relative expires_in, which means nothing after a restart. This is Hermes's
		// "Fix A" — it found that a reloaded relative expiry leaves the SDK believing a
		// long-dead token is still valid until the first 401.
		ExpiresAt: tok.Expiry,
	}
	return grant, nil
}

func tokenFrom(grant mcpoauth.Grant) *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  grant.AccessToken,
		TokenType:    grant.TokenType,
		RefreshToken: grant.RefreshToken,
		// A zero ExpiresAt stays zero: oauth2 reads that as "no expiry advertised" and
		// uses the token until it is refused, which is the same reading Grant.Expired
		// documents. Substituting a made-up expiry here would refresh tokens that never
		// needed it.
		Expiry: grant.ExpiresAt,
	}
}

// persistingTokenSource writes every NEW token back to the store.
//
// It exists because a refresh is invisible: oauth2's reuseTokenSource swaps the token
// inside itself and tells nobody, so a process that refreshed happily for a week would
// still restart with the week-old token it first persisted — and, worse, with a refresh
// token the provider may have already rotated away, which is an authorization that
// silently cannot be recovered.
type persistingTokenSource struct {
	ctx         context.Context
	inner       oauth2.TokenSource
	store       GrantStore
	logger      *slog.Logger
	name        string
	resourceURL string
	client      resolvedClient

	mu           sync.Mutex
	last         string
	reloadStored bool
}

var oauthTokenRefreshes singleflight.Group

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	owner := identityctx.IdentityID(p.ctx)
	if owner == "" {
		return p.token()
	}
	value, err, _ := oauthTokenRefreshes.Do(owner+"\x00"+p.name, func() (any, error) {
		return p.token()
	})
	if err != nil {
		return nil, err
	}
	return value.(*oauth2.Token), nil
}

func (p *persistingTokenSource) token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.reload(); err != nil {
		return nil, err
	}
	tok, err := p.inner.Token()
	if err != nil {
		p.logger.Warn("mcp oauth: token refresh failed",
			"server", p.name, "error", err)
		return nil, err
	}
	if tok == nil || tok.AccessToken == p.last {
		return tok, nil
	}
	grant, err := grantFrom(p.name, p.resourceURL, tok, p.client)
	if err != nil {
		return nil, err
	}
	if err := p.store.Save(p.ctx, grant); err != nil {
		// Deliberately NOT fatal, and deliberately not silent. The token in hand is
		// valid and refusing it would break a working session over a storage fault;
		// but a save that failed means the next boot re-authorizes, so it must be
		// visible rather than inferred later from an unexplained browser prompt.
		p.logger.Warn("mcp oauth: could not persist refreshed token",
			"server", p.name, "error", err)
		return tok, nil
	}
	p.last = tok.AccessToken
	p.reloadStored = true
	return tok, nil
}

func (p *persistingTokenSource) reload() error {
	if !p.reloadStored {
		return nil
	}
	grant, err := p.store.Load(p.ctx, p.name)
	if errors.Is(err, mcpoauth.ErrNoGrant) {
		return fmt.Errorf("%w: %s", ErrOAuthAuthorizationRequired, p.name)
	}
	if err != nil {
		p.logger.Warn("mcp oauth: could not reload stored token",
			"server", p.name, "error", err)
		return nil
	}
	if grant.AccessToken == p.last {
		return nil
	}
	client, err := decodeResolvedClient(grant.ClientInfo)
	if err != nil || client.TokenEndpoint == "" {
		p.logger.Warn("mcp oauth: could not adopt stored token",
			"server", p.name, "error", err)
		return nil
	}
	p.inner = client.oauth2Config().TokenSource(p.ctx, tokenFrom(grant))
	p.client = client
	p.resourceURL = grant.ResourceURL
	p.last = grant.AccessToken
	return nil
}

// newPersistingTokenSource wraps src so refreshes are stored under the identity on ctx.
//
// ctx must already be detached from the caller's cancellation (see detachedForRefresh):
// oauth2 captures it for every future refresh request, and the save below runs on it
// too, so a handshake context would leave both dead the moment the mount returned.
func newPersistingTokenSource(ctx context.Context, src oauth2.TokenSource, serverName, resourceURL string, rc resolvedClient, store GrantStore, logger *slog.Logger, seed string, reloadStored bool) *persistingTokenSource {
	return &persistingTokenSource{
		ctx: ctx, inner: src, store: store, logger: resolveLogger(logger), name: serverName,
		resourceURL: resourceURL, client: rc, last: seed, reloadStored: reloadStored,
	}
}

// detachedForRefresh keeps the context's VALUES — the identity the store scopes every
// row by — while dropping its cancellation.
//
// This is the HTTP twin of the two-context split OpenSDKSessionForConfig already
// documents for stdio: the context that bounds a handshake must not be the context that
// bounds everything the session later does. A token source built on the handshake
// context refreshes exactly once, never, because the context it captured died when the
// mount returned.
func detachedForRefresh(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// restoreTokenSource turns a stored grant into the SDK's InitialTokenSource, which is
// what stops a mount from opening a browser for an identity that already authorized.
//
// It returns (nil, nil) — meaning "let the SDK run the flow" — for the three cases that
// are normal rather than broken: no grant, a grant with no token endpoint (nothing to
// refresh against), and a grant whose client blob no longer parses. The last one is a
// deliberate downgrade to re-authorization instead of an error: a corrupt blob is
// recoverable by asking the human, and failing the mount instead would strand the
// identity with no way back.
//
// What this does NOT do is send RFC 8707's resource parameter on refresh. x/oauth2's
// tokenRefresher posts grant_type and refresh_token and offers no hook for a third
// field (v0.36.0, oauth2.go:274), and the MCP SDK's own default token source is that
// same call — so a provider requiring resource on refresh already fails the SDK's
// default path, and its failure mode here is a 401 that triggers a full, working
// re-authorization. The indicator is stored anyway (Grant.ResourceURL) so closing that
// gap later is code, not a migration.
func restoreTokenSource(ctx context.Context, serverName string, store GrantStore, httpClient *http.Client, logger *slog.Logger) (oauth2.TokenSource, error) {
	grant, err := store.Load(ctx, serverName)
	if errors.Is(err, mcpoauth.ErrNoGrant) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	log := resolveLogger(logger)
	rc, err := decodeResolvedClient(grant.ClientInfo)
	if err != nil {
		log.Warn("mcp oauth: stored client is unreadable, re-authorization required",
			"server", serverName, "error", err)
		return nil, nil
	}
	if rc.TokenEndpoint == "" {
		log.Warn("mcp oauth: stored grant has no token endpoint, re-authorization required",
			"server", serverName)
		return nil, nil
	}
	// oauth2.HTTPClient, not the SDK's Config.Client: a refresh rebuilt HERE never
	// passes through the handler, so without this line every restored mount would
	// refresh its token over http.DefaultClient — no pinned DNS, no redirect guard, no
	// SSRF policy, against a token endpoint that came from a document the MCP server
	// served. This is the documented x/oauth2 seam for supplying that client.
	detached := detachedForRefresh(ctx)
	if httpClient != nil {
		detached = context.WithValue(detached, oauth2.HTTPClient, httpClient)
	}
	tok := tokenFrom(grant)
	source := rc.oauth2Config().TokenSource(detached, tok)
	// Seeded with the token just loaded so a mount that never refreshes does not write
	// the row back unchanged on its first call.
	return newPersistingTokenSource(detached, source, serverName, grant.ResourceURL, rc, store, log, grant.AccessToken, true), nil
}

// StoredOAuthAccessToken resolves the current bearer from the same identity-scoped
// grant and refresh path used by an MCP session. It exists for adjacent HTTP surfaces
// exposed by the same resource server, so they cannot grow a second credential model.
func StoredOAuthAccessToken(ctx context.Context, serverName string, store GrantStore, egress EgressPolicy, logger *slog.Logger) (string, error) {
	source, err := restoreTokenSource(ctx, serverName, store, oauthHTTPClient(egress), logger)
	if err != nil {
		return "", err
	}
	if source == nil {
		return "", ErrOAuthAuthorizationRequired
	}
	token, err := source.Token()
	if err != nil {
		return "", fmt.Errorf("mcp oauth: load access token for %q: %w", serverName, err)
	}
	access := strings.TrimSpace(token.AccessToken)
	if access == "" {
		return "", fmt.Errorf("mcp oauth: stored grant for %q returned an empty access token", serverName)
	}
	return access, nil
}
