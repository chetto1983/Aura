//go:build ignore

// baseline runs today's multimodal.DownscaleForVision on one image and prints what it returned.
// x/image/bmp is registered so the BMP that `djpeg -scale 1/N -bmp` writes can be handed to the
// same function: that is the "decode at 1/N outside, let the Go path finish" leg.
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"os"
	"time"

	_ "golang.org/x/image/bmp"

	"github.com/chetto1983/aura/internal/multimodal"
)

func main() {
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Println("read:", err)
		os.Exit(1)
	}
	start := time.Now()
	got, err := multimodal.DownscaleForVision(context.Background(), raw, 0)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		fmt.Printf("in_bytes=%d ms=%d ERROR=%q\n", len(raw), ms, err.Error())
		return
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(got.Bytes))
	fmt.Printf("in_bytes=%d in=%dx%d ms=%d out_mime=%q out_bytes=%d out=%dx%d\n",
		len(raw), got.Width, got.Height, ms, got.MIMEType, len(got.Bytes), cfg.Width, cfg.Height)
	if len(os.Args) > 2 {
		if err := os.WriteFile(os.Args[2], got.Bytes, 0o644); err != nil {
			fmt.Println("write:", err)
		}
	}
}
