package mediagen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// ImageURL is the wire shape OpenRouter's image and video APIs share for a
// single reference image: either an HTTP(S) URL or a data: URL.
type ImageURL struct {
	URL string `json:"url"`
}

// ImageReference is one entry of ImageRequest.References or
// VideoRequest.InputReferences: a style/content reference image.
type ImageReference struct {
	Type     string   `json:"type"`
	ImageURL ImageURL `json:"image_url"`
}

// ImageRequest is a caller-ready image generation call: every field has
// already been clamped to what the target model declares.
type ImageRequest struct {
	Model       string
	Prompt      string
	AspectRatio string
	References  []ImageReference
}

// ImageResult is one generated image. CostUSD is nil when OpenRouter
// reported no usage cost at all, never coerced to zero: a nil and an
// explicit zero cost are different facts.
type ImageResult struct {
	Bytes    []byte
	MIMEType string
	CostUSD  *float64
}

type imageGenerationWire struct {
	Data []struct {
		B64JSON   string `json:"b64_json"`
		MediaType string `json:"media_type"`
	} `json:"data"`
	Usage *struct {
		Cost *float64 `json:"cost"`
	} `json:"usage"`
}

// sniffableImageMIME is the set of media types net/http.DetectContentType
// can confirm from bytes. SVG is XML text and has no byte signature
// DetectContentType recognizes, so it is never in this set: see
// resolveImageMIME for how a declared image/svg+xml is handled instead.
var sniffableImageMIME = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// GenerateImage calls OpenRouter's image generation endpoint and returns one
// decoded, byte-limit-checked image. It decodes the raw response itself
// (rather than the SDK's typed ImagesResponse) because OpenRouter's
// media_type and usage.cost fields are not part of the upstream OpenAI
// schema the SDK models. It never emits a successful result from a response
// with no usable output.
func (c *Client) GenerateImage(ctx context.Context, baseURL, apiKey string, req ImageRequest) (ImageResult, error) {
	client := sdkClient(c.http, baseURL, option.WithAPIKey(apiKey))
	images := openai.NewImageService(client.Options...)

	var opts []option.RequestOption
	if req.AspectRatio != "" {
		opts = append(opts, option.WithJSONSet("aspect_ratio", req.AspectRatio))
	}
	if len(req.References) > 0 {
		opts = append(opts, option.WithJSONSet("input_references", req.References))
	}
	var raw []byte
	opts = append(opts, option.WithResponseBodyInto(&raw))

	params := openai.ImageGenerateParams{Model: openai.ImageModel(req.Model), Prompt: req.Prompt}
	if _, err := images.Generate(ctx, params, opts...); err != nil {
		return ImageResult{}, classifyProviderError(err)
	}

	var wire imageGenerationWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return ImageResult{}, fmt.Errorf("mediagen: decode image generation response: %w", err)
	}
	if len(wire.Data) == 0 || wire.Data[0].B64JSON == "" {
		return ImageResult{}, fmt.Errorf("mediagen: image generation response carried no usable output")
	}
	out := wire.Data[0]

	data, err := decodeCappedBase64(out.B64JSON, c.maxImageBytes)
	if err != nil {
		return ImageResult{}, err
	}
	mimeType, err := resolveImageMIME(out.MediaType, data)
	if err != nil {
		return ImageResult{}, err
	}

	result := ImageResult{Bytes: data, MIMEType: mimeType}
	if wire.Usage != nil {
		result.CostUSD = wire.Usage.Cost
	}
	return result, nil
}

// decodeCappedBase64 rejects an oversized encoded payload from its length
// alone, before allocating a buffer to decode it, then verifies the decoded
// size too: DecodedLen is an upper bound on the decoded length, not always
// the exact value once padding is accounted for.
func decodeCappedBase64(encoded string, maxBytes int64) ([]byte, error) {
	if err := validByteLimit(maxBytes); err != nil {
		return nil, err
	}
	if int64(base64.StdEncoding.DecodedLen(len(encoded))) > maxBytes {
		return nil, &Error{Code: "too_large", Message: "Generated image exceeds the configured byte limit."}
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("mediagen: decode image bytes: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, &Error{Code: "too_large", Message: "Generated image exceeds the configured byte limit."}
	}
	return data, nil
}

// resolveImageMIME validates the declared media_type against the decoded
// bytes' sniffed type using net/http.DetectContentType (never a hand-rolled
// signature table), or sniffs alone when none was declared. SVG cannot be
// sniffed that way, so a declared image/svg+xml is trusted directly and
// returned as a downloadable asset rather than a displayable-image guess.
// Any other mismatch or unrecognized type is refused, never guessed.
func resolveImageMIME(declared string, data []byte) (string, error) {
	sniffed := http.DetectContentType(data)
	declared = strings.TrimSpace(declared)
	if declared == "" {
		if !sniffableImageMIME[sniffed] {
			return "", &Error{Code: "unsupported", Message: "Generated image has an unrecognized media type."}
		}
		return sniffed, nil
	}
	mediaType, _, err := mime.ParseMediaType(declared)
	if err != nil {
		return "", &Error{Code: "unsupported", Message: "Generated image declared an unparseable media type."}
	}
	if mediaType == "image/svg+xml" {
		return mediaType, nil
	}
	if mediaType != sniffed {
		return "", &Error{Code: "unsupported", Message: "Generated image content does not match its declared media type."}
	}
	return mediaType, nil
}
