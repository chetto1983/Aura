package tools

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/pdftext"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// An attached PDF rides every later request of the turn, and the token preflight
// (conversations.ValidateFinalRequestBudget) never sees tool media, so its cost is bounded
// here, in bytes and in pages. The figures are Claude's, the one route that reads a PDF:
// platform.claude.com/docs/en/build-with-claude/pdf-support, read 2026-11-28.
const (
	// maxPDFAttachBytes is the image attach cap, which bounds what every later request of the
	// turn re-sends; it is far under the page's "Maximum request size 128 MB". A PDF cannot be
	// downscaled the way an image is, so a larger one is not attached at all.
	maxPDFAttachBytes = maxAttachBytes
	// pdfWorstPageTokens is the page's worst case: text that "typically uses 1,500–3,000
	// tokens" plus an image of "at most 1,568 visual tokens".
	pdfWorstPageTokens = 3_000 + 1_568
	// pdfWindowTokens is the smallest working window the Claude route defaults to: plan
	// decision 5 takes a model's long-prompt threshold when its price has one (Haiku 5.5:
	// 100,000), else 200,000. read_file has no route config to read the live one from.
	pdfWindowTokens = 100_000
	// pdfWindowShare: one PDF takes at most a quarter of the window, so the system prompt, the
	// tools, the loaded history and the turn's own tool loop keep the other three quarters.
	pdfWindowShare = 4
	// maxPDFAttachPages is 25,000 / 4,568 = 5 pages.
	maxPDFAttachPages = pdfWindowTokens / pdfWindowShare / pdfWorstPageTokens
	// pdfInspectTimeout bounds pdfinfo on a pathological or hostile file.
	pdfInspectTimeout = 10 * time.Second
)

const pdfMIMEType = "application/pdf"

// inspectPDF is a variable so a test can answer for pdfinfo without a real PDF.
var inspectPDF = pdftext.Inspect

func isPDF(head []byte) bool { return http.DetectContentType(head) == pdfMIMEType }

// pdfWaysOut are the ways to a PDF's text that never depend on the model reading PDFs. The box
// carries no PDF toolchain, so its text comes out through a Python library installed there.
const pdfWaysOut = "shell_exec can extract its text (for example after `python3 -m pip install pypdf`), " +
	"send_file can hand it to the user"

// attachPDF hands the PDF at boxPath to the model through the turn's llm.ToolMedia, the way
// attachImage hands an image, and counts against the turn's MaxToolMediaPerTurn like one.
// Each provider decides whether the model reads it; a model that does not is told it is not
// shown, so the result names the ways out. A page count pdfinfo cannot read is a refusal.
func (t *ReadFile) attachPDF(
	ctx context.Context,
	h usersandbox.BoxHandle,
	media *llm.ToolMedia,
	boxPath string,
	head []byte,
	readCap int64,
) (string, error) {
	data, err := t.readPastTextCap(ctx, h, boxPath, head, readCap, maxPDFAttachBytes)
	if err != nil {
		return "", err
	}
	if len(data) > maxPDFAttachBytes {
		return "", fmt.Errorf("read_file: %s is a PDF over the %d-byte cap a document can ride the turn with; %s",
			boxPath, maxPDFAttachBytes, pdfWaysOut)
	}
	inspectCtx, cancel := context.WithTimeout(ctx, pdfInspectTimeout)
	info, err := inspectPDF(inspectCtx, data)
	cancel()
	switch {
	case err != nil:
		return "", fmt.Errorf("read_file: %s is not attached: its pages cannot be counted (%v); %s", boxPath, err, pdfWaysOut)
	case info.Encrypted:
		return "", fmt.Errorf("read_file: %s is an encrypted PDF, which no model reads; %s", boxPath, pdfWaysOut)
	case info.Pages > maxPDFAttachPages:
		return "", fmt.Errorf("read_file: %s is a PDF of %d pages, over the %d a document can ride the turn with; %s",
			boxPath, info.Pages, maxPDFAttachPages, pdfWaysOut)
	}
	part := llm.ProjectedRequestPart{Type: "media", MIMEType: pdfMIMEType, Text: boxPath, Bytes: data}
	if err := media.Add(ToolCallIDFromContext(ctx), part); err != nil {
		return "", fmt.Errorf("read_file: %s is not attached: %w", boxPath, err)
	}
	return fmt.Sprintf("%s is a PDF document (%d pages, %d bytes) and is attached below for you to read. "+
		"If it is not shown to you, %s.", boxPath, info.Pages, len(data), pdfWaysOut), nil
}
