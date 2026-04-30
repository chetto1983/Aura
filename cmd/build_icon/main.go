// build_icon regenerates internal/tray/icon.ico from Logo/logo.png.
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
// Source crop is hand-tuned to focus on the glowing orb (the "AURA" wordmark
// is excluded). Adjust the cropRect if the source image changes.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"

	"github.com/disintegration/imaging"
)

const (
	srcPath = "Logo/logo.png"
	dstPath = "internal/tray/icon.ico"
)

// cropRect isolates the bright orb from the source PNG. The original is
// 821x705; the orb's visible glow sits between roughly (200,50) and
// (620,470). A tighter crop avoids letting the dark navy background dominate
// the small frames where every pixel counts.
var cropRect = image.Rect(200, 50, 620, 470)

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

	cropped := imaging.Crop(src, cropRect)

	entries := make([]icoEntry, 0, len(targetSizes))
	for _, size := range targetSizes {
		resized := imaging.Resize(cropped, size, size, imaging.Lanczos)
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
