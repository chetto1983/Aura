package webadapter

import (
	"encoding/json"
	"fmt"
	"io"
)

// UIMessageStreamHeader is the AI SDK UI opt-in header for custom backends.
const UIMessageStreamHeader = "x-vercel-ai-ui-message-stream"

// UIMessageStreamHeaderValue is the current AI SDK UI Message Stream version.
const UIMessageStreamHeaderValue = "v1"

func writeUIMessageChunk(w io.Writer, chunk any) error {
	raw, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("web stream: encode chunk: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
		return fmt.Errorf("web stream: write chunk: %w", err)
	}
	return nil
}

func writeUIMessageDone(w io.Writer) error {
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return fmt.Errorf("web stream: write done: %w", err)
	}
	return nil
}

// WriteUIMessageError emits the UI Message Stream error part. It is exported so
// the API bridge can terminate setup failures after SSE headers have been sent.
func WriteUIMessageError(w io.Writer, message string) error {
	return writeUIMessageChunk(w, map[string]any{
		"type":      "error",
		"errorText": message,
	})
}

// WriteUIMessageDone emits the literal stream terminator expected by AI SDK UI.
func WriteUIMessageDone(w io.Writer) error {
	return writeUIMessageDone(w)
}
