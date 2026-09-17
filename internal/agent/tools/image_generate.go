package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/mediagen"
)

// ImageGenerate creates or edits one image on the identity's own OpenRouter key with
// the operator-selected model, and delivers it as an owned artifact exactly as send_file
// does. Every dependency is injected at serve boot; the static and manifest registries
// hold a zero value whose Execute refuses before any request.
type ImageGenerate struct {
	// Generator is the shared path that pays for an image; the cockpit Studio generates through
	// the same one, so the clamp and the reference reads are never decided twice.
	Generator *mediagen.ImageGenerator
	Settings  mediagen.Settings
	Assets    AssetDeliverer
}

const imageGenerateParameters = `{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "description": "What to generate, or the edit to make to the reference images."},
    "aspect_ratio": {"type": "string", "enum": ["1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"], "description": "Requested shape; the nearest ratio the model offers is used."},
    "reference_asset_ids": {"type": "array", "items": {"type": "string"}, "description": "Asset ids of images to edit or follow, from attachments or earlier generation results."}
  },
  "required": ["prompt"],
  "additionalProperties": false
}`

type imageGenerateArgs struct {
	Prompt            string   `json:"prompt"`
	AspectRatio       string   `json:"aspect_ratio"`
	ReferenceAssetIDs []string `json:"reference_asset_ids"`
}

type imageGenerateUsed struct {
	AspectRatio       string   `json:"aspect_ratio,omitempty"`
	ReferenceAssetIDs []string `json:"reference_asset_ids,omitempty"`
}

func (g *ImageGenerate) Spec() Spec {
	return Spec{
		Name:                "image_generate",
		Summary:             "Generate an image or picture, or edit an image using reference assets.",
		Description:         "Create one image from a prompt or edit supplied image assets. Use reference_asset_ids from attachments or previous generation results. The operator chooses the model. The image is shown to the user when this call returns; do not send it again. Read adjustments and errors; do not invent a model or a download URL.",
		Parameters:          json.RawMessage(imageGenerateParameters),
		Deferred:            true,
		Mutating:            true,
		OperationScope:      OperationScopeAgent,
		OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy:        ReplayToolResult,
	}
}

// Execute resolves the credential, the live model and the catalog clamp, reads the kept
// references, generates once, then stages and ingests the image. Every refusal that can
// be decided locally is decided before the paid request; every failure is an
// {error,message} result, never a Go error.
func (g *ImageGenerate) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args imageGenerateArgs
	if err := decodeStrictArgs(raw, &args); err != nil {
		return errorResult("unsupported", "Pass an object with a prompt and, optionally, aspect_ratio and reference_asset_ids; the operator chooses the model, so no other field is accepted."), nil
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return errorResult("unsupported", "A prompt is required: describe the image to generate or the edit to make."), nil
	}
	if !g.configured() {
		return errorResult("unsupported", "Image generation is not available in this session."), nil
	}
	owner, ok := mediaDeliveryOwner(ctx)
	if !ok {
		return errorResult("unsupported", "Image generation needs a signed-in conversation to deliver the image into."), nil
	}

	model, err := g.Settings.Model(ctx, mediagen.KindImage)
	if err != nil {
		return mediaErrorResult(err), nil
	}
	generated, err := g.Generator.Generate(ctx, mediagen.ImageGeneration{
		Owner: owner, Model: model,
		Input: mediagen.ImageInput{
			Prompt: args.Prompt, AspectRatio: args.AspectRatio, ReferenceAssetIDs: args.ReferenceAssetIDs,
		},
	})
	if err != nil {
		return mediaErrorResult(err), nil
	}

	path, filename, err := stageImage(ctx, generated.Result.Bytes, generated.Result.MIMEType)
	if err != nil {
		return errorResult("job_failed", "The image was generated but could not be staged for delivery."), nil
	}
	size := int64(len(generated.Result.Bytes))
	assetID, mimeType, delivered := ingestForDelivery(ctx, g.Assets, path, filename, size)
	if !delivered {
		discardStagedMedia(path)
		return errorResult("job_failed", "The image was generated but could not be saved, so it was not delivered."), nil
	}
	preview := mediaResult{
		AssetID: assetID, MIMEType: mimeType, Model: model, CostUSD: generated.Result.CostUSD,
		Used: imageGenerateUsed{
			AspectRatio: generated.Used.AspectRatio, ReferenceAssetIDs: generated.Used.ReferenceAssetIDs,
		},
		Adjustments: generated.Adjustments,
	}
	return mediaArtifactResult(ctx, path, filename, mimeType, assetID, generated.Prompt, size, preview), nil
}

func (g *ImageGenerate) configured() bool {
	return g.Generator.Configured() && g.Settings != nil && g.Assets != nil
}
