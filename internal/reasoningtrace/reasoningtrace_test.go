package reasoningtrace

import (
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
