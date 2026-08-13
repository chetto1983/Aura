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

		// Shapes that used to live only in the ledger's separate table. They are asserted
		// HERE now because this package owns the single table — a caller that reaches
		// String (a log line, a wire response, the context summarizer) gets them too.
		{name: "authorization header", input: `curl -H "Authorization: Bearer sk-secret.tok.value" https://x`, leaks: []string{"sk-secret.tok.value"}},
		{name: "authorization basic", input: "Authorization: Basic Zm9vOmJhcg==", leaks: []string{"Zm9vOmJhcg=="}},
		{name: "openai key", input: "export OPENAI_API_KEY=sk-abcd1234efgh5678ijkl", leaks: []string{"sk-abcd1234efgh5678ijkl"}},
		{name: "openrouter key", input: `--header "x: sk-or-v1abcdef1234567890ghij"`, leaks: []string{"sk-or-v1abcdef1234567890ghij"}},
		{name: "aws access key id", input: "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", leaks: []string{"AKIAIOSFODNN7EXAMPLE"}},
		{name: "aws temporary key id", input: "AWS_ACCESS_KEY_ID=ASIAIOSFODNN7EXAMPLE", leaks: []string{"ASIAIOSFODNN7EXAMPLE"}},
		{name: "json credential field", input: `{"password":"hunter2","path":"a.txt"}`, leaks: []string{"hunter2"}},
		// A one-character credential is still a credential: a {4,} lower bound used to let
		// this through verbatim.
		{name: "json credential one char", input: `{"password":"x"}`, leaks: []string{`"x"`}},
		{name: "json credential two chars", input: `{"token":"ab"}`, leaks: []string{`"ab"`}},
		// The ledger caps BEFORE redacting, so an over-cap JSON credential arrives here
		// with no closing quote. Requiring one let the retained bytes survive whole.
		{name: "json credential truncated by a cap", input: `{"api_key":"live-secret-abcdef`, leaks: []string{"live-secret-abcdef"}},
		{name: "json password truncated by a cap", input: `{"password":"supersecretvalue`, leaks: []string{"supersecretvalue"}},
		{name: "query string token", input: "https://api.example.com/v1?token=abc123def456&page=2", leaks: []string{"abc123def456"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := String(test.input)
			if !strings.Contains(got, Placeholder) {
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

// TestStringKeepsTheKeyName asserts the redaction names what it scrubbed. A bare
// [REDACTED] tells a reader nothing; `token=[REDACTED]` tells them which credential
// was present, and the key name is not the secret.
func TestStringKeepsTheKeyName(t *testing.T) {
	got := String("api_key=key-value")
	if got != "api_key="+Placeholder {
		t.Fatalf("String dropped the key name: got %q", got)
	}
}

// TestStringCleanPassthrough guards the false-positive direction: ordinary tool
// arguments and prose must come back byte-identical, or every summary and ledger row
// starts carrying [REDACTED] where nothing was ever secret.
func TestStringCleanPassthrough(t *testing.T) {
	for _, in := range []string{
		`{"command":"ls -la /workspace"}`,
		`{"path":"report.xlsx","content":"col1,col2\n1,2\n"}`,
		`echo "hello world" && python3 build.py`,
		"the user asked for a summary of the Q3 numbers",
		"",
	} {
		if got := String(in); got != in {
			t.Errorf("clean string altered: String(%q) = %q", in, got)
		}
	}
}
