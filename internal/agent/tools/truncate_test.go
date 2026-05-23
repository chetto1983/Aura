package truncate

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateMiddle_Empty(t *testing.T) {
	if got := TruncateMiddle("", 8192); got != "" {
		t.Fatalf("empty input: got %q", got)
	}
}

func TestTruncateMiddle_SmallInputNoOp(t *testing.T) {
	s := "hello world"
	if got := TruncateMiddle(s, 8192); got != s {
		t.Fatalf("small input should be unchanged; got %q", got)
	}
}

func TestTruncateMiddle_ExactBudgetNoOp(t *testing.T) {
	s := strings.Repeat("x", 8192)
	if got := TruncateMiddle(s, 8192); got != s {
		t.Fatalf("input exactly at budget should be unchanged")
	}
}

func TestTruncateMiddle_ZeroBudgetNoOp(t *testing.T) {
	s := "any content"
	if got := TruncateMiddle(s, 0); got != s {
		t.Fatalf("zero maxBytes should be a no-op; got %q", got)
	}
}

func TestTruncateMiddle_MarkerPresent(t *testing.T) {
	s := strings.Repeat("x", 50000)
	out := TruncateMiddle(s, 8192)
	if !strings.Contains(out, "tokens truncated") {
		t.Fatal("marker 'tokens truncated' not present in output")
	}
	if !strings.Contains(out, "…") {
		t.Fatal("ellipsis marker '…' not present in output")
	}
}

func TestTruncateMiddle_ByteBudget_50KB(t *testing.T) {
	s := strings.Repeat("x", 50*1024)
	out := TruncateMiddle(s, 8192)
	if len(out) > 8192 {
		t.Fatalf("output exceeds maxBytes: got %d bytes, want ≤ 8192", len(out))
	}
	if !strings.Contains(out, "tokens truncated") {
		t.Fatal("marker missing from truncated output")
	}
	// Verify head content exists (non-empty head)
	if !strings.HasPrefix(out, "x") {
		t.Fatal("head content missing from output")
	}
}

func TestTruncateMiddle_HeadAndTailPreserved(t *testing.T) {
	// Input with distinct head and tail markers.
	head := "HEAD-" + strings.Repeat("a", 20000)
	tail := strings.Repeat("b", 20000) + "-TAIL"
	s := head + tail

	out := TruncateMiddle(s, 512)
	if !strings.HasPrefix(out, "HEAD-") {
		t.Fatal("head not preserved in output")
	}
	if !strings.HasSuffix(out, "-TAIL") {
		t.Fatalf("tail not preserved; output ends: %q", out[max(0, len(out)-20):])
	}
	if !strings.Contains(out, "tokens truncated") {
		t.Fatal("marker missing from output")
	}
}

func TestTruncateMiddle_UTF8Boundary(t *testing.T) {
	// 'à' is U+00E0: 2 bytes (0xC3 0xA0) in UTF-8.
	// 10 000 repetitions = 20 000 bytes. Budget = 1000 forces a boundary cut.
	s := strings.Repeat("à", 10000) // 20 000 bytes
	out := TruncateMiddle(s, 1000)
	if !utf8.ValidString(out) {
		t.Fatal("output is not valid UTF-8")
	}
	for _, r := range out {
		if r == utf8.RuneError {
			t.Fatal("broken rune (RuneError) found in output")
		}
	}
}

func TestTruncateMiddleByTokens_DelegatesToMiddle(t *testing.T) {
	// 100 tokens × 4 bytes = 400 bytes budget.
	s := strings.Repeat("x", 10000)
	out := TruncateMiddleByTokens(s, 100)
	if len(out) > 400+markerOverhead {
		t.Fatalf("token-budget output too large: got %d bytes", len(out))
	}
	if !strings.Contains(out, "tokens truncated") {
		t.Fatal("marker not present in token-budget variant")
	}
}

func TestTruncateMiddleByTokens_NoOp_ZeroTokens(t *testing.T) {
	s := strings.Repeat("x", 100)
	if got := TruncateMiddleByTokens(s, 0); got != s {
		t.Fatalf("zero maxTokens should be no-op; got len=%d", len(got))
	}
}

