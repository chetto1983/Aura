package telegram

import (
	"bytes"
	"image"
	"image/jpeg"
	_ "image/png" // register the PNG decoder for image.Decode

	xdraw "golang.org/x/image/draw"
)

// visionMaxEdge caps the longest edge of an image before the vision sidecar sees
// it. CPU OCR latency scales with pixel count: spike-026 measured ~3.3s p95 on a
// small probe image, but a full-resolution phone photo took ~55s live and blew the
// request timeout. A vision model needs far less than a phone's native resolution,
// so downscaling restores spike-range latency with no meaningful recognition loss.
const visionMaxEdge = 1024

// visionJPEGQuality re-encodes the downscaled image (a good size/clarity tradeoff
// for OCR).
const visionJPEGQuality = 85

// downscaleForVision shrinks an oversized image so the vision sidecar stays fast.
// It returns the re-encoded bytes and a non-empty mime ONLY when it actually
// downscaled; on any decode/encode failure or an already-small image it returns
// the original bytes and "" so the caller keeps the original mime. It never errors
// — a vision call on the original bytes beats no call.
func downscaleForVision(raw []byte) (out []byte, mime string) {
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
