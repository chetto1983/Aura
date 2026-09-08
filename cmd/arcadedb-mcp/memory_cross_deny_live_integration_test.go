//go:build arcadedb_integration

// memory_cross_deny_live_integration_test.go asserts D-11 item 3: a verified access
// token whose subject is identity B reaches B's memory through the MCP boundary and
// none of A's, a hostile header naming A does not change that, and a call with no
// verified subject is refused -- all against a live ArcadeDB, through an IN-PROCESS
// httptest MCP server, NOT the deployed arcadedb-mcp compose service. That is
// deliberate: arcadedb-mcp declares `depends_on: aura: condition: service_started`
// (compose.yaml:695-699), so any `docker compose up` naming it starts the WHOLE aura
// daemon -- scheduler included -- against the same Postgres a tagged tier writes to.
// That is the measured CI #1809 race the Makefile documents at its memory-up-core
// target. Plan 01-04 must NOT add arcadedb-mcp to the musr-e2e job's bring-up on the
// assumption this test needs it -- it does not.
//
// Composes over newAgentMemoryLiveMCPWithOptions (memory_live_integration_helpers_test.go)
// for the expensive setup -- two identities, their ArcadeDB tenants, the embedder health
// probe, and the tenant resolver -- rather than standing up a parallel harness. This file
// builds only what that helper's own oauth fixture cannot prove: a resource server that
// trusts the PRODUCTION Authula issuer (webauth.NewLiveMCPTokenIssuer), so the token
// verification path under test is the real one, not the helper's own hand-signed
// ed25519 test fixture (newArcadeAuthFixture) every other live test in this package uses.
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	officialmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
	auramcp "github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/webauth"
)

// TestMemoryCrossDenyThroughTheMCPBoundary is the D-11 item 3 acceptance test.
func TestMemoryCrossDenyThroughTheMCPBoundary(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	ctx := t.Context()

	// Cleanup for both tenants is ALREADY registered by newAgentMemoryLiveMCPWithOptions
	// itself, via cleanupAgentMemoryLiveTenants (its own t.Cleanup, LIFO-ordered before
	// this test's own cleanups below run) -- this file writes no new teardown logic.
	_, identities, _, tenantClients := newAgentMemoryLiveMCPWithOptions(
		t, 2, "musr-cross-deny", agentMemoryLiveMCPOptions{strictDependencies: true},
	)
	idA, idB := identities[0], identities[1]

	// Precondition (halt, never skip -- this is asserted before any provisioning above
	// even completes doing real work): the migrated Authula schema and its signing
	// secret, both required for webauth.NewLiveMCPTokenIssuer.
	dsn := strings.TrimSpace(os.Getenv("AURA_DB_URL"))
	if dsn == "" {
		agentMemoryLiveDependencyGap(t, true, "AURA_DB_URL is not set")
	}
	secret := strings.TrimSpace(os.Getenv("AURA_AUTHULA_SECRET"))
	if secret == "" {
		agentMemoryLiveDependencyGap(t, true, "AURA_AUTHULA_SECRET is not set")
	}
	issuer, err := webauth.NewLiveMCPTokenIssuer(dsn, secret)
	if err != nil {
		t.Fatalf("webauth.NewLiveMCPTokenIssuer: %v", err)
	}
	t.Cleanup(func() {
		if err := issuer.Close(); err != nil {
			t.Errorf("close live MCP token issuer: %v", err)
		}
	})

	// A second in-process MCP resource server, over the SAME tenant resolver the helper
	// built, trusting the production Authula issuer through issuer.JWKSHandler -- so a
	// REAL AccessToken's verification path (not a hand-signed stand-in) is exercised.
	server := newServer(tenantClients, time.Now, "musr-cross-deny")
	jwks := httptest.NewServer(http.HandlerFunc(issuer.JWKSHandler))
	t.Cleanup(jwks.Close)

	httpSrv := httptest.NewUnstartedServer(nil)
	resource := "http://" + httpSrv.Listener.Addr().String() + "/mcp/"
	oauthConfig := oauthResourceConfig{
		Issuers:  []trustedIssuer{newTrustedIssuer(auramcp.AuraAuthorizationServerIssuer, jwks.URL)},
		Resource: resource, Scope: defaultOAuthScope,
	}
	verifier := newArcadeTokenVerifier(oauthConfig, jwks.Client())
	mux := http.NewServeMux()
	mux.Handle("/mcp/", protectedArcadeMCP(oauthConfig, verifier.Verify,
		officialmcp.NewStreamableHTTPHandler(func(*http.Request) *officialmcp.Server { return server }, nil)))
	httpSrv.Config.Handler = mux
	httpSrv.Start()
	t.Cleanup(httpSrv.Close)
	t.Cleanup(func() {
		for serverSession := range server.Sessions() {
			if err := serverSession.Close(); err != nil {
				t.Errorf("close live MCP server session: %v", err)
			}
		}
	})

	// A writes one fact carrying a token unique to this run, directly through the
	// production tenant resolver -- the same one the tool layer below reads through.
	token := "musr-mcp-cross-deny-" + idA
	writeClient, err := tenantClients.For(ctx, idA)
	if err != nil {
		t.Fatalf("tenantClients.For(A): %v", err)
	}
	if _, err := writeClient.UpsertFact(ctx, arcadedb.Fact{
		Subject: "musr-mcp-cross-deny-probe", Predicate: "carries_token", Object: token,
		Statement: "musr MCP-boundary cross-deny probe carries token " + token,
		Source:    arcadedb.FactSource{RunID: "musr-mcp-cross-deny", WriterRole: arcadedb.WriterParent},
	}, time.Now()); err != nil {
		t.Fatalf("A's UpsertFact: %v", err)
	}

	tokenA, err := issuer.AccessToken(ctx, resource, idA)
	if err != nil {
		t.Fatalf("issue A's access token: %v", err)
	}
	tokenB, err := issuer.AccessToken(ctx, resource, idB)
	if err != nil {
		t.Fatalf("issue B's access token: %v", err)
	}

	openSession := func(t *testing.T, bearer string, headerFunc func(context.Context) map[string]string) *officialmcp.ClientSession {
		t.Helper()
		managed := auramcp.ManagedServer{
			Type: auramcp.ServerTypeStreamableHTTP, URL: resource,
			Env:   []string{"MCP_BEARER_TOKEN=" + bearer},
			Trust: auramcp.ManagedTrust{Class: auramcp.TrustTrustedRecipe},
		}
		session, err := auramcp.OpenSDKSession(ctx, "musr-mcp-cross-deny", managed, auramcp.EgressPolicy{},
			auramcp.SessionOptions{HeaderFunc: headerFunc})
		if err != nil {
			t.Fatalf("open MCP session: %v", err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}

	search := func(t *testing.T, session *officialmcp.ClientSession) MemorySearchOutput {
		t.Helper()
		return callAgentMemoryLiveJSON[MemorySearchOutput](t, ctx, session, "memory_search",
			map[string]any{"query": token, "limit": 20})
	}
	containsToken := func(out MemorySearchOutput) bool {
		for _, fact := range out.Facts {
			if fact.Object == token {
				return true
			}
		}
		return false
	}

	t.Run("A reads its own fact (positive control)", func(t *testing.T) {
		sessionA := openSession(t, tokenA, nil)
		if out := search(t, sessionA); !containsToken(out) {
			t.Errorf("A's search for its own token found nothing: %+v", out)
		}
	})

	t.Run("B is denied A's fact", func(t *testing.T) {
		sessionB := openSession(t, tokenB, nil)
		if out := search(t, sessionB); containsToken(out) {
			t.Errorf("B's search found A's fact carrying token %q: %+v", token, out)
		}
	})

	t.Run("hostile header naming A does not change B's result", func(t *testing.T) {
		// The tenant selector reads only the verified token's subject
		// (identityFromToken -> req.Extra.TokenInfo.UserID); a caller-supplied header
		// naming a different identity must be ignored entirely.
		hostileHeader := func(context.Context) map[string]string {
			return map[string]string{"X-Musr-Hostile-Identity": idA}
		}
		sessionHostile := openSession(t, tokenB, hostileHeader)
		if out := search(t, sessionHostile); containsToken(out) {
			t.Errorf("B's search with a hostile header naming A found A's fact: %+v", out)
		}
	})

	t.Run("a call with no verified subject is refused", func(t *testing.T) {
		// No bearer token at all: protectedArcadeMCP wraps the WHOLE /mcp/ handler, so
		// this must be refused at the HTTP layer before any tool ever resolves an
		// identity -- proving a missing subject never falls back to a default or
		// shared database. Mirrors newAgentMemoryLiveMCPWithOptions's own anonymous-
		// request check (memory_live_integration_helpers_test.go), against THIS
		// production-issuer-trusting server rather than the helper's own.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpSrv.URL+"/mcp/",
			nil)
		if err != nil {
			t.Fatalf("build anonymous MCP request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("anonymous MCP request: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous MCP status = %d, want 401 (refused, not resolved to a default)", resp.StatusCode)
		}
	})
}
