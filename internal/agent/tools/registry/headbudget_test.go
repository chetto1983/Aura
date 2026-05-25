package tools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestApplyFreshToolResultBudgetPassThroughUnderCap(t *testing.T) {
	input := "short result"
	out, outcome := ApplyFreshToolResultBudget(input, 1024)
	if out != input {
		t.Fatalf("out = %q, want input", out)
	}
	if outcome.Truncated || outcome.OriginalBytes != len(input) || outcome.FinalBytes != len(input) {
		t.Fatalf("outcome = %+v, want unchanged byte counts", outcome)
	}
}

func TestApplyFreshToolResultBudgetExactBoundary(t *testing.T) {
	input := strings.Repeat("x", 128)
	out, outcome := ApplyFreshToolResultBudget(input, 128)
	if out != input {
		t.Fatalf("out = %q, want exact-boundary input", out)
	}
	if outcome.Truncated {
		t.Fatalf("outcome = %+v, want not truncated", outcome)
	}
}

func TestApplyFreshToolResultBudgetZeroBudgetNoop(t *testing.T) {
	input := "keep me"
	out, outcome := ApplyFreshToolResultBudget(input, 0)
	if out != input {
		t.Fatalf("out = %q, want input", out)
	}
	if outcome.Truncated {
		t.Fatalf("outcome = %+v, want not truncated", outcome)
	}
}

func TestApplyFreshToolResultBudgetTruncatesHeadOnlyWithTrailer(t *testing.T) {
	input := "HEAD-" + strings.Repeat("x", 2000) + "-TAIL"
	out, outcome := ApplyFreshToolResultBudget(input, 512)
	if !outcome.Truncated {
		t.Fatalf("outcome = %+v, want truncated", outcome)
	}
	if !strings.HasPrefix(out, "HEAD-") {
		t.Fatalf("out prefix = %q, want HEAD-", out[:min(len(out), 20)])
	}
	if strings.Contains(out, "-TAIL") {
		t.Fatalf("out kept tail, want head-only truncation")
	}
	if !strings.Contains(out, "bytes truncated by tool_result_budget") {
		t.Fatalf("out missing trailer: %q", out)
	}
	if !strings.Contains(out, "re-run with a narrower query") {
		t.Fatalf("out missing narrower-query hint: %q", out)
	}
	if outcome.DroppedBytes <= 0 || outcome.FinalBytes != len(out) || outcome.OriginalBytes != len(input) {
		t.Fatalf("bad outcome counts: %+v, out len=%d input len=%d", outcome, len(out), len(input))
	}
}

func TestApplyFreshToolResultBudgetUTF8Boundary(t *testing.T) {
	input := strings.Repeat("é", 800)
	out, outcome := ApplyFreshToolResultBudget(input, 513)
	if !outcome.Truncated {
		t.Fatalf("outcome = %+v, want truncated", outcome)
	}
	if !utf8.ValidString(out) {
		t.Fatalf("output is not valid UTF-8")
	}
	head, _, ok := strings.Cut(out, "\n\n[")
	if !ok {
		t.Fatalf("missing trailer separator: %q", out)
	}
	for _, r := range head {
		if r != 'é' {
			t.Fatalf("head contains split or unexpected rune %q", r)
		}
	}
}
