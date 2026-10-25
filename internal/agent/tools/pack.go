package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/packs"
)

// PackTool reads, installs and removes a knowledge-work plugin as ONE unit. It is named
// `plugin_pack` and not `pack`: the deferred-tool guard checks by SUBSTRING that the system
// prompt never names a tool the model cannot see, and `pack` is a substring of ordinary
// English the prompt already uses. Exempting it would have weakened a real guard in order to
// keep a worse name.
//
// It exists as a TOOL and not only as `aura pack` because the CLI is unreachable from where
// the model works: `shell_exec` runs inside the per-identity sandbox box, which carries no
// aura binary. Measured 2026-08-23 by driving the real agent — it searched PATH, /workspace,
// npm, pip and the shell history, found nothing, correctly refused to invent an answer, and
// fell back to reading the GitHub API by hand. A capability the model cannot reach is a
// capability it does not have.
//
// list and show only read. install and remove go through the same plugin backend as the
// cockpit (spec 2026-10-25-pack-install §4.6), behind the tool's own approval card: the
// first call returns a challenge the model relays with ask_user, and only the repeat after
// the operator accepts acts.
type PackTool struct {
	// Resolve is the seam. Nil uses a real shallow clone.
	Resolve func(ctx context.Context, ref packs.Ref) ([]packs.Pack, error)
	// Backend installs and removes. Serve boot binds it once the registry, the library and
	// the live mount exist; nil answers install and remove with "not available".
	Backend PluginBackend
	// Approvals is the plugin approvals' own store, separate from the shell's.
	Approvals *PluginApprovals
}

const packToolDescription = "Read a knowledge-work plugin PACK from its repository, and install or remove one as a unit: its skills, its MCP connectors, and its commands. " +
	"The skills catalogue indexes a pack's skills individually and DROPS the grouping — `call-prep` is listed as `anthropics/knowledge-work-plugins/call-prep` with no sign that it belongs to `sales`, nor of the connectors that ship beside it. This puts the pack back together.\n\n" +
	"action=list <owner/repo> summarizes every pack in a repository. action=show <owner/repo/plugin> gives one in full.\n\n" +
	"action=install <owner/repo/plugin> installs the plugin's skills and connectors together. action=remove name=<plugin> removes an installed plugin; delete_data=true also deletes its data in your box. " +
	"Neither acts on the first call: it returns plugin_approval_required with the operator's question. Relay it with ask_user kind=\"approval\", copying question and resume_context exactly, and after the operator accepts call plugin_pack again with exactly the arguments in repeat. " +
	"Only the operator may install or remove plugins; anyone else is told to ask them."

const packParamsSchema = `{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list", "show", "install", "remove"],
      "description": "list: every pack in the repository. show: one pack in full. install: install a plugin. remove: remove an installed plugin."
    },
    "ref": {
      "type": "string",
      "description": "owner/repo for list, owner/repo/plugin for show and install. Example: anthropics/knowledge-work-plugins/sales"
    },
    "name": {
      "type": "string",
      "description": "remove: the installed plugin's name."
    },
    "commit": {
      "type": "string",
      "description": "Only when repeating an approved install or removal: copy it from repeat."
    },
    "digest": {
      "type": "string",
      "description": "Only when repeating an approved install: copy it from repeat."
    },
    "delete_data": {
      "type": "boolean",
      "description": "remove: also delete the plugin's data in your box. Default false."
    }
  },
  "required": ["action"]
}`

// Spec returns the deferred manifest entry. Deferred because the description above carries
// the whole reason the tool exists, and a model that never touches a pack should not pay for
// that on every turn. It never depends on whether a backend is bound: the prompt cache keys
// on it.
func (t *PackTool) Spec() Spec {
	return Spec{
		Name:           "plugin_pack",
		Summary:        "Read, install or remove a knowledge-work plugin pack — its skills, connectors and commands — as one unit.",
		Description:    packToolDescription,
		Parameters:     json.RawMessage(packParamsSchema),
		Deferred:       true,
		Mutating:       true,
		Multiplexed:    true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
	}
}

type packArgs struct {
	Action     string `json:"action"`
	Ref        string `json:"ref"`
	Name       string `json:"name"`
	Commit     string `json:"commit"`
	Digest     string `json:"digest"`
	DeleteData bool   `json:"delete_data"`
}

// Execute dispatches the action. list and show render the CLI's text, so the operator
// reading `aura pack show` and the model reading this tool see the SAME text — a divergence
// between the two would be a second description of one thing.
func (t *PackTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a packArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("plugin_pack args: %w", err)
	}
	switch action := strings.TrimSpace(a.Action); action {
	case "list", "show":
		return t.read(ctx, action, a.Ref)
	case "install":
		return t.install(ctx, a)
	case "remove":
		return t.remove(ctx, a)
	default:
		return ToolResult{}, fmt.Errorf("plugin_pack: unknown action %q (want list, show, install or remove)", a.Action)
	}
}

func (t *PackTool) read(ctx context.Context, action, rawRef string) (ToolResult, error) {
	ref, err := packs.ParseRef(rawRef)
	if err != nil {
		return ToolResult{}, err
	}
	if action == "show" && ref.Directory == "" {
		return ToolResult{}, fmt.Errorf("show needs a plugin: %s/<plugin> — call action=list on %s first to see them", ref.Source, ref.Source)
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

// Evict reclaims a finished conversation's plugin approvals (SessionEvictor, R-41).
func (t *PackTool) Evict(sessionID string) {
	t.Approvals.Evict(sessionID)
}
