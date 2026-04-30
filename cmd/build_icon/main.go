// build_icon regenerates internal/tray/icon.ico from a source PNG in Logo/.
//
// The output is a multi-resolution ICO (16, 20, 24, 32, 40, 48, 64, 128, 256)
// using Lanczos resampling so Windows can pick the best frame for the system
// tray at any DPI instead of downsampling a single 256 frame on the fly. Each
// frame is PNG-encoded inside the ICO container — supported by Windows Vista+.
//
// Run from the repo root:
//
//	go run ./cmd/build_icon
//
// Pipeline: open → auto-crop to bright content + pad to square → apply
// anti-aliased circular alpha mask (everything outside the inscribed circle
// goes transparent) → resize per frame → per-size sharpening for tiny frames.
// Masking happens at full resolution so Lanczos smooths the edge naturally.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"

	"github.com/disintegration/imaging"
)

const (
	srcPath = "Logo/loho new.png"
	dstPath = "internal/tray/icon.ico"
)

// targetSizes mirrors the Microsoft "Icons in Win32" recommendation for tray
// + taskbar coverage at 100%–250% DPI scaling.
var targetSizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "build_icon:", err)
		os.Exit(1)
	}
}

func run() error {
	src, err := imaging.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}

	square := cropToSquare(src)
	circle := applyCircularMask(square)

	entries := make([]icoEntry, 0, len(targetSizes))
	for _, size := range targetSizes {
		resized := imaging.Resize(circle, size, size, imaging.Lanczos)
		// Smaller frames need more help: the dark navy background otherwise
		// swallows the orb at 16/20/24 px, and Lanczos can dull edges. Boost
		// brightness, contrast, saturation, and unsharp mask for the smallest
		// frames; lighter touch for 32; nothing for 40+ where the original
		// detail already reads cleanly.
		switch {
		case size <= 24:
			resized = imaging.AdjustBrightness(resized, 12)
			resized = imaging.AdjustContrast(resized, 18)
			resized = imaging.AdjustSaturation(resized, 20)
			resized = imaging.Sharpen(resized, 0.8)
		case size <= 32:
			resized = imaging.AdjustContrast(resized, 8)
			resized = imaging.Sharpen(resized, 0.5)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, resized); err != nil {
			return fmt.Errorf("encode %dpx: %w", size, err)
		}
		entries = append(entries, icoEntry{size: size, png: buf.Bytes()})
	}

	out, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", dstPath, err)
	}
	defer out.Close()

	if err := writeICO(out, entries); err != nil {
		return fmt.Errorf("write ico: %w", err)
	}

	stat, _ := os.Stat(dstPath)
	fmt.Printf("wrote %s (%d frames, %d bytes)\n", dstPath, len(entries), stat.Size())
	return nil
}

type icoEntry struct {
	size int
	png  []byte
}

// applyCircularMask makes pixels outside the inscribed circle of img fully
// transparent, with a soft 1.5-pixel anti-aliased edge so the boundary stays
// smooth after Lanczos downsampling. Operating at full resolution lets the
// resize step further smooth the perimeter for free.
func applyCircularMask(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx := float64(w) / 2
	cy := float64(h) / 2
	r := math.Min(cx, cy)
	const aaWidth = 1.5
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := float64(x) - cx + 0.5
			dy := float64(y) - cy + 0.5
			d := math.Sqrt(dx*dx + dy*dy)
			edge := r - d
			if edge <= 0 {
				continue // outside circle → fully transparent
			}
			r0, g0, b0, a0 := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			alphaMul := 1.0
			if edge < aaWidth {
				alphaMul = edge / aaWidth
			}
			newA := uint32(float64(a0) * alphaMul)
			out.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r0 >> 8),
				G: uint8(g0 >> 8),
				B: uint8(b0 >> 8),
				A: uint8(newA >> 8),
			})
		}
	}
	return out
}

// cropToSquare returns the source cropped to a square centered on the orb,
// with side sized to encompass the orb radius. The center is the chroma-
// weighted centroid of high-saturation pixels (so off-orb decorations like
// sparkles don't pull the center off), and the radius comes from the weighted
// RMS distance from that centroid (outliers contribute less than bbox-based
// detection). A 1.3× scale on RMS covers ring-shaped orb glow with a small
// margin so the circular mask doesn't clip the outer halo.
func cropToSquare(src image.Image) image.Image {
	b := src.Bounds()
	const chromaMin = 100 * 257 // 8-bit 100 in 16-bit channels
	const lumMin = 100 * 257

	// Pass 1: weighted centroid of saturated, bright pixels.
	var sumW, sumWX, sumWY float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := src.At(x, y).RGBA()
			if a < 0x4000 {
				continue
			}
			lo, hi := r, r
			if g < lo {
				lo = g
			}
			if g > hi {
				hi = g
			}
			if bl < lo {
				lo = bl
			}
			if bl > hi {
				hi = bl
			}
			chroma := hi - lo
			lum := (r + g + bl) / 3
			if chroma < chromaMin || lum < lumMin {
				continue
			}
			w := float64(chroma)
			sumW += w
			sumWX += w * float64(x)
			sumWY += w * float64(y)
		}
	}
	if sumW == 0 {
		return src
	}
	cxF := sumWX / sumW
	cyF := sumWY / sumW

	// Pass 2: weighted RMS distance from centroid → orb radius proxy.
	var sumWD2 float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := src.At(x, y).RGBA()
			if a < 0x4000 {
				continue
			}
			lo, hi := r, r
			if g < lo {
				lo = g
			}
			if g > hi {
				hi = g
			}
			if bl < lo {
				lo = bl
			}
			if bl > hi {
				hi = bl
			}
			chroma := hi - lo
			lum := (r + g + bl) / 3
			if chroma < chromaMin || lum < lumMin {
				continue
			}
			dx := float64(x) - cxF
			dy := float64(y) - cyF
			sumWD2 += float64(chroma) * (dx*dx + dy*dy)
		}
	}
	rmsD := math.Sqrt(sumWD2 / sumW)
	half := int(rmsD*1.3 + 0.5)

	cx := int(cxF + 0.5)
	cy := int(cyF + 0.5)
	sx := cx - half
	sy := cy - half
	ex := cx + half
	ey := cy + half
	if sx < b.Min.X {
		sx = b.Min.X
	}
	if sy < b.Min.Y {
		sy = b.Min.Y
	}
	if ex > b.Max.X {
		ex = b.Max.X
	}
	if ey > b.Max.Y {
		ey = b.Max.Y
	}
	return imaging.Crop(src, image.Rect(sx, sy, ex, ey))
}

// writeICO emits a multi-resolution ICO with PNG-encoded entries.
//
// Format reference: https://en.wikipedia.org/wiki/ICO_(file_format)
//
//	ICONDIR     (6 bytes):  reserved=0, type=1, count
//	ICONDIRENTRY (16 each): width, height, colors=0, reserved=0,
//	                        planes=1, bitCount=32, bytesInRes, offset
//	[image data: PNG file, one per entry]
//
// Width/height bytes wrap at 256 (a width byte of 0 means 256).
func writeICO(w io.Writer, entries []icoEntry) error {
	// ICONDIR
	if err := binary.Write(w, binary.LittleEndian, uint16(0)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(len(entries))); err != nil {
		return err
	}

	offset := uint32(6 + 16*len(entries))
	for _, e := range entries {
		dim := byte(e.size)
		if e.size >= 256 {
			dim = 0
		}
		if _, err := w.Write([]byte{dim, dim, 0, 0}); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil {
			return err // planes
		}
		if err := binary.Write(w, binary.LittleEndian, uint16(32)); err != nil {
			return err // bitCount
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(len(e.png))); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, offset); err != nil {
			return err
		}
		offset += uint32(len(e.png))
	}
	for _, e := range entries {
		if _, err := w.Write(e.png); err != nil {
			return err
		}
	}
	return nil
}
