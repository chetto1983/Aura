package anthropic

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"mime"
	"strings"

	// The formats an image block takes, so DecodeConfig can read their dimensions.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/chetto1983/aura/internal/llm"
)

// Claude's documented input limits, read 2026-11-28 on
// https://platform.claude.com/docs/en/build-with-claude/vision ("Request limits") and
// https://platform.claude.com/docs/en/build-with-claude/pdf-support ("Check PDF
// requirements"). Not yet measured live (plan Task 10). The API answers a part over them
// with a 400, which no retry and no other part of Aura would ever get past.
const (
	// maxImageBase64Bytes is "10 MB (base64-encoded)" per image on the Claude API, read as
	// 10^7 bytes, the smaller of the two meanings.
	maxImageBase64Bytes = 10_000_000
	// maxImageEdge: "The maximum dimensions for each image are 8000x8000 px."
	maxImageEdge = 8000
	// maxRequestBase64Bytes is the 128 MB request size of the first-party API: a PDF over
	// it can never be sent, whatever else the request carries.
	maxRequestBase64Bytes = 128_000_000
)

// tooLarge names why Claude would refuse data as mediaType, "" when it would not. It
// judges the part alone, so a part renders the same way in every round of a turn and the
// replayed prefix never changes under it. An image whose header does not decode is left
// to the API, which judges it the same way every time.
func tooLarge(mediaType string, data []byte) string {
	size := base64.StdEncoding.EncodedLen(len(data))
	if mediaType == pdfMIMEType {
		if size > maxRequestBase64Bytes {
			return "it is over Claude's 128 MB request limit"
		}
		return ""
	}
	if size > maxImageBase64Bytes {
		return "it is over Claude's 10 MB image limit"
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil && (cfg.Width > maxImageEdge || cfg.Height > maxImageEdge) {
		return fmt.Sprintf("its %dx%d pixels are over Claude's %dx%d limit", cfg.Width, cfg.Height, maxImageEdge, maxImageEdge)
	}
	return ""
}

func bareMediaType(value string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return mediaType
}

// sizedLoader reloads each upload through loader and clears the media type of one Claude
// would refuse for its size: llm.ProjectContentPart then finds no capability for it and
// keeps its stored text, the same rendering a type the model does not read gets.
type sizedLoader struct{ loader llm.ContentPartLoader }

func (l sizedLoader) LoadContentPart(ctx context.Context, tenant, owner, id string) (llm.VerifiedContentPart, error) {
	part, err := l.loader.LoadContentPart(ctx, tenant, owner, id)
	if err == nil && tooLarge(bareMediaType(part.MIMEType), part.Bytes) != "" {
		part.MIMEType = ""
	}
	return part, err
}
