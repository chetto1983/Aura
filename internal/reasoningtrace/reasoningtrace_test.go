package reasoningtrace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/redact"
)

// TestRecord_RotatesAtCap covers M-06: the append-only JSONL trace must not grow
// monotonically. When the active file passes the byte cap it is rotated to a single
// .1 backup so the live file stays bounded.
func TestRecord_RotatesAtCap(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "trace.jsonl")
	t.Setenv(Env, "1")
	t.Setenv(fileEnv, p)
	t.Setenv(maxBytesEnv, "300")

	for i := range 60 {
		Record("stage", map[string]any{"i": i, "pad": strings.Repeat("x", 40)})
	}

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("active trace missing: %v", err)
	}
	// Bounded to the cap plus at most one over-cap row.
	if fi.Size() > 300+256 {
		t.Errorf("active trace not rotated: %d bytes (cap 300)", fi.Size())
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Errorf("expected a rotated .1 backup, got %v", err)
	}
}

func TestRecord_DefaultOmitsVerbatimHistoryAndUser(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "trace.jsonl")
	t.Setenv(Env, "1")
	t.Setenv(fileEnv, p)

	Record("agent_request_built", map[string]any{
		"history": []map[string]any{
			{"role": "user", "content": "my private plaintext prompt"},
		},
		"user": "raw user prompt with private token",
	})

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", p, err)
	}
	if strings.Contains(string(raw), "my private plaintext prompt") ||
		strings.Contains(string(raw), "raw user prompt with private token") {
		t.Fatalf("trace leaked default-private text: %s", raw)
	}
	var row map[string]any
	if err := json.Unmarshal(bytesTrimSpace(raw), &row); err != nil {
		t.Fatalf("trace row JSON: %v\n%s", err, raw)
	}
	for _, key := range []string{"history", "user"} {
		summary, ok := row[key].(map[string]any)
		if !ok || summary["redacted"] != true || summary["sha256"] == "" {
			t.Fatalf("row[%s] = %#v, want redacted hash summary", key, row[key])
		}
	}
}

func TestRecord_FullModeWritesOnlyEncryptedHistory(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "trace.enc")
	t.Setenv(Env, "full")
	t.Setenv(fileEnv, p)
	t.Setenv("AURA_TRACE_FULL_ACK", "1")
	t.Setenv("AURA_TRACE_ENCRYPT_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	Record("agent_request_built", map[string]any{
		"history": []map[string]any{
			{"role": "user", "content": "full mode plaintext"},
		},
	})

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", p, err)
	}
	if strings.Contains(string(raw), "full mode plaintext") || json.Valid(bytesTrimSpace(raw)) {
		t.Fatalf("full trace was persisted as plaintext JSON: %q", raw)
	}
}

func TestRecord_FullModeWithoutKeyFailsClosed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "trace.enc")
	t.Setenv(Env, "full")
	t.Setenv(fileEnv, p)
	t.Setenv("AURA_TRACE_FULL_ACK", "1")
	t.Setenv("AURA_TRACE_ENCRYPT_KEY", "")

	Record("agent_request_built", map[string]any{"history": "must never reach plaintext"})

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("full trace without an encryption key created %s (err=%v)", p, err)
	}
}

func TestRecordRedaction(t *testing.T) {
	const configuredDSN = "postgres://configured:password@db.internal/aura"
	const literalDSN = "postgres://typed:credential@external.example/private"
	const escapedSecret = "line-one\n\"quoted-secret\""
	p := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(Env, "summary")
	t.Setenv(fileEnv, p)
	t.Setenv("AURA_DB_URL", configuredDSN)
	t.Setenv("AURA_SERVICE_TOKEN", escapedSecret)

	Record("redaction", map[string]any{
		"configured": configuredDSN,
		"free_text":  "upstream error: " + literalDSN,
		"escaped":    "prefix " + escapedSecret,
		"marker":     "[REDACTED_EXISTING_TOKEN] [capped]",
	})

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", p, err)
	}
	got := string(raw)
	for _, leak := range []string{
		configuredDSN, "configured:password", literalDSN, "typed:credential",
		"quoted-secret", `\"quoted-secret\"`,
	} {
		if strings.Contains(got, leak) {
			t.Fatalf("trace leaked %q: %s", leak, got)
		}
	}
	if !strings.Contains(got, "[REDACTED_AURA_DB_URL]") {
		t.Fatalf("configured-value placeholder missing: %s", got)
	}
	if !strings.Contains(got, "[REDACTED_AURA_SERVICE_TOKEN]") {
		t.Fatalf("escaped configured-value placeholder missing: %s", got)
	}
	if !strings.Contains(got, "postgres://"+redact.Placeholder) {
		t.Fatalf("literal DSN pattern redaction missing: %s", got)
	}
	if strings.Count(got, "[REDACTED_EXISTING_TOKEN]") != 1 ||
		strings.Count(got, "[capped]") != 1 {
		t.Fatalf("existing redaction markers were altered or duplicated: %s", got)
	}
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
