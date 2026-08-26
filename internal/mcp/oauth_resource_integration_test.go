//go:build calendar_integration || whatsapp_integration

package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const oauthResourceTestIssuer = "http://127.0.0.1:18090"

type oauthResourceIssuer struct {
	private      jwk.Key
	jwksRequests atomic.Int32
}

func newOAuthResourceIssuer(t *testing.T) *oauthResourceIssuer {
	t.Helper()
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate OAuth fixture key: %v", err)
	}
	private, err := jwk.Import(raw)
	if err != nil {
		t.Fatalf("import OAuth fixture key: %v", err)
	}
	if err := jwk.AssignKeyID(private); err != nil {
		t.Fatal(err)
	}
	if err := private.Set(jwk.AlgorithmKey, jwa.RS256()); err != nil {
		t.Fatal(err)
	}
	public, err := private.PublicKey()
	if err != nil {
		t.Fatalf("derive OAuth fixture public key: %v", err)
	}
	keys := jwk.NewSet()
	if err := keys.AddKey(public); err != nil {
		t.Fatal(err)
	}

	fixture := &oauthResourceIssuer{private: private}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		fixture.jwksRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(keys)
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                oauthResourceTestIssuer,
			"jwks_uri":                              "http://" + r.Host + "/jwks",
			"authorization_endpoint":                oauthResourceTestIssuer + "/authorize",
			"token_endpoint":                        oauthResourceTestIssuer + "/token",
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"token_endpoint_auth_methods_supported": []string{"none"},
			"code_challenge_methods_supported":      []string{"S256"},
		})
	})
	listener, err := net.Listen("tcp", "0.0.0.0:18090")
	if err != nil {
		t.Fatalf("listen on OAuth fixture %s: %v", oauthResourceTestIssuer, err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("shutdown OAuth fixture: %v", err)
		}
		if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("OAuth fixture: %v", err)
		}
	})
	return fixture

}

func (f *oauthResourceIssuer) token(t *testing.T, audience, subject string) string {
	t.Helper()
	token := jwt.New()
	for name, value := range map[string]any{
		"iss": oauthResourceTestIssuer, "aud": audience, "sub": subject,
		"scope": "mcp:tools", "client_id": "resource-integration", "exp": time.Now().Add(time.Hour),
	} {
		if err := token.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256(), f.private))
	if err != nil {
		t.Fatalf("sign OAuth fixture token: %v", err)
	}
	segments := strings.Split(string(signed), ".")
	if len(segments) != 3 {
		t.Fatalf("OAuth fixture produced %d JWT segments", len(segments))
	}
	encodedHeader, err := base64.RawURLEncoding.DecodeString(segments[0])
	if err != nil {
		t.Fatalf("decode OAuth fixture header: %v", err)
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := json.Unmarshal(encodedHeader, &header); err != nil {
		t.Fatalf("decode OAuth fixture header JSON: %v", err)
	}
	if header.Algorithm != "RS256" || header.KeyID == "" {
		t.Fatalf("OAuth fixture header alg=%q kid_present=%t", header.Algorithm, header.KeyID != "")
	}
	return string(signed)
}

func assertOAuthResourceAuthenticates(t *testing.T, ctx context.Context, issuer *oauthResourceIssuer, endpoint, token string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte("{}")))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("OAuth resource preflight: %v", err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
			return
		}
		requests := issuer.jwksRequests.Load()
		if requests > 0 || time.Now().After(deadline) {
			t.Fatalf("OAuth resource rejected an explicit bearer with HTTP %d (JWKS requests: %d)", response.StatusCode, requests)
		}
		// Docker Desktop publishes a newly opened WSL listener asynchronously. Retry
		// only while the issuer proves the resource server has not reached it yet.
		time.Sleep(50 * time.Millisecond)
	}
}

func openOAuthResourceSession(t *testing.T, ctx context.Context, issuer *oauthResourceIssuer, name, endpoint, token string) *sdkmcp.ClientSession {
	t.Helper()
	server := ManagedServer{
		Type:  ServerTypeStreamableHTTP,
		URL:   endpoint,
		Env:   []string{"MCP_BEARER_TOKEN=" + token},
		Trust: ManagedTrust{Class: TrustTrustedRecipe},
	}
	session, err := OpenSDKSession(ctx, name, server, EgressPolicy{}, SessionOptions{})
	if err != nil {
		t.Fatalf("OpenSDKSession %s at %s: %v (JWKS requests: %d)", name, endpoint, err, issuer.jwksRequests.Load())
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func assertOAuthResourceRejectsAnonymous(t *testing.T, ctx context.Context, name, endpoint string) {
	t.Helper()
	server := ManagedServer{
		Type:  ServerTypeStreamableHTTP,
		URL:   endpoint,
		Trust: ManagedTrust{Class: TrustTrustedRecipe},
	}
	if session, err := OpenSDKSession(ctx, name, server, EgressPolicy{}, SessionOptions{}); err == nil {
		_ = session.Close()
		t.Fatalf("%s accepted an anonymous MCP session", name)
	}
}
