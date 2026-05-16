package telegramadapter

import (
	"testing"
)

// --- formatAskUserQuestion ---

func TestFormatAskUserQuestion_WithOptions(t *testing.T) {
	got := formatAskUserQuestion("Which file should I update?", []string{"main.go", "config.go", "other"}, "clarification")
	if got == "" {
		t.Fatal("expected non-empty formatted string")
	}
	for _, want := range []string{"❓", "Which file should I update?", "1.", "main.go", "2.", "config.go", "3.", "other", "reply with number"} {
		if !containsSubstring(got, want) {
			t.Errorf("formatted question missing %q:\n%s", want, got)
		}
	}
}

func TestFormatAskUserQuestion_WithoutOptions(t *testing.T) {
	got := formatAskUserQuestion("What is the deadline?", nil, "clarification")
	if got == "" {
		t.Fatal("expected non-empty formatted string")
	}
	for _, want := range []string{"❓", "What is the deadline?", "reply with text"} {
		if !containsSubstring(got, want) {
			t.Errorf("formatted question missing %q:\n%s", want, got)
		}
	}
	if containsSubstring(got, "1.") {
		t.Errorf("no-option question should not contain numbered list, got:\n%s", got)
	}
}

func TestFormatAskUserQuestion_EmptyQuestion(t *testing.T) {
	got := formatAskUserQuestion("", []string{"a", "b"}, "clarification")
	if got != "" {
		t.Errorf("empty question should return empty string, got %q", got)
	}
}

func TestFormatAskUserQuestion_ApprovalKindWithoutOptions(t *testing.T) {
	got := formatAskUserQuestion("Delete all logs from last week?", nil, "approval")
	if got == "" {
		t.Fatal("expected non-empty formatted string")
	}
	for _, want := range []string{"approve_once", "approve_session", "approve_persist", "deny", "cancel", "reply with number"} {
		if !containsSubstring(got, want) {
			t.Errorf("approval question missing %q:\n%s", want, got)
		}
	}
}

func TestFormatAskUserQuestion_MultiOption(t *testing.T) {
	opts := []string{"Option A", "Option B", "Option C", "Option D"}
	got := formatAskUserQuestion("Pick one", opts, "clarification")
	for i, opt := range opts {
		numStr := "1234"[i : i+1]
		if !containsSubstring(got, numStr+". "+opt) {
			t.Errorf("missing %q in formatted question:\n%s", numStr+". "+opt, got)
		}
	}
}

// --- parseAskUserReply ---

func TestParseAskUserReply_NumericSelectsOption(t *testing.T) {
	opts := []string{"JSON", "CSV", "Markdown"}
	tests := []struct {
		reply string
		want  string
	}{
		{"1", "JSON"},
		{"2", "CSV"},
		{"3", "Markdown"},
	}
	for _, tc := range tests {
		content, rejected, _ := parseAskUserReply(tc.reply, opts)
		if rejected {
			t.Errorf("reply %q should not be rejected", tc.reply)
		}
		if content != tc.want {
			t.Errorf("reply %q: got content %q, want %q", tc.reply, content, tc.want)
		}
	}
}

func TestParseAskUserReply_FreeTextPassthrough(t *testing.T) {
	opts := []string{"yes", "no"}
	tests := []string{"maybe", "let me think", "none of the above", ""}
	for _, reply := range tests {
		content, rejected, _ := parseAskUserReply(reply, opts)
		if rejected {
			t.Errorf("free-text reply %q should not be rejected", reply)
		}
		if content != reply {
			t.Errorf("free-text reply %q: got content %q, want passthrough", reply, content)
		}
	}
}

func TestParseAskUserReply_OutOfRangeRejected(t *testing.T) {
	opts := []string{"A", "B", "C"}
	for _, reply := range []string{"0", "4", "99", "-1"} {
		_, rejected, rejectMsg := parseAskUserReply(reply, opts)
		if !rejected {
			t.Errorf("reply %q should be rejected (out of range 1..3)", reply)
		}
		if rejectMsg == "" {
			t.Errorf("reply %q: expected non-empty rejectMsg", reply)
		}
		if !containsSubstring(rejectMsg, "1..3") {
			t.Errorf("rejectMsg should mention range 1..3, got %q", rejectMsg)
		}
	}
}

func TestParseAskUserReply_NoOptionsAlwaysFreeText(t *testing.T) {
	for _, reply := range []string{"42", "hello", ""} {
		content, rejected, _ := parseAskUserReply(reply, nil)
		if rejected {
			t.Errorf("reply %q with no options should never be rejected", reply)
		}
		if content != reply {
			t.Errorf("reply %q with no options: got %q, want passthrough", reply, content)
		}
	}
}

func TestParseAskUserReply_MultiOptionRoundTrip(t *testing.T) {
	opts := []string{"alpha", "beta", "gamma", "delta"}
	for n, want := range opts {
		reply := string(rune('1' + n))
		content, rejected, _ := parseAskUserReply(reply, opts)
		if rejected {
			t.Errorf("numeric reply %q (1..4) should not be rejected", reply)
		}
		if content != want {
			t.Errorf("numeric reply %q: got %q, want %q", reply, content, want)
		}
	}
}

func TestAskUserSelectedOptionIDs(t *testing.T) {
	opts := []string{"alpha", "beta"}
	if got := askUserSelectedOptionIDs("2", opts); len(got) != 1 || got[0] != "2" {
		t.Fatalf("selected ids = %v, want [2]", got)
	}
	if got := askUserSelectedOptionIDs("free text", opts); len(got) != 0 {
		t.Fatalf("free-text selected ids = %v, want none", got)
	}
}

// containsSubstring is a small helper to avoid importing strings in tests.
func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
