package tools

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// maxImageReadBytes caps an image read_file shows the model at the largest file an MCP tool
// lands in the box, so every image a tool delivered can be looked at.
const maxImageReadBytes = mcp.MaxFileBytes

// maxImagePixels bounds one decode, which the byte cap cannot: a flat PNG declaring millions of
// pixels compresses to almost nothing. At 40 MP the decoded source (up to 4 bytes a pixel) plus
// x/image/draw's dw*sh*32-byte kernel buffer (scale.go) stay near a third of the aura
// container's 768 MiB.
const maxImagePixels = 40_000_000

// visionDecodeSlot lets one full-resolution decode run at a time. Tool batches run in parallel,
// and a single 12 MP photo already holds ~116 MB while it is downscaled.
var visionDecodeSlot = make(chan struct{}, 1)

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
	// A corrupt image would fail every later request of the turn, which all re-send it.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("read_file: %s looks like %s but does not decode as an image: %w", boxPath, mimeType, err)
	}
	if cfg.Width*cfg.Height > maxImagePixels {
		return "", fmt.Errorf("read_file: %s is %dx%d, over the %d-pixel image cap; shrink it with shell_exec first",
			boxPath, cfg.Width, cfg.Height, maxImagePixels)
	}
	shown, shownType, err := downscaleOneAtATime(ctx, data)
	if err != nil {
		return "", err
	}
	if shownType == "" {
		shownType = mimeType
	}
	part := llm.ProjectedRequestPart{Type: "media", MIMEType: shownType, Text: boxPath, Bytes: shown}
	if err := media.Add(ToolCallIDFromContext(ctx), part); err != nil {
		return "", fmt.Errorf("read_file: %s is not attached: %w", boxPath, err)
	}
	return fmt.Sprintf("%s is an image (%s, %dx%d) and is attached below for you to look at.",
		boxPath, mimeType, cfg.Width, cfg.Height), nil
}

// downscaleOneAtATime runs multimodal.DownscaleForVision in visionDecodeSlot, released even if
// a malformed image panics the decoder.
func downscaleOneAtATime(ctx context.Context, data []byte) ([]byte, string, error) {
	select {
	case visionDecodeSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	defer func() { <-visionDecodeSlot }()
	shown, mimeType := multimodal.DownscaleForVision(data)
	return shown, mimeType, nil
}
