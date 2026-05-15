package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// DevToolTool consolidates the legacy verb-tools (list_tools, read_tool,
// save_tool) into a single action-enum surface. The picobot pattern:
// one tool with the action enum acting as the verb keeps related
// functionality together so the LLM never has to guess which of three
// near-identical tools to pick.
//
// "Dev" tools live in the Python script registry — they are user-saved
// snippets the execute_code sandbox can re-run later. They are NOT the
// Anthropic tool_use registry. The dev_tool name makes that distinction
// explicit in every prompt where the tool surfaces.
//
//	action=list — enumerate all registered scripts. No args.
//	action=read — fetch the source of one script. Requires name.
//	action=save — persist a script as reusable. Requires name, description, code.
type DevToolTool struct {
	store ToolStore
}

// NewDevToolTool returns the consolidated dev_tool. nil store returns
// nil so callers can compose registration unconditionally.
func NewDevToolTool(store ToolStore) *DevToolTool {
	if store == nil {
		return nil
	}
	return &DevToolTool{store: store}
}

func (t *DevToolTool) Name() string { return "dev_tool" }

func (t *DevToolTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		IdempotentHint: true, // save with same name is an upsert; list/read are side-effect free
		VisibilityTier: VisibilityDeferred,
	}
}

func (t *DevToolTool) Description() string {
	return `Manage the Python script registry used by execute_code.

Actions (pick one via the "action" field):

  • list — enumerate every registered script (name, description, params).
    No arguments. Call this BEFORE writing new code — a snippet may
    already exist for the task.

  • read — fetch the source code of one script. Required: name.
    Use this to understand what an existing script does before reusing
    or extending it.

  • save — persist a script as a permanent, reusable tool.
    Required: name (lowercase_underscores), description (one-liner),
    code (Python source). Optional: params ("filepath (str), n (int)"),
    usage ("when the user uploaded a CSV"). Call this AFTER a successful
    execute_code run that solved a reusable problem so the next session
    can pick it up via list+read.

The scripts here are not the same as the built-in Aura tools. They are
user snippets the Python sandbox can execute on demand.`
}

func (t *DevToolTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "read", "save"},
				"description": "Which operation to perform: list (enumerate), read (fetch source), save (persist new script).",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Script name. Required for read and save. Use lowercase_underscores for save.",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "save only: one-line description of what the script does.",
			},
			"params": map[string]any{
				"type":        "string",
				"description": "save only, optional: comma-separated parameter names with types, e.g. 'filepath (str), col1 (str)'.",
			},
			"code": map[string]any{
				"type":        "string",
				"description": "save only: the full Python source code.",
			},
			"usage": map[string]any{
				"type":        "string",
				"description": "save only, optional: when to reach for this script, e.g. 'user uploaded a CSV and wants statistics'.",
			},
		},
	}
}

func (t *DevToolTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	action := strings.TrimSpace(stringArg(args, "action"))
	switch action {
	case "list":
		return t.doList(ctx)
	case "read":
		return t.doRead(ctx, args)
	case "save":
		return t.doSave(ctx, args)
	case "":
		return "", ActionRequiredError("dev_tool", devToolValidActions, args, devToolActionHints, "list")
	default:
		return "", UnknownActionError("dev_tool", action, devToolValidActions, args)
	}
}

var (
	devToolValidActions = []string{"list", "read", "save"}
	devToolActionHints  = []ActionHint{
		// save is the most specific (needs code + description). read only
		// needs name. list needs nothing.
		{Name: "save", RequiredKeys: []string{"code"}},
		{Name: "read", RequiredKeys: []string{"name"}},
	}
)

func (t *DevToolTool) doList(_ context.Context) (string, error) {
	scripts, err := t.store.ListTools()
	if err != nil {
		return "", fmt.Errorf("dev_tool list: %w", err)
	}
	if len(scripts) == 0 {
		return "No scripts registered yet.", nil
	}
	var b strings.Builder
	for _, s := range scripts {
		fmt.Fprintf(&b, "- **%s**: %s (params: %s)\n", s.Name, s.Description, s.Params)
	}
	return b.String(), nil
}

func (t *DevToolTool) doRead(_ context.Context, args map[string]any) (string, error) {
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		return "", fmt.Errorf("dev_tool read: name is required: %w", llm.ErrSchemaValidation)
	}
	code, err := t.store.GetToolCode(name)
	if err != nil {
		return "", fmt.Errorf("dev_tool read %s: %w", name, err)
	}
	return code, nil
}

func (t *DevToolTool) doSave(ctx context.Context, args map[string]any) (string, error) {
	name := strings.TrimSpace(stringArg(args, "name"))
	description := strings.TrimSpace(stringArg(args, "description"))
	code := strings.TrimSpace(stringArg(args, "code"))
	if name == "" {
		return "", fmt.Errorf("dev_tool save: name is required: %w", llm.ErrSchemaValidation)
	}
	if description == "" {
		return "", fmt.Errorf("dev_tool save: description is required: %w", llm.ErrSchemaValidation)
	}
	if code == "" {
		return "", fmt.Errorf("dev_tool save: code is required: %w", llm.ErrSchemaValidation)
	}
	params := strings.TrimSpace(stringArg(args, "params"))
	usage := strings.TrimSpace(stringArg(args, "usage"))
	if err := t.store.SaveTool(ctx, name, description, params, code, usage); err != nil {
		return "", fmt.Errorf("dev_tool save: %w", err)
	}
	return fmt.Sprintf("Script %q saved to registry.", name), nil
}
