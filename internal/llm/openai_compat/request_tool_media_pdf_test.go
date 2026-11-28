package openai_compat

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// read_file attaches a PDF through the same carrier as an image. No backend here takes a
// document part, and an OpenRouter card that lists the "file" modality still declares no
// MIME type a PDF matches, so the PDF must arrive as the not-shown note, never as bytes.
func TestToolPDFBecomesReferenceText(t *testing.T) {
	pdf := []byte("%PDF-1.7 sheet bytes")
	fileCaps := staticContentCaps{caps: llm.ProviderContentCapabilities{Modalities: map[string]bool{"text": true, "image": true, "file": true}}, detected: true}
	for name, caps := range map[string]llm.ContentCapabilitySource{"vision and file model": fileCaps, "vision model": visionCaps} {
		t.Run(name, func(t *testing.T) {
			raw := captureMessages(t, caps, llm.Request{Model: "vision", Messages: toolRound(), ToolMedia: map[string][]llm.ProjectedRequestPart{
				"c1": {{Type: "media", MIMEType: "application/pdf", Text: "/workspace/photo.png", Bytes: pdf}},
			}})
			if strings.Contains(string(raw), base64.StdEncoding.EncodeToString(pdf)) || strings.Contains(string(raw), "application/pdf") {
				t.Fatalf("the PDF reached the wire: %s", raw)
			}
			messages := decodeMessages(t, raw)
			if len(messages) != 9 || messages[5].Role != llm.RoleUser {
				t.Fatalf("messages = %s, want a user note after the tool block", raw)
			}
			var note string
			if err := json.Unmarshal(messages[5].Content, &note); err != nil || !strings.Contains(note, "could not be shown") {
				t.Fatalf("note = %s, want the not-shown note", messages[5].Content)
			}
			assertDisownedThenQuoted(t, note)
		})
	}
}
