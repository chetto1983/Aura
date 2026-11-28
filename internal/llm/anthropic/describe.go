package anthropic

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/chetto1983/aura/internal/llm"
)

// Describe reads one image with the route's model on the house key, through the same
// request and stream a chat turn takes.
//
// llm.ErrMandatoryModalityUnavailable, wrapped with the reason, means asking again would
// get the same answer: no house key is set, the model reads no image of this type, or it
// refused. Every other error, a key the store could not read included, is worth retrying.
func (c *Client) Describe(ctx context.Context, image []byte, mimeType, prompt string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(mimeType))
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return "", fmt.Errorf("%w: anthropic: %q is not an image", llm.ErrMandatoryModalityUnavailable, mimeType)
	}
	key, err := readHouseKey(ctx, c.key)
	if errors.Is(err, ErrNoKey) {
		return "", fmt.Errorf("%w: %w", llm.ErrMandatoryModalityUnavailable, err)
	}
	if err != nil {
		return "", err
	}
	caps, err := c.capabilities(ctx, key, c.cfg.Model)
	if err != nil {
		return "", err
	}
	if !caps.SupportsMIME(mediaType) {
		return "", fmt.Errorf("%w: anthropic: %s does not read %s", llm.ErrMandatoryModalityUnavailable, c.cfg.Model, mediaType)
	}
	block, _, err := mediaBlock(llm.ProjectedRequestPart{MIMEType: mediaType, Bytes: image})
	if err != nil {
		return "", err
	}
	req := llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}}}
	built, err := c.buildRequest(req, requestMedia{uploads: []anthropic.BetaContentBlockParamUnion{{OfImage: block}}})
	if err != nil {
		return "", err
	}
	stream, err := c.open(ctx, key, built)
	if err != nil {
		return "", err
	}
	return readDescription(stream)
}

// readDescription drains the stream, so its goroutine always ends, and accepts only a
// finished answer: a refusal or a cut description indexed as what an image shows would be
// kept for good.
func readDescription(stream <-chan llm.Chunk) (string, error) {
	var text strings.Builder
	var finish string
	var failure error
	for chunk := range stream {
		switch {
		case chunk.Err != nil:
			failure = chunk.Err
		case chunk.FinishReason != "":
			finish = chunk.FinishReason
		default:
			text.WriteString(chunk.Text)
		}
	}
	switch {
	case failure != nil:
		return "", failure
	case finish == llm.FinishContentFilter:
		return "", fmt.Errorf("%w: anthropic: the model refused to read the image", llm.ErrMandatoryModalityUnavailable)
	case finish != "stop":
		return "", fmt.Errorf("anthropic: the image description ended on %q", finish)
	}
	return text.String(), nil
}
