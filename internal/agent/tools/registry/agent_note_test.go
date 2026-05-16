package tools

import (
	"context"
	"strings"
	"testing"
)

// fakeAgentNoteStore is an in-memory implementation of agentNoteStore for tests.
type fakeAgentNoteStore struct {
	notes map[string]string
}

func (f *fakeAgentNoteStore) Get(_ context.Context, convID string) (string, bool, error) {
	if f.notes == nil {
		return "", false, nil
	}
	content, ok := f.notes[convID]
	return content, ok, nil
}

func (f *fakeAgentNoteStore) Set(_ context.Context, convID, content string) error {
	if f.notes == nil {
		f.notes = make(map[string]string)
	}
	f.notes[convID] = content
	return nil
}

func (f *fakeAgentNoteStore) Append(_ context.Context, convID, line string) error {
	if f.notes == nil {
		f.notes = make(map[string]string)
	}
	current := f.notes[convID]
	if current == "" {
		f.notes[convID] = line
	} else {
		f.notes[convID] = current + "\n" + line
	}
	return nil
}

func (f *fakeAgentNoteStore) Clear(_ context.Context, convID string) error {
	if f.notes != nil {
		delete(f.notes, convID)
	}
	return nil
}

// newTestAgentNoteTool builds an AgentNoteTool with a fake store and a fixed conversation ID.
func newTestAgentNoteTool(convID string) (*AgentNoteTool, *fakeAgentNoteStore) {
	store := &fakeAgentNoteStore{}
	tool := &AgentNoteTool{
		store: store,
		conversationIDProvider: func(_ context.Context) (string, error) {
			return convID, nil
		},
	}
	return tool, store
}

// TestAgentNoteTool_SetCreatesNote verifies that action=set stores content and
// returns the expected "ok (N chars)" response.
func TestAgentNoteTool_SetCreatesNote(t *testing.T) {
	tool, _ := newTestAgentNoteTool("conv-set")
	out, err := tool.Execute(context.Background(), map[string]any{
		"action":  "set",
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("Execute set: %v", err)
	}
	if !strings.HasPrefix(out, "ok (") || !strings.Contains(out, "chars") {
		t.Errorf("unexpected set response: %q", out)
	}
	if !strings.Contains(out, "11") {
		t.Errorf("expected 11 chars in response (len of 'hello world'): %q", out)
	}
}

// TestAgentNoteTool_AppendOnEmptyCreates verifies that append on an empty note
// stores the line without a leading newline.
func TestAgentNoteTool_AppendOnEmptyCreates(t *testing.T) {
	tool, store := newTestAgentNoteTool("conv-append-empty")
	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "append",
		"line":   "first line",
	})
	if err != nil {
		t.Fatalf("Execute append on empty: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected ok response: %q", out)
	}
	content, exists, _ := store.Get(context.Background(), "conv-append-empty")
	if !exists {
		t.Fatal("note should exist after append on empty")
	}
	if content != "first line" {
		t.Errorf("append on empty: content = %q, want %q", content, "first line")
	}
}

// TestAgentNoteTool_AppendOnExistingConcatenates verifies that appending to an
// existing note joins lines with a single newline.
func TestAgentNoteTool_AppendOnExistingConcatenates(t *testing.T) {
	tool, store := newTestAgentNoteTool("conv-append-existing")
	store.notes = map[string]string{"conv-append-existing": "line one"}

	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "append",
		"line":   "line two",
	})
	if err != nil {
		t.Fatalf("Execute append on existing: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected ok response: %q", out)
	}
	content, _, _ := store.Get(context.Background(), "conv-append-existing")
	want := "line one\nline two"
	if content != want {
		t.Errorf("append on existing: content = %q, want %q", content, want)
	}
}

// TestAgentNoteTool_GetReturnsContent verifies that action=get returns the
// note content wrapped in a fenced code block.
func TestAgentNoteTool_GetReturnsContent(t *testing.T) {
	tool, store := newTestAgentNoteTool("conv-get")
	store.notes = map[string]string{"conv-get": "my plan"}

	out, err := tool.Execute(context.Background(), map[string]any{"action": "get"})
	if err != nil {
		t.Fatalf("Execute get: %v", err)
	}
	if !strings.Contains(out, "my plan") {
		t.Errorf("get should return content: %q", out)
	}
	if !strings.Contains(out, "```") {
		t.Errorf("get should wrap content in fenced code block: %q", out)
	}
}

// TestAgentNoteTool_GetEmptyReturnsEmptySentinel verifies that get on a
// missing note returns "(empty)".
func TestAgentNoteTool_GetEmptyReturnsEmptySentinel(t *testing.T) {
	tool, _ := newTestAgentNoteTool("conv-get-empty")

	out, err := tool.Execute(context.Background(), map[string]any{"action": "get"})
	if err != nil {
		t.Fatalf("Execute get empty: %v", err)
	}
	if out != "(empty)" {
		t.Errorf("get empty: got %q, want %q", out, "(empty)")
	}
}

// TestAgentNoteTool_ClearDeletes verifies that action=clear removes the note
// and a subsequent get returns "(empty)".
func TestAgentNoteTool_ClearDeletes(t *testing.T) {
	tool, store := newTestAgentNoteTool("conv-clear")
	store.notes = map[string]string{"conv-clear": "to be cleared"}

	out, err := tool.Execute(context.Background(), map[string]any{"action": "clear"})
	if err != nil {
		t.Fatalf("Execute clear: %v", err)
	}
	if out != "cleared" {
		t.Errorf("clear: got %q, want %q", out, "cleared")
	}
	_, exists, _ := store.Get(context.Background(), "conv-clear")
	if exists {
		t.Error("note should not exist after clear")
	}
}

// TestAgentNoteTool_Roundtrip exercises the full set → append → get → clear
// sequence and verifies each step.
func TestAgentNoteTool_Roundtrip(t *testing.T) {
	tool, _ := newTestAgentNoteTool("conv-roundtrip")
	ctx := context.Background()

	// set
	if _, err := tool.Execute(ctx, map[string]any{"action": "set", "content": "step A"}); err != nil {
		t.Fatalf("set: %v", err)
	}

	// append
	appendOut, err := tool.Execute(ctx, map[string]any{"action": "append", "line": "step B"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if !strings.Contains(appendOut, "2 lines") {
		t.Errorf("append should report 2 lines: %q", appendOut)
	}

	// get — both steps must be visible
	getOut, err := tool.Execute(ctx, map[string]any{"action": "get"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(getOut, "step A") || !strings.Contains(getOut, "step B") {
		t.Errorf("get should contain both steps: %q", getOut)
	}

	// clear — note gone
	if _, err := tool.Execute(ctx, map[string]any{"action": "clear"}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	emptyOut, err := tool.Execute(ctx, map[string]any{"action": "get"})
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if emptyOut != "(empty)" {
		t.Errorf("after clear, get should return (empty): %q", emptyOut)
	}
}
