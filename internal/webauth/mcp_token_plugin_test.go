package webauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	jwtservices "github.com/Authula/authula/plugins/jwt/services"
	jwtoptions "github.com/Authula/authula/plugins/jwt/types"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

type recordingTokenService struct {
	claims map[string]any
}

func (r *recordingTokenService) GenerateUserToken(_ context.Context, _, _ string, claims map[string]any) (*jwtoptions.TokenPair, error) {
	r.claims = claims
	return &jwtoptions.TokenPair{}, nil
}

func (*recordingTokenService) GenerateMachineToken(context.Context, string, string, []string) (*jwtoptions.TokenPair, error) {
	return &jwtoptions.TokenPair{}, nil
}

func (*recordingTokenService) ExtractClaims(context.Context, string) (map[string]any, error) {
	return nil, nil
}

func TestSubjectTokenServicePreservesResourceOwnerSubject(t *testing.T) {
	inner := &recordingTokenService{}
	service := &subjectTokenService{TokenService: inner}
	original := map[string]any{mcpSubjectClaim: "tenant-uuid", "aud": "https://mcp.example"}

	if _, err := service.GenerateUserToken(t.Context(), "login-user", "session", original); err != nil {
		t.Fatal(err)
	}
	if got := inner.claims["sub"]; got != "tenant-uuid" {
		t.Fatalf("sub = %v, want tenant-uuid", got)
	}
	if _, changed := original["sub"]; changed {
		t.Fatal("GenerateUserToken mutated caller claims")
	}
}

type fixedJWKSCache struct {
	jwtservices.CacheService
	set jwk.Set
}

func (f fixedJWKSCache) GetJWKSWithFallback(context.Context) (jwk.Set, error) {
	return f.set, nil
}

func TestSubjectTokenServiceVerifiesRefreshClaims(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := jwk.Import(private)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := jwk.Import(public)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []jwk.Key{privateKey, publicKey} {
		if err := key.Set(jwk.KeyIDKey, "fixture-key"); err != nil {
			t.Fatal(err)
		}
		if err := key.Set(jwk.AlgorithmKey, "EdDSA"); err != nil {
			t.Fatal(err)
		}
	}
	set := jwk.NewSet()
	if err := set.AddKey(publicKey); err != nil {
		t.Fatal(err)
	}
	token := jwt.New()
	if err := token.Set("client_id", "aura"); err != nil {
		t.Fatal(err)
	}
	if err := token.Set(jwt.ExpirationKey, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSA(), privateKey))
	if err != nil {
		t.Fatal(err)
	}
	service := &subjectTokenService{keys: fixedJWKSCache{set: set}}
	claims, err := service.ExtractClaims(t.Context(), string(signed))
	if err != nil {
		t.Fatal(err)
	}
	if claims["client_id"] != "aura" {
		t.Fatalf("client_id = %v, want aura", claims["client_id"])
	}
	signed[len(signed)-1] ^= 1
	if _, err := service.ExtractClaims(t.Context(), string(signed)); err == nil {
		t.Fatal("tampered refresh token passed signature verification")
	}
}
