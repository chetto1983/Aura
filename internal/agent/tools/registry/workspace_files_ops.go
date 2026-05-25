package tools

import (
	"context"
	"fmt"

	"github.com/aura/aura/internal/workspace"
)

// The handlers below back the new file actions (remove/move/copy/tree/
// info/resolve) added to FileTool. Each is a thin shell over a
// workspace.Root method; the kernel and denylist gates live there. Keep
// these wrappers small so the policy stays in one place.

type RemoveFileTool struct {
	root *workspace.Root
}

func NewRemoveFileTool(root *workspace.Root) *RemoveFileTool { return &RemoveFileTool{root: root} }

func (t *RemoveFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("remove: workspace unavailable")
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	if err := t.root.Remove(rel, boolArg(args, "allow_dir")); err != nil {
		return "", fmt.Errorf("remove: %w", err)
	}
	return jsonString(map[string]any{"path": rel, "status": "removed"})
}

type MoveFileTool struct {
	root *workspace.Root
}

func NewMoveFileTool(root *workspace.Root) *MoveFileTool { return &MoveFileTool{root: root} }

func (t *MoveFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("move: workspace unavailable")
	}
	src, err := requiredString(args, "src")
	if err != nil {
		return "", err
	}
	dst, err := requiredString(args, "dst")
	if err != nil {
		return "", err
	}
	if err := t.root.Move(src, dst); err != nil {
		return "", fmt.Errorf("move: %w", err)
	}
	return jsonString(map[string]any{"src": src, "dst": dst, "status": "moved"})
}

type CopyFileTool struct {
	root *workspace.Root
}

func NewCopyFileTool(root *workspace.Root) *CopyFileTool { return &CopyFileTool{root: root} }

func (t *CopyFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("copy: workspace unavailable")
	}
	src, err := requiredString(args, "src")
	if err != nil {
		return "", err
	}
	dst, err := requiredString(args, "dst")
	if err != nil {
		return "", err
	}
	if err := t.root.Copy(src, dst); err != nil {
		return "", fmt.Errorf("copy: %w", err)
	}
	return jsonString(map[string]any{"src": src, "dst": dst, "status": "copied"})
}

type TreeFilesTool struct {
	root *workspace.Root
}

func NewTreeFilesTool(root *workspace.Root) *TreeFilesTool { return &TreeFilesTool{root: root} }

func (t *TreeFilesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("tree: workspace unavailable")
	}
	node, err := t.root.Tree(
		stringArg(args, "path"),
		intArg(args, "max_depth", 4, 1, 16),
		intArg(args, "max_entries", 200, 1, 2000),
	)
	if err != nil {
		return "", fmt.Errorf("tree: %w", err)
	}
	return jsonString(node)
}

type InfoFileTool struct {
	root *workspace.Root
}

func NewInfoFileTool(root *workspace.Root) *InfoFileTool { return &InfoFileTool{root: root} }

func (t *InfoFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("info: workspace unavailable")
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	info, err := t.root.Info(rel)
	if err != nil {
		return "", fmt.Errorf("info: %w", err)
	}
	return jsonString(info)
}

type ResolveFileTool struct {
	root *workspace.Root
}

func NewResolveFileTool(root *workspace.Root) *ResolveFileTool { return &ResolveFileTool{root: root} }

func (t *ResolveFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("resolve: workspace unavailable")
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	matches, err := t.root.Find(name, intArg(args, "limit", 50, 1, 500))
	if err != nil {
		return "", fmt.Errorf("resolve: %w", err)
	}
	return jsonString(matches)
}
