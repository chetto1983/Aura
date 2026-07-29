package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeToolCallErrorPreservesTypedOutcome(t *testing.T) {
	t.Parallel()

	text := `{"outcome":"rejected","code":"invalid_argument","message":"subject is required","effect":"none"}`
	err := decodeToolCallError("memory", "memory_add_fact", text)

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

	err := decodeToolCallError(
		"legacy",
		"write",
		strings.Repeat("x", maxToolCallErrorMessageBytes+100),
	)
	if err.Outcome != ToolOutcomeError || err.Effect != ToolEffectUnknown ||
		err.DeterministicNoEffect() || len(err.Message) > maxToolCallErrorMessageBytes {
		t.Fatalf("legacy error = %+v", err)
	}
}
