package agui

import (
	"strings"
	"testing"
)

// TestSanitizeSkillSource covers the install-source credential redaction: query-param
// secrets, URL userinfo (password + opaque token), the bare owner/repo shorthand with a
// secret tail, and the leave-non-secrets-intact contract.
func TestSanitizeSkillSource(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"bare repo unchanged", "owner/repo", "owner/repo"},
		{"https no creds unchanged", "https://github.com/owner/repo", "https://github.com/owner/repo"},
		{"query token redacted", "owner/repo?token=sk-secret-123", "owner/repo?token=<redacted>"},
		{"query access_token redacted", "https://host/r?access_token=AAA&ref=main", "https://host/r?access_token=<redacted>&ref=main"},
		{"query api_key redacted, order kept", "https://host/r?a=1&api_key=KKK&b=2", "https://host/r?a=1&api_key=<redacted>&b=2"},
		{"url userpass redacted", "https://user:p4ss@github.com/owner/repo", "https://user:%3Credacted%3E@github.com/owner/repo"},
		{"url opaque token redacted", "https://ghp_TOKEN@github.com/owner/repo", "https://%3Credacted%3E@github.com/owner/repo"},
		{"non-secret query preserved", "https://host/r?ref=main&tag=v1", "https://host/r?ref=main&tag=v1"},
		{"empty stays empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeSkillSource(c.in); got != c.want {
				t.Fatalf("sanitizeSkillSource(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}

	// Belt: the literal secret VALUE must never survive its own redaction.
	belt := []struct{ in, secret string }{
		{"owner/repo?token=sk-secret-123", "sk-secret-123"},
		{"https://user:p4ss@github.com/owner/repo", "p4ss"},
		{"https://ghp_TOKEN@github.com/owner/repo", "ghp_TOKEN"},
		{"https://host/r?access_token=AAA", "AAA"},
	}
	for _, c := range belt {
		if got := sanitizeSkillSource(c.in); strings.Contains(got, c.secret) {
			t.Errorf("sanitizeSkillSource(%q) leaked secret %q: got %q", c.in, c.secret, got)
		}
	}
}
