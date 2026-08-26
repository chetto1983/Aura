package webauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/mcp"
)

// mcp_oauth_first_party.go mints a grant for an MCP resource server AURA ITSELF SHIPS,
// with no browser in the loop.
//
// It exists because a05c92cfe made Calendar, WhatsApp and Memory OAuth resource servers
// isolated by token subject (Amendment #147) without giving them any way to obtain a
// token. The only issuance path was AuthorizationHandler, which needs a human at a
// consent screen — so the three sidecars Aura owns, provisions and starts itself sat
// unmounted forever, and the daemon's memory readiness probe failed on every boot with
// "required memory capability is not mounted".
//
// Asking the operator to consent to Aura's own sidecar is not consent, it is ceremony:
// there is no third party to authorize, and the resource server is a process this same
// deployment launched. What the OAuth surface buys for a first-party server is the
// per-identity SUBJECT — the tenancy boundary — and that survives self-issuance intact,
// because the subject minted here is one concrete identity and nothing else.
//
// The one thing this must never become is a way around consent for a server Aura does
// NOT own. That is enforced by the CALLER (see manager.FirstPartyRecipe): this method
// takes a resource URL, and allowedMCPResource — the same allow-list the browser
// authorization endpoint enforces — is the second, independent gate here.

// firstPartyRefreshTTL matches the lifetime exchangeAuthorizationCode gives a browser
// grant, so a self-issued grant is not a second, quieter token policy.
const firstPartyRefreshTTL = 7 * 24 * time.Hour

// ErrFirstPartyResourceNotOwned rejects a resource URL that is not one of Aura's own MCP
// resource servers. It is the refusal that keeps self-issuance from being a backdoor.
var ErrFirstPartyResourceNotOwned = errors.New("webauth: refusing to self-issue a token for a resource Aura does not own")

// IssuedMCPToken is one credential pair Aura minted for itself. It carries the fields a
// stored grant needs and nothing else — no session, no cookie, no user record.
type IssuedMCPToken struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ClientID     string
	ExpiresAt    time.Time
}

// IssueFirstPartyToken mints an audience-bound access token plus its rotating refresh
// token for identityID against one of Aura's own MCP resource servers.
//
// It is deliberately the SAME two calls exchangeAuthorizationCode makes, through the same
// mcpTokenPlugin, with the same claim shape (mcpTokenClaims): a self-issued token that
// differed from a browser-issued one in any claim would be a token the sidecars validate
// differently, which is the kind of divergence nobody finds until production.
func (s *OAuthServer) IssueFirstPartyToken(ctx context.Context, resource, identityID string) (IssuedMCPToken, error) {
	if s == nil || s.tokens == nil || s.tokens.jwt == nil || s.tokens.refresh == nil {
		return IssuedMCPToken{}, errors.New("webauth: MCP token services are unavailable")
	}
	resource = strings.TrimSpace(resource)
	if !allowedMCPResource(resource) {
		return IssuedMCPToken{}, fmt.Errorf("%w: %q", ErrFirstPartyResourceNotOwned, resource)
	}
	subject := strings.TrimSpace(identityID)
	// A non-UUID subject would be signed happily and then select a tenant nobody owns:
	// the resource servers derive database, SQLite directory and account from `sub`
	// verbatim. Refusing here is cheaper than a sidecar creating a stray tenant.
	if _, err := uuid.Parse(subject); err != nil {
		return IssuedMCPToken{}, fmt.Errorf("webauth: MCP subject %q is not a uuid: %w", identityID, err)
	}
	// A fresh session id per issuance, never a borrowed cockpit session: binding Aura's
	// own sidecar credential to a human's login would revoke the agent's memory the
	// moment that human logged out.
	sessionID := uuid.NewString()
	scope := mcp.AuraOAuthToolsScope
	pair, err := s.tokens.jwt.GenerateUserToken(ctx, subject, sessionID,
		mcpTokenClaims(resource, scope, mcpOAuthClientID, subject))
	if err != nil {
		return IssuedMCPToken{}, fmt.Errorf("webauth: issue first-party MCP token: %w", err)
	}
	if err := s.tokens.refresh.StoreInitialRefreshToken(ctx, pair.RefreshToken, sessionID,
		s.now().Add(firstPartyRefreshTTL)); err != nil {
		return IssuedMCPToken{}, fmt.Errorf("webauth: store first-party MCP refresh token: %w", err)
	}
	return IssuedMCPToken{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    "Bearer",
		Scope:        scope,
		ClientID:     mcpOAuthClientID,
		ExpiresAt:    s.now().Add(pair.ExpiresIn),
	}, nil
}
