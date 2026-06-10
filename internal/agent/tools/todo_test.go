package tools

import (
	"strings"
	"testing"
)

func TestTodoWriteSpec(t *testing.T) {
	s := (&TodoTool{}).Spec()
	if s.Name != "todo_write" {
		t.Fatalf("name = %q, want todo_write", s.Name)
	}
	if s.Deferred {
		t.Fatal("todo_write should be non-deferred (a core planning verb)")
	}
}

func TestTodoWrite_StoresAndRenders(t *testing.T) {
	tool := &TodoTool{}
	ctx := ctxWith(t, "sess-todo", "call-todo")
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{
		"todos": []map[string]string{
			{"content": "Read the spec", "status": "completed"},
			{"content": "Write the code", "status": "in_progress", "activeForm": "Writing the code"},
			{"content": "Run tests", "status": "pending"},
		},
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, needle := range []string{"[x] Read the spec", "[~] Write the code", "[ ] Run tests"} {
		if !strings.Contains(res.Preview, needle) {
			t.Fatalf("render missing %q: %q", needle, res.Preview)
		}
	}
	if got := len(tool.byID[shellSessionKey(ctx)]); got != 3 {
		t.Fatalf("stored %d todos under the session key, want 3", got)
	}
}

func TestTodoWrite_ReplacesList(t *testing.T) {
	tool := &TodoTool{}
	ctx := ctxWith(t, "sess-todo2", "call-todo2")
	if _, err := tool.Execute(ctx, mustJSON(t, map[string]any{
		"todos": []map[string]string{{"content": "A", "status": "pending"}},
	})); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := tool.Execute(ctx, mustJSON(t, map[string]any{
		"todos": []map[string]string{{"content": "B", "status": "completed"}, {"content": "C", "status": "pending"}},
	})); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got := tool.byID[shellSessionKey(ctx)]
	if len(got) != 2 || got[0].Content != "B" {
		t.Fatalf("list not replaced: %+v", got)
	}
}

func TestTodoWrite_Validation(t *testing.T) {
	tool := &TodoTool{}
	ctx := ctxWith(t, "sess-todo3", "call-todo3")
	cases := []struct {
		name string
		body map[string]any
	}{
		{"bad status", map[string]any{"todos": []map[string]string{{"content": "x", "status": "doing"}}}},
		{"empty content", map[string]any{"todos": []map[string]string{{"content": " ", "status": "pending"}}}},
		{"two in_progress", map[string]any{"todos": []map[string]string{{"content": "a", "status": "in_progress"}, {"content": "b", "status": "in_progress"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := tool.Execute(ctx, mustJSON(t, c.body)); err == nil {
				t.Fatalf("expected an error for %s", c.name)
			}
		})
	}
}

func TestTodoWrite_EmptyClears(t *testing.T) {
	tool := &TodoTool{}
	ctx := ctxWith(t, "sess-todo4", "call-todo4")
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"todos": []map[string]string{}}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "cleared") {
		t.Fatalf("empty list should render a cleared message, got %q", res.Preview)
	}
}
