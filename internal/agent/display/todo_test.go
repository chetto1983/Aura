package display

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTodoPreviewProjectsOrderedItems(t *testing.T) {
	in := PreviewInput{
		ToolCallID: "todo-1", ToolName: "todo_write",
		Arguments:     `{"todos":[{"content":"Plan","status":"completed"},{"content":"Build","status":"in_progress","activeForm":"Building"},{"content":"Test","status":"pending"}]}`,
		ResultPreview: "[x] Plan\n[~] Build\n[ ] Test",
	}
	p, ok := NormalizeToolPreview(in, NewRegistry())
	if !ok || p.Type != KindTodo || p.ToolCallID != "todo-1" || p.Todo == nil || len(p.Todo.Items) != 3 {
		t.Fatalf("todo payload = %+v, recognized=%v", p, ok)
	}
	if p.Todo.Items[1].Status != "in_progress" || p.Todo.Items[1].ActiveForm != "Building" {
		t.Fatalf("active item lost: %+v", p.Todo.Items[1])
	}
	want := `"active_form":"Building"`
	encoded, _ := json.Marshal(p)
	if !strings.Contains(string(encoded), want) {
		t.Fatalf("wire lost activeForm: %s", encoded)
	}
}

func TestTodoPreviewProjectsClearedList(t *testing.T) {
	p, ok := NormalizeToolPreview(PreviewInput{
		ToolCallID: "clear", ToolName: "todo_write",
		Arguments: `{"todos":[]}`, ResultPreview: "[todo list cleared]",
	}, NewRegistry())
	if !ok || p.Todo == nil || len(p.Todo.Items) != 0 {
		t.Fatalf("clear payload = %+v, recognized=%v", p, ok)
	}
}

func TestTodoPreviewRejectsArgsOrResultThatCouldMislead(t *testing.T) {
	valid := `{"todos":[{"content":"Build","status":"in_progress","activeForm":"Building"}]}`
	for _, tc := range []struct {
		name, args, preview string
	}{
		{"malformed JSON", `{"todos":[`, "[~] Build"},
		{"missing list", `{}`, "[todo list cleared]"},
		{"null list", `{"todos":null}`, "[todo list cleared]"},
		{"invalid status", `{"todos":[{"content":"Build","status":"blocked"}]}`, "[~] Build"},
		{"two active", `{"todos":[{"content":"A","status":"in_progress"},{"content":"B","status":"in_progress"}]}`, "[~] A\n[~] B"},
		{"error result", valid, "todo_write: invalid status"},
		{"mismatched result", valid, "[x] Build"},
		{"false clear", `{"todos":[]}`, "ok"},
		{"oversized item", `{"todos":[{"content":"` + strings.Repeat("x", 513) + `","status":"pending"}]}`, "[ ] " + strings.Repeat("x", 513)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if p, ok := NormalizeToolPreview(PreviewInput{
				ToolCallID: "bad", ToolName: "todo_write", Arguments: tc.args, ResultPreview: tc.preview,
			}, NewRegistry()); ok {
				t.Fatalf("untrusted todo promoted: %+v", p)
			}
		})
	}
}
