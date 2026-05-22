package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aura/aura/internal/workspace"
)

const (
	workspaceReadDefaultBytes = 64 * 1024
	workspaceReadMaxBytes     = 512 * 1024
	workspacePatchMaxBytes    = 1024 * 1024
)

type ListFilesTool struct {
	root *workspace.Root
}

func NewListFilesTool(root *workspace.Root) *ListFilesTool {
	return &ListFilesTool{root: root}
}

func (t *ListFilesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("list_files: workspace unavailable")
	}
	items, err := t.root.List(stringArg(args, "path"), intArg(args, "limit", 200, 1, 1000))
	if err != nil {
		return "", fmt.Errorf("list_files: %w", err)
	}
	return jsonString(items)
}

type ReadFileTool struct {
	root *workspace.Root
}

func NewReadFileTool(root *workspace.Root) *ReadFileTool {
	return &ReadFileTool{root: root}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("read_file: workspace unavailable")
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	result, err := t.root.ReadBest(rel, intArg(args, "max_bytes", workspaceReadDefaultBytes, 1, workspaceReadMaxBytes))
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	return jsonString(result)
}

type SearchFilesTool struct {
	root *workspace.Root
}

func NewSearchFilesTool(root *workspace.Root) *SearchFilesTool {
	return &SearchFilesTool{root: root}
}

func (t *SearchFilesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("search_files: workspace unavailable")
	}
	pattern, err := requiredString(args, "pattern")
	if err != nil {
		return "", err
	}
	matches, err := t.root.Search(pattern, stringSliceArg(args, "globs"), intArg(args, "limit", 50, 1, 200))
	if err != nil {
		return "", fmt.Errorf("search_files: %w", err)
	}
	return jsonString(matches)
}

type WriteFileTool struct {
	root *workspace.Root
}

func NewWriteFileTool(root *workspace.Root) *WriteFileTool {
	return &WriteFileTool{root: root}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("write_file: workspace unavailable")
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("content must be a string")
	}
	validated, err := validateWorkspaceWrite(rel, []byte(content))
	if err != nil {
		return "", fmt.Errorf("write_file: validation failed: %w", err)
	}
	if err := t.root.WriteAtomic(rel, []byte(content)); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	out := map[string]any{"path": rel, "bytes": len(content), "status": "written"}
	if validated != "" {
		out["validated"] = validated
	}
	return jsonString(out)
}

type ApplyPatchTool struct {
	root *workspace.Root
}

func NewApplyPatchTool(root *workspace.Root) *ApplyPatchTool {
	return &ApplyPatchTool{root: root}
}

func (t *ApplyPatchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("apply_patch: workspace unavailable")
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	oldText, err := requiredString(args, "old")
	if err != nil {
		return "", err
	}
	newText, ok := args["new"].(string)
	if !ok {
		return "", fmt.Errorf("new must be a string")
	}
	data, err := t.root.Read(rel, workspacePatchMaxBytes)
	if err != nil {
		return "", fmt.Errorf("apply_patch: %w", err)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("apply_patch: file is not UTF-8 text")
	}
	current := string(data)
	matches := strings.Count(current, oldText)
	if matches == 0 {
		return "", fmt.Errorf("apply_patch: old text not found")
	}
	replaceAll := boolArg(args, "replace_all")
	if matches > 1 && !replaceAll {
		return "", fmt.Errorf("apply_patch: old text matches %d locations; provide a longer unique excerpt or set replace_all=true", matches)
	}
	replacements := 1
	updated := strings.Replace(current, oldText, newText, 1)
	if replaceAll {
		replacements = matches
		updated = strings.ReplaceAll(current, oldText, newText)
	}
	validated, err := validateWorkspaceWrite(rel, []byte(updated))
	if err != nil {
		return "", fmt.Errorf("apply_patch: validation failed: %w", err)
	}
	if err := t.root.WriteAtomic(rel, []byte(updated)); err != nil {
		return "", fmt.Errorf("apply_patch: %w", err)
	}
	out := map[string]any{"path": rel, "status": "patched", "replacements": replacements}
	if validated != "" {
		out["validated"] = validated
	}
	return jsonString(out)
}

func jsonString(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
