package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxToolCallErrorMessageBytes = 2048

// ToolOutcome is how a tool call ended, in the stable MCP domain-error vocabulary.
type ToolOutcome string

// ToolEffect is how much of the mutation actually landed, in the same vocabulary: a
// rejected call and a half-applied one need different recovery from the caller.
type ToolEffect string

// The MCP domain-error vocabulary. Stable strings: they cross the MCP boundary.
const (
	ToolOutcomeRejected ToolOutcome = "rejected"
	ToolOutcomePartial  ToolOutcome = "partial"
	ToolOutcomeError    ToolOutcome = "error"

	ToolEffectNone    ToolEffect = "none"
	ToolEffectPartial ToolEffect = "partial"
	ToolEffectUnknown ToolEffect = "unknown"
)

// ToolCallError preserves MCP isError=true as a typed domain failure.
type ToolCallError struct {
	Server  string      `json:"-"`
	Tool    string      `json:"-"`
	Outcome ToolOutcome `json:"outcome"`
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Effect  ToolEffect  `json:"effect"`
}

func (e *ToolCallError) Error() string {
	if e == nil {
		return "MCP tool reported an error"
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "tool reported an error"
	}
	return fmt.Sprintf("mcp %q: tool %s %s: %s", e.Server, e.Tool, e.Outcome, message)
}

// DeterministicNoEffect reports that retry bookkeeping may record a rejection,
// rather than conservatively treating the operation as indeterminate.
func (e *ToolCallError) DeterministicNoEffect() bool {
	return e != nil && e.Effect == ToolEffectNone
}

// DecodeToolCallError translates a tools/call isError=true text body into the
// stable ToolCallError vocabulary. Exported for bridge_supervisor.go's
// CallToolText — the SDK-era result-decode chain's only call site
// (RESEARCH Pitfall 1); logic is UNCHANGED from the pre-SDK decodeToolCallError.
func DecodeToolCallError(serverName, toolName, text string) *ToolCallError {
	toolErr := &ToolCallError{
		Server: serverName, Tool: toolName,
		Outcome: ToolOutcomeError, Code: "mcp_tool_error",
		Message: boundedToolCallErrorText(text), Effect: ToolEffectUnknown,
	}
	if message, ok := whatsappMissingChatMessage(serverName, toolName, text); ok {
		toolErr.Outcome = ToolOutcomeRejected
		toolErr.Code = "not_found"
		toolErr.Message = message
		toolErr.Effect = ToolEffectNone
		return toolErr
	}
	var envelope struct {
		Outcome ToolOutcome `json:"outcome"`
		Code    string      `json:"code"`
		Message string      `json:"message"`
		Effect  ToolEffect  `json:"effect"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return toolErr
	}
	switch envelope.Outcome {
	case ToolOutcomeRejected, ToolOutcomePartial, ToolOutcomeError:
	default:
		return toolErr
	}
	switch envelope.Effect {
	case ToolEffectNone, ToolEffectPartial, ToolEffectUnknown:
	default:
		return toolErr
	}
	toolErr.Outcome = envelope.Outcome
	toolErr.Effect = envelope.Effect
	toolErr.Code = boundedToolCallErrorText(envelope.Code)
	toolErr.Message = boundedToolCallErrorText(envelope.Message)
	return toolErr
}

// whatsappMissingChatMessage translates the sidecar's return-annotation validation failure.
// Both tools return None for a legitimate miss despite advertising dict[str, Any], so FastMCP
// exposes a Pydantic implementation detail instead of the domain outcome.
func whatsappMissingChatMessage(serverName, toolName, text string) (string, bool) {
	if serverName != "whatsapp" ||
		!strings.Contains(text, "validation error for DictModel") ||
		!strings.Contains(text, "input_value=None") {
		return "", false
	}
	switch toolName {
	case "get_chat":
		return "WhatsApp chat not found", true
	case "get_direct_chat_by_contact":
		return "WhatsApp direct chat not found", true
	default:
		return "", false
	}
}

func boundedToolCallErrorText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxToolCallErrorMessageBytes {
		return value
	}
	value = value[:maxToolCallErrorMessageBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
