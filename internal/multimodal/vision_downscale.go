package multimodal

import (
	"bytes"
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

// DownscaleForVision shrinks an image whose long edge exceeds 1024 px to that edge,
// re-encoded as JPEG at quality 85, so neither the CPU/4 GB-GPU OCR sidecar nor a chat
// model is handed a full-resolution photo. An image that does not decode, or already
// fits, comes back as raw with an empty MIME type: the caller keeps the original bytes
// and type.
func DownscaleForVision(raw []byte) (out []byte, mimeType string) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw, ""
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= visionMaxEdge && h <= visionMaxEdge {
		return raw, ""
	}
	nw, nh := visionMaxEdge, visionMaxEdge
	if w >= h {
		nh = h * visionMaxEdge / w
	} else {
		nw = w * visionMaxEdge / h
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: visionJPEGQuality}); err != nil {
		return raw, ""
	}
	return buf.Bytes(), "image/jpeg"
}
