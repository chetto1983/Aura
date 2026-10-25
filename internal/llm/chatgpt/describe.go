package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"

	"github.com/chetto1983/aura/internal/chatgptplan"
	"github.com/chetto1983/aura/internal/llm"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// Describe reads one image with the selected model on the account of the identity in ctx.
// The plan token is refused on /chat/completions (401 rejected_by_access_enforcement, measured
// on the lab VM 2026-10-25), so an image goes through Responses like a chat attachment.
//
// llm.ErrMandatoryModalityUnavailable, wrapped with the reason, means the image cannot be read
// here and asking again would get the same answer: the model reads no images, the owner has
// not connected ChatGPT or granted the plan, the model refused, or the bytes are no image.
// Every other error is a failure worth retrying.
func (c *Client) Describe(ctx context.Context, image []byte, mimeType, prompt string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(mimeType))
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return "", fmt.Errorf("%w: chatgpt: %q is not an image", llm.ErrMandatoryModalityUnavailable, mimeType)
	}
	token, err := accessToken(ctx, c.tokens)
	if errors.Is(err, chatgptplan.ErrAuthorizationRequired) || errors.Is(err, chatgptplan.ErrPlanDisabled) {
		return "", fmt.Errorf("%w: %w", llm.ErrMandatoryModalityUnavailable, err)
	}
	if err != nil {
		return "", err
	}
	models, err := fetchModelWire(ctx, c.httpClient, c.baseURL, token)
	if err != nil {
		return "", err
	}
	if !catalogCapabilities(models, c.cfg.Model).Modalities["image"] {
		return "", fmt.Errorf("%w: chatgpt: %s does not read images", llm.ErrMandatoryModalityUnavailable, c.cfg.Model)
	}
	part, err := mediaPart(llm.ProjectedRequestPart{MIMEType: mediaType, Bytes: image})
	if err != nil {
		return "", err
	}
	message := textMessage("user", "")
	message.OfMessage.Content = responses.EasyInputMessageContentUnionParam{
		OfInputItemContentList: []responses.ResponseInputContentUnionParam{textPart(prompt), part},
	}
	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(c.cfg.Model), Store: openai.Bool(false),
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: responses.ResponseInputParam{message}},
	}
	stream, firedIdle, cleanup, err := c.openStream(ctx, params, token)
	if err != nil {
		return "", err
	}
	defer cleanup()
	defer func() { _ = stream.Close() }()
	return readImageStream(stream, firedIdle, token)
}

// readImageStream is consumeStream's twin for one image. It must see refusals, which the chat
// shows as text, because a refusal indexed as what a page says would be memoized for good.
func readImageStream(stream *ssestream.Stream[responses.ResponseStreamEventUnion], firedIdle func() bool, token string) (string, error) {
	var text strings.Builder
	refused := false
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			text.WriteString(event.Delta)
		case "response.refusal.delta":
			refused = true
		case "response.failed", "response.incomplete", "error":
			return "", terminalError(event, token)
		case "response.completed":
			if refused {
				return "", fmt.Errorf("%w: chatgpt: the model refused to read the image", llm.ErrMandatoryModalityUnavailable)
			}
			return text.String(), nil
		}
	}
	return "", streamEndError(stream, firedIdle, token)
}
