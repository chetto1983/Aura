package tools

import (
	"context"
	"strings"
	"testing"
)

func TestFileTool_DispatchesRemoveMoveCopy(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	tool := NewFileTool(root)
	ctx := context.Background()

	// seed
	if _, err := tool.Execute(ctx, map[string]any{
		"action":  "write",
		"path":    "notes/draft.md",
		"content": "hello",
	}); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	// copy → notes/draft.bak.md
	out, err := tool.Execute(ctx, map[string]any{
		"action": "copy",
		"src":    "notes/draft.md",
		"dst":    "notes/draft.bak.md",
	})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !strings.Contains(out, `"status": "copied"`) {
		t.Fatalf("copy out = %s", out)
	}

	// move → notes/final.md
	if _, err := tool.Execute(ctx, map[string]any{
		"action": "move",
		"src":    "notes/draft.md",
		"dst":    "notes/final.md",
	}); err != nil {
		t.Fatalf("move: %v", err)
	}

	// info on the moved file
	out, err = tool.Execute(ctx, map[string]any{
		"action": "info",
		"path":   "notes/final.md",
	})
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if !strings.Contains(out, `"exists": true`) {
		t.Fatalf("info out = %s", out)
	}

	// resolve by basename
	out, err = tool.Execute(ctx, map[string]any{
		"action": "resolve",
		"name":   "final.md",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(out, "notes/final.md") {
		t.Fatalf("resolve out = %s", out)
	}

	// tree the workspace
	out, err = tool.Execute(ctx, map[string]any{"action": "tree"})
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if !strings.Contains(out, "notes") {
		t.Fatalf("tree out = %s", out)
	}

	// remove the copy
	if _, err := tool.Execute(ctx, map[string]any{
		"action": "remove",
		"path":   "notes/draft.bak.md",
	}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	out, err = tool.Execute(ctx, map[string]any{
		"action": "info",
		"path":   "notes/draft.bak.md",
	})
	if err != nil {
		t.Fatalf("info after remove: %v", err)
	}
	if !strings.Contains(out, `"exists": false`) {
		t.Fatalf("post-remove info = %s", out)
	}
}

func TestFileTool_InferenceForMoveAndResolve(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	tool := NewFileTool(root)
	ctx := context.Background()
	if _, err := tool.Execute(ctx, map[string]any{
		"action":  "write",
		"path":    "a.md",
		"content": "x",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// src+dst with no action → move
	if _, err := tool.Execute(ctx, map[string]any{
		"src": "a.md",
		"dst": "b.md",
	}); err != nil {
		t.Fatalf("inferred move: %v", err)
	}
	// name with no action → resolve
	if _, err := tool.Execute(ctx, map[string]any{"name": "b.md"}); err != nil {
		t.Fatalf("inferred resolve: %v", err)
	}
}
