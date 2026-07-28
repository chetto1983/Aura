package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestAskUser_Spec_NonDeferred(t *testing.T) {
	s := AskUser{}.Spec()
	if s.Name != "ask_user" {
		t.Fatalf("name: want ask_user, got %q", s.Name)
	}
	if s.Deferred {
		t.Fatal("ask_user must be non-deferred (always-visible primitive)")
	}
	// Parameters must be valid JSON-schema (decodable).
	var schema map[string]any
	if err := json.Unmarshal(s.Parameters, &schema); err != nil {
		t.Fatalf("Parameters not valid JSON: %v", err)
	}
}

func TestErrAwaitingUserInputError(t *testing.T) {
	err := (&ErrAwaitingUserInput{Question: "continue?"}).Error()
	if err != "awaiting user input" {
		t.Fatalf("Error() = %q, want stable sentinel message", err)
	}
}

func TestAskUser_Execute_ValidApproval_ReturnsSentinel(t *testing.T) {
	res, err := AskUser{}.Execute(context.Background(),
		json.RawMessage(`{"question":"delete the file?","kind":"approval","priority":50}`))

	var pause *ErrAwaitingUserInput
	if !errors.As(err, &pause) {
		t.Fatalf("Execute: want *ErrAwaitingUserInput, got %v", err)
	}
	if pause.Question != "delete the file?" || pause.Kind != KindApproval || pause.Priority != 50 {
		t.Fatalf("sentinel payload wrong: %+v", pause)
	}
	if pause.ToolCallID != "" {
		t.Errorf("ToolCallID must be empty (stamped by the agent, not the tool), got %q", pause.ToolCallID)
	}
	if res != (ToolResult{}) {
		t.Errorf("ToolResult must be zero on a pause, got %+v", res)
	}
}

func TestAskUser_Execute_DefaultPriorityZero(t *testing.T) {
	_, err := AskUser{}.Execute(context.Background(),
		json.RawMessage(`{"question":"which branch?","kind":"clarification"}`))
	var pause *ErrAwaitingUserInput
	if !errors.As(err, &pause) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if pause.Priority != 0 {
		t.Errorf("omitted priority must default to 0, got %d", pause.Priority)
	}
}

func TestAskUser_Execute_ChoiceOptions_StringAndObjectForms(t *testing.T) {
	_, err := AskUser{}.Execute(context.Background(),
		json.RawMessage(`{"question":"pick","kind":"choice","options":["red",{"label":"green","value":"g"}]}`))
	var pause *ErrAwaitingUserInput
	if !errors.As(err, &pause) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if len(pause.Options) != 2 {
		t.Fatalf("want 2 options, got %d", len(pause.Options))
	}
	if pause.Options[0].Label != "red" || pause.Options[0].Value != "red" {
		t.Errorf("string option must set label==value, got %+v", pause.Options[0])
	}
	if pause.Options[1].Label != "green" || pause.Options[1].Value != "g" {
		t.Errorf("object option mis-decoded, got %+v", pause.Options[1])
	}
}

func TestAskUser_Execute_Rejects(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"empty question", `{"question":"","kind":"approval"}`},
		{"unknown kind", `{"question":"q","kind":"bogus"}`},
		{"exactly one option", `{"question":"q","kind":"choice","options":["only-one"]}`},
		{"five options", `{"question":"q","kind":"choice","options":["a","b","c","d","e"]}`},
		{"non-distinct labels", `{"question":"q","kind":"choice","options":["dup","dup"]}`},
		{"priority -1", `{"question":"q","kind":"approval","priority":-1}`},
		{"priority 101", `{"question":"q","kind":"approval","priority":101}`},
		{"malformed json", `{"question":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := AskUser{}.Execute(context.Background(), json.RawMessage(tc.args))
			if err == nil {
				t.Fatalf("want a rejection error, got nil")
			}
			var pause *ErrAwaitingUserInput
			if errors.As(err, &pause) {
				t.Fatalf("a rejected call must NOT return the pause sentinel, got %v", err)
			}
			if res != (ToolResult{}) {
				t.Errorf("ToolResult must be zero on rejection, got %+v", res)
			}
		})
	}
}

func TestAskUser_Execute_ProxiedIDsParsed(t *testing.T) {
	_, err := AskUser{}.Execute(context.Background(),
		json.RawMessage(`{"question":"relay: which file?","kind":"clarification","proxied_from_child_id":"11111111-1111-1111-1111-111111111111","proxied_tool_call_id":"tc1"}`))
	var pause *ErrAwaitingUserInput
	if !errors.As(err, &pause) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if pause.ProxiedFromChildID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("proxied_from_child_id not carried: got %q", pause.ProxiedFromChildID)
	}
	if pause.ProxiedToolCallID != "tc1" {
		t.Errorf("proxied_tool_call_id not carried: got %q", pause.ProxiedToolCallID)
	}
}

func TestAskUser_Execute_ProxiedIDsOptional(t *testing.T) {
	// A direct (non-proxied) call leaves both proxied fields empty and still pauses.
	_, err := AskUser{}.Execute(context.Background(),
		json.RawMessage(`{"question":"direct?","kind":"approval"}`))
	var pause *ErrAwaitingUserInput
	if !errors.As(err, &pause) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if pause.ProxiedFromChildID != "" || pause.ProxiedToolCallID != "" {
		t.Errorf("absent proxied fields must stay empty, got from=%q tc=%q",
			pause.ProxiedFromChildID, pause.ProxiedToolCallID)
	}
}

func TestAskUser_Execute_ResumeContextParsed(t *testing.T) {
	_, err := AskUser{}.Execute(context.Background(),
		json.RawMessage(`{"question":"run this command?","kind":"approval","resume_context":{"type":"shell_exec_approval","command_sha256":"abc123"}}`))
	var pause *ErrAwaitingUserInput
	if !errors.As(err, &pause) {
		t.Fatalf("want sentinel, got %v", err)
	}
	want := `{"type":"shell_exec_approval","command_sha256":"abc123"}`
	if string(pause.ResumeContext) != want {
		t.Fatalf("resume_context = %s, want %s", pause.ResumeContext, want)
	}
}

func TestAskUser_Spec_ProxiedFieldsNotRequired(t *testing.T) {
	s := AskUser{}.Spec()
	var schema struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(s.Parameters, &schema); err != nil {
		t.Fatalf("Parameters not valid JSON: %v", err)
	}
	for _, p := range []string{"proxied_from_child_id", "proxied_tool_call_id", "resume_context"} {
		if _, ok := schema.Properties[p]; !ok {
			t.Errorf("Spec params missing optional property %q", p)
		}
	}
	if len(schema.Required) != 2 || schema.Required[0] != "question" || schema.Required[1] != "kind" {
		t.Errorf("required must stay exactly [question, kind], got %v", schema.Required)
	}
}

func TestAskUser_Execute_BoundaryPriorities(t *testing.T) {
	for _, p := range []int{0, 100} {
		args := json.RawMessage(`{"question":"q","kind":"approval","priority":` + itoa(p) + `}`)
		_, err := AskUser{}.Execute(context.Background(), args)
		var pause *ErrAwaitingUserInput
		if !errors.As(err, &pause) {
			t.Fatalf("priority %d must be accepted, got %v", p, err)
		}
		if pause.Priority != p {
			t.Errorf("priority round-trip: want %d, got %d", p, pause.Priority)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
