package mcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type observedMemoryClaims struct {
	Subject  string `json:"sub"`
	Issuer   string `json:"iss"`
	Audience string `json:"aud"`
	Scope    string `json:"scope"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

func decodeMemoryClaims(t *testing.T, secret, authorization string) observedMemoryClaims {
	t.Helper()
	raw := strings.TrimPrefix(authorization, "Bearer ")
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("Authorization token has %d segments, want 3", len(parts))
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		t.Fatal("Authorization token has an invalid HS256 signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var claims observedMemoryClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("decode JWT claims: %v", err)
	}
	return claims
}

func TestOpenServerMemoryFailsClosedWithoutAuthSecret(t *testing.T) {
	t.Setenv("AURA_AGENT_MEMORY_MCP_AUTH_SECRET", "")

	_, err := OpenServer(context.Background(), "memory", ManagedServer{
		Type:   ServerTypeStreamableHTTP,
		URL:    "http://127.0.0.1:1/mcp/",
		Source: SourceRecipeMemory,
	})

	if err == nil || !strings.Contains(err.Error(), "AURA_AGENT_MEMORY_MCP_AUTH_SECRET") {
		t.Fatalf("OpenServer(memory) error = %v, want missing-secret failure before network", err)
	}
}

func TestOpenServerMemorySignsServiceAndTenantRequests(t *testing.T) {
	const secret = "go-memory-auth-test-secret-that-is-at-least-32-bytes"
	t.Setenv("AURA_AGENT_MEMORY_MCP_AUTH_SECRET", secret)
	var mu sync.Mutex
	var observed []observedMemoryClaims

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := decodeMemoryClaims(t, secret, r.Header.Get("Authorization"))
		mu.Lock()
		observed = append(observed, claims)
		mu.Unlock()
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "tenant-auth-session")
			writeHTTPRPC(t, w, req.ID, map[string]any{
				"protocolVersion": httpProtocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "memory", "version": "1"},
			})
			return
		}
		writeHTTPRPC(t, w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer server.Close()

	transport, err := OpenServer(context.Background(), "memory", ManagedServer{
		Type:   ServerTypeStreamableHTTP,
		URL:    server.URL,
		Source: SourceRecipeMemory,
	})
	if err != nil {
		t.Fatalf("OpenServer(memory): %v", err)
	}
	defer func() { _ = transport.Close() }()

	if _, err := transport.CallTool(context.Background(), "memory_search", map[string]any{
		"query":           "operator name",
		"user_identifier": "identity-alice",
	}); err != nil {
		t.Fatalf("CallTool(memory_search): %v", err)
	}

	mu.Lock()
	got := append([]observedMemoryClaims(nil), observed...)
	mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("observed %d authenticated requests, want initialize, notification, tool call", len(got))
	}
	for i, claims := range got {
		if claims.Issuer != "aura" || claims.Audience != "agent-memory" ||
			claims.Scope != "memory:access" {
			t.Fatalf("request %d claims = %+v", i, claims)
		}
		if lifetime := claims.Expires - claims.IssuedAt; lifetime <= 0 || lifetime > 60 {
			t.Fatalf("request %d token lifetime = %d seconds, want 1..60", i, lifetime)
		}
	}
	if got[0].Subject != "aura-service" || got[1].Subject != "aura-service" {
		t.Fatalf("service request subjects = %q, %q", got[0].Subject, got[1].Subject)
	}
	if got[2].Subject != "identity-alice" {
		t.Fatalf("tool request subject = %q, want identity-alice", got[2].Subject)
	}
}

func TestMemoryToolCallRejectsMissingTenantBeforeNetwork(t *testing.T) {
	const secret = "go-memory-auth-test-secret-that-is-at-least-32-bytes"
	t.Setenv("AURA_AGENT_MEMORY_MCP_AUTH_SECRET", secret)
	var calls int
	server := httptest.NewServer(httpInitHandler(t, "tenant-auth-session", func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		calls++
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeHTTPRPC(t, w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "unexpected"}},
		})
	}))
	defer server.Close()

	transport, err := OpenServer(context.Background(), "memory", ManagedServer{
		Type:   ServerTypeStreamableHTTP,
		URL:    server.URL,
		Source: SourceRecipeMemory,
	})
	if err != nil {
		t.Fatalf("OpenServer(memory): %v", err)
	}
	defer func() { _ = transport.Close() }()
	before := calls

	_, err = transport.CallTool(
		context.Background(),
		"memory_search",
		map[string]any{"query": "operator name"},
	)

	if err == nil || !strings.Contains(err.Error(), "user_identifier") {
		t.Fatalf("CallTool without tenant error = %v", err)
	}
	if calls != before {
		t.Fatalf("tool call reached network: calls %d -> %d", before, calls)
	}
}
