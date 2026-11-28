//go:build ignore

// jpegliprobe decodes a JPEG with github.com/gen2brain/jpegli (jpegli in WASM on wazero) with
// DecodingOptions.ScaleTarget set to the 1024 px long edge. Its C wrapper (lib/jpegli.c) then
// picks the smallest scale_num/8 whose output still covers the target, so libjpeg's scaled IDCT
// does the reduction. The probe finishes as DownscaleForVision does: CatmullRom to 1024 px on
// white, JPEG quality 85. 20_build.sh builds a copy in its own module under /tmp.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"time"

	"github.com/gen2brain/jpegli"
	xdraw "golang.org/x/image/draw"
)

func main() {
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Println("read:", err)
		os.Exit(1)
	}
	start := time.Now()
	cfg, err := jpegli.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(1)
	}
	tw, th := 1024, max(1, cfg.Height*1024/cfg.Width)
	if cfg.Height > cfg.Width {
		tw, th = max(1, cfg.Width*1024/cfg.Height), 1024
	}
	img, err := jpegli.DecodeWithOptions(bytes.NewReader(raw), &jpegli.DecodingOptions{
		ScaleTarget: image.Rect(0, 0, tw, th), FancyUpsampling: true,
	})
	if err != nil {
		fmt.Println("decode:", err)
		os.Exit(1)
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.Draw(dst, dst.Bounds(), image.White, image.Point{}, xdraw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		fmt.Println("encode:", err)
		os.Exit(1)
	}
	fmt.Printf("in_bytes=%d in=%dx%d decoded=%dx%d (%T) ms=%d out_bytes=%d out=%dx%d\n",
		len(raw), cfg.Width, cfg.Height, b.Dx(), b.Dy(), img, time.Since(start).Milliseconds(), out.Len(), tw, th)
	if len(os.Args) > 2 {
		if err := os.WriteFile(os.Args[2], out.Bytes(), 0o644); err != nil {
			fmt.Println("write:", err)
		}
	}
}
