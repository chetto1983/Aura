package approvaltext

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// ToolApprovalType is the resume-context type of a call the host parked for the operator. Only
// the runner writes it, from the parked call it holds, so a context of this type carries the
// host's own card and FromContext trusts it as stored.
const ToolApprovalType = "tool_approval"

// maxBriefRunes bounds Brief, the short "what" the model and the record read beside the tool.
const maxBriefRunes = 120

var errInvalidToolApproval = errors.New("approvaltext: a tool approval needs a tool and a valid card")

// ToolApprovalContext is the resume context of a parked call: its type, its tool and its card.
func ToolApprovalContext(tool string, p Presentation) (json.RawMessage, error) {
	if tool == "" || !Valid(p) {
		return nil, errInvalidToolApproval
	}
	return json.Marshal(struct {
		Type         string       `json:"type"`
		Tool         string       `json:"tool"`
		Presentation Presentation `json:"approval_presentation"`
	}{ToolApprovalType, tool, p})
}

// IsToolApproval reports whether a resume context has the tool_approval type, read by its exact
// key as the database trigger reads it.
func IsToolApproval(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	return RootString(fields, "type") == ToolApprovalType
}

// Brief is a card's short "what": the gateway's argument keys and target, the shell's first
// command line and directory, the plugin, or the task to cancel. It is "" for any other card.
func Brief(p Presentation) string {
	if !Valid(p) {
		return ""
	}
	var brief string
	switch p.Key {
	case GatewayKey:
		brief = "(args: " + p.Params["args"] + ")"
		if target := p.Params["target"]; target != "" {
			brief += " on " + target
		}
	case ShellKey:
		first, _, _ := strings.Cut(p.Params["command"], "\n")
		brief = "`" + first + "` in " + p.Params["cwd"]
	case PluginInstallKey, PluginRemoveKey:
		brief = "plugin " + p.Params["plugin"]
	case TaskCancelKey:
		brief = "cancel of task " + p.Params["task"]
	}
	return clipRunes(brief, maxBriefRunes)
}

// clipRunes cuts s to at most limit runes, the last one an ellipsis when it cuts.
func clipRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit-1]) + "…"
}
