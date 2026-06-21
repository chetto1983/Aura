package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// governance_write_auth_sweep_test.go is the Phase-29-05 CROSS-SURFACE auth sweep
// (T-29-05-04): EVERY new governance-write route is gated. It pins two dimensions over the
// full route set in one table (mirroring auth_test.go:494):
//
//   - 401 unauthenticated (the handler layer): a write with NO bound principal is 401 — the
//     audit actor is required even though the parent-mux capability gate normally binds it.
//     The six MCP + the six mutating skills routes go through beginMCPWrite/beginSkillsWrite,
//     which 401 on an absent principal. (The catalog GET writes nothing → no audit actor → it
//     is exempt from the 401 dimension; it is covered by the 403 dimension below.)
//   - 403 without governance.write (the production RequireCapability mount): the `*`-holding
//     local identity reaches the handler (not asserted here — the per-route handler tests do),
//     an identity WITHOUT the grant is 403, and a request with no principal is 403, for ALL 13
//     routes including the privileged catalog GET (it triggers an external fetch behind the
//     same gate).

// mutatingWriteRoute is one new mutating route + a representative body. These are the routes
// that require an audit actor (→ 401 when unauthenticated at the handler layer).
type writeRoute struct {
	method, target, body string
}

func mutatingWriteRoutes() []writeRoute {
	return []writeRoute{
		// Six MCP write routes.
		{http.MethodPost, "/api/governance/mcp", `{"name":"x","command":"x"}`},
		{http.MethodPatch, "/api/governance/mcp/x/env", `{"env":[]}`},
		{http.MethodPost, "/api/governance/mcp/x/trust", `{"class":"trusted_local"}`},
		{http.MethodPost, "/api/governance/mcp/x/enable", ""},
		{http.MethodPost, "/api/governance/mcp/x/disable", ""},
		{http.MethodDelete, "/api/governance/mcp/x", ""},
		// Six MUTATING skills write routes (install/restore/archive/create/update/delete).
		{http.MethodPost, "/api/governance/skills/install", `{"source":"owner/repo"}`},
		{http.MethodPost, "/api/governance/skills/x/restore", ""},
		{http.MethodPost, "/api/governance/skills/x/archive", ""},
		{http.MethodPost, "/api/governance/skills", `{"name":"x","body":"b"}`},
		{http.MethodPatch, "/api/governance/skills/x", `{"body":"b"}`},
		{http.MethodDelete, "/api/governance/skills/x", ""},
	}
}

// connectGatedRoutes are the cockpit "Connect" routes — operator write-class proxies (WhatsApp:
// logout drops the session, a QR scan links a device; calendar: create stores the operator's own
// Google OAuth client, delete drops the linked account) mounted behind the SAME governance.write
// gate. They do NOT take an audit actor (they are sidecar forwards, not audited mutations), so they
// are absent from the 401 handler-layer dimension but present in the 403 production-mount dimension
// below.
func connectGatedRoutes() []writeRoute {
	return []writeRoute{
		{http.MethodGet, "/api/connect/whatsapp/status", ""},
		{http.MethodGet, "/api/connect/whatsapp/qr.png", ""},
		{http.MethodPost, "/api/connect/whatsapp/logout", ""},
		{http.MethodGet, "/api/connect/pim/accounts", ""},
		{http.MethodPost, "/api/connect/pim/accounts", `{"id":"x","provider":"google"}`},
		{http.MethodDelete, "/api/connect/pim/accounts/x", ""},
		{http.MethodGet, "/api/connect/pim/accounts/x/google/start", ""},
		{http.MethodPost, "/api/connect/pim/accounts/x/logout", ""},
	}
}

// allGatedRoutes is the FULL route set the production mount gates (the 12 mutating + the
// privileged catalog GET + the 3 cockpit Connect WhatsApp proxies).
func allGatedRoutes() []writeRoute {
	all := append(mutatingWriteRoutes(), writeRoute{http.MethodGet, "/api/governance/skills/catalog?q=x", ""})
	return append(all, connectGatedRoutes()...)
}

func newRequestFor(rt writeRoute) *http.Request {
	if rt.body == "" {
		return httptest.NewRequest(rt.method, rt.target, nil)
	}
	return httptest.NewRequest(rt.method, rt.target, strings.NewReader(rt.body))
}

// TestGovernanceWriteAuthSweep401 asserts EVERY mutating write route returns 401 when the
// provider is wired but NO principal is bound (the audit-actor-required handler gate).
func TestGovernanceWriteAuthSweep401(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceWriteProviders(GovernanceWriteProviders{MCP: newFakeMCPWrite(), Skills: newFakeSkillsWrite()})

	for _, rt := range mutatingWriteRoutes() {
		t.Run(rt.method+" "+rt.target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.Mux().ServeHTTP(rec, newRequestFor(rt)) // no principal bound
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s unauthenticated = %d, want 401", rt.method, rt.target, rec.Code)
			}
		})
	}
}

// TestGovernanceWriteAuthSweep403 asserts EVERY gated route (all 13, incl. the catalog GET)
// returns 403 through the production RequireCapability(governance.write) mount when the caller
// lacks the grant — both for an identity without the grant AND for a request with no principal.
func TestGovernanceWriteAuthSweep403(t *testing.T) {
	const granteeID = "00000000-0000-0000-0000-0000000000ab"
	const ungrantedID = "00000000-0000-0000-0000-0000000000ac"

	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceWriteProviders(GovernanceWriteProviders{MCP: newFakeMCPWrite(), Skills: newFakeSkillsWrite()})

	deps := AuthDeps{
		Secret:           "operator-secret",
		SigningKey:       deriveSigningKey("operator-secret"),
		SecretConfigured: true,
		LocalIdentityID:  granteeID,
		Identities: &fakeIdentities{
			known: map[string]Identity{
				granteeID:   {ID: granteeID, Name: "local", Kind: "user"},
				ungrantedID: {ID: ungrantedID, Name: "other", Kind: "user"},
			},
			capabilities: map[string]bool{granteeID + "|governance.write": true},
		},
	}
	// The production mount: RequireCapability(governance.write) fronts the AG-UI handler.
	mount := RequireCapability(s.Mux(), deps, "governance.write")

	for _, rt := range allGatedRoutes() {
		t.Run("no-grant "+rt.method+" "+rt.target, func(t *testing.T) {
			req := withPrincipal(newRequestFor(rt), ungrantedID)
			rec := httptest.NewRecorder()
			mount.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s without governance.write = %d, want 403", rt.method, rt.target, rec.Code)
			}
		})
		t.Run("no-principal "+rt.method+" "+rt.target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mount.ServeHTTP(rec, newRequestFor(rt))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s with no principal = %d, want 403", rt.method, rt.target, rec.Code)
			}
		})
	}
}

// TestGovernanceWriteAuthSweepGranteeReaches asserts the grantee (the `*`-holding local
// identity) passes the gate for a representative route of each surface — the sweep is not
// vacuously 403 for everyone (it would be wrong if the gate rejected the grantee too).
func TestGovernanceWriteAuthSweepGranteeReaches(t *testing.T) {
	const granteeID = "00000000-0000-0000-0000-0000000000ab"

	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceWriteProviders(GovernanceWriteProviders{MCP: newFakeMCPWrite(), Skills: newFakeSkillsWrite()})
	deps := AuthDeps{
		Secret:           "operator-secret",
		SigningKey:       deriveSigningKey("operator-secret"),
		SecretConfigured: true,
		LocalIdentityID:  granteeID,
		Identities: &fakeIdentities{
			known:        map[string]Identity{granteeID: {ID: granteeID, Name: "local", Kind: "user"}},
			capabilities: map[string]bool{granteeID + "|governance.write": true},
		},
	}
	mount := RequireCapability(s.Mux(), deps, "governance.write")

	// A representative MCP write + a representative skills write reach their handlers (not 401/403).
	for _, rt := range []writeRoute{
		{http.MethodPost, "/api/governance/mcp", `{"name":"calc","command":"calc-bin"}`},
		{http.MethodPost, "/api/governance/skills/install", `{"source":"owner/repo"}`},
	} {
		req := withPrincipal(newRequestFor(rt), granteeID)
		rec := httptest.NewRecorder()
		mount.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
			t.Fatalf("grantee %s %s = %d, want the handler reached (not 401/403)", rt.method, rt.target, rec.Code)
		}
	}
}
