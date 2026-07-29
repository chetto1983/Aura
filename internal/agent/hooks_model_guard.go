package agent

import (
	"fmt"
	"reflect"

	"github.com/chetto1983/aura/internal/llm"
)

// cloneModelRequest takes a deep snapshot of every mutable request field so a
// faulting or policy-violating hook cannot leave changes behind through slice,
// RawMessage, or pointer aliases.
func cloneModelRequest(req *llm.Request) llm.Request {
	if req == nil {
		return llm.Request{}
	}
	cloned := *req
	if req.Messages != nil {
		cloned.Messages = make([]llm.Message, len(req.Messages))
		for i := range req.Messages {
			cloned.Messages[i] = req.Messages[i]
			if req.Messages[i].ToolCalls != nil {
				cloned.Messages[i].ToolCalls = append([]llm.ToolCall{}, req.Messages[i].ToolCalls...)
			}
		}
	}
	if req.Tools != nil {
		cloned.Tools = make([]llm.ToolDef, len(req.Tools))
		for i := range req.Tools {
			cloned.Tools[i] = req.Tools[i]
			if req.Tools[i].Function.Parameters != nil {
				cloned.Tools[i].Function.Parameters = append([]byte{}, req.Tools[i].Function.Parameters...)
			}
		}
	}
	cloned.Reasoning.Exclude = cloneBool(req.Reasoning.Exclude)
	cloned.Reasoning.Enabled = cloneBool(req.Reasoning.Enabled)
	return cloned
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// validateModelHookMutation enforces Amendment #99's hook boundary. Sampling
// and reasoning fields may change; provider identity, exact context, tools, and
// sticky-routing/cache directives are protected.
func validateModelHookMutation(before, after *llm.Request) error {
	if before == nil || after == nil {
		return fmt.Errorf("before_model hook received a nil request")
	}
	switch {
	case before.Model != after.Model:
		return protectedModelHookFieldError("model")
	case !reflect.DeepEqual(before.Messages, after.Messages):
		return protectedModelHookFieldError("messages")
	case !reflect.DeepEqual(before.Tools, after.Tools):
		return protectedModelHookFieldError("tools")
	case before.SessionID != after.SessionID:
		return protectedModelHookFieldError("session_id")
	case before.ToolsCacheControl != after.ToolsCacheControl:
		return protectedModelHookFieldError("tools_cache_control")
	case before.ToolChoice != after.ToolChoice:
		return protectedModelHookFieldError("tool_choice")
	default:
		return nil
	}
}

// validateCommandModelHookMutation compares the command-visible request shape.
// DynamicTailID is intentionally absent from command-hook JSON, so it is
// excluded from this comparison and retained from the live request on apply.
func validateCommandModelHookMutation(before, after *llm.Request) error {
	expected := cloneModelRequest(before)
	proposed := cloneModelRequest(after)
	for i := range expected.Messages {
		expected.Messages[i].DynamicTailID = ""
	}
	for i := range proposed.Messages {
		proposed.Messages[i].DynamicTailID = ""
	}
	return validateModelHookMutation(&expected, &proposed)
}

func protectedModelHookFieldError(field string) error {
	return fmt.Errorf("before_model hook changed protected request field %q", field)
}
