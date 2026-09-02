package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const (
	arcadeJWKSCacheTTL  = 5 * time.Minute
	arcadeJWKSBodyLimit = 1 << 20
	defaultOAuthScope   = "mcp:tools"
	// oauthClientIDClaim is carried through into auth.TokenInfo.Extra because it
	// is the only stable name an external MCP client has once the session id is
	// gone, and hostDerivedActor writes it as fact provenance.
	oauthClientIDClaim = "client_id"
)

type oauthResourceConfig struct {
	Issuer  string
	JWKSURL string
	// Resource is the CANONICAL resource identifier: the one advertised in the
	// protected-resource metadata, which MCP clients also treat as the server's
	// address. It must therefore be the URL the CLIENT can reach.
	Resource string
	// Audiences is every resource identifier a token may legitimately carry,
	// canonical first. One server is reachable under two names: an MCP client on the
	// host dials http://127.0.0.1:8096/mcp while `aura` inside the Compose network
	// dials http://aura-arcadedb-mcp:8096/mcp/. Validating a single value breaks
	// whichever client is not the one the operator picked.
	//
	// Both halves are load-bearing, measured 2026-09-02:
	//   - advertising the CONTAINER name made Claude Code resolve it as the server
	//     address and fail with ENOTFOUND before authentication could even start;
	//   - advertising ONLY the loopback name would break the in-container agent,
	//     because cmd/aura/mcp_first_party_grants.go pins the self-issued grant's
	//     audience to the server's own dial URL (ResourceURL: server.URL), which in
	//     Compose must stay aura-arcadedb-mcp:8096.
	// Accepting both is what lets one server answer to both callers.
	Audiences []string
	Scope     string
}

func oauthResourceConfigFromEnv() oauthResourceConfig {
	issuer := envOrDefault("MCP_OAUTH_ISSUER", "http://localhost:9080")
	audiences := splitAudiences(envOrDefault("MCP_OAUTH_RESOURCE", "http://localhost:8096/mcp/"))
	return oauthResourceConfig{
		Issuer:    strings.TrimRight(issuer, "/"),
		JWKSURL:   envOrDefault("MCP_OAUTH_JWKS_URL", strings.TrimRight(issuer, "/")+"/oauth/jwks"),
		Resource:  audiences[0],
		Audiences: audiences,
		Scope:     envOrDefault("MCP_OAUTH_SCOPE", defaultOAuthScope),
	}
}

// splitAudiences reads MCP_OAUTH_RESOURCE as a comma-separated list so one server
// can answer to the several names it is reachable under. The FIRST entry is
// canonical and the only one advertised; the rest are merely accepted. Always
// returns at least one element, so callers need no emptiness check.
func splitAudiences(raw string) []string {
	audiences := make([]string, 0, 2)
	for part := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			audiences = append(audiences, trimmed)
		}
	}
	if len(audiences) == 0 {
		return []string{"http://localhost:8096/mcp/"}
	}
	return audiences
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

type arcadeTokenVerifier struct {
	config oauthResourceConfig
	client *http.Client
	now    func() time.Time

	mu        sync.Mutex
	keys      jwk.Set
	fetchedAt time.Time
}

func newArcadeTokenVerifier(config oauthResourceConfig, client *http.Client) *arcadeTokenVerifier {
	if client == nil {
		client = &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &arcadeTokenVerifier{config: config, client: client, now: time.Now}
}

func (v *arcadeTokenVerifier) Verify(ctx context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
	keys, err := v.keySet(ctx, false)
	if err != nil {
		return nil, err
	}
	token, err := v.parse(raw, keys)
	if err != nil {
		keys, refreshErr := v.keySet(ctx, true)
		if refreshErr != nil {
			return nil, refreshErr
		}
		token, err = v.parse(raw, keys)
		if err != nil {
			return nil, fmt.Errorf("%w: bearer token validation failed", auth.ErrInvalidToken)
		}
	}
	subject, ok := stringClaim(token, "sub")
	if !ok || strings.TrimSpace(subject) == "" {
		return nil, fmt.Errorf("%w: bearer token has no subject", auth.ErrInvalidToken)
	}
	scope, _ := stringClaim(token, "scope")
	expiration, ok := token.Expiration()
	if !ok {
		return nil, fmt.Errorf("%w: bearer token has no expiration", auth.ErrInvalidToken)
	}
	info := &auth.TokenInfo{
		Scopes: strings.Fields(scope), Expiration: expiration, UserID: strings.TrimSpace(subject),
	}
	if clientID, ok := stringClaim(token, oauthClientIDClaim); ok && strings.TrimSpace(clientID) != "" {
		info.Extra = map[string]any{oauthClientIDClaim: strings.TrimSpace(clientID)}
	}
	return info, nil
}

func (v *arcadeTokenVerifier) parse(raw string, keys jwk.Set) (jwt.Token, error) {
	// Audience is checked separately: repeated jwt.WithAudience options are ANDed by
	// the library, so a token would have to carry EVERY accepted name at once --
	// the opposite of what a server with several names needs.
	token, err := jwt.Parse([]byte(raw),
		jwt.WithKeySet(keys, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.config.Issuer),
		jwt.WithRequiredClaim("scope"),
		jwt.WithRequiredClaim("sub"),
	)
	if err != nil {
		return nil, err
	}
	audience, _ := token.Audience()
	accepted := v.config.acceptedAudiences()
	if !audienceAccepted(audience, accepted) {
		return nil, fmt.Errorf("%w: bearer token audience %v is not one of %v",
			auth.ErrInvalidToken, audience, accepted)
	}
	return token, nil
}

// acceptedAudiences is the audience allow-list, falling back to the canonical
// Resource when Audiences was never populated. A config built literally (tests, and
// any future caller that sets only Resource) then keeps the single-audience
// behaviour instead of silently accepting nothing.
func (c oauthResourceConfig) acceptedAudiences() []string {
	if len(c.Audiences) > 0 {
		return c.Audiences
	}
	if strings.TrimSpace(c.Resource) == "" {
		return nil
	}
	return []string{c.Resource}
}

// audienceAccepted reports whether the token names ANY accepted resource. A token
// with no audience is rejected: a bearer bound to nothing is replayable against
// every resource that trusts this issuer.
func audienceAccepted(tokenAudience, accepted []string) bool {
	for _, candidate := range tokenAudience {
		if slices.Contains(accepted, candidate) {
			return true
		}
	}
	return false
}

func (v *arcadeTokenVerifier) keySet(ctx context.Context, force bool) (jwk.Set, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !force && v.keys != nil && v.now().Sub(v.fetchedAt) < arcadeJWKSCacheTTL {
		return v.keys, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.config.JWKSURL, nil)
	if err != nil {
		return nil, fmt.Errorf("MCP OAuth JWKS request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MCP OAuth JWKS fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP OAuth JWKS fetch: HTTP %d", resp.StatusCode)
	}
	keys, err := jwk.ParseReader(io.LimitReader(resp.Body, arcadeJWKSBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("MCP OAuth JWKS decode: %w", err)
	}
	v.keys, v.fetchedAt = keys, v.now()
	return keys, nil
}

func stringClaim(token jwt.Token, name string) (string, bool) {
	var value string
	if err := token.Get(name, &value); err != nil {
		return "", false
	}
	return value, true
}

func requestResourceURL(r *http.Request, path string) string {
	scheme := "http"
	if r != nil && r.TLS != nil {
		scheme = "https"
	}
	host := ""
	if r != nil {
		host = r.Host
	}
	return scheme + "://" + host + path
}

func protectedArcadeMCP(config oauthResourceConfig, verifier auth.TokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadataURL := requestResourceURL(r, "/.well-known/oauth-protected-resource/mcp/")
		auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
			ResourceMetadataURL: metadataURL,
			Scopes:              []string{config.Scope},
		})(next).ServeHTTP(w, r)
	})
}

func arcadeProtectedResourceMetadata(config oauthResourceConfig) http.Handler {
	metadata := &oauthex.ProtectedResourceMetadata{
		Resource:             config.Resource,
		AuthorizationServers: []string{config.Issuer},
		ScopesSupported:      []string{config.Scope},
	}
	return auth.ProtectedResourceMetadataHandler(metadata)
}
