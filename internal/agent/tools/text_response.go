package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// TextResponse is the canonical terminal tool — the LLM uses it to emit the
// final user-visible reply for the current turn. Non-deferred, single string
// argument. Calling it signals "I am done thinking, here is the answer."
type TextResponse struct{}

type textResponseArgs struct {
	Text string `json:"text"`
}

func (TextResponse) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "text": {"type": "string", "description": "Final reply text to deliver to the user."}
  },
  "required": ["text"]
}`)
	return Spec{
		Name:        "text_response",
		Summary:     "Emit the final user-visible reply and end the current turn.",
		Description: "Use this once you have gathered enough information and are ready to answer the user. The string passed becomes the assistant message of record. Calling this terminates the agent loop for the current turn.",
		Parameters:  params,
		Deferred:    false,
	}
}

func (TextResponse) Execute(_ context.Context, raw json.RawMessage) (ToolResult, error) {
	var a textResponseArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("text_response args: %w", err)
	}
	if a.Text == "" {
		return ToolResult{}, fmt.Errorf("text_response: text is required")
	}
	// Terminal channel: the text is the assistant message of record and is small
	// by construction, so it never spills to a sidecar.
	return ToolResult{Preview: a.Text, Bytes: len(a.Text)}, nil
}
