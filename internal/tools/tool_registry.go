package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
)

// ToolStore is the interface the tool management tools depend on.
// ToolRegistry is the production implementation backed by wiki/tools/.
type ToolStore interface {
	ListTools() ([]ToolInfo, error)
	GetToolCode(name string) (string, error)
	SaveTool(ctx context.Context, name, description, params, code, usage string) error
}

// ToolInfo holds metadata about a registered tool.
type ToolInfo struct {
	Name        string
	Description string
	Params      string
	Requires    string
	Created     string
	Usage       string
	FilePath    string
}

// ToolRegistry manages persistent LLM-written Python tools stored under
// wiki/tools/. Each tool is a .py file with a companion .md wiki page.
type ToolRegistry struct {
	wikiStore *wiki.Store
	toolsDir  string
}

// NewToolRegistry creates a tool registry backed by the wiki/tools/ directory.
func NewToolRegistry(wikiStore *wiki.Store) (*ToolRegistry, error) {
	toolsDir := filepath.Join(wikiStore.Dir(), "tools")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		return nil, fmt.Errorf("create tools dir: %w", err)
	}
	return &ToolRegistry{wikiStore: wikiStore, toolsDir: toolsDir}, nil
}

// ListTools returns all tools registered in the tools directory.
func (r *ToolRegistry) ListTools() ([]ToolInfo, error) {
	entries, err := os.ReadDir(r.toolsDir)
	if err != nil {
		return nil, err
	}

	var tools []ToolInfo
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".py") {
			continue
		}
		info, err := r.parseToolHeader(entry.Name())
		if err != nil {
			continue
		}
		tools = append(tools, info)
	}
	return tools, nil
}

// SaveTool writes a new Python tool to the tools directory and creates a
// companion wiki page.
func (r *ToolRegistry) SaveTool(ctx context.Context, name, description, params, code, usage string) error {
	name = sanitizeToolName(name)
	if name == "" {
		return fmt.Errorf("invalid tool name")
	}

	pyPath := filepath.Join(r.toolsDir, name+".py")

	header := fmt.Sprintf(`# tool: %s
# description: %s
# params: %s
# requires: stdlib
# created: %s
# usage: %s

`, name, description, params, time.Now().Format("2006-01-02"), usage)

	if err := os.WriteFile(pyPath, []byte(header+code), 0644); err != nil {
		return fmt.Errorf("write tool file: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		Title:         "Tool: " + name,
		Tags:          []string{"tool", "auto-generated"},
		Category:      "tool",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
		Body:          fmt.Sprintf("# Tool: %s\n\n%s\n\n**Parameters:** %s\n\n**Usage:** %s\n\n```python\n%s\n```", name, description, params, usage, code),
	}

	if err := r.wikiStore.WritePage(ctx, page); err != nil {
		os.Remove(pyPath)
		return fmt.Errorf("write wiki page: %w", err)
	}

	return r.regenerateIndex()
}

// GetToolCode reads the source of a registered tool (without the header).
func (r *ToolRegistry) GetToolCode(name string) (string, error) {
	name = sanitizeToolName(name)
	pyPath := filepath.Join(r.toolsDir, name+".py")
	data, err := os.ReadFile(pyPath)
	if err != nil {
		return "", err
	}
	lines := strings.SplitN(string(data), "\n\n", 2)
	if len(lines) == 2 {
		return lines[1], nil
	}
	return string(data), nil
}

// DeleteTool removes a tool's .py file.
func (r *ToolRegistry) DeleteTool(ctx context.Context, name string) error {
	name = sanitizeToolName(name)
	pyPath := filepath.Join(r.toolsDir, name+".py")
	if err := os.Remove(pyPath); err != nil {
		return err
	}
	return r.regenerateIndex()
}

func (r *ToolRegistry) regenerateIndex() error {
	tools, err := r.ListTools()
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# Tool Registry\n\n")
	b.WriteString("Auto-generated index of available tools. Updated on every tool save/delete.\n\n")

	if len(tools) == 0 {
		b.WriteString("_No tools registered yet. The LLM will create tools as they become useful._\n")
	} else {
		for _, t := range tools {
			b.WriteString(fmt.Sprintf("- **%s** — %s  \n  params: %s | created: %s\n\n",
				t.Name, t.Description, t.Params, t.Created))
		}
	}

	indexPath := filepath.Join(r.toolsDir, "index.md")
	return os.WriteFile(indexPath, []byte(b.String()), 0644)
}

func (r *ToolRegistry) parseToolHeader(filename string) (ToolInfo, error) {
	pyPath := filepath.Join(r.toolsDir, filename)
	data, err := os.ReadFile(pyPath)
	if err != nil {
		return ToolInfo{}, err
	}

	info := ToolInfo{
		Name:     strings.TrimSuffix(filename, ".py"),
		FilePath: filename,
	}

	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "# ") {
			break
		}
		line = strings.TrimPrefix(line, "# ")
		switch {
		case strings.HasPrefix(line, "description:"):
			info.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		case strings.HasPrefix(line, "params:"):
			info.Params = strings.TrimSpace(strings.TrimPrefix(line, "params:"))
		case strings.HasPrefix(line, "requires:"):
			info.Requires = strings.TrimSpace(strings.TrimPrefix(line, "requires:"))
		case strings.HasPrefix(line, "created:"):
			info.Created = strings.TrimSpace(strings.TrimPrefix(line, "created:"))
		case strings.HasPrefix(line, "usage:"):
			info.Usage = strings.TrimSpace(strings.TrimPrefix(line, "usage:"))
		}
	}
	return info, nil
}

func sanitizeToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
