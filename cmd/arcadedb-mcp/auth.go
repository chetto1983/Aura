package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
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
	arcadeMCPPath       = "/mcp/"
	arcadeJWKSCacheTTL  = 5 * time.Minute
	arcadeJWKSBodyLimit = 1 << 20
	defaultOAuthScope   = "mcp:tools"
)

type oauthResourceConfig struct {
	Issuer   string
	JWKSURL  string
	Resource string
	Scope    string
}

func oauthResourceConfigFromEnv() oauthResourceConfig {
	issuer := envOrDefault("MCP_OAUTH_ISSUER", "http://localhost:9080")
	return oauthResourceConfig{
		Issuer:   strings.TrimRight(issuer, "/"),
		JWKSURL:  envOrDefault("MCP_OAUTH_JWKS_URL", strings.TrimRight(issuer, "/")+"/oauth/jwks"),
		Resource: envOrDefault("MCP_OAUTH_RESOURCE", "http://localhost:8096/mcp/"),
		Scope:    envOrDefault("MCP_OAUTH_SCOPE", defaultOAuthScope),
	}
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
	return &auth.TokenInfo{
		Scopes: strings.Fields(scope), Expiration: expiration, UserID: strings.TrimSpace(subject),
	}, nil
}

func (v *arcadeTokenVerifier) parse(raw string, keys jwk.Set) (jwt.Token, error) {
	return jwt.Parse([]byte(raw),
		jwt.WithKeySet(keys, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.config.Issuer),
		jwt.WithAudience(v.config.Resource),
		jwt.WithRequiredClaim("scope"),
		jwt.WithRequiredClaim("sub"),
	)
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
