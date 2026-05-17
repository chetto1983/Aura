package telegramadapter

import (
	"testing"

	"github.com/aura/aura/internal/llm"
	tgtelegram "github.com/aura/aura/internal/telegram"
)

// --- formatAskUserQuestion ---

func TestFormatAskUserQuestion_WithOptions(t *testing.T) {
	got := formatAskUserQuestion("Which file should I update?", []string{"main.go", "config.go", "other"}, "clarification")
	if got == "" {
		t.Fatal("expected non-empty formatted string")
	}
	for _, want := range []string{"❓", "Which file should I update?", "1.", "main.go", "2.", "config.go", "3.", "other", "tap a button", "reply with number"} {
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

func TestAskUserQuestionMarkup_WithOptions(t *testing.T) {
	markup := askUserQuestionMarkup([]string{"main.go", "config.go"}, "clarification")
	if markup == nil {
		t.Fatal("markup = nil, want inline keyboard")
	}
	if got := len(markup.InlineKeyboard); got != 2 {
		t.Fatalf("rows = %d, want 2", got)
	}
	first := markup.InlineKeyboard[0][0]
	if first.Unique != tgtelegram.AskUserCallbackUnique {
		t.Fatalf("first.Unique = %q, want %q", first.Unique, tgtelegram.AskUserCallbackUnique)
	}
	if first.Data != "1" {
		t.Fatalf("first.Data = %q, want 1", first.Data)
	}
	if first.Text != "1. main.go" {
		t.Fatalf("first.Text = %q, want numbered option", first.Text)
	}
}

func TestAskUserQuestionMarkup_ApprovalKindUsesCanonicalButtons(t *testing.T) {
	markup := askUserQuestionMarkup(nil, "approval")
	if markup == nil {
		t.Fatal("markup = nil, want approval inline keyboard")
	}
	if got := len(markup.InlineKeyboard); got != len(canonicalApprovalOptions) {
		t.Fatalf("rows = %d, want %d", got, len(canonicalApprovalOptions))
	}
	first := markup.InlineKeyboard[0][0]
	if first.Text != "1. Approve once" {
		t.Fatalf("first.Text = %q, want friendly approval label", first.Text)
	}
	if first.Data != "1" {
		t.Fatalf("first.Data = %q, want 1", first.Data)
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

// --- prepareAskUserResumeInput ---

func TestPrepareAskUserResumeInputUsesDurablePendingWithoutToolCall(t *testing.T) {
	resume, ok := prepareAskUserResumeInput("1", nil, []string{"Aura", "Gamma"}, "clarification", true)
	if !ok {
		t.Fatal("prepareAskUserResumeInput returned ok=false, want durable pending resume")
	}
	if resume.HasPendingCall {
		t.Fatal("HasPendingCall = true, want false for post-restart durable question")
	}
	if resume.Content != "Aura" {
		t.Fatalf("Content = %q, want Aura", resume.Content)
	}
	if len(resume.SelectedOptionIDs) != 1 || resume.SelectedOptionIDs[0] != "1" {
		t.Fatalf("SelectedOptionIDs = %v, want [1]", resume.SelectedOptionIDs)
	}
	if resume.Kind != "clarification" {
		t.Fatalf("Kind = %q, want clarification", resume.Kind)
	}
}

func TestPrepareAskUserResumeInputRejectsDurableOutOfRangeReply(t *testing.T) {
	resume, ok := prepareAskUserResumeInput("3", nil, []string{"Aura", "Gamma"}, "clarification", true)
	if !ok {
		t.Fatal("prepareAskUserResumeInput returned ok=false, want durable pending resume")
	}
	if !resume.Rejected {
		t.Fatal("Rejected = false, want true")
	}
	if !containsSubstring(resume.RejectMessage, "1..2") {
		t.Fatalf("RejectMessage = %q, want range 1..2", resume.RejectMessage)
	}
}

func TestPrepareAskUserResumeInputPrefersPendingToolCallOptions(t *testing.T) {
	messages := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID:   "ask-1",
			Name: "ask_user",
			Arguments: map[string]any{
				"kind":    "clarification",
				"options": []any{"JSON", "CSV"},
			},
		}}},
	}
	resume, ok := prepareAskUserResumeInput("2", messages, []string{"Aura", "Gamma"}, "clarification", true)
	if !ok {
		t.Fatal("prepareAskUserResumeInput returned ok=false, want pending tool call resume")
	}
	if !resume.HasPendingCall {
		t.Fatal("HasPendingCall = false, want true")
	}
	if resume.CallID != "ask-1" {
		t.Fatalf("CallID = %q, want ask-1", resume.CallID)
	}
	if resume.Content != "CSV" {
		t.Fatalf("Content = %q, want CSV", resume.Content)
	}
	if len(resume.SelectedOptionIDs) != 1 || resume.SelectedOptionIDs[0] != "2" {
		t.Fatalf("SelectedOptionIDs = %v, want [2]", resume.SelectedOptionIDs)
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
