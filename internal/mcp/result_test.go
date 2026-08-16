package mcp

import (
	"errors"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDecodeToolResult_ConcatenatesTextParts(t *testing.T) {
	result := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: "hello\n"},
			&sdkmcp.TextContent{Text: "world\n"},
		},
	}
	text, isError := DecodeToolResult(result)
	if text != "hello\nworld" {
		t.Fatalf("text = %q, want concatenated+right-trimmed", text)
	}
	if isError {
		t.Fatal("isError = true, want false")
	}
}

func TestDecodeToolResult_SkipsNonTextContent(t *testing.T) {
	result := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: "kept"},
			&sdkmcp.ImageContent{Data: []byte("binary"), MIMEType: "image/png"},
		},
	}
	text, _ := DecodeToolResult(result)
	if text != "kept" {
		t.Fatalf("text = %q, want non-text content skipped, not stringified", text)
	}
}

func TestDecodeToolResult_IsErrorTruePropagates(t *testing.T) {
	result := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "boom"}},
		IsError: true,
	}
	_, isError := DecodeToolResult(result)
	if !isError {
		t.Fatal("IsError:true on the wire must propagate as isError=true")
	}
}

func TestDecodeToolResult_StructuredContentExplicitFailureEscalates(t *testing.T) {
	result := &sdkmcp.CallToolResult{
		Content:           []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}},
		StructuredContent: map[string]any{"success": false},
		IsError:           false,
	}
	_, isError := DecodeToolResult(result)
	if !isError {
		t.Fatal("StructuredContent success:false must escalate isError to true even though IsError=false on the wire")
	}
}

func TestDecodeToolResult_TextJSONExplicitFailureEscalates(t *testing.T) {
	result := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: `{"success": false, "reason": "not found"}`}},
		IsError: false,
	}
	_, isError := DecodeToolResult(result)
	if !isError {
		t.Fatal("a text body that is itself JSON {success:false} must escalate isError to true")
	}
}

func TestDecodeToolResult_NilResultIsSafe(t *testing.T) {
	text, isError := DecodeToolResult(nil)
	if text != "" || isError {
		t.Fatalf("DecodeToolResult(nil) = (%q, %v), want (\"\", false)", text, isError)
	}
}

// TestDecodeToolCallError_WhatsAppDictModelTranslation pins the exact WhatsApp
// FastMCP/Pydantic translation the pre-SDK chain produced, now reached through the
// exported DecodeToolCallError.
func TestDecodeToolCallError_WhatsAppDictModelTranslation(t *testing.T) {
	raw := "1 validation error for DictModel\nchat\n  Input should be a valid dictionary [type=dict_type, input_value=None, input_type=NoneType]"
	err := DecodeToolCallError("whatsapp", "get_chat", raw)
	if err.Outcome != ToolOutcomeRejected {
		t.Fatalf("Outcome = %q, want rejected", err.Outcome)
	}
	if err.Code != "not_found" {
		t.Fatalf("Code = %q, want not_found", err.Code)
	}
	if err.Effect != ToolEffectNone {
		t.Fatalf("Effect = %q, want none", err.Effect)
	}
	if err.Message != "WhatsApp chat not found" {
		t.Fatalf("Message = %q, want WhatsApp chat not found", err.Message)
	}
}

// TestDecodeToolCallError_MatchesToolCallErrorType pins the exact assertion
// llm_agent_retry.go:181 makes: `var toolCallErr *mcp.ToolCallError;
// errors.As(err, &toolCallErr)` — HARN-01..09's retry/idempotency-effect
// decisions depend on this still matching post-swap.
func TestDecodeToolCallError_MatchesToolCallErrorType(t *testing.T) {
	err := error(DecodeToolCallError("memory", "memory_upsert_fact", "boom"))
	var toolCallErr *ToolCallError
	if !errors.As(err, &toolCallErr) {
		t.Fatal("errors.As(err, &toolCallErr) must match *mcp.ToolCallError")
	}
}
