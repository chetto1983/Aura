package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type arcadeAuthFixture struct {
	issuer  string
	private jwk.Key
	server  *httptest.Server
	calls   atomic.Int32
}

func newArcadeAuthFixture(t *testing.T) *arcadeAuthFixture {
	t.Helper()
	return newArcadeAuthFixtureFor(t, "https://issuer.example")
}

// newArcadeAuthFixtureFor builds an authorization server with its OWN signing key, so a
// test can hold two issuers at once and check that neither one's keys validate the
// other's tokens.
func newArcadeAuthFixtureFor(t *testing.T, issuer string) *arcadeAuthFixture {
	t.Helper()
	_, privateRaw, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private, err := jwk.Import(privateRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := private.Set(jwk.KeyIDKey, "test-key"); err != nil {
		t.Fatal(err)
	}
	if err := private.Set(jwk.AlgorithmKey, jwa.EdDSA()); err != nil {
		t.Fatal(err)
	}
	public, err := private.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(public); err != nil {
		t.Fatal(err)
	}
	fixture := &arcadeAuthFixture{issuer: issuer, private: private}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fixture.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(server.Close)
	fixture.server = server
	return fixture
}

func (f *arcadeAuthFixture) token(t *testing.T, audience, identity, scope string, expires time.Time) string {
	t.Helper()
	return f.tokenIssuedAs(t, f.issuer, audience, identity, scope, expires)
}

// tokenIssuedAs signs with THIS fixture's key while claiming an arbitrary issuer, which
// is how a test forges the one thing the unverified issuer peek must not buy.
func (f *arcadeAuthFixture) tokenIssuedAs(t *testing.T, issuer, audience, identity, scope string, expires time.Time) string {
	t.Helper()
	token := jwt.New()
	claims := map[string]any{
		"iss": issuer, "aud": audience, "sub": identity,
		"scope": scope, "exp": expires,
	}
	for name, value := range claims {
		if err := token.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSA(), f.private))
	if err != nil {
		t.Fatal(err)
	}
	return string(signed)
}

func TestArcadeOAuthMiddlewareRequiresAudienceBoundBearer(t *testing.T) {
	fixture := newArcadeAuthFixture(t)
	const resource = "http://memory.example/mcp/"
	const identity = "00000000-0000-0000-0000-000000000001"
	config := oauthResourceConfig{
		Issuers:  []trustedIssuer{{Issuer: fixture.issuer, JWKSURL: fixture.server.URL}},
		Resource: resource, Scope: defaultOAuthScope,
	}
	verifier := newArcadeTokenVerifier(config, fixture.server.Client())
	valid := fixture.token(t, resource, identity, defaultOAuthScope, time.Now().Add(time.Hour))
	wrongAudience := fixture.token(t, "http://foreign.example/mcp/", identity, defaultOAuthScope, time.Now().Add(time.Hour))
	expired := fixture.token(t, resource, identity, defaultOAuthScope, time.Now().Add(-time.Minute))

	seenIdentity := ""
	handler := protectedArcadeMCP(config, verifier.Verify, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenIdentity = auth.TokenInfoFromContext(r.Context()).UserID
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		name, token string
		status      int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "wrong audience", token: wrongAudience, status: http.StatusUnauthorized},
		{name: "expired", token: expired, status: http.StatusUnauthorized},
		{name: "valid", token: valid, status: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, resource, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status == http.StatusUnauthorized {
				challenge := rec.Header().Get("WWW-Authenticate")
				if !strings.Contains(challenge, "resource_metadata=\"http://memory.example/.well-known/oauth-protected-resource/mcp/\"") {
					t.Fatalf("WWW-Authenticate = %q", challenge)
				}
			}
		})
	}
	if seenIdentity != identity {
		t.Fatalf("verified identity = %q, want %q", seenIdentity, identity)
	}
}

func TestArcadeProtectedResourceMetadataUsesConfiguredOAuthContract(t *testing.T) {
	config := oauthResourceConfig{
		Issuers:  []trustedIssuer{{Issuer: "https://issuer.example"}},
		Resource: "https://memory.example/mcp/", Scope: "memory:tools",
	}
	req := httptest.NewRequest(http.MethodGet, "http://memory.example/.well-known/oauth-protected-resource/mcp/", nil)
	rec := httptest.NewRecorder()
	arcadeProtectedResourceMetadata(config).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var metadata oauthex.ProtectedResourceMetadata
	if err := json.NewDecoder(rec.Body).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Resource != config.Resource {
		t.Fatalf("resource = %q", metadata.Resource)
	}
	if len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != config.homeIssuer().Issuer {
		t.Fatalf("authorization servers = %#v", metadata.AuthorizationServers)
	}
}

func TestArcadeVerifierCachesJWKS(t *testing.T) {
	fixture := newArcadeAuthFixture(t)
	const resource = "http://memory.example/mcp/"
	config := oauthResourceConfig{
		Issuers:  []trustedIssuer{{Issuer: fixture.issuer, JWKSURL: fixture.server.URL}},
		Resource: resource, Scope: defaultOAuthScope,
	}
	verifier := newArcadeTokenVerifier(config, fixture.server.Client())
	raw := fixture.token(t, resource, "tenant", defaultOAuthScope, time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodPost, resource, nil)
	for range 2 {
		if _, err := verifier.Verify(context.Background(), raw, req); err != nil {
			t.Fatal(err)
		}
	}
	if got := fixture.calls.Load(); got != 1 {
		t.Fatalf("JWKS requests = %d, want 1 cached fetch", got)
	}
}

func TestOAuthResourceConfigUsesGenericEnvironment(t *testing.T) {
	t.Setenv("MCP_OAUTH_ISSUER", "https://issuer.example/")
	t.Setenv("MCP_OAUTH_JWKS_URL", "https://issuer.example/keys")
	t.Setenv("MCP_OAUTH_RESOURCE", "https://memory.example/mcp/")
	t.Setenv("MCP_OAUTH_SCOPE", "memory:tools")

	got := oauthResourceConfigFromEnv()
	if got.homeIssuer().Issuer != "https://issuer.example" ||
		got.homeIssuer().JWKSURL != "https://issuer.example/keys" ||
		got.Resource != "https://memory.example/mcp/" || got.Scope != "memory:tools" {
		t.Fatalf("OAuth config = %#v", got)
	}
}
