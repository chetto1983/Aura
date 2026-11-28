package anthropic

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/chetto1983/aura/internal/llm"
)

// imageMIMETypes are the types an image block's base64 source accepts.
var imageMIMETypes = []anthropic.BetaBase64ImageSourceMediaType{
	anthropic.BetaBase64ImageSourceMediaTypeImageJPEG,
	anthropic.BetaBase64ImageSourceMediaTypeImagePNG,
	anthropic.BetaBase64ImageSourceMediaTypeImageGIF,
	anthropic.BetaBase64ImageSourceMediaTypeImageWebP,
}

const pdfMIMEType = "application/pdf"

// capabilityCache keeps each model's content capabilities for the client's life: a
// request without media never asks, and one with media asks once per model. A failed
// read is not kept, so the next request asks again.
type capabilityCache struct {
	mu      sync.Mutex
	byModel map[string]llm.ProviderContentCapabilities
}

func (c *Client) capabilities(ctx context.Context, key, model string) (llm.ProviderContentCapabilities, error) {
	c.caps.mu.Lock()
	cached, ok := c.caps.byModel[model]
	c.caps.mu.Unlock()
	if ok {
		return cached, nil
	}
	models, err := listModels(ctx, key, c.httpClient, c.baseURL)
	if err != nil {
		return llm.ProviderContentCapabilities{}, err
	}
	caps := contentCapabilities(models, model)
	c.caps.mu.Lock()
	defer c.caps.mu.Unlock()
	if c.caps.byModel == nil {
		c.caps.byModel = map[string]llm.ProviderContentCapabilities{}
	}
	c.caps.byModel[model] = caps
	return caps, nil
}

// requestMedia is what Request.ContentProjection and Request.ToolMedia add to the
// conversation: blocks after the current user message's text, and blocks inside each
// call's tool_result.
type requestMedia struct {
	uploads []anthropic.BetaContentBlockParamUnion
	tools   map[string][]anthropic.BetaToolResultBlockParamContentUnion
}

// projectMedia turns the turn's uploads and tool media into image and document blocks
// where the model reads their type; any other part reaches it as text.
func (c *Client) projectMedia(ctx context.Context, key, model string, req llm.Request) (requestMedia, error) {
	media := requestMedia{tools: map[string][]anthropic.BetaToolResultBlockParamContentUnion{}}
	projection := req.ContentProjection
	if projection == nil {
		if p, ok := llm.ContentProjectionFromContext(ctx); ok {
			projection = &p
		}
	}
	uploads := projection != nil && len(projection.ReferenceIDs) > 0
	if uploads && projection.Loader == nil {
		return media, errors.New("anthropic: content projection has no loader")
	}
	tools := req.ToolMedia
	if tools == nil {
		tools = llm.ToolMediaFromContext(ctx).Snapshot()
	}
	if !uploads && len(tools) == 0 {
		return media, nil
	}
	caps, err := c.capabilities(ctx, key, model)
	if err != nil {
		return media, err
	}
	if uploads {
		for _, id := range projection.ReferenceIDs {
			part, err := llm.ProjectContentPart(ctx, projection.Loader, projection.Principal, id, caps)
			if err != nil {
				return media, err
			}
			if part.ReferenceOnly {
				media.uploads = append(media.uploads, textBlocks(part.Text)...)
				continue
			}
			image, document, err := mediaBlock(part)
			if err != nil {
				return media, err
			}
			media.uploads = append(media.uploads, anthropic.BetaContentBlockParamUnion{OfImage: image, OfDocument: document})
		}
	}
	for id, parts := range tools {
		for _, part := range parts {
			if part.ReferenceOnly || len(part.Bytes) == 0 || !caps.SupportsMIME(part.MIMEType) {
				media.tools[id] = append(media.tools[id], anthropic.BetaToolResultBlockParamContentUnion{OfText: &anthropic.BetaTextBlockParam{Text: unshown(part)}})
				continue
			}
			image, document, err := mediaBlock(part)
			if err != nil {
				return media, err
			}
			media.tools[id] = append(media.tools[id], anthropic.BetaToolResultBlockParamContentUnion{OfImage: image, OfDocument: document})
		}
	}
	return media, nil
}

// mediaBlock is part as an image or a PDF document block, whichever its type names;
// the capabilities admit no other type.
func mediaBlock(part llm.ProjectedRequestPart) (*anthropic.BetaImageBlockParam, *anthropic.BetaRequestDocumentBlockParam, error) {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(part.MIMEType))
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: invalid media type %q", part.MIMEType)
	}
	if len(part.Bytes) == 0 {
		return nil, nil, errors.New("anthropic: media has no content")
	}
	data := base64.StdEncoding.EncodeToString(part.Bytes)
	if mediaType == pdfMIMEType {
		return nil, &anthropic.BetaRequestDocumentBlockParam{Source: anthropic.BetaRequestDocumentBlockSourceUnionParam{
			OfBase64: &anthropic.BetaBase64PDFSourceParam{Data: data},
		}}, nil
	}
	imageType := anthropic.BetaBase64ImageSourceMediaType(mediaType)
	if !slices.Contains(imageMIMETypes, imageType) {
		return nil, nil, fmt.Errorf("anthropic: Claude reads no %s input", mediaType)
	}
	return &anthropic.BetaImageBlockParam{Source: anthropic.BetaImageBlockParamSourceUnion{
		OfBase64: &anthropic.BetaBase64ImageSourceParam{Data: data, MediaType: imageType},
	}}, nil, nil
}

// unshown is the text a tool's part becomes when the model cannot read it. The tool's
// own result announces the attachment, so the model must learn it is not there.
func unshown(part llm.ProjectedRequestPart) string {
	note := "Not shown: the current model does not read " + strings.TrimSpace(part.MIMEType) + " input"
	if name := strings.TrimSpace(part.Text); name != "" {
		note += ": " + strconv.Quote(name)
	}
	return note
}

// uploadTarget is the current user message: the one right after the loaded history when
// the builder recorded it (the last user message is then the per-round hint), else the
// last user message.
func uploadTarget(req llm.Request, needed bool) (int, error) {
	if !needed {
		return -1, nil
	}
	if n := req.LoadedHistoryMessages; n > 0 && n < len(req.Messages) && req.Messages[n].Role == llm.RoleUser {
		return n, nil
	}
	for i, message := range slices.Backward(req.Messages) {
		if message.Role == llm.RoleUser {
			return i, nil
		}
	}
	return -1, errors.New("anthropic: media requires a user message")
}
