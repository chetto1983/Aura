package tools

import (
	"context"
	"fmt"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// maxPDFAttachBytes caps a PDF as it rides the turn. It is the tighter of the image attach
// cap, which bounds what every later request of the turn re-sends, and Claude's 128 MB request
// limit (platform.claude.com/docs/en/build-with-claude/pdf-support): a PDF cannot be
// downscaled the way an image is, so a larger one is not attached at all.
const maxPDFAttachBytes = maxAttachBytes

const pdfMIMEType = "application/pdf"

func isPDF(head []byte) bool { return http.DetectContentType(head) == pdfMIMEType }

// attachPDF hands the PDF at boxPath to the model through the turn's llm.ToolMedia, the way
// attachImage hands an image. Each provider decides whether the model reads it: a model that
// does not is told it is not shown, so the result names the ways out that always work. It
// counts against the turn's MaxToolMediaPerTurn like an image.
func (t *ReadFile) attachPDF(
	ctx context.Context,
	h usersandbox.BoxHandle,
	media *llm.ToolMedia,
	boxPath string,
	head []byte,
	readCap int64,
) (string, error) {
	data := head
	if int64(len(head)) > readCap {
		var deny *ToolResult
		var err error
		data, deny, err = boxReadHead(ctx, t.Router, h, "read_file", boxPath, maxPDFAttachBytes)
		if deny != nil {
			return "", &sandboxDenyError{result: *deny}
		}
		if err != nil {
			return "", err
		}
	}
	if len(data) > maxPDFAttachBytes {
		return "", fmt.Errorf("read_file: %s is a PDF over the %d-byte cap a document can ride the turn with; "+
			"shell_exec can extract its text (for example with pdftotext), send_file can hand it to the user",
			boxPath, maxPDFAttachBytes)
	}
	part := llm.ProjectedRequestPart{Type: "media", MIMEType: pdfMIMEType, Text: boxPath, Bytes: data}
	if err := media.Add(ToolCallIDFromContext(ctx), part); err != nil {
		return "", fmt.Errorf("read_file: %s is not attached: %w", boxPath, err)
	}
	return fmt.Sprintf("%s is a PDF document (%d bytes) and is attached below for you to read. If it is not "+
		"shown to you, shell_exec can extract its text (for example with pdftotext) and send_file can hand it to the user.",
		boxPath, len(data)), nil
}
