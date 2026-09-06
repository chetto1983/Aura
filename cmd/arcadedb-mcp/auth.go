package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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
	// oauthIssuerClaim and oauthSubjectClaim carry the pair the identity was derived
	// from, so an audit can answer "which account is this" for a foreign subject whose
	// derived UUID says nothing on its own.
	oauthIssuerClaim  = "iss"
	oauthSubjectClaim = "sub"
)

type oauthResourceConfig struct {
	// Issuers is every authorization server whose tokens are accepted, HOME FIRST.
	// A list rather than a scalar because an MCP server is a resource server and the
	// spec lets its authorization server be a separate entity -- see auth_issuers.go,
	// which also owns the (issuer, subject) -> identity rule.
	Issuers []trustedIssuer
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
	home := newTrustedIssuer(
		envOrDefault("MCP_OAUTH_ISSUER", "http://localhost:9080"),
		os.Getenv("MCP_OAUTH_JWKS_URL"),
	)
	audiences := splitAudiences(envOrDefault("MCP_OAUTH_RESOURCE", "http://localhost:8096/mcp/"))
	return oauthResourceConfig{
		Issuers:   append([]trustedIssuer{home}, parseTrustedIssuers(os.Getenv("MCP_OAUTH_TRUSTED_ISSUERS"))...),
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

	// One key set per trusted issuer. A single cache would serve whichever issuer
	// asked last, so a token from the home issuer could be validated against a
	// foreign issuer's keys -- and vice versa.
	mu   sync.Mutex
	keys map[string]cachedKeySet
	// lastJWKSFailure remembers what was last reported per issuer so an unreachable
	// authorization server is stated once, not once per request. See logJWKSFailure.
	lastJWKSFailure map[string]jwksFailure
}

// jwksFailure is the last reported JWKS problem for one issuer: what went wrong and when
// it was said out loud.
type jwksFailure struct {
	reason string
	at     time.Time
}

// jwksFailureRestateAfter is how long an UNCHANGED JWKS failure stays quiet before it is
// restated. It exists because the obvious fix — log every failure — is how a single
// misconfiguration becomes a log flood: this server is called on every agent turn.
const jwksFailureRestateAfter = time.Minute

type cachedKeySet struct {
	set       jwk.Set
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
	return &arcadeTokenVerifier{config: config, client: client, now: time.Now, keys: map[string]cachedKeySet{}}
}

func (v *arcadeTokenVerifier) Verify(ctx context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
	issuer, err := v.trustedIssuerOf(raw)
	if err != nil {
		return nil, err
	}
	keys, err := v.keySet(ctx, issuer, false)
	if err != nil {
		return nil, err
	}
	token, err := v.parse(raw, issuer, keys)
	if err != nil {
		keys, refreshErr := v.keySet(ctx, issuer, true)
		if refreshErr != nil {
			return nil, refreshErr
		}
		token, err = v.parse(raw, issuer, keys)
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
		Scopes:     strings.Fields(scope),
		Expiration: expiration,
		// The IDENTITY, not the raw subject. Which memory a caller reaches is a
		// property of (issuer, subject) together, and resolving it here — where the
		// trusted-issuer set lives — is what keeps every handler downstream reading
		// one unambiguous field. The raw subject stays in Extra for provenance.
		UserID: v.config.tenantIdentity(issuer.Issuer, subject),
		Extra: map[string]any{
			oauthIssuerClaim:  issuer.Issuer,
			oauthSubjectClaim: strings.TrimSpace(subject),
		},
	}
	if clientID, ok := stringClaim(token, oauthClientIDClaim); ok && strings.TrimSpace(clientID) != "" {
		info.Extra[oauthClientIDClaim] = strings.TrimSpace(clientID)
	}
	return info, nil
}

// trustedIssuerOf reads the token's `iss` WITHOUT verifying it, purely to decide which
// key set to verify it against — there is no way to pick a JWKS before knowing who
// claims to have signed the token. Nothing is trusted on the strength of this read:
// parse() below pins jwt.WithIssuer to the matched issuer, so a token naming one
// issuer and signed by another fails there.
func (v *arcadeTokenVerifier) trustedIssuerOf(raw string) (trustedIssuer, error) {
	token, err := jwt.Parse([]byte(raw), jwt.WithVerify(false), jwt.WithValidate(false))
	if err != nil {
		return trustedIssuer{}, fmt.Errorf("%w: bearer token is not a JWT", auth.ErrInvalidToken)
	}
	claimed, _ := token.Issuer()
	issuer, ok := v.config.issuerNamed(claimed)
	if !ok {
		return trustedIssuer{}, fmt.Errorf("%w: bearer token issuer %q is not one of %v",
			auth.ErrInvalidToken, claimed, v.config.issuerNames())
	}
	return issuer, nil
}

func (v *arcadeTokenVerifier) parse(raw string, issuer trustedIssuer, keys jwk.Set) (jwt.Token, error) {
	// Audience is checked separately: repeated jwt.WithAudience options are ANDed by
	// the library, so a token would have to carry EVERY accepted name at once --
	// the opposite of what a server with several names needs.
	token, err := jwt.Parse([]byte(raw),
		jwt.WithKeySet(keys, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
		jwt.WithIssuer(issuer.Issuer),
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

func (v *arcadeTokenVerifier) keySet(ctx context.Context, issuer trustedIssuer, force bool) (jwk.Set, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if cached, ok := v.keys[issuer.Issuer]; ok && !force && v.now().Sub(cached.fetchedAt) < arcadeJWKSCacheTTL {
		return cached.set, nil
	}
	keys, err := v.fetchKeySet(ctx, issuer)
	if err != nil {
		// A JWKS this server cannot reach is an OPERATIONAL failure, not a bad token, so it
		// is deliberately NOT wrapped in auth.ErrInvalidToken: telling the client its token
		// is invalid would send it to re-authenticate forever against a server that cannot
		// verify anything. The middleware renders it as a 500 — correct, and until now
		// completely silent.
		//
		// Measured 2026-09-06 on a live stack: with Aura running as a NATIVE process rather
		// than inside compose, the default MCP_OAUTH_JWKS_URL (http://aura:9080/oauth/jwks,
		// a compose service name) does not resolve, so every `initialize` answered
		// "Internal Server Error" and the sidecar log stayed empty. The operator had no way
		// to learn that a DNS name was the whole problem.
		v.logJWKSFailure(issuer, err)
		return nil, err
	}
	if v.keys == nil {
		v.keys = map[string]cachedKeySet{}
	}
	v.keys[issuer.Issuer] = cachedKeySet{set: keys, fetchedAt: v.now()}
	delete(v.lastJWKSFailure, issuer.Issuer)
	return keys, nil
}

// fetchKeySet performs the JWKS round trip. Split from keySet so there is exactly one
// place that decides an attempt failed, and therefore exactly one place that reports it.
func (v *arcadeTokenVerifier) fetchKeySet(ctx context.Context, issuer trustedIssuer) (jwk.Set, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer.JWKSURL, nil)
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
	return keys, nil
}

// logJWKSFailure states the problem once per issuer and then keeps quiet until the reason
// CHANGES or jwksFailureRestateAfter has passed. Callers hold v.mu.
//
// The restatement matters as much as the suppression: an outage that has lasted an hour
// should still be visible in the last minute of logs, not only in the first.
func (v *arcadeTokenVerifier) logJWKSFailure(issuer trustedIssuer, err error) {
	reason := err.Error()
	now := v.now()
	if last, ok := v.lastJWKSFailure[issuer.Issuer]; ok &&
		last.reason == reason && now.Sub(last.at) < jwksFailureRestateAfter {
		return
	}
	if v.lastJWKSFailure == nil {
		v.lastJWKSFailure = map[string]jwksFailure{}
	}
	v.lastJWKSFailure[issuer.Issuer] = jwksFailure{reason: reason, at: now}
	slog.Error("MCP OAuth: the issuer's JWKS is unreachable, so every bearer token is refused",
		"issuer", issuer.Issuer, "jwks_url", issuer.JWKSURL, "err", reason)
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
		AuthorizationServers: config.issuerNames(),
		ScopesSupported:      []string{config.Scope},
	}
	return auth.ProtectedResourceMetadataHandler(metadata)
}
