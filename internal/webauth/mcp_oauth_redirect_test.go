package webauth

import "testing"

// An MCP client cannot pre-register a redirect: it picks a free port at launch and
// invents its own path. RFC 8252 section 7.3 makes loopback the redirect for exactly
// that reason. Pinning named paths meant Dynamic Client Registration existed and
// rejected every real client -- measured 2026-09-02, Claude Code's registration came
// back "invalid_redirect_uri: redirect URI is outside Aura's callbacks".
func TestValidatedRedirectAcceptsAnyLoopbackCallback(t *testing.T) {
	server := &OAuthServer{}
	for _, raw := range []string{
		"http://localhost:53291/callback",
		"http://127.0.0.1:41999/aura/mcp/oauth/callback",
		"http://127.0.0.1:8765/oauth/callback",
		"http://[::1]:9000/cb",
	} {
		if _, ok := server.validatedRedirect(raw); !ok {
			t.Fatalf("loopback callback %q must be accepted", raw)
		}
	}
}

// Widening to any loopback path must not widen to any HOST: a redirect off the
// operator's machine would send the authorization code somewhere else entirely.
func TestValidatedRedirectStillRejectsOffHostAndMalformed(t *testing.T) {
	server := &OAuthServer{}
	for _, raw := range []string{
		"http://evil.example/callback",
		"http://127.0.0.1.evil.example/callback",
		"https://example.com/aura/mcp/oauth/callback",
		"http://user:pass@127.0.0.1:5000/callback",
		"http://127.0.0.1:5000/callback#fragment",
		" http://127.0.0.1:5000/callback",
		"not-a-url",
		"",
	} {
		if _, ok := server.validatedRedirect(raw); ok {
			t.Fatalf("redirect %q must be rejected", raw)
		}
	}
}
