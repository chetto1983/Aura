package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/packs"
)

// PackTool reads a knowledge-work plugin as ONE unit. It is named `plugin_pack`
// and not `pack`: the deferred-tool guard checks by SUBSTRING that the system
// prompt never names a tool the model cannot see, and `pack` is a substring of
// ordinary English the prompt already uses. Exempting it would have weakened a
// real guard in order to keep a worse name.
//
// It exists as a TOOL and not only as `aura pack` because the CLI is unreachable
// from where the model works: `shell_exec` runs inside the per-identity sandbox
// box, which carries no aura binary. Measured 2026-08-23 by driving the real
// agent — it searched PATH, /workspace, npm, pip and the shell history, found
// nothing, correctly refused to invent an answer, and fell back to reading the
// GitHub API by hand. A capability the model cannot reach is a capability it
// does not have.
//
// Read-only by construction: it resolves and describes. Installing a pack's
// skills goes through `skill_manage`, and its connectors through the MCP install
// path, so neither acquires a second implementation and neither loses the
// operator approval that guards it.
type PackTool struct {
	// Resolve is the seam. Nil uses a real shallow clone.
	Resolve func(ctx context.Context, ref packs.Ref) ([]packs.Pack, error)
}

const packToolDescription = "Read a knowledge-work plugin PACK from its repository: its skills, its MCP connectors, and its commands, as one unit. " +
	"The skills catalogue indexes a pack's skills individually and DROPS the grouping — `call-prep` is listed as `anthropics/knowledge-work-plugins/call-prep` with no sign that it belongs to `sales`, nor of the connectors that ship beside it. This puts the pack back together.\n\n" +
	"action=list <owner/repo> summarizes every pack in a repository. action=show <owner/repo/plugin> gives one in full.\n\n" +
	"This tool READS. It installs nothing: use skill_manage to install a skill the pack names (the `owner/repo@skill` form is printed for you), and tell the operator about the connectors — each one needs their approval, and a connector marked as needing OAuth cannot be brought up without them."

const packParamsSchema = `{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list", "show"],
      "description": "list: every pack in the repository. show: one pack in full."
    },
    "ref": {
      "type": "string",
      "description": "owner/repo for list, owner/repo/plugin for show. Example: anthropics/knowledge-work-plugins/sales"
    }
  },
  "required": ["action", "ref"]
}`

// Spec returns the deferred manifest entry. Deferred because the description
// above carries the whole reason the tool exists, and a model that never touches
// a pack should not pay for that on every turn.
func (t *PackTool) Spec() Spec {
	return Spec{
		Name:        "plugin_pack",
		Summary:     "Read a knowledge-work plugin pack — its skills, connectors and commands — as one unit.",
		Description: packToolDescription,
		Parameters:  json.RawMessage(packParamsSchema),
		Deferred:    true,
		Mutating:    false,
		Multiplexed: false,
	}
}

type packArgs struct {
	Action string `json:"action"`
	Ref    string `json:"ref"`
}

// Execute resolves and renders. The rendering is the CLI's, so the operator reading
// `aura pack show` and the model reading this tool see the SAME text — a
// divergence between the two would be a second description of one thing.
func (t *PackTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a packArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("plugin_pack args: %w", err)
	}
	ref, err := packs.ParseRef(a.Ref)
	if err != nil {
		return ToolResult{}, err
	}

	action := strings.TrimSpace(a.Action)
	if action == "show" && ref.Directory == "" {
		return ToolResult{}, fmt.Errorf("show needs a plugin: %s/<plugin> — call action=list on %s first to see them", ref.Source, ref.Source)
	}
	if action != "list" && action != "show" {
		return ToolResult{}, fmt.Errorf("plugin_pack: unknown action %q (want list or show)", a.Action)
	}

	resolve := t.Resolve
	if resolve == nil {
		resolve = (&packs.Resolver{}).Resolve
	}
	found, err := resolve(ctx, ref)
	if err != nil {
		return ToolResult{}, err
	}

	var out bytes.Buffer
	if action == "list" {
		packs.WriteList(&out, found)
	} else {
		packs.WriteDetail(&out, found[0])
	}
	return NewResult(ctx, out.String())
}
