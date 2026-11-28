package chatgpt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// read_file attaches a PDF through the same carrier as an image. A catalogue entry that lists
// the "file" modality declares no MIME type a PDF matches, and plan usage takes no native file
// input, so the PDF must arrive as its reference text, never as bytes or a refusal.
func TestToolPDFBecomesReferenceText(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[{"slug":"vision","visibility":"list","input_modalities":["image","file"]}]}`)
	})
	pdf := []byte("%PDF-1.7 sheet bytes")
	req := llm.Request{Model: "vision",
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "Read it"}, {Role: llm.RoleTool, ToolCallID: "call1", Content: "attached below"}},
		ToolMedia: map[string][]llm.ProjectedRequestPart{"call1": {{Type: "media", MIMEType: "application/pdf", Text: "/workspace/sheet.pdf", Bytes: pdf}}},
	}
	params, err := client.buildRequest(context.Background(), req, "oauth-test-token")
	if err != nil {
		t.Fatalf("a PDF tool part failed the request: %v", err)
	}
	wire, _ := json.Marshal(params)
	if strings.Contains(string(wire), base64.StdEncoding.EncodeToString(pdf)) || strings.Contains(string(wire), "input_file") ||
		!strings.Contains(string(wire), "/workspace/sheet.pdf") {
		t.Fatalf("wire %s, want the PDF's reference text only", wire)
	}
}
