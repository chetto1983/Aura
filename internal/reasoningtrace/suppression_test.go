package reasoningtrace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOptionalSuppressionPreventsTraceCreationAndRestoresBelowPressure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(Env, "1")
	t.Setenv(fileEnv, path)
	SetOptionalSuppressed(true)
	t.Cleanup(func() { SetOptionalSuppressed(false) })

	if Enabled() {
		t.Fatal("Enabled returned true while optional traces were pressure-suppressed")
	}
	Record("suppressed", map[string]any{"value": "must not be written"})
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("suppressed trace stat error = %v, want file not created", err)
	}

	SetOptionalSuppressed(false)
	if !Enabled() {
		t.Fatal("Enabled did not restore after pressure suppression cleared")
	}
	Record("restored", map[string]any{"value": "written"})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("restored trace missing: %v", err)
	}
}
