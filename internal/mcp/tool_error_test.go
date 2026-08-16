package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeToolCallErrorPreservesTypedOutcome(t *testing.T) {
	t.Parallel()

	text := `{"outcome":"rejected","code":"invalid_argument","message":"subject is required","effect":"none"}`
	err := DecodeToolCallError("memory", "memory_upsert_fact", text)

	if err.Outcome != ToolOutcomeRejected || err.Code != "invalid_argument" ||
		err.Message != "subject is required" || !err.DeterministicNoEffect() {
		t.Fatalf("decoded error = %+v", err)
	}
	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil || !strings.Contains(string(encoded), `"effect":"none"`) {
		t.Fatalf("marshal = %s / %v", encoded, marshalErr)
	}
}

func TestDecodeToolCallErrorFailsClosedForLegacyText(t *testing.T) {
	t.Parallel()

	err := DecodeToolCallError(
		"legacy",
		"write",
		strings.Repeat("x", maxToolCallErrorMessageBytes+100),
	)
	if err.Outcome != ToolOutcomeError || err.Effect != ToolEffectUnknown ||
		err.DeterministicNoEffect() || len(err.Message) > maxToolCallErrorMessageBytes {
		t.Fatalf("legacy error = %+v", err)
	}
}

func TestDecodeToolCallErrorNormalizesWhatsAppMissingChat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool string
		want string
	}{
		{tool: "get_chat", want: "WhatsApp chat not found"},
		{tool: "get_direct_chat_by_contact", want: "WhatsApp direct chat not found"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()
			raw := "Error executing tool " + tc.tool + ": 1 validation error for DictModel\n" +
				"  Input should be a valid dictionary [type=dict_type, input_value=None, input_type=NoneType]\n" +
				"    For further information visit https://errors.pydantic.dev/2.11/v/dict_type"

			err := DecodeToolCallError("whatsapp", tc.tool, raw)
			if err.Outcome != ToolOutcomeRejected || err.Code != "not_found" ||
				err.Message != tc.want || err.Effect != ToolEffectNone {
				t.Fatalf("decoded error = %+v", err)
			}
		})
	}
}

func TestDecodeToolCallErrorDoesNotNormalizeUnrelatedDictModelError(t *testing.T) {
	t.Parallel()

	const raw = "1 validation error for DictModel: input_value=None"
	err := DecodeToolCallError("calendar", "get_chat", raw)
	if err.Message != raw || err.Code != "mcp_tool_error" {
		t.Fatalf("unrelated error was normalized: %+v", err)
	}
}

// TestToolCallErrorImplementsError closes a coverage gap the profile named
// directly: (*ToolCallError).Error() had no test anywhere in the tree — every
// caller checks the typed fields (Outcome/Code/Message/Effect) and never formats
// the value through the error interface. It IS still an error (bridge_supervisor.go
// returns it as one), so its message shape is load-bearing for logs.
func TestToolCallErrorImplementsError(t *testing.T) {
	t.Parallel()

	var nilErr *ToolCallError
	if got, want := nilErr.Error(), "MCP tool reported an error"; got != want {
		t.Errorf("nil *ToolCallError.Error() = %q, want %q", got, want)
	}

	populated := &ToolCallError{Server: "memory", Tool: "memory_upsert_fact", Outcome: ToolOutcomeRejected, Message: "subject is required"}
	if got, want := populated.Error(), `mcp "memory": tool memory_upsert_fact rejected: subject is required`; got != want {
		t.Errorf("populated.Error() = %q, want %q", got, want)
	}

	blank := &ToolCallError{Server: "whatsapp", Tool: "send_message", Outcome: ToolOutcomeError, Message: "   "}
	if got, want := blank.Error(), `mcp "whatsapp": tool send_message error: tool reported an error`; got != want {
		t.Errorf("blank-message.Error() = %q, want %q (fallback text)", got, want)
	}

	// A *ToolCallError must satisfy the error interface — asserted structurally, not
	// merely by calling .Error() directly, since that is what every real caller does
	// (errors.As, fmt %w/%v).
	var asErr error = populated
	if asErr.Error() == "" {
		t.Fatal("*ToolCallError does not behave as a non-empty error through the interface")
	}
}
