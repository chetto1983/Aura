package tools

import (
	"context"
	"fmt"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// maxImageReadBytes caps an image read_file shows the model at the largest file an MCP tool
// lands in the box, so every image a tool delivered can be looked at.
const maxImageReadBytes = mcp.MaxFileBytes

// maxAttachBytes caps an image as it rides the turn: every later request of the turn re-sends
// it, a third larger again as base64.
const maxAttachBytes = 2 << 20

// visionImageTypes are the image formats chat vision models read; multimodal registers a
// decoder for each.
var visionImageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}

// visionImageType sniffs head's magic bytes and returns its MIME type when it is an image a
// chat model can look at, or "".
func visionImageType(head []byte) string {
	if mimeType := http.DetectContentType(head); visionImageTypes[mimeType] {
		return mimeType
	}
	return ""
}

// attachImage hands the image at boxPath to the model through the turn's llm.ToolMedia and
// returns read_file's text result; the bytes themselves never enter the result. head is the
// read bounded at readCap, and an image longer than that is read again up to its own cap.
// Outside a chat turn there is no model to show it to, so it is refused like any binary.
func (t *ReadFile) attachImage(
	ctx context.Context,
	h usersandbox.BoxHandle,
	boxPath, mimeType string,
	head []byte,
	readCap int64,
) (string, error) {
	media := llm.ToolMediaFromContext(ctx)
	if media == nil {
		return "", binaryFileRefusal(boxPath)
	}
	data := head
	if int64(len(head)) > readCap {
		var deny *ToolResult
		var err error
		data, deny, err = boxReadHead(ctx, t.Router, h, "read_file", boxPath, maxImageReadBytes)
		if deny != nil {
			return "", &sandboxDenyError{result: *deny}
		}
		if err != nil {
			return "", err
		}
	}
	if len(data) > maxImageReadBytes {
		return "", fmt.Errorf("read_file: %s is over the %d-byte image cap; send_file can still hand it to the user",
			boxPath, maxImageReadBytes)
	}
	// Anything short of a full decode is refused: a broken image would fail every later
	// request of the turn, which all re-send it.
	shown, err := multimodal.DownscaleForVision(ctx, data, maxAttachBytes)
	if err != nil {
		// %v: a decoder's EOF must not read as a transient failure the agent retries.
		return "", fmt.Errorf("read_file: %s cannot be shown: %v; shell_exec can convert or shrink it, "+
			"send_file can hand it to the user", boxPath, err)
	}
	shownType := shown.MIMEType
	if shownType == "" {
		shownType = mimeType
	}
	part := llm.ProjectedRequestPart{Type: "media", MIMEType: shownType, Text: boxPath, Bytes: shown.Bytes}
	if err := media.Add(ToolCallIDFromContext(ctx), part); err != nil {
		return "", fmt.Errorf("read_file: %s is not attached: %w", boxPath, err)
	}
	return fmt.Sprintf("%s is an image (%s, %dx%d) and is attached below for you to look at.",
		boxPath, mimeType, shown.Width, shown.Height), nil
}
