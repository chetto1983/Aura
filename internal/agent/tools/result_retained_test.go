package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A result that FITS under the preview cap but is large still gets a sidecar, because the
// context ladder may only evict what it can page back. Before this, evictability
// accidentally meant "was truncated": measured on the live deployment, a 27,515-byte result
// under a 30,000-byte cap sat in every subsequent request forever.
func TestNewResultRetainsALargeResultThatFitsUnderTheCap(t *testing.T) {
	runDir := t.TempDir()
	content := strings.Repeat("b", spillRetainFloorBytes+500)
	ctx := WithToolCallContext(t.Context(), "sess-retain", "call-retain", runDir, len(content)+1000)

	res, err := NewResult(ctx, content)
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	if res.Truncated {
		t.Fatal("Truncated=true: nothing was cut, the whole result is in context")
	}
	if res.FullPath == "" {
		t.Fatal("no sidecar written, so the ladder can never reclaim this result")
	}
	if !strings.Contains(res.Preview, retainedFooterMarker) {
		t.Fatalf("preview carries no retained marker, so the ladder cannot recognise it:\n%s",
			res.Preview[max(0, len(res.Preview)-200):])
	}
	if !strings.HasPrefix(res.Preview, content) {
		t.Fatal("the model no longer sees the full content it was meant to keep")
	}
	saved, err := os.ReadFile(res.FullPath)
	if err != nil || string(saved) != content {
		t.Fatalf("sidecar does not hold the exact bytes (err=%v)", err)
	}
}

// Below the floor nothing is written: a 233-byte result -- the measured median -- would pay
// a file write and gain an eviction nobody would ever want to make.
func TestNewResultLeavesSmallResultsAlone(t *testing.T) {
	runDir := t.TempDir()
	content := strings.Repeat("c", spillRetainFloorBytes-1)
	ctx := WithToolCallContext(t.Context(), "sess-small", "call-small", runDir, len(content)+1000)

	res, err := NewResult(ctx, content)
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	if res.FullPath != "" || strings.Contains(res.Preview, retainedFooterMarker) {
		t.Fatalf("a small result was spilled: path=%q", res.FullPath)
	}
	if res.Preview != content {
		t.Fatal("a small result must reach the model byte-for-byte")
	}
}

// A sidecar that cannot be written costs the ladder an option, never the turn: the content
// is already complete in context.
func TestNewResultKeepsTheContentWhenTheSidecarFails(t *testing.T) {
	content := strings.Repeat("d", spillRetainFloorBytes+10)
	// A FILE where the run dir should be: unwritable for any user, including the root the
	// container runs as -- an unreadable path alone would still be created by MkdirAll.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := WithToolCallContext(t.Context(), "sess-fail", "call-fail", blocker, len(content)+1000)

	res, err := NewResult(ctx, content)
	if err != nil {
		t.Fatalf("NewResult returned an error for an unwritable sidecar: %v", err)
	}
	if res.Preview != content {
		t.Fatal("the content was damaged by a sidecar failure")
	}
}
