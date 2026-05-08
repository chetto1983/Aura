package tools

import (
	"context"
	"encoding/base64"
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

func NewWorkspaceFileTools(root *workspace.Root) []Tool {
	if root == nil {
		return nil
	}
	return []Tool{
		NewListFilesTool(root),
		NewReadFileTool(root),
		NewSearchFilesTool(root),
		NewWriteFileTool(root),
		NewApplyPatchTool(root),
	}
}

type ListFilesTool struct {
	root *workspace.Root
}

func NewListFilesTool(root *workspace.Root) *ListFilesTool {
	return &ListFilesTool{root: root}
}

func (t *ListFilesTool) Name() string { return "list_files" }

func (t *ListFilesTool) Description() string {
	return "List files and directories inside Aura's configured workspace root. Path must be relative. Sensitive paths such as .env, .git, live DB files, and wiki/raw are hidden."
}

func (t *ListFilesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative directory to list. Defaults to workspace root.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum entries to return. Default 200, max 1000.",
			},
		},
	}
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

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Read one small file from Aura's configured workspace root. Path must be relative. Sensitive paths are denied; small binary files return base64."
}

func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative file path to read.",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": "Maximum bytes to read. Default 65536, max 524288.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.root == nil {
		return "", fmt.Errorf("read_file: workspace unavailable")
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	data, err := t.root.Read(rel, intArg(args, "max_bytes", workspaceReadDefaultBytes, 1, workspaceReadMaxBytes))
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	out := map[string]any{
		"path":  rel,
		"bytes": len(data),
	}
	if utf8.Valid(data) {
		out["encoding"] = "utf-8"
		out["content"] = string(data)
	} else {
		out["encoding"] = "base64"
		out["content"] = base64.StdEncoding.EncodeToString(data)
	}
	return jsonString(out)
}

type SearchFilesTool struct {
	root *workspace.Root
}

func NewSearchFilesTool(root *workspace.Root) *SearchFilesTool {
	return &SearchFilesTool{root: root}
}

func (t *SearchFilesTool) Name() string { return "search_files" }

func (t *SearchFilesTool) Description() string {
	return "Search text files inside Aura's configured workspace root by case-insensitive substring. Sensitive paths and binary files are skipped."
}

func (t *SearchFilesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Case-insensitive text to find.",
			},
			"globs": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional relative glob filters such as **/*.go or *.md.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum matches. Default 50, max 200.",
			},
		},
		"required": []string{"pattern"},
	}
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

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return "Atomically write a UTF-8 text file inside Aura's configured workspace root. Path must be relative. Sensitive paths and binary/executable extensions are denied."
}

func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative file path to write.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "UTF-8 file content.",
			},
		},
		"required": []string{"path", "content"},
	}
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
	if err := t.root.WriteAtomic(rel, []byte(content)); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	return jsonString(map[string]any{"path": rel, "bytes": len(content), "status": "written"})
}

type ApplyPatchTool struct {
	root *workspace.Root
}

func NewApplyPatchTool(root *workspace.Root) *ApplyPatchTool {
	return &ApplyPatchTool{root: root}
}

func (t *ApplyPatchTool) Name() string { return "apply_patch" }

func (t *ApplyPatchTool) Description() string {
	return "Apply an exact text replacement inside one workspace file. This is not shell access: provide path, old text, and new text. Sensitive paths and binary/executable extensions are denied."
}

func (t *ApplyPatchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative file path to patch.",
			},
			"old": map[string]any{
				"type":        "string",
				"description": "Exact text to replace.",
			},
			"new": map[string]any{
				"type":        "string",
				"description": "Replacement text.",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace every occurrence instead of just the first one.",
			},
		},
		"required": []string{"path", "old", "new"},
	}
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
	if !strings.Contains(current, oldText) {
		return "", fmt.Errorf("apply_patch: old text not found")
	}
	replacements := 1
	updated := strings.Replace(current, oldText, newText, 1)
	if boolArg(args, "replace_all") {
		replacements = strings.Count(current, oldText)
		updated = strings.ReplaceAll(current, oldText, newText)
	}
	if err := t.root.WriteAtomic(rel, []byte(updated)); err != nil {
		return "", fmt.Errorf("apply_patch: %w", err)
	}
	return jsonString(map[string]any{"path": rel, "status": "patched", "replacements": replacements})
}

func jsonString(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
