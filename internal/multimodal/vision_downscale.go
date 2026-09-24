package multimodal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
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

// The pixel bounds stop a decode before it starts. Go's image package asks for exactly this
// with untrusted input — DecodeConfig, check the size, then Decode (package image, "Security
// Considerations") — because a small file can declare a huge canvas. The bound depends on the
// color model DecodeConfig reports, because that decides what the decode holds.
//
// Measured 2026-09-24 in WSL (go1.27.1 linux/amd64, GOGC=100 and no GOMEMLIMIT: the aura
// container sets neither, under its 768 MiB mem_limit). Method: VmHWM of one DownscaleForVision
// call, each image in a fresh process, minus that of an empty test process; Pillow images of
// gradients plus Gaussian noise. MiB added:
//
//	family  worst case (its DecodeConfig model)  4032x3024  7680x5200  bound, and its square
//	YCbCr   progressive 4:4:4 JPEG               279.7      756.2      3515² = 12.36 MP: 298.6
//	CMYK    progressive CMYK JPEG                389.5      1115.9     2910² =  8.47 MP: 298.9
//	gray    progressive gray JPEG                160.6      366.9      4772² = 22.77 MP: 269.6
//	other   progressive RGB JPEG (RGBA)          331.2      924.9      3180² = 10.11 MP: 298.1
//
// Also measured: baseline 4:2:0 JPEG 120.4 / 235.1, 8-bit RGBA PNG 179.3 / 427.2, 16-bit RGBA
// PNG 272.7 / 733.8. Progressive JPEG is each family's worst because Go keeps every coefficient,
// 4 bytes a sample, until the last scan. A square is the worst shape, because x/image/draw's
// buffer is dw*sh*32 bytes (draw/scale.go) and grows with the short edge. Each bound is 300 MiB
// over the family's worst measured bytes per pixel, taken down to a square measured within it.
//
// Not measured, only worked out to sit under its family's worst: 16-bit gray PNG (in "other"),
// lossy and VP8X WebP, interlaced PNG. The figures are what one decode adds to an empty ~19 MiB
// process; aura adds them to its own heap (VM .158, 2026-09-24: two 12 MP photos read in one
// turn took it from 126 MiB to a 452 MiB peak).
const (
	maxPixelsYCbCr = 3515 * 3515
	maxPixelsCMYK  = 2910 * 2910
	maxPixelsGray  = 4772 * 4772
	maxPixelsOther = 3180 * 3180
)

// maxVisionPixels is the pixel bound for an image whose DecodeConfig reports model.
func maxVisionPixels(model color.Model) int {
	switch model {
	case color.YCbCrModel, color.NYCbCrAModel:
		return maxPixelsYCbCr
	case color.CMYKModel:
		return maxPixelsCMYK
	case color.GrayModel:
		return maxPixelsGray
	default:
		return maxPixelsOther
	}
}

// decodeSlot lets one full decode run at a time in this process: chat uploads and read_file
// share it. aura-media-index runs as its own process, in the aura-ingest container, with its
// own. A decode at the YCbCr bound alone adds ~300 MiB (measured above), and tool batches run
// in parallel.
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
// CPU/4 GB-GPU OCR sidecar nor a chat model is handed a full-resolution photo. A JPEG or PNG
// that already fits comes back unchanged, unless it is larger than maxBytes (0: no cap).
// Anything else that fits — a GIF, a WebP, or an image over maxBytes — is re-encoded the same
// way at its own size: a GIF may be animated, and not every vision backend decodes WebP
// (llama.cpp's stb_image does not). Either JPEG is refused if it is still over maxBytes.
//
// Every image is decoded — a GIF to its first frame, all Go's decoder reads — so a body that
// is truncated or that the decoder cannot read (an animated WebP) fails here rather than
// reaching a model. A context that has ended fails with its own error, wrapped, and never
// decodes. On any error a caller that can fall back keeps its original bytes.
func DownscaleForVision(ctx context.Context, raw []byte, maxBytes int) (VisionImage, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		// %v, not %w: a truncated header is io.ErrUnexpectedEOF, which the agent retries as
		// a transient failure.
		return VisionImage{}, fmt.Errorf("%w: %v", ErrImageUndecodable, err)
	}
	if limit := maxVisionPixels(cfg.ColorModel); cfg.Width*cfg.Height > limit {
		return VisionImage{}, fmt.Errorf("%w: %dx%d is over %d pixels", ErrImageTooManyPixels, cfg.Width, cfg.Height, limit)
	}
	// Checked before the select too: with the slot free and ctx done, select picks at random.
	if err := ctx.Err(); err != nil {
		return VisionImage{}, fmt.Errorf("before decoding: %w", err)
	}
	select {
	case decodeSlot <- struct{}{}:
	case <-ctx.Done():
		return VisionImage{}, fmt.Errorf("wait for the decode slot: %w", ctx.Err())
	}
	defer func() { <-decodeSlot }()
	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return VisionImage{}, fmt.Errorf("%w: %v", ErrImageUndecodable, err)
	}
	prepared := VisionImage{Bytes: raw, Width: cfg.Width, Height: cfg.Height}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	fits := w <= visionMaxEdge && h <= visionMaxEdge
	passThrough := format == "jpeg" || format == "png"
	if fits && passThrough && (maxBytes <= 0 || len(raw) <= maxBytes) {
		return prepared, nil
	}
	nw, nh := w, h
	if !fits {
		nw, nh = longEdgeTo(w, h, visionMaxEdge)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flattenOnWhite(img, nw, nh), &jpeg.Options{Quality: visionJPEGQuality}); err != nil {
		return VisionImage{}, fmt.Errorf("encode vision JPEG: %w", err)
	}
	if maxBytes > 0 && buf.Len() > maxBytes {
		return VisionImage{}, fmt.Errorf("%w: %d bytes as JPEG, over %d", ErrImageOverByteCap, buf.Len(), maxBytes)
	}
	prepared.Bytes, prepared.MIMEType = buf.Bytes(), "image/jpeg"
	return prepared, nil
}

// longEdgeTo scales w x h so its long edge is edge, keeping the aspect ratio. The short edge
// never rounds to zero: a 2000x1 divider would otherwise encode as a JPEG with no rows.
func longEdgeTo(w, h, edge int) (int, int) {
	if w >= h {
		return edge, max(1, h*edge/w)
	}
	return max(1, w*edge/h), edge
}

// flattenOnWhite draws img onto an opaque white w x h canvas, scaling it when the size
// differs. JPEG has no alpha and jpeg.Encode drops it (image/jpeg writer.go, toYCbCr), so
// transparency would come out black and dark content drawn on it would vanish.
func flattenOnWhite(img image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.Draw(dst, dst.Bounds(), image.White, image.Point{}, xdraw.Src)
	b := img.Bounds()
	if b.Dx() == w && b.Dy() == h {
		xdraw.Draw(dst, dst.Bounds(), img, b.Min, xdraw.Over)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	}
	return dst
}
