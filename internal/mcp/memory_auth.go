package mcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	memoryAuthSecretEnv  = "AURA_AGENT_MEMORY_MCP_AUTH_SECRET" //nolint:gosec // G101 false positive: env var NAME, not a credential
	memoryJWTIssuer      = "aura"
	memoryJWTAudience    = "agent-memory"
	memoryJWTScope       = "memory:access"
	memoryServiceSubject = "aura-service"
	memoryJWTLifetime    = 60 * time.Second
)

type bearerSubjectKey struct{}

type memoryJWTSource struct {
	secret []byte
	now    func() time.Time
}

type memoryJWTClaims struct {
	Subject  string `json:"sub"`
	Issuer   string `json:"iss"`
	Audience string `json:"aud"`
	Scope    string `json:"scope"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

func newMemoryJWTSource(secret string) (*memoryJWTSource, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("%s is required", memoryAuthSecretEnv)
	}
	if len([]byte(secret)) < 32 {
		return nil, fmt.Errorf("%s must be at least 32 bytes", memoryAuthSecretEnv)
	}
	return &memoryJWTSource{secret: []byte(secret), now: time.Now}, nil
}

func (s *memoryJWTSource) token(ctx context.Context) (string, error) {
	subject, _ := ctx.Value(bearerSubjectKey{}).(string)
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = memoryServiceSubject
	}
	now := s.now().UTC().Truncate(time.Second)
	claims := memoryJWTClaims{
		Subject: subject, Issuer: memoryJWTIssuer, Audience: memoryJWTAudience,
		Scope: memoryJWTScope, IssuedAt: now.Unix(), Expires: now.Add(memoryJWTLifetime).Unix(),
	}
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("encode JWT header: %w", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode JWT claims: %w", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func withBearerSubject(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, bearerSubjectKey{}, subject)
}
