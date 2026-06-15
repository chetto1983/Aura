package reasoningtrace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestRecord_FullModeAllowsVerbatimHistory(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "trace.jsonl")
	t.Setenv(Env, "full")
	t.Setenv(fileEnv, p)

	Record("agent_request_built", map[string]any{
		"history": []map[string]any{
			{"role": "user", "content": "full mode plaintext"},
		},
	})

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", p, err)
	}
	if !strings.Contains(string(raw), "full mode plaintext") {
		t.Fatalf("full trace did not preserve explicit history: %s", raw)
	}
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
