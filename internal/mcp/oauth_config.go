package mcp

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// oauth_config.go reads the operator's OAuth settings for a remote MCP server off the same
// ManagedServer.Env that already carries MCP_BEARER_TOKEN and MCP_HEADER_*. Env rather than
// new ManagedServer fields on purpose: Env is already saved, loaded, validated, exported and
// REDACTED (internal/secret marks any key containing "secret"), so a new struct field would
// mean re-deriving five behaviours that already work.
//
// It is also where the operator's credentials BELONG. A pre-registered client_id/secret is
// deployment configuration with the deployment's lifetime; the per-identity access and
// refresh tokens live in internal/mcpoauth, keyed by identity, because those identify a
// person. Hermes and LibreChat split it the same way -- config file for the client, database
// for the tokens -- and the split is what makes "delete the identity" not destroy the
// operator's app registration.
//
// EVERY FIELD IS OPTIONAL, and that is the whole design. Measured 2026-08-24: the remote MCP
// fleet is genuinely split on client registration -- Linear and Atlassian implement Dynamic
// Client Registration, so a client self-registers and needs NO configuration at all; Slack
// and GitHub reject DCR and require an app the operator registered by hand; Notion sits in
// between. Branching on the provider would mean writing the wrong branch for half the fleet,
// so nothing here names a provider: the SDK's AuthorizationCodeHandlerConfig tries Client ID
// Metadata Document, then a pre-registered client, then DCR, and whichever the server
// supports is the one that answers.

// Env keys, sharing the MCP_ prefix of the static-credential keys above them.
const (
	envOAuthClientID     = "MCP_OAUTH_CLIENT_ID"
	envOAuthClientSecret = "MCP_OAUTH_CLIENT_SECRET" //nolint:gosec // G101 false positive: env var NAME, not a credential
	envOAuthScopes       = "MCP_OAUTH_SCOPES"
	envOAuthAuthURL      = "MCP_OAUTH_AUTHORIZATION_URL"
	envOAuthTokenURL     = "MCP_OAUTH_TOKEN_URL" //nolint:gosec // G101 false positive: env var NAME, not a credential
	envOAuthRedirectURL  = "MCP_OAUTH_REDIRECT_URL"
	envOAuthDisabled     = "MCP_OAUTH_DISABLED"
)

// ErrOAuthSecretNeedsPinnedEndpoints refuses to hand a confidential client secret to
// endpoints that came from discovery.
//
// This is not defensive tidiness, it is the confused-deputy hole. The MCP authorization flow
// learns where the authorization server IS by fetching the protected-resource metadata
// document FROM THE MCP SERVER. A compromised or hostile server can therefore name an
// authorization server it controls, and a client that posts the operator's client_secret to
// whatever endpoint it was told would hand that secret to the attacker. A public client
// (PKCE, no secret) has nothing to lose there, which is why DCR and CIMD stay unrestricted;
// a confidential client does, so its endpoints must be ones the operator wrote down.
//
// LibreChat states the same rule in the same words (packages/api/src/mcp/oauth/handler.ts:
// "refusing to use it with auto-discovered OAuth endpoints").
var ErrOAuthSecretNeedsPinnedEndpoints = errors.New(
	"mcp oauth: a client secret requires both " + envOAuthAuthURL + " and " + envOAuthTokenURL +
		" — refusing to send it to endpoints resolved by discovery")

// ErrOAuthSecretWithoutClientID rejects half a client. A secret with no id cannot
// authenticate anything, so it is a configuration mistake rather than a usable public client.
var ErrOAuthSecretWithoutClientID = errors.New(
	"mcp oauth: " + envOAuthClientSecret + " requires " + envOAuthClientID)

// OAuthSettings is the operator's half of a server's OAuth configuration. A zero value is
// meaningful and common: it says "this server registers its client dynamically, or by client
// ID metadata document, and needs nothing from the operator".
type OAuthSettings struct {
	ClientID     string
	ClientSecret string
	Scopes       []string
	// AuthorizationURL and TokenURL are set only when the operator pinned them. Empty
	// means "resolve them from the server's metadata", which is the normal path for a
	// public client.
	AuthorizationURL string
	TokenURL         string
	// RedirectURL is where the authorization server sends the human back. Empty lets the
	// caller choose — the CLI uses a loopback listener, the cockpit its own route.
	RedirectURL string
	// Disabled is the operator's explicit opt-out, for a server that speaks static tokens
	// only and would otherwise have an authorization flow attempted against it.
	Disabled bool
}

// Confidential reports whether these settings describe a confidential client, which is the
// only case that carries a secret and therefore the only case that needs pinned endpoints.
func (o OAuthSettings) Confidential() bool { return o.ClientSecret != "" }

// Preregistered reports whether the operator supplied a client the authorization server
// already knows. When false the SDK falls through to dynamic registration.
func (o OAuthSettings) Preregistered() bool { return o.ClientID != "" }

// OAuthSettingsFromEnv parses a managed server's Env into OAuthSettings and refuses the two
// configurations that are unsafe rather than merely incomplete.
func OAuthSettingsFromEnv(env []string) (OAuthSettings, error) {
	var out OAuthSettings
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case envOAuthClientID:
			out.ClientID = value
		case envOAuthClientSecret:
			out.ClientSecret = value
		case envOAuthScopes:
			out.Scopes = splitScopes(value)
		case envOAuthAuthURL:
			out.AuthorizationURL = value
		case envOAuthTokenURL:
			out.TokenURL = value
		case envOAuthRedirectURL:
			out.RedirectURL = value
		case envOAuthDisabled:
			out.Disabled = isTruthy(value)
		}
	}
	if err := out.validate(); err != nil {
		return OAuthSettings{}, err
	}
	return out, nil
}

func (o OAuthSettings) validate() error {
	if o.Confidential() && !o.Preregistered() {
		return ErrOAuthSecretWithoutClientID
	}
	if o.Confidential() && (o.AuthorizationURL == "" || o.TokenURL == "") {
		return ErrOAuthSecretNeedsPinnedEndpoints
	}
	// A pinned endpoint that is not HTTPS defeats the point of pinning it: the operator
	// wrote it down so the secret goes to a known party, and plaintext HTTP means anyone
	// on the path is that party. Loopback is exempt because a redirect lands there by
	// design and carries no secret.
	for name, raw := range map[string]string{
		envOAuthAuthURL:  o.AuthorizationURL,
		envOAuthTokenURL: o.TokenURL,
	} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("mcp oauth: %s is not a URL: %w", name, err)
		}
		if u.Scheme != "https" {
			return fmt.Errorf("mcp oauth: %s must be https, got %q", name, u.Scheme)
		}
	}
	return nil
}

// splitScopes accepts either the space separation OAuth uses on the wire or the comma an
// operator is likelier to type, because guessing wrong costs a silent scope mismatch that
// only shows up as a permission error much later.
func splitScopes(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isTruthy reads an operator's opt-out generously. Anything that is not recognizably true
// leaves the flag off, so a typo cannot silently disable an authorization flow.
func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// UsesOAuth reports whether a mount should carry an authorization flow for this server.
//
// The precedence is the interesting part. A static MCP_BEARER_TOKEN WINS: it is an explicit,
// unambiguous statement by the operator that this server is reached with this credential,
// and starting a browser flow over the top of it would be Aura overriding an instruction.
// Everything else remote gets OAuth attempted, INCLUDING a server with no oauth config at
// all — that is the Linear/Atlassian case, where dynamic client registration means correct
// behaviour requires zero configuration. A server that supports no authorization simply
// never returns a 401, so the handler is never asked to do anything.
func UsesOAuth(server ManagedServer, settings OAuthSettings) bool {
	if settings.Disabled {
		return false
	}
	// normalizedServerType, NOT mcptools.isStreamableHTTPManagedServer: the two resolve an
	// unclassifiable config in OPPOSITE directions, each correctly for its own caller. The
	// mount path calls a broken config HTTP so the transport surfaces the classification
	// error instead of silently spawning a subprocess; here the safer answer is the other
	// one, because opening a browser against a config Aura cannot even classify is worse
	// than opening none. The mount still reports the error either way.
	if normalizedServerType(server) != ServerTypeStreamableHTTP {
		return false
	}
	_, bearer := httpAuthFromEnv(server.Env)
	return bearer == ""
}
