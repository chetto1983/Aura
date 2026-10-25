// Package approvaltext owns locale-neutral presentation metadata for recognized approval
// prompts. The canonical question remains the consent payload; this package derives a bounded,
// non-authoritative semantic view from that question and its machine resume context so renderers
// never trust presentation data relayed by the model.
package approvaltext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"unicode/utf8"
)

const (
	contextField = "approval_presentation"
	// GatewayKey identifies a mutating gateway-tool approval.
	GatewayKey = "approval.gateway.mutation"
	// ShellKey identifies a destructive shell command approval.
	ShellKey = "approval.shell.command"
	// ScheduledKey identifies a scheduled-task activation approval.
	ScheduledKey = "approval.scheduled.task"
	// PluginInstallKey identifies a plugin install approval.
	PluginInstallKey = "approval.plugin.install"
	// PluginRemoveKey identifies a plugin removal approval.
	PluginRemoveKey = "approval.plugin.remove"

	maxShortParamBytes = 256
	maxArgsBytes       = 1024
	maxCommandBytes    = 8192
	// maxTargetBytes holds the gateway's 120-rune target at four bytes a rune.
	maxTargetBytes = 512
)

// Presentation is persisted inside resume_context beside the canonical question. Key is stable
// API; Params contain data only, never prose or instructions.
type Presentation struct {
	Key    string            `json:"key"`
	Params map[string]string `json:"params"`
}

// Enrich removes any model-supplied presentation and derives a canonical-bound replacement from
// the question plus resume coordinates. Unknown/legacy questions keep no metadata and therefore
// render through their original question.
func Enrich(question string, raw json.RawMessage) (json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := json.Unmarshal(trimmed, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("approval presentation context must be a JSON object")
		}
	}
	delete(fields, contextField)
	if presentation, ok := derive(question, fields); ok {
		encoded, err := json.Marshal(presentation)
		if err != nil {
			return nil, fmt.Errorf("marshal approval presentation: %w", err)
		}
		fields[contextField] = encoded
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("marshal approval presentation context: %w", err)
	}
	return out, nil
}

// FromContext returns metadata only when the persisted view still exactly matches a fresh
// derivation from the canonical question and root resume coordinates. Malformed, forged and
// future keys fail closed to the canonical question.
func FromContext(question string, raw json.RawMessage) (Presentation, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return Presentation{}, false
	}
	encoded, ok := fields[contextField]
	if !ok {
		return Presentation{}, false
	}
	var p Presentation
	if json.Unmarshal(encoded, &p) != nil || !valid(p) {
		return Presentation{}, false
	}
	expected, ok := derive(question, fields)
	if !ok || p.Key != expected.Key || !maps.Equal(p.Params, expected.Params) {
		return Presentation{}, false
	}
	return expected, true
}

func derive(question string, fields map[string]json.RawMessage) (Presentation, bool) {
	switch RootString(fields, "type") {
	case "gateway_approval":
		return deriveGateway(question, fields)
	case "shell_exec_approval":
		return deriveShell(question, fields)
	case "scheduled_task_approval":
		return deriveScheduled(question, fields)
	case "plugin_pack_approval":
		return derivePlugin(question, fields)
	default:
		return Presentation{}, false
	}
}

func deriveGateway(question string, fields map[string]json.RawMessage) (Presentation, bool) {
	tool, risk := RootString(fields, "tool"), RootString(fields, "tier")
	if tool == "" || risk == "" {
		return Presentation{}, false
	}
	prefix := fmt.Sprintf("Approve %s (risk=%s)?\nThis mutating action is WITHHELD until you accept.\nargs: ", tool, risk)
	args, ok := strings.CutPrefix(question, prefix)
	if !ok {
		return Presentation{}, false
	}
	p := Presentation{Key: GatewayKey, Params: map[string]string{"tool": tool, "risk": risk, "args": args}}
	// The runner stamps target from the gateway's challenge when the tool described the call;
	// the question the model relays never carries it.
	if target := RootString(fields, "target"); target != "" {
		p.Params["target"] = target
	}
	return p, valid(p)
}

func deriveShell(question string, fields map[string]json.RawMessage) (Presentation, bool) {
	digest := RootString(fields, "command_sha256")
	if digest == "" {
		return Presentation{}, false
	}
	rest, ok := strings.CutPrefix(question, "Approve shell_exec command?\ncwd: ")
	if !ok {
		return Presentation{}, false
	}
	cwd, commandAndDigest, ok := strings.Cut(rest, "\ncommand:\n")
	if !ok {
		return Presentation{}, false
	}
	marker := "\nsha256: "
	idx := strings.LastIndex(commandAndDigest, marker)
	if idx < 0 || commandAndDigest[idx+len(marker):] != digest {
		return Presentation{}, false
	}
	p := Presentation{Key: ShellKey, Params: map[string]string{
		"cwd": cwd, "command": commandAndDigest[:idx], "digest": digest,
	}}
	return p, valid(p)
}

func deriveScheduled(question string, fields map[string]json.RawMessage) (Presentation, bool) {
	taskID := RootString(fields, "task_id")
	if taskID == "" {
		return Presentation{}, false
	}
	task := taskID
	if len(task) > 8 {
		task = task[:8]
	}
	if p, ok := deriveScheduledCreated(question, task); ok {
		return p, true
	}
	const suffix = " needs your approval. Approve or reject it below."
	rest, ok := strings.CutPrefix(question, "Scheduled ")
	if !ok || !strings.HasSuffix(rest, suffix) {
		return Presentation{}, false
	}
	kind, tail, ok := strings.Cut(strings.TrimSuffix(rest, suffix), " task ")
	if !ok || tail != task {
		return Presentation{}, false
	}
	p := Presentation{Key: ScheduledKey, Params: map[string]string{"task": task, "kind": kind}}
	return p, valid(p)
}

func deriveScheduledCreated(question, task string) (Presentation, bool) {
	rest, ok := strings.CutPrefix(question, "Approve scheduled ")
	if !ok {
		return Presentation{}, false
	}
	kind, tail, ok := strings.Cut(rest, " task "+task+" (")
	const suffix = ")? It will not fire until you approve."
	if !ok || !strings.HasSuffix(tail, suffix) {
		return Presentation{}, false
	}
	details := strings.TrimSuffix(tail, suffix)
	schedule, risk, ok := strings.CutLast(details, ", risk=")
	if !ok {
		return Presentation{}, false
	}
	p := Presentation{Key: ScheduledKey, Params: map[string]string{
		"task": task, "kind": kind, "schedule": schedule, "risk": risk,
	}}
	return p, valid(p)
}

// RootString reads a root string field of a resume context by its exact key, as Postgres's ->>
// reads it: "" when the field is absent, empty or not a string. Every reader of a resume
// context goes through it, because a struct tag would also match "TYPE" or "Tool_Call_ID", a
// spelling the model can write and the runner's stamp never sees.
func RootString(fields map[string]json.RawMessage, name string) string {
	var value string
	_ = json.Unmarshal(fields[name], &value)
	return value
}

func valid(p Presentation) bool {
	if p.Params == nil {
		return false
	}
	var limits map[string]int
	switch p.Key {
	case GatewayKey:
		limits = map[string]int{"tool": maxShortParamBytes, "risk": maxShortParamBytes, "args": maxArgsBytes, "target": maxTargetBytes}
	case ShellKey:
		limits = map[string]int{"cwd": maxArgsBytes, "command": maxCommandBytes, "digest": maxShortParamBytes}
	case ScheduledKey:
		limits = map[string]int{"task": maxShortParamBytes, "kind": maxShortParamBytes, "schedule": maxArgsBytes, "risk": maxShortParamBytes}
	case PluginInstallKey, PluginRemoveKey:
		limits = pluginLimits(p.Key)
	default:
		return false
	}
	for name, value := range p.Params {
		limit, known := limits[name]
		if !known || !utf8.ValidString(value) || len(value) > limit || strings.ContainsRune(value, 0) {
			return false
		}
	}
	for _, required := range requiredParams(p.Key) {
		if _, ok := p.Params[required]; !ok {
			return false
		}
	}
	return true
}

func requiredParams(key string) []string {
	switch key {
	case GatewayKey:
		return []string{"tool", "risk", "args"}
	case ShellKey:
		return []string{"cwd", "command", "digest"}
	case ScheduledKey:
		return []string{"task", "kind"}
	case PluginInstallKey, PluginRemoveKey:
		return pluginRequired(key)
	default:
		return nil
	}
}

// RenderItalian is the Telegram channel's fixed-Italian renderer. Browser surfaces use the same
// key/params through i18next. Fallback is the canonical consent question for legacy/unknown data.
func RenderItalian(p Presentation, fallback string) string {
	if !valid(p) {
		return fallback
	}
	switch p.Key {
	case GatewayKey:
		args := p.Params["args"]
		if args == "(none)" {
			args = "(nessuno)"
		}
		question := fmt.Sprintf("Approva %s (rischio=%s)?\nQuesta azione mutante resta BLOCCATA finché non accetti.\nargomenti: %s", p.Params["tool"], p.Params["risk"], args)
		if target := p.Params["target"]; target != "" {
			question += "\noggetto: " + target
		}
		return question
	case ShellKey:
		//nolint:misspell // "comando" is the correct Italian noun, not English "commando".
		return fmt.Sprintf("Approva il comando shell_exec?\ndirectory: %s\ncomando:\n%s\nsha256: %s", p.Params["cwd"], p.Params["command"], p.Params["digest"])
	case ScheduledKey:
		if p.Params["schedule"] != "" && p.Params["risk"] != "" {
			return fmt.Sprintf("Approva l'attività pianificata %s %s (%s, rischio=%s)? Non verrà eseguita finché non approvi.", p.Params["kind"], p.Params["task"], p.Params["schedule"], p.Params["risk"])
		}
		return fmt.Sprintf("Approva l'attività pianificata %s %s? Non verrà eseguita finché non approvi.", p.Params["kind"], p.Params["task"])
	case PluginInstallKey, PluginRemoveKey:
		return renderPluginItalian(p)
	default:
		return fallback
	}
}
