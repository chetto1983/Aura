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

	maxShortParamBytes = 256
	maxArgsBytes       = 1024
	maxCommandBytes    = 8192
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
	typeName, ok := stringField(fields, "type")
	if !ok {
		return Presentation{}, false
	}
	switch typeName {
	case "gateway_approval":
		return deriveGateway(question, fields)
	case "shell_exec_approval":
		return deriveShell(question, fields)
	case "scheduled_task_approval":
		return deriveScheduled(question, fields)
	default:
		return Presentation{}, false
	}
}

func deriveGateway(question string, fields map[string]json.RawMessage) (Presentation, bool) {
	tool, toolOK := stringField(fields, "tool")
	risk, riskOK := stringField(fields, "tier")
	if !toolOK || !riskOK {
		return Presentation{}, false
	}
	prefix := fmt.Sprintf("Approve %s (risk=%s)?\nThis mutating action is WITHHELD until you accept.\nargs: ", tool, risk)
	args, ok := strings.CutPrefix(question, prefix)
	if !ok {
		return Presentation{}, false
	}
	p := Presentation{Key: GatewayKey, Params: map[string]string{"tool": tool, "risk": risk, "args": args}}
	return p, valid(p)
}

func deriveShell(question string, fields map[string]json.RawMessage) (Presentation, bool) {
	digest, ok := stringField(fields, "command_sha256")
	if !ok {
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
	taskID, ok := stringField(fields, "task_id")
	if !ok || taskID == "" {
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
	idx := strings.LastIndex(details, ", risk=")
	if idx < 0 {
		return Presentation{}, false
	}
	p := Presentation{Key: ScheduledKey, Params: map[string]string{
		"task": task, "kind": kind, "schedule": details[:idx], "risk": details[idx+len(", risk="):],
	}}
	return p, valid(p)
}

func stringField(fields map[string]json.RawMessage, name string) (string, bool) {
	var value string
	if json.Unmarshal(fields[name], &value) != nil || value == "" {
		return "", false
	}
	return value, true
}

func valid(p Presentation) bool {
	if p.Params == nil {
		return false
	}
	var limits map[string]int
	switch p.Key {
	case GatewayKey:
		limits = map[string]int{"tool": maxShortParamBytes, "risk": maxShortParamBytes, "args": maxArgsBytes}
	case ShellKey:
		limits = map[string]int{"cwd": maxArgsBytes, "command": maxCommandBytes, "digest": maxShortParamBytes}
	case ScheduledKey:
		limits = map[string]int{"task": maxShortParamBytes, "kind": maxShortParamBytes, "schedule": maxArgsBytes, "risk": maxShortParamBytes}
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
		return fmt.Sprintf("Approva %s (rischio=%s)?\nQuesta azione mutante resta BLOCCATA finché non accetti.\nargomenti: %s", p.Params["tool"], p.Params["risk"], args)
	case ShellKey:
		//nolint:misspell // "comando" is the correct Italian noun, not English "commando".
		return fmt.Sprintf("Approva il comando shell_exec?\ndirectory: %s\ncomando:\n%s\nsha256: %s", p.Params["cwd"], p.Params["command"], p.Params["digest"])
	case ScheduledKey:
		if p.Params["schedule"] != "" && p.Params["risk"] != "" {
			return fmt.Sprintf("Approva l'attività pianificata %s %s (%s, rischio=%s)? Non verrà eseguita finché non approvi.", p.Params["kind"], p.Params["task"], p.Params["schedule"], p.Params["risk"])
		}
		return fmt.Sprintf("Approva l'attività pianificata %s %s? Non verrà eseguita finché non approvi.", p.Params["kind"], p.Params["task"])
	default:
		return fallback
	}
}
