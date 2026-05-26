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

func (t *FileTool) executeGrep(ctx context.Context, args map[string]any) (string, error) {
	root, err := t.workspaceRoot("grep")
	if err != nil {
		return "", err
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	searchText, err := requiredString(args, "search_text")
	if err != nil {
		return "", err
	}
	matches, err := root.Grep(rel, searchText, boolArg(args, "case_insensitive"), intArg(args, "limit", 50, 1, 200))
	if err != nil {
		return "", fmt.Errorf("grep_file: %w", err)
	}
	return jsonString(matches)
}

func (t *FileTool) executePathInfo(ctx context.Context, args map[string]any) (string, error) {
	root, err := t.workspaceRoot("path_info")
	if err != nil {
		return "", err
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	info, err := root.Info(rel)
	if err != nil {
		return "", fmt.Errorf("path_info: %w", err)
	}
	return jsonString(info)
}

func (t *FileTool) executeMkdir(ctx context.Context, args map[string]any) (string, error) {
	root, err := t.workspaceRoot("mkdir")
	if err != nil {
		return "", err
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	info, err := root.Mkdir(rel)
	if err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	return jsonString(map[string]any{"status": "created", "path": info.Path, "type": info.Type})
}

func (t *FileTool) executeRmdir(ctx context.Context, args map[string]any) (string, error) {
	root, err := t.workspaceRoot("rmdir")
	if err != nil {
		return "", err
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	if err := root.RemoveDir(rel); err != nil {
		return "", fmt.Errorf("rmdir: %w", err)
	}
	return jsonString(map[string]any{"status": "removed", "path": rel, "type": "dir"})
}

func (t *FileTool) executeRemoveFile(ctx context.Context, args map[string]any) (string, error) {
	root, err := t.workspaceRoot("remove_file")
	if err != nil {
		return "", err
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	if err := root.RemoveFile(rel); err != nil {
		return "", fmt.Errorf("remove_file: %w", err)
	}
	return jsonString(map[string]any{"status": "removed", "path": rel, "type": "file"})
}

func (t *FileTool) executeMove(ctx context.Context, args map[string]any) (string, error) {
	root, err := t.workspaceRoot("move")
	if err != nil {
		return "", err
	}
	src, err := requiredString(args, "src")
	if err != nil {
		return "", err
	}
	dst, err := requiredString(args, "dst")
	if err != nil {
		return "", err
	}
	info, err := root.Move(src, dst, validateWorkspaceFileOperationWrite)
	if err != nil {
		return "", fmt.Errorf("move: %w", err)
	}
	return jsonString(map[string]any{"status": "moved", "from": src, "path": info.Path, "type": info.Type})
}

func (t *FileTool) executeCopy(ctx context.Context, args map[string]any) (string, error) {
	root, err := t.workspaceRoot("copy")
	if err != nil {
		return "", err
	}
	src, err := requiredString(args, "src")
	if err != nil {
		return "", err
	}
	dst, err := requiredString(args, "dst")
	if err != nil {
		return "", err
	}
	info, err := root.Copy(src, dst, validateWorkspaceFileOperationWrite)
	if err != nil {
		return "", fmt.Errorf("copy: %w", err)
	}
	return jsonString(map[string]any{"status": "copied", "from": src, "path": info.Path, "type": info.Type})
}

func (t *FileTool) executeWalk(ctx context.Context, args map[string]any) (string, error) {
	root, err := t.workspaceRoot("walk")
	if err != nil {
		return "", err
	}
	rel, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	tree, err := root.Walk(rel, intArg(args, "limit", 500, 1, 5000))
	if err != nil {
		return "", fmt.Errorf("walk: %w", err)
	}
	return jsonString(tree)
}

func (t *FileTool) executePWD(ctx context.Context, args map[string]any) (string, error) {
	root, err := t.workspaceRoot("pwd")
	if err != nil {
		return "", err
	}
	return jsonString(map[string]any{"path": ".", "root": "workspace", "physical_root": root.Path()})
}

func (t *FileTool) workspaceRoot(action string) (*workspace.Root, error) {
	if t == nil || t.root == nil {
		return nil, fmt.Errorf("%s: workspace unavailable", action)
	}
	return t.root, nil
}

func validateWorkspaceFileOperationWrite(rel string, content []byte) error {
	_, err := validateWorkspaceWrite(rel, content)
	return err
}

func jsonString(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
