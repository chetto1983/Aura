package chatgpt

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

type requestMedia struct {
	uploads []responses.ResponseInputContentUnionParam
	tools   map[string][]responses.ResponseInputContentUnionParam
}

func (c *Client) projectMedia(ctx context.Context, model, token string, projection *llm.ContentProjection, tools map[string][]llm.ProjectedRequestPart) (requestMedia, error) {
	result := requestMedia{tools: map[string][]responses.ResponseInputContentUnionParam{}}
	if (projection == nil || len(projection.ReferenceIDs) == 0) && len(tools) == 0 {
		return result, nil
	}
	models, err := fetchModelWire(ctx, c.httpClient, c.baseURL, token)
	if err != nil {
		return result, err
	}
	caps := llm.ProviderContentCapabilities{Modalities: map[string]bool{"text": true}}
	for _, entry := range models {
		if entry.Slug != model || entry.Visibility != "list" {
			continue
		}
		for _, modality := range entry.InputModalities {
			if modality == "image" || modality == "file" {
				caps.Modalities[modality] = true
			}
		}
		if entry.SupportsImage {
			caps.Modalities["image"] = true
		}
	}
	if projection != nil {
		if projection.Loader == nil {
			return result, fmt.Errorf("chatgpt: content projection has no loader")
		}
		for _, id := range projection.ReferenceIDs {
			part, err := llm.ProjectContentPart(ctx, projection.Loader, projection.Principal, id, caps)
			if err != nil {
				return result, err
			}
			content, err := mediaPart(part)
			if err != nil {
				return result, err
			}
			result.uploads = append(result.uploads, content)
		}
	}
	for id, parts := range tools {
		for _, part := range parts {
			if !caps.SupportsMIME(part.MIMEType) {
				part.ReferenceOnly = true
			}
			content, err := mediaPart(part)
			if err != nil {
				return result, err
			}
			result.tools[id] = append(result.tools[id], content)
		}
	}
	return result, nil
}

func mediaPart(part llm.ProjectedRequestPart) (responses.ResponseInputContentUnionParam, error) {
	if part.ReferenceOnly {
		return textPart(part.Text), nil
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(part.MIMEType))
	if err != nil {
		return responses.ResponseInputContentUnionParam{}, fmt.Errorf("chatgpt: invalid media type")
	}
	if len(part.Bytes) == 0 {
		return responses.ResponseInputContentUnionParam{}, fmt.Errorf("chatgpt: media has no content")
	}
	if strings.HasPrefix(mediaType, "image/") {
		return responses.ResponseInputContentUnionParam{OfInputImage: &responses.ResponseInputImageParam{Detail: "auto", ImageURL: openai.String("data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(part.Bytes))}}, nil
	}
	return responses.ResponseInputContentUnionParam{}, fmt.Errorf("chatgpt: native %s input is unsupported by ChatGPT plan usage", mediaType)
}
