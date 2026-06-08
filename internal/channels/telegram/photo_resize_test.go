package telegram

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

// TestDownscaleForVision proves an oversized photo is shrunk to <=visionMaxEdge
// before OCR (the live cat photo took ~55s at full res), aspect ratio preserved,
// while a small image and undecodable bytes pass through untouched.
func TestDownscaleForVision(t *testing.T) {
	t.Parallel()

	t.Run("oversized is downscaled, aspect preserved", func(t *testing.T) {
		t.Parallel()
		raw := encodeTestJPEG(t, 4000, 3000) // a 4000x3000 phone-ish photo
		out, mime := downscaleForVision(raw)
		if mime != "image/jpeg" {
			t.Fatalf("mime = %q, want image/jpeg (re-encoded)", mime)
		}
		cfg := decodeBounds(t, out)
		if cfg.Dx() != visionMaxEdge {
			t.Errorf("width = %d, want %d (longest edge capped)", cfg.Dx(), visionMaxEdge)
		}
		if cfg.Dy() != visionMaxEdge*3000/4000 {
			t.Errorf("height = %d, want %d (aspect preserved)", cfg.Dy(), visionMaxEdge*3000/4000)
		}
		if len(out) >= len(raw) {
			t.Errorf("downscaled bytes (%d) not smaller than original (%d)", len(out), len(raw))
		}
	})

	t.Run("already-small passes through untouched", func(t *testing.T) {
		t.Parallel()
		raw := encodeTestJPEG(t, 640, 480)
		out, mime := downscaleForVision(raw)
		if mime != "" {
			t.Errorf("mime = %q, want \"\" (no re-encode for a small image)", mime)
		}
		if !bytes.Equal(out, raw) {
			t.Error("small image bytes were modified")
		}
	})

	t.Run("undecodable bytes pass through", func(t *testing.T) {
		t.Parallel()
		raw := []byte("not an image")
		out, mime := downscaleForVision(raw)
		if mime != "" || !bytes.Equal(out, raw) {
			t.Errorf("undecodable input should pass through; got mime=%q changed=%v", mime, !bytes.Equal(out, raw))
		}
	})
}

func encodeTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode test jpeg: %v", err)
	}
	return buf.Bytes()
}

func decodeBounds(t *testing.T, raw []byte) image.Rectangle {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode downscaled: %v", err)
	}
	return img.Bounds()
}
