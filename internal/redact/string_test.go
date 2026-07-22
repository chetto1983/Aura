package redact

import (
	"strings"
	"testing"
)

func TestStringRedactsDatabaseURLsUserinfoAndTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		leaks []string
	}{
		{name: "database URL", input: "dial postgresql://user:pass@db.internal/aura failed", leaks: []string{"user", "pass", "db.internal"}},
		{name: "generic URL userinfo", input: "GET https://alice:hunter2@example.test/path", leaks: []string{"alice", "hunter2"}},
		{name: "bearer and key", input: "bearer abc123 api_key=key-value password: swordfish", leaks: []string{"abc123", "key-value", "swordfish"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := String(test.input)
			if !strings.Contains(got, "[redacted]") {
				t.Fatalf("String(%q)=%q, want redaction", test.input, got)
			}
			for _, leak := range test.leaks {
				if strings.Contains(got, leak) {
					t.Fatalf("String(%q) leaked %q in %q", test.input, leak, got)
				}
			}
		})
	}
	if got := String("ordinary message"); got != "ordinary message" {
		t.Fatalf("ordinary message changed to %q", got)
	}
}
