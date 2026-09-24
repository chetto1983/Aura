package multimodal

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, 0, color.RGBA{R: uint8(x), A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodedSize(t *testing.T, raw []byte) (format string, w, h int) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return format, cfg.Width, cfg.Height
}

func TestDownscaleForVisionShrinksTheLongEdgeTo1024AsJPEG(t *testing.T) {
	for name, tc := range map[string]struct{ w, h, wantW, wantH int }{
		"landscape": {2048, 1024, 1024, 512},
		"portrait":  {600, 3000, 204, 1024},
	} {
		t.Run(name, func(t *testing.T) {
			out, mimeType := DownscaleForVision(pngOf(t, tc.w, tc.h))
			if mimeType != "image/jpeg" {
				t.Fatalf("mime = %q, want image/jpeg", mimeType)
			}
			format, w, h := decodedSize(t, out)
			if format != "jpeg" || w != tc.wantW || h != tc.wantH {
				t.Fatalf("out = %s %dx%d, want jpeg %dx%d", format, w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestDownscaleForVisionKeepsWhatAlreadyFitsOrDoesNotDecode(t *testing.T) {
	for name, raw := range map[string][]byte{
		"small":           pngOf(t, 800, 600),
		"exactly 1024":    pngOf(t, 1024, 1024),
		"not an image":    []byte("%PDF-1.7 not an image"),
		"truncated image": pngOf(t, 2048, 1024)[:40],
	} {
		t.Run(name, func(t *testing.T) {
			out, mimeType := DownscaleForVision(raw)
			if mimeType != "" || !bytes.Equal(out, raw) {
				t.Fatalf("got (%d bytes, %q), want the original bytes and no MIME type", len(out), mimeType)
			}
		})
	}
}

// read_file shows jpeg, png, gif and webp to the model; each must decode here, or a large one
// would reach the model at full resolution and its size could not be reported.
func TestDownscaleForVisionDecodesEveryTypeAVisionModelReads(t *testing.T) {
	var big bytes.Buffer
	palette := image.NewPaletted(image.Rect(0, 0, 2000, 100), color.Palette{color.Black, color.White})
	if err := gif.Encode(&big, palette, nil); err != nil {
		t.Fatal(err)
	}
	if _, mimeType := DownscaleForVision(big.Bytes()); mimeType != "image/jpeg" {
		t.Fatalf("a 2000px GIF was not downscaled (mime %q): its decoder is not registered", mimeType)
	}
	// Modernizr's 1x1 lossless WebP probe.
	webp, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	if format, w, h := decodedSize(t, webp); format != "webp" || w != 1 || h != 1 {
		t.Fatalf("webp decoded as %s %dx%d, want webp 1x1", format, w, h)
	}
}
