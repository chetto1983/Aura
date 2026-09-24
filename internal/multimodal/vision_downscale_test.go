package multimodal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"math/rand/v2"
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

// pngHeader declares w x h in a valid IHDR and holds no pixels.
func pngHeader(w, h uint32) []byte {
	chunk := binary.BigEndian.AppendUint32([]byte("IHDR"), w)
	chunk = binary.BigEndian.AppendUint32(chunk, h)
	chunk = append(chunk, 8, 6, 0, 0, 0)
	out := binary.BigEndian.AppendUint32([]byte("\x89PNG\r\n\x1a\n"), uint32(len(chunk)-4))
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

// webpCanvas is a RIFF/WebP with a VP8X canvas header of w x h followed by chunks x/image/webp
// skips — an animated sticker's shape — so its header decodes and its body never does.
func webpCanvas(w, h uint32) []byte {
	chunk := func(id string, data []byte) []byte {
		return append(binary.LittleEndian.AppendUint32([]byte(id), uint32(len(data))), data...)
	}
	vp8x := []byte{0x02, 0, 0, 0, byte(w - 1), byte((w - 1) >> 8), byte((w - 1) >> 16), byte(h - 1), byte((h - 1) >> 8), byte((h - 1) >> 16)}
	body := append([]byte("WEBP"), chunk("VP8X", vp8x)...)
	body = append(body, chunk("ANIM", make([]byte, 6))...)
	body = append(body, chunk("ANMF", make([]byte, 16))...)
	return append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body))), body...)
}

// gifScreen is a GIF whose logical screen is w x h and holds no frame.
func gifScreen(w, h uint16) []byte {
	out := binary.LittleEndian.AppendUint16([]byte("GIF89a"), w)
	out = binary.LittleEndian.AppendUint16(out, h)
	return append(out, 0, 0, 0, ';')
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
			got, err := DownscaleForVision(t.Context(), pngOf(t, tc.w, tc.h), 0)
			if err != nil || got.MIMEType != "image/jpeg" || got.Width != tc.w || got.Height != tc.h {
				t.Fatalf("got {%q %dx%d} err %v, want a JPEG reporting the original %dx%d", got.MIMEType, got.Width, got.Height, err, tc.w, tc.h)
			}
			format, w, h := decodedSize(t, got.Bytes)
			if format != "jpeg" || w != tc.wantW || h != tc.wantH {
				t.Fatalf("out = %s %dx%d, want jpeg %dx%d", format, w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestDownscaleForVisionKeepsWhatAlreadyFits(t *testing.T) {
	for name, raw := range map[string][]byte{
		"small":        pngOf(t, 800, 600),
		"exactly 1024": pngOf(t, 1024, 1024),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DownscaleForVision(t.Context(), raw, 0)
			if err != nil || got.MIMEType != "" || !bytes.Equal(got.Bytes, raw) {
				t.Fatalf("got (%d bytes, %q, err %v), want the original bytes and no MIME type", len(got.Bytes), got.MIMEType, err)
			}
		})
	}
}

// Every refusal happens before a caller could attach or upload a body that never decoded. A
// truncated body and an animated WebP both pass DecodeConfig; only the full decode finds out.
func TestDownscaleForVisionReportsWhatWillNotDecode(t *testing.T) {
	truncated := pngOf(t, 40, 40)
	truncated = truncated[:len(truncated)-20]
	for name, raw := range map[string][]byte{
		"not an image":          []byte("%PDF-1.7 not an image"),
		"a truncated header":    []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRtrunc"),
		"a truncated body":      truncated,
		"an animated WebP body": webpCanvas(100, 100),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DownscaleForVision(t.Context(), raw, 0)
			if !errors.Is(err, ErrImageUndecodable) {
				t.Fatalf("err = %v, want ErrImageUndecodable", err)
			}
			if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
				t.Fatalf("err wraps an EOF, which callers read as transient: %v", err)
			}
		})
	}
}

// The bound comes from the header alone: none of these carries a body, so a decode would have
// reported them undecodable instead — the pixel error proves nothing was decoded.
func TestDownscaleForVisionBoundsPixelsBeforeDecoding(t *testing.T) {
	for name, raw := range map[string][]byte{
		"png":  pngHeader(10_000, 5_000),
		"gif":  gifScreen(65_535, 65_535),
		"webp": webpCanvas(16_384, 16_384),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DownscaleForVision(t.Context(), raw, 0)
			if !errors.Is(err, ErrImageTooManyPixels) {
				t.Fatalf("err = %v, want ErrImageTooManyPixels", err)
			}
		})
	}
}

// jpegHeader is a progressive JPEG header of w x h with ncomp components and no scan data: 1 is
// gray, 3 (ids 1-3, no JFIF or Adobe marker) YCbCr, 4 CMYK. The SOS marker ends DecodeConfig.
func jpegHeader(w, h uint16, ncomp byte) []byte {
	sof := []byte{0xFF, 0xC2, 0, 8 + 3*ncomp, 8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), ncomp}
	for id := range ncomp {
		sof = append(sof, id+1, 0x11, 0)
	}
	return append(append([]byte{0xFF, 0xD8}, sof...), 0xFF, 0xDA, 0, 12)
}

// The bound depends on the color model the header declares: a header past its family's bound
// is refused as too many pixels, and one within it reaches the decode, which finds no body.
func TestDownscaleForVisionBoundsDependOnTheColorModel(t *testing.T) {
	for name, tc := range map[string]struct {
		raw     []byte
		bounded bool
	}{
		"YCbCr at its bound":         {jpegHeader(3515, 3515, 3), false},
		"YCbCr one row past":         {jpegHeader(3515, 3516, 3), true},
		"CMYK at the YCbCr bound":    {jpegHeader(3515, 3515, 4), true},
		"CMYK at its bound":          {jpegHeader(2910, 2910, 4), false},
		"gray past the YCbCr bound":  {jpegHeader(4000, 4000, 1), false},
		"gray one row past its own":  {jpegHeader(4772, 4773, 1), true},
		"RGBA PNG at its bound":      {pngHeader(3180, 3180), false},
		"RGBA PNG one row past":      {pngHeader(3180, 3181), true},
		"YCbCr at the RGBA PNG size": {jpegHeader(3180, 3181, 3), false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DownscaleForVision(t.Context(), tc.raw, 0)
			want := ErrImageUndecodable
			if tc.bounded {
				want = ErrImageTooManyPixels
			}
			if !errors.Is(err, want) {
				t.Fatalf("err = %v, want %v", err, want)
			}
		})
	}
}

// Every model DecodeConfig can report lands in a family; the ones no header above produces
// (lossy WebP with alpha, 16-bit gray, a palette) are pinned here.
func TestMaxVisionPixelsFamilies(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model color.Model
		want  int
	}{
		{"YCbCr", color.YCbCrModel, maxPixelsYCbCr},
		{"NYCbCrA (lossy WebP with alpha)", color.NYCbCrAModel, maxPixelsYCbCr},
		{"CMYK", color.CMYKModel, maxPixelsCMYK},
		{"gray", color.GrayModel, maxPixelsGray},
		{"gray16", color.Gray16Model, maxPixelsOther},
		{"RGBA", color.RGBAModel, maxPixelsOther},
		{"NRGBA", color.NRGBAModel, maxPixelsOther},
		{"NRGBA64", color.NRGBA64Model, maxPixelsOther},
		{"palette (GIF, paletted PNG)", color.Palette{color.Black, color.White}, maxPixelsOther},
	} {
		if got := maxVisionPixels(tc.model); got != tc.want {
			t.Errorf("%s: bound %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestDownscaleForVisionReencodesPastTheByteCap(t *testing.T) {
	padded := append(pngOf(t, 2, 2), make([]byte, 1<<20)...)
	got, err := DownscaleForVision(t.Context(), padded, 64<<10)
	if err != nil || got.MIMEType != "image/jpeg" || len(got.Bytes) > 64<<10 {
		t.Fatalf("got {%q, %d bytes} err %v, want a JPEG within the cap", got.MIMEType, len(got.Bytes), err)
	}
	if format, w, h := decodedSize(t, got.Bytes); format != "jpeg" || w != 2 || h != 2 {
		t.Fatalf("re-encoded as %s %dx%d, want the same 2x2", format, w, h)
	}
	if kept, err := DownscaleForVision(t.Context(), padded, 0); err != nil || kept.MIMEType != "" || !bytes.Equal(kept.Bytes, padded) {
		t.Fatalf("with no byte cap the original must be kept: {%q, %d bytes} err %v", kept.MIMEType, len(kept.Bytes), err)
	}

	noise := image.NewRGBA(image.Rect(0, 0, 64, 64))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range noise.Pix {
		noise.Pix[i] = byte(rng.Uint32())
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, noise); err != nil {
		t.Fatal(err)
	}
	if _, err := DownscaleForVision(t.Context(), buf.Bytes(), 100); !errors.Is(err, ErrImageOverByteCap) {
		t.Fatalf("err = %v, want ErrImageOverByteCap for noise that no JPEG fits in 100 bytes", err)
	}
}

// transparentWithDarkCenter is a fully transparent PNG with an opaque near-black square in its
// middle: the shape of a logo or a sticker.
func transparentWithDarkCenter(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := h / 4; y < 3*h/4; y++ {
		for x := w / 4; x < 3*w/4; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 20, G: 20, B: 20, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// JPEG has no alpha, so transparency must become white: black would hide dark content drawn
// on it. Both JPEG paths are checked — the downscale and the re-encode at the byte cap.
func TestDownscaleForVisionFlattensTransparencyOnWhite(t *testing.T) {
	for name, tc := range map[string]struct {
		raw      []byte
		maxBytes int
	}{
		"downscaled":             {transparentWithDarkCenter(t, 2048, 1024), 0},
		"re-encoded at its size": {append(transparentWithDarkCenter(t, 64, 64), make([]byte, 64<<10)...), 32 << 10},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DownscaleForVision(t.Context(), tc.raw, tc.maxBytes)
			if err != nil || got.MIMEType != "image/jpeg" {
				t.Fatalf("got %q err %v, want a JPEG", got.MIMEType, err)
			}
			img, _, err := image.Decode(bytes.NewReader(got.Bytes))
			if err != nil {
				t.Fatal(err)
			}
			b := img.Bounds()
			luma := func(x, y int) uint8 { return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y }
			if corner := luma(b.Min.X+1, b.Min.Y+1); corner < 240 {
				t.Fatalf("transparent corner came out at luma %d, want white", corner)
			}
			if center := luma(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2); center > 60 {
				t.Fatalf("dark center came out at luma %d, want it still dark", center)
			}
		})
	}
}

// One full decode at a time for the whole process: a caller waits for the slot, and stops
// waiting when its context ends.
func TestDownscaleForVisionWaitsForTheDecodeSlot(t *testing.T) {
	decodeSlot <- struct{}{}
	defer func() { <-decodeSlot }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := DownscaleForVision(ctx, pngOf(t, 2, 2), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the caller's cancellation while the slot is held", err)
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
	if got, err := DownscaleForVision(t.Context(), big.Bytes(), 0); err != nil || got.MIMEType != "image/jpeg" {
		t.Fatalf("a 2000px GIF was not downscaled (mime %q, err %v): its decoder is not registered", got.MIMEType, err)
	}
	// Modernizr's 1x1 lossless WebP probe.
	webp, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DownscaleForVision(t.Context(), webp, 0); err != nil || got.Width != 1 || got.Height != 1 {
		t.Fatalf("webp = {%dx%d} err %v, want a decodable 1x1", got.Width, got.Height, err)
	}
}
