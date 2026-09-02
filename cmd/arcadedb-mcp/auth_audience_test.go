package main

import "testing"

// One server, two names. The canonical entry is what the protected-resource metadata
// advertises AND what MCP clients resolve as the server's address; the rest are only
// accepted, so the in-container agent's self-issued grant -- whose audience is pinned
// to its own dial URL -- keeps validating.
func TestSplitAudiencesKeepsCanonicalFirst(t *testing.T) {
	got := splitAudiences(" http://127.0.0.1:8096/mcp/ , http://aura-arcadedb-mcp:8096/mcp/ ")
	want := []string{"http://127.0.0.1:8096/mcp/", "http://aura-arcadedb-mcp:8096/mcp/"}
	if len(got) != len(want) {
		t.Fatalf("audiences = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("audiences[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplitAudiencesNeverReturnsEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", ",,", " , , "} {
		if got := splitAudiences(raw); len(got) == 0 {
			t.Fatalf("splitAudiences(%q) returned nothing; callers index [0]", raw)
		}
	}
}

func TestAudienceAcceptedMatchesAnyConfiguredName(t *testing.T) {
	accepted := []string{"http://127.0.0.1:8096/mcp/", "http://aura-arcadedb-mcp:8096/mcp/"}
	for name, audience := range map[string][]string{
		"host client token":    {"http://127.0.0.1:8096/mcp/"},
		"in-container grant":   {"http://aura-arcadedb-mcp:8096/mcp/"},
		"multi-audience token": {"http://elsewhere/mcp/", "http://127.0.0.1:8096/mcp/"},
	} {
		if !audienceAccepted(audience, accepted) {
			t.Fatalf("%s: audience %v must be accepted", name, audience)
		}
	}
}

// The security direction: widening to two names must not widen to ANY name, and a
// bearer bound to nothing is replayable against every resource trusting this issuer.
func TestAudienceRejectsForeignAndEmpty(t *testing.T) {
	accepted := []string{"http://127.0.0.1:8096/mcp/", "http://aura-arcadedb-mcp:8096/mcp/"}
	for name, audience := range map[string][]string{
		"foreign resource": {"http://evil.example/mcp/"},
		"no audience":      nil,
		"empty audience":   {},
		"near miss port":   {"http://127.0.0.1:9096/mcp/"},
		"near miss path":   {"http://127.0.0.1:8096/mcp"},
	} {
		if audienceAccepted(audience, accepted) {
			t.Fatalf("%s: audience %v must be rejected", name, audience)
		}
	}
}
