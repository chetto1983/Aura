//nolint:revive // Internal asset processors expose small cross-package wiring APIs.
package assets

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"

	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/objectstore"
	xdraw "golang.org/x/image/draw"
)

// assetVisionPrompt is the instruction sent with an uploaded image. Kept here (not
// in multimodal) because it is the asset pipeline's product decision; the telegram
// channel passes its own localized prompt.
const assetVisionPrompt = "Describe this image."

// ImageProcessor turns an uploaded image asset into a text summary. The OpenAI-
// compatible vision call (local sidecar or OpenRouter cloud, with the one
// config-only swap) lives in internal/multimodal; this processor owns only the
// objectstore read, the VRAM-friendly downscale, and the Result mapping.
type ImageProcessor struct {
	Objects objectstore.Store
	Vision  *multimodal.VisionClient
}

// NewImageProcessor builds an image processor over the shared vision client.
func NewImageProcessor(objects objectstore.Store, cfg multimodal.VisionConfig) *ImageProcessor {
	return &ImageProcessor{Objects: objects, Vision: multimodal.NewVisionClient(cfg)}
}

func (p *ImageProcessor) ProcessAsset(ctx context.Context, asset Asset) (Result, error) {
	if p == nil || p.Objects == nil || p.Vision == nil {
		return Result{}, fmt.Errorf("image processor is not configured")
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	rc, attrs, err := p.Objects.Get(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = rc.Close() }()
	imageBytes, err := io.ReadAll(rc)
	if err != nil {
		return Result{}, err
	}
	mimeType := asset.MIMEType
	if mimeType == "" {
		mimeType = attrs.MIMEType
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if ds, dsMime := downscaleAssetForVision(imageBytes); dsMime != "" {
		imageBytes, mimeType = ds, dsMime
	}
	summary, err := p.Vision.Describe(ctx, imageBytes, mimeType, assetVisionPrompt)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Status:  StatusComplete,
		Summary: summary,
		Metadata: map[string]any{
			"vision_model": p.Vision.VisionModel(),
		},
	}, nil
}

const (
	assetVisionMaxEdge     = 1024
	assetVisionJPEGQuality = 85
)

// downscaleAssetForVision shrinks an oversized image to assetVisionMaxEdge on its
// long edge (JPEG) so the CPU/4 GB-GPU OCR sidecar isn't handed a full-res photo.
// A decode failure or an already-small image returns ("", "") — the caller keeps
// the original bytes/mime.
func downscaleAssetForVision(raw []byte) (out []byte, mime string) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw, ""
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= assetVisionMaxEdge && h <= assetVisionMaxEdge {
		return raw, ""
	}
	nw, nh := assetVisionMaxEdge, assetVisionMaxEdge
	if w >= h {
		nh = h * assetVisionMaxEdge / w
	} else {
		nw = w * assetVisionMaxEdge / h
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: assetVisionJPEGQuality}); err != nil {
		return raw, ""
	}
	return buf.Bytes(), "image/jpeg"
}
