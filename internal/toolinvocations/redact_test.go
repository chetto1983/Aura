package toolinvocations

import (
	"strings"
	"testing"
)

// TestRedactForLedger_Patterns is the WR-02 redactor table: each credential shape
// the ledger must never persist verbatim is asserted to land as [REDACTED], a clean
// string passes through untouched, and the placeholder is present where expected.
func TestRedactForLedger_Patterns(t *testing.T) {
	for _, tc := range []struct {
		name       string
		in         string
		wantGone   string // a substring that MUST NOT survive
		wantRedact bool   // [REDACTED] must appear
	}{
		{
			name:       "authorization bearer header",
			in:         `{"command":"curl -H \"Authorization: Bearer sk-secret.tok.value\" https://x"}`,
			wantGone:   "sk-secret.tok.value",
			wantRedact: true,
		},
		{
			name:       "authorization basic header",
			in:         `Authorization: Basic Zm9vOmJhcg==`,
			wantGone:   "Zm9vOmJhcg==",
			wantRedact: true,
		},
		{
			name:       "bare bearer token",
			in:         `bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`,
			wantGone:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantRedact: true,
		},
		{
			name:       "bare bearer standard base64 token",
			in:         `using bearer abc+def/ghi== for request`,
			wantGone:   "def/ghi",
			wantRedact: true,
		},
		{
			name:       "openai sk key",
			in:         `export OPENAI_API_KEY=sk-abcd1234efgh5678ijkl`,
			wantGone:   "sk-abcd1234efgh5678ijkl",
			wantRedact: true,
		},
		{
			name:       "openrouter sk-or key",
			in:         `--header "x: sk-or-v1abcdef1234567890ghij"`,
			wantGone:   "sk-or-v1abcdef1234567890ghij",
			wantRedact: true,
		},
		{
			name:       "aws access key id",
			in:         `AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE`,
			wantGone:   "AKIAIOSFODNN7EXAMPLE",
			wantRedact: true,
		},
		{
			name:       "aws temporary access key id",
			in:         `AWS_ACCESS_KEY_ID=ASIAIOSFODNN7EXAMPLE`,
			wantGone:   "ASIAIOSFODNN7EXAMPLE",
			wantRedact: true,
		},
		{
			name:       "inline password assignment",
			in:         `psql "postgres://u:password=supersecret@host"`,
			wantGone:   "supersecret",
			wantRedact: true,
		},
		{
			name:       "inline token query param",
			in:         `https://api.example.com/v1?token=abc123def456&page=2`,
			wantGone:   "abc123def456",
			wantRedact: true,
		},
		{
			name:       "inline api_key assignment",
			in:         `api_key: 1234567890abcdef`,
			wantGone:   "1234567890abcdef",
			wantRedact: true,
		},
		{
			name:       "inline apikey no underscore",
			in:         `apikey=ZZZZsecretZZZZ`,
			wantGone:   "ZZZZsecretZZZZ",
			wantRedact: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactForLedger(tc.in, ArgsRawCapBytes)
			if strings.Contains(got, tc.wantGone) {
				t.Errorf("secret survived redaction: %q still contains %q", got, tc.wantGone)
			}
			if tc.wantRedact && !strings.Contains(got, redactedPlaceholder) {
				t.Errorf("expected %q in redacted output, got %q", redactedPlaceholder, got)
			}
		})
	}
}

// TestRedactForLedger_CleanPassthrough asserts a credential-free value is returned
// byte-identical (no false-positive redaction of ordinary tool arguments).
func TestRedactForLedger_CleanPassthrough(t *testing.T) {
	for _, in := range []string{
		`{"command":"ls -la /workspace"}`,
		`{"path":"report.xlsx","content":"col1,col2\n1,2\n"}`,
		`echo "hello world" && python3 build.py`,
		``,
	} {
		if got := RedactForLedger(in, ArgsRawCapBytes); got != in {
			t.Errorf("clean string altered: RedactForLedger(%q) = %q", in, got)
		}
	}
}

// TestRedactForLedger_Cap asserts the byte cap bounds the output and appends the
// cap marker, and that a secret beyond the cap is truncated away (not persisted).
func TestRedactForLedger_Cap(t *testing.T) {
	body := strings.Repeat("a", ArgsRawCapBytes+500)
	got := RedactForLedger(body, ArgsRawCapBytes)
	if !strings.HasSuffix(got, capMarker) {
		t.Errorf("capped output must carry the cap marker, got suffix %q", got[len(got)-len(capMarker)-4:])
	}
	// The retained slice (output minus the marker) must not exceed the cap.
	if retained := len(got) - len(capMarker); retained > ArgsRawCapBytes {
		t.Errorf("retained %d bytes exceeds cap %d", retained, ArgsRawCapBytes)
	}

	// A secret placed past the cap is truncated away before it can be persisted.
	prefix := strings.Repeat("x", ArgsRawCapBytes)
	withTrailingSecret := prefix + " Authorization: Bearer sk-pasttheCap1234567890"
	out := RedactForLedger(withTrailingSecret, ArgsRawCapBytes)
	if strings.Contains(out, "sk-pasttheCap1234567890") {
		t.Errorf("secret beyond the cap survived: %q", out)
	}
}

// TestCapUTF8_RuneBoundary asserts the cap never splits a multi-byte rune (a naive
// byte slice could corrupt stored UTF-8). The é (2 bytes) straddling the cap must
// be dropped whole, leaving valid UTF-8.
func TestCapUTF8_RuneBoundary(t *testing.T) {
	// "aaaé" — 'a'*3 (3 bytes) + 'é' (2 bytes) = 5 bytes. Cap at 4 lands mid-é.
	s := "aaaé"
	got := capUTF8(s, 4)
	trimmed := strings.TrimSuffix(got, capMarker)
	if trimmed != "aaa" {
		t.Errorf("capUTF8 split a rune: trimmed = %q, want %q", trimmed, "aaa")
	}
	// The full result must remain valid UTF-8.
	for i, r := range got {
		if r == '�' {
			t.Errorf("capped output contains replacement char at byte %d: %q", i, got)
		}
	}
}

// TestRedactForLedger_NoCap asserts capBytes<=0 redacts without truncation.
func TestRedactForLedger_NoCap(t *testing.T) {
	in := strings.Repeat("z", 100) + " token=leakme123 " + strings.Repeat("z", 100)
	got := RedactForLedger(in, 0)
	if strings.Contains(got, "leakme123") {
		t.Errorf("secret survived no-cap redaction: %q", got)
	}
	if strings.Contains(got, capMarker) {
		t.Errorf("no-cap output must not carry the cap marker: %q", got)
	}
}
