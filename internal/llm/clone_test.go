package llm

import "testing"

func TestCloneMessages_NilReturnsNil(t *testing.T) {
	if got := CloneMessages(nil); got != nil {
		t.Fatalf("CloneMessages(nil) = %v, want nil", got)
	}
}

func TestCloneMessages_EmptyReturnsEmpty(t *testing.T) {
	src := []Message{}
	got := CloneMessages(src)
	if got == nil {
		t.Fatal("CloneMessages of empty returned nil; want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}

func TestCloneMessages_FreshHeader(t *testing.T) {
	src := []Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"}}
	got := CloneMessages(src)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Mutating got must not change src.
	got[0].Content = "x"
	if src[0].Content != "a" {
		t.Fatalf("src mutated after clone: %q", src[0].Content)
	}
	// Appending to got must not extend src.
	got = append(got, Message{Role: "system", Content: "c"})
	if len(src) != 2 {
		t.Fatalf("src extended after append to clone: len=%d", len(src))
	}
}

func TestCloneMessages_PreservesToolCalls(t *testing.T) {
	src := []Message{{Role: "assistant", ToolCalls: []ToolCall{{ID: "t1", Name: "x"}}}}
	got := CloneMessages(src)
	if len(got[0].ToolCalls) != 1 || got[0].ToolCalls[0].ID != "t1" {
		t.Fatalf("tool calls not preserved: %+v", got[0].ToolCalls)
	}
}
