package mcp

import (
	"time"

	"golang.org/x/oauth2"

	"github.com/chetto1983/aura/internal/mcpoauth"
)

// Aura-owned HTTP MCPs use the same OAuth contract as arbitrary remote MCPs.
// The issuer is loopback because the generic client runs in the Aura process;
// resource servers fetch the same issuer's keys over the private Compose name.
const (
	AuraAuthorizationServerIssuer  = "http://127.0.0.1:9080"
	AuraAuthorizationServerJWKSURL = "http://aura:9080/oauth/jwks"
	AuraOAuthToolsScope            = "mcp:tools"
	AuraOAuthManageScope           = "mcp:manage"
)

// AuraAuthorizationEndpoint is where Aura's own authorization server sends a human. It is
// exported because a grant Aura minted for itself never passed through discovery, so the
// endpoints a later refresh or re-authorization needs have to be written into the stored
// grant by the minting side.
func AuraAuthorizationEndpoint() string { return AuraAuthorizationServerIssuer + "/oauth/authorize" }

// AuraTokenEndpoint is where a stored first-party grant refreshes, for the same reason.
func AuraTokenEndpoint() string { return AuraAuthorizationServerIssuer + "/oauth/token" }

// AuraIssuedGrant is a credential Aura minted for one of its OWN MCP resource servers, on
// its way into the grant store.
//
// It exists so the ClientInfo blob has exactly one writer. restoreTokenSource refuses a
// stored grant whose blob carries no token endpoint — it downgrades to "re-authorization
// required", which for a first-party server means a browser prompt nobody is at — so a
// self-issued grant that forgot the endpoint would store cleanly and then fail to mount
// on the next boot, with the blob being the only place to look.
//
// There is no refresh token here, deliberately: Authula cannot store one without a login
// session (webauth.IssueFirstPartyToken documents the measurement), and a grant carrying
// an unredeemable refresh token would spend a round trip discovering that on every
// expiry. The issuer re-mints instead.
type AuraIssuedGrant struct {
	ServerName  string
	ResourceURL string
	ClientID    string
	Scopes      []string
	AccessToken string
	ExpiresAt   time.Time
}

// Grant renders the storable grant, including the resolved-client blob a cold start
// needs. AuthStyle is pinned to InParams because Aura's own token endpoint reads
// client_id from the POST form (webauth.OAuthServer.TokenHandler); leaving it to
// autodetect would cost a failed probe on every refresh.
func (g AuraIssuedGrant) Grant() (mcpoauth.Grant, error) {
	return grantFrom(g.ServerName, g.ResourceURL, &oauth2.Token{
		AccessToken: g.AccessToken,
		TokenType:   "Bearer",
		Expiry:      g.ExpiresAt,
	}, resolvedClient{
		ClientID:              g.ClientID,
		AuthorizationEndpoint: AuraAuthorizationEndpoint(),
		TokenEndpoint:         AuraTokenEndpoint(),
		AuthStyle:             int(oauth2.AuthStyleInParams),
		Scopes:                g.Scopes,
	})
}
