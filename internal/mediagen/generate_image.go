package mediagen

import "context"

// ImageGeneration is one image to generate: whose it is, with which model, and what was asked.
type ImageGeneration struct {
	Owner string
	Model string
	Input ImageInput
}

// GeneratedImage is one paid image and what produced it: the clamped input as the provider
// received it, and the notes the clamp made.
type GeneratedImage struct {
	Result      ImageResult
	Prompt      string
	Used        ImageInput
	Adjustments []string
}

// ImageGenerator is the one path that pays for an image: the image_generate tool and the
// cockpit Studio both generate through it. Unlike video there is no job — the call returns
// the bytes — so the caller stores them.
type ImageGenerator struct {
	Credentials   MediaCredentials
	Catalog       *Catalog
	Client        *Client
	References    ReferenceReader
	MaxImageBytes int64
}

// Configured reports whether every dependency is present; a caller refuses before any paid
// request otherwise.
func (g *ImageGenerator) Configured() bool {
	return g != nil && g.Credentials != nil && g.Catalog != nil && g.Client != nil &&
		g.References != nil && g.MaxImageBytes > 0
}

// Generate settles credentials, the catalog entry, the clamp and the references before the
// one paid call, which is never repeated.
func (g *ImageGenerator) Generate(ctx context.Context, gen ImageGeneration) (GeneratedImage, error) {
	baseURL, apiKey, err := g.Credentials.For(ctx, gen.Owner)
	if err != nil {
		return GeneratedImage{}, err
	}
	entry, adjustments, err := g.Catalog.Entry(ctx, baseURL, KindImage, gen.Model)
	if err != nil {
		return GeneratedImage{}, err
	}
	input, notes, err := ClampImage(gen.Input, entry)
	if err != nil {
		return GeneratedImage{}, err
	}
	references, err := LoadReferences(ctx, g.References, gen.Owner, input.ReferenceAssetIDs, g.MaxImageBytes)
	if err != nil {
		return GeneratedImage{}, err
	}
	result, err := g.Client.GenerateImage(ctx, baseURL, apiKey, ImageRequest{
		Model: gen.Model, Prompt: input.Prompt, AspectRatio: input.AspectRatio, References: references,
	})
	if err != nil {
		return GeneratedImage{}, err
	}
	return GeneratedImage{Result: result, Prompt: input.Prompt, Used: input, Adjustments: append(adjustments, notes...)}, nil
}
