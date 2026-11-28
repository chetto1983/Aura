//go:build ignore

// jpegnprobe decodes a JPEG with github.com/gen2brain/jpegn at the largest IDCT scale (1/2, 1/4,
// 1/8) that keeps the long edge at or over 1024 px, then finishes as DownscaleForVision does:
// CatmullRom to a 1024 px long edge on white, JPEG quality 85. Its own module, so the repo's
// go.mod gains nothing: 20_build.sh builds a copy of this file in its own module under /tmp.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"time"

	"github.com/gen2brain/jpegn"
	xdraw "golang.org/x/image/draw"
)

func main() {
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Println("read:", err)
		os.Exit(1)
	}
	start := time.Now()
	cfg, err := jpegn.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(1)
	}
	long := max(cfg.Width, cfg.Height)
	denom := 1
	for _, d := range []int{8, 4, 2} {
		if (long+d-1)/d >= 1024 {
			denom = d
			break
		}
	}
	img, err := jpegn.Decode(bytes.NewReader(raw), &jpegn.Options{ScaleDenom: denom})
	if err != nil {
		fmt.Println("decode:", err)
		os.Exit(1)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := 1024, max(1, h*1024/w)
	if h > w {
		nw, nh = max(1, w*1024/h), 1024
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.Draw(dst, dst.Bounds(), image.White, image.Point{}, xdraw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		fmt.Println("encode:", err)
		os.Exit(1)
	}
	fmt.Printf("in_bytes=%d in=%dx%d denom=%d decoded=%dx%d (%T) ms=%d out_bytes=%d out=%dx%d\n",
		len(raw), cfg.Width, cfg.Height, denom, w, h, img, time.Since(start).Milliseconds(), out.Len(), nw, nh)
	if len(os.Args) > 2 {
		if err := os.WriteFile(os.Args[2], out.Bytes(), 0o644); err != nil {
			fmt.Println("write:", err)
		}
	}
}
