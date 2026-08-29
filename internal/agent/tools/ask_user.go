package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// AskUser is the non-deferred HITL pause primitive (PRD 1.5, SPEC Req#1). The
// model calls it to ask the user a structured question; instead of returning a
// ToolResult it returns the *ErrAwaitingUserInput sentinel, which the agent's
// dispatch loop intercepts (llm_agent_pause.go) to pause the turn — it NEVER
// produces a RoleTool result. Pure types only: this file imports no DB/sqlc
// package (D-A1-04). Durability lives in askuser.Store, wired by the Runner.
//
// ask_user is a deliberate primitive, never auto-fired (D-A3-04): the model is
// made tool-aware without being encouraged to overuse it.
type AskUser struct{}

// Kind values constrain how the channel renders the pause (D-A3-02).
const (
	KindClarification = "clarification"
	KindApproval      = "approval"
	KindChoice        = "choice"
)

// Option is one selectable answer for kind=choice. The model may supply either a
// bare string (decoded as {Label, Value} both set to the string) or an explicit
// {label, value} object. Distinct labels are enforced in validation.
type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// UnmarshalJSON accepts either a JSON string ("foo" → {Label:"foo", Value:"foo"})
// or a {label, value} object, so the SPEC Req#1 `options?:[2-4 string|{label,value}]`
// shape decodes uniformly.
func (o *Option) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		o.Label, o.Value = s, s
		return nil
	}
	var obj struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("ask_user option: want a string or {label,value} object: %w", err)
	}
	if obj.Label == "" {
		return fmt.Errorf("ask_user option: object form requires a non-empty label")
	}
	o.Label = obj.Label
	o.Value = obj.Value
	if o.Value == "" {
		o.Value = obj.Label
	}
	return nil
}

// ErrAwaitingUserInput is the pause sentinel ask_user.Execute returns on a valid
// call (D-A1-04). It is a struct error (not a bare errors.New) so the dispatch
// loop can errors.As(err, &target) and carry the pause payload into the
// Actions.AwaitingInput Event. ToolCallID is stamped by the agent before the
// Event is emitted (the tool itself does not see the call id).
type ErrAwaitingUserInput struct {
	Question   string
	Options    []Option
	Kind       string
	Priority   int
	ToolCallID string
	// ResumeContext is optional machine-readable context for the caller that will
	// handle the human answer. It is persisted by the Runner but never rendered as
	// answer text; e.g. a shell approval carries the challenged command's digest.
	ResumeContext json.RawMessage
	// ProxiedFromChildID / ProxiedToolCallID are the optional swarm-relay ids the
	// model MAY fill when this ask_user relays a child's needs_user_input report
	// (D-05). Empty on a direct (non-proxied) pause; they stamp into
	// aura.paused_states so a future resume can map the answer back to the child.
	ProxiedFromChildID string
	ProxiedToolCallID  string
}

// Error makes *ErrAwaitingUserInput satisfy the error interface. The message is
// generic; the payload travels in the struct fields, not the string.
func (e *ErrAwaitingUserInput) Error() string { return "awaiting user input" }

// askUserArgs is the wire shape of the ask_user arguments. The proxied_* fields
// are optional (D-05): the model fills them only when relaying a child's
// needs_user_input report, never on a direct pause.
type askUserArgs struct {
	Question           string          `json:"question"`
	Options            []Option        `json:"options"`
	Kind               string          `json:"kind"`
	Priority           *int            `json:"priority"`
	ResumeContext      json.RawMessage `json:"resume_context"`
	ProxiedFromChildID string          `json:"proxied_from_child_id"`
	ProxiedToolCallID  string          `json:"proxied_tool_call_id"`
}

func (AskUser) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "question": {"type": "string", "description": "The question or request to put to the user. Be specific and self-contained."},
    "options": {"type": "array", "minItems": 2, "maxItems": 4, "items": {"anyOf": [{"type": "string"}, {"type": "object"}]}, "description": "For kind=choice: 2-4 distinct options, each a string or {label, value} object."},
    "kind": {"type": "string", "enum": ["clarification", "approval", "choice"], "description": "clarification = free-text answer; approval = yes/no for an action; choice = pick one of the supplied options."},
    "priority": {"type": "integer", "minimum": 0, "maximum": 100, "description": "Optional 0-100 ordering hint when several pauses are pending (higher = answered first). Defaults to 0."},
    "resume_context": {"type": "object", "description": "Optional machine-readable resume payload for host-side approval handlers. Omit it unless a tool told you which payload to send — ordinary user questions never need one."},
    "proxied_from_child_id": {"type": "string", "description": "Optional, model-discretionary. Fill ONLY when relaying a child agent's needs_user_input report: the originating child's id (the flat worker id from the swarm report, e.g. \"w2\"). Omit on a direct question to the user."},
    "proxied_tool_call_id": {"type": "string", "description": "Optional, model-discretionary. Fill ONLY when relaying a child agent's needs_user_input report: the child's originating tool_call id (ground-truth from the swarm report). Omit on a direct question to the user."}
  },
  "required": ["question", "kind"]
}`)
	return Spec{
		Name:    "ask_user",
		Summary: "Pause and ask the user a structured question (clarification, approval, or choice).",
		Description: "Pause the current turn and wait for structured input from the user. Use kind=clarification for a free-text answer, kind=approval before an irreversible or sensitive action, or kind=choice with 2-4 distinct options. " +
			"NEVER use ask_user to collect passwords, API keys, tokens, or payment credentials — secrets must not flow through this prompt. " +
			"Call it deliberately, only when you genuinely need the user's decision; do not ask for confirmation of routine steps.",
		Parameters: params,
		Deferred:   false,
	}
}

// Execute validates the arguments and returns the *ErrAwaitingUserInput sentinel
// on success (SPEC Req#1) — never a populated ToolResult. Validation rejects an
// empty question, an options count of exactly 1, non-distinct option labels, and
// a priority outside 0-100 (threat T-04-09). The returned ToolResult is always
// the zero value.
func (AskUser) Execute(_ context.Context, raw json.RawMessage) (ToolResult, error) {
	var a askUserArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("ask_user args: %w", err)
	}
	if a.Question == "" {
		return ToolResult{}, fmt.Errorf("ask_user: question is required")
	}
	switch a.Kind {
	case KindClarification, KindApproval, KindChoice:
	default:
		return ToolResult{}, fmt.Errorf("ask_user: kind %q must be one of clarification|approval|choice", a.Kind)
	}
	if err := validateOptions(a.Options); err != nil {
		return ToolResult{}, fmt.Errorf("ask_user: %w", err)
	}
	priority := 0
	if a.Priority != nil {
		priority = *a.Priority
		if priority < 0 || priority > 100 {
			return ToolResult{}, fmt.Errorf("ask_user: priority %d is outside 0-100", priority)
		}
	}
	resumeContext := json.RawMessage(nil)
	if len(a.ResumeContext) > 0 && string(a.ResumeContext) != "null" {
		resumeContext = append(resumeContext, a.ResumeContext...)
	}
	return ToolResult{}, &ErrAwaitingUserInput{
		Question:           a.Question,
		Options:            a.Options,
		Kind:               a.Kind,
		Priority:           priority,
		ResumeContext:      resumeContext,
		ProxiedFromChildID: a.ProxiedFromChildID,
		ProxiedToolCallID:  a.ProxiedToolCallID,
	}
}

// validateOptions enforces the options invariants: zero options is allowed (e.g.
// clarification/approval), but a single option is meaningless, and duplicate
// labels make a choice ambiguous. A non-empty set must hold 2-4 distinct labels.
func validateOptions(opts []Option) error {
	if len(opts) == 0 {
		return nil
	}
	if len(opts) == 1 {
		return fmt.Errorf("options must hold at least 2 entries, got 1")
	}
	if len(opts) > 4 {
		return fmt.Errorf("options must hold at most 4 entries, got %d", len(opts))
	}
	seen := make(map[string]struct{}, len(opts))
	for _, o := range opts {
		if o.Label == "" {
			return fmt.Errorf("option label must be non-empty")
		}
		if _, dup := seen[o.Label]; dup {
			return fmt.Errorf("option labels must be distinct, %q repeats", o.Label)
		}
		seen[o.Label] = struct{}{}
	}
	return nil
}
