package multimodal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // the four image types vision models read must all decode here
	"image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	visionMaxEdge     = 1024
	visionJPEGQuality = 85
)

// maxVisionPixels bounds a decode before it starts. Go's image package asks for exactly this
// with untrusted input — call DecodeConfig and check the size before Decode (package image,
// "Security Considerations") — because a small file can declare a huge canvas: a flat PNG, a
// GIF's logical screen, a WebP VP8X canvas. Worked out from the decoders and x/image/draw,
// not measured: at 40 MP the decoded source is 40 MB (paletted GIF), 60 MB (4:2:0 JPEG),
// 160 MB (8-bit RGBA) or 320 MB (16-bit PNG, 8 bytes a pixel), and the kernel buffer
// (dw*sh*32 bytes, draw/scale.go) of a square one is ~207 MB: up to ~527 MB of the aura
// container's 768 MiB.
const maxVisionPixels = 40_000_000

// decodeSlot lets one full decode run at a time in the whole process — uploads, the media
// indexer and read_file all come through here. Downscaling a 12 MP photo alone holds ~116 MB
// (worked out as above), and tool batches run in parallel.
var decodeSlot = make(chan struct{}, 1)

// The ways DownscaleForVision refuses an image.
var (
	ErrImageUndecodable   = errors.New("image does not decode")
	ErrImageTooManyPixels = errors.New("image is over the pixel cap")
	ErrImageOverByteCap   = errors.New("image is over the byte cap")
)

// VisionImage is an image prepared for a vision model. MIMEType is empty when Bytes is the
// input, unchanged; Width and Height are always the input's own size.
type VisionImage struct {
	Bytes         []byte
	MIMEType      string
	Width, Height int
}

// DownscaleForVision prepares an image for a vision model. One whose long edge exceeds
// 1024 px is shrunk to that edge and re-encoded as JPEG at quality 85, so neither the
// CPU/4 GB-GPU OCR sidecar nor a chat model is handed a full-resolution photo. One that
// already fits comes back unchanged, unless it is larger than maxBytes (0: no cap); then it
// is re-encoded the same way at its own size, and refused if that is still too large.
//
// Every image is decoded in full, so a body that is truncated or that the decoder cannot
// read (an animated WebP) fails here rather than reaching a model. On any error a caller
// that can fall back keeps its original bytes.
func DownscaleForVision(ctx context.Context, raw []byte, maxBytes int) (VisionImage, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		// %v, not %w: a truncated header is io.ErrUnexpectedEOF, which the agent retries as
		// a transient failure.
		return VisionImage{}, fmt.Errorf("%w: %v", ErrImageUndecodable, err)
	}
	if cfg.Width*cfg.Height > maxVisionPixels {
		return VisionImage{}, fmt.Errorf("%w: %dx%d is over %d pixels", ErrImageTooManyPixels, cfg.Width, cfg.Height, maxVisionPixels)
	}
	select {
	case decodeSlot <- struct{}{}:
	case <-ctx.Done():
		return VisionImage{}, ctx.Err()
	}
	defer func() { <-decodeSlot }()
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return VisionImage{}, fmt.Errorf("%w: %v", ErrImageUndecodable, err)
	}
	prepared := VisionImage{Bytes: raw, Width: cfg.Width, Height: cfg.Height}
	b := img.Bounds()
	fits := b.Dx() <= visionMaxEdge && b.Dy() <= visionMaxEdge
	if fits && (maxBytes <= 0 || len(raw) <= maxBytes) {
		return prepared, nil
	}
	if !fits {
		img = scaleToMaxEdge(img)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: visionJPEGQuality}); err != nil {
		return VisionImage{}, fmt.Errorf("encode vision JPEG: %w", err)
	}
	if maxBytes > 0 && buf.Len() > maxBytes {
		return VisionImage{}, fmt.Errorf("%w: %d bytes as JPEG, over %d", ErrImageOverByteCap, buf.Len(), maxBytes)
	}
	prepared.Bytes, prepared.MIMEType = buf.Bytes(), "image/jpeg"
	return prepared, nil
}

// scaleToMaxEdge shrinks img so its long edge is visionMaxEdge, keeping the aspect ratio.
func scaleToMaxEdge(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := visionMaxEdge, visionMaxEdge
	if w >= h {
		nh = h * visionMaxEdge / w
	} else {
		nw = w * visionMaxEdge / h
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	return dst
}
