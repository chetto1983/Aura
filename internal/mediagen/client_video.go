package mediagen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/openai/openai-go/v3/option"
)

// FrameReference is one entry of VideoRequest.FrameImages: a first- or
// last-frame image for image-to-video generation.
type FrameReference struct {
	Type      string   `json:"type"`
	ImageURL  ImageURL `json:"image_url"`
	FrameType string   `json:"frame_type"`
}

// VideoRequest is a caller-ready video generation call: every field has
// already been clamped to what the target model declares. It has no custom
// MarshalJSON, so the SDK's fallback encoding/json serialization applies the
// field tags directly, omitting every unset optional field.
type VideoRequest struct {
	Model           string           `json:"model"`
	Prompt          string           `json:"prompt"`
	Duration        int              `json:"duration,omitempty"`
	Resolution      string           `json:"resolution,omitempty"`
	AspectRatio     string           `json:"aspect_ratio,omitempty"`
	FrameImages     []FrameReference `json:"frame_images,omitempty"`
	InputReferences []ImageReference `json:"input_references,omitempty"`
	GenerateAudio   *bool            `json:"generate_audio,omitempty"`
}

// RemoteVideo is OpenRouter's video job state, from either the submit
// response or a poll. Error is set only for a terminal failure status
// (failed, expired, cancelled); OpenRouter's own error field there is a
// plain string with no documented failure code (ruling R14), so Error.Code
// is derived from Status, never parsed from that provider text.
type RemoteVideo struct {
	ID      string
	Status  Status
	CostUSD *float64
	Error   *Error
}

type videoStatusWire struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
	Usage  *struct {
		Cost *float64 `json:"cost"`
	} `json:"usage"`
}

// SubmitVideo submits a video generation job and returns its initial state.
func (c *Client) SubmitVideo(ctx context.Context, baseURL, apiKey string, req VideoRequest) (RemoteVideo, error) {
	client := sdkClient(c.http, baseURL, option.WithAPIKey(apiKey))
	var raw []byte
	if err := client.Post(ctx, "videos", req, &raw); err != nil {
		return RemoteVideo{}, classifyProviderError(err)
	}
	return decodeVideoStatus(raw)
}

// GetVideo polls a submitted job's current state.
func (c *Client) GetVideo(ctx context.Context, baseURL, apiKey, providerID string) (RemoteVideo, error) {
	id, err := validProviderID(providerID)
	if err != nil {
		return RemoteVideo{}, err
	}
	client := sdkClient(c.http, baseURL, option.WithAPIKey(apiKey))
	var raw []byte
	if err := client.Get(ctx, "videos/"+url.PathEscape(id), nil, &raw); err != nil {
		return RemoteVideo{}, classifyProviderError(err)
	}
	return decodeVideoStatus(raw)
}

// DownloadVideo streams a completed job's content, bounded to maxBytes. It
// always requests OpenRouter's own content endpoint for providerID; it never
// follows a URL the provider supplied (e.g. a poll response's
// unsigned_urls), which would hand an identity's Authorization header to
// whatever host that field named.
func (c *Client) DownloadVideo(ctx context.Context, baseURL, apiKey, providerID string, maxBytes int64) ([]byte, error) {
	id, err := validProviderID(providerID)
	if err != nil {
		return nil, err
	}
	client := sdkClient(c.http, baseURL, option.WithAPIKey(apiKey))
	var resp *http.Response
	if err := client.Get(ctx, "videos/"+url.PathEscape(id)+"/content", nil, &resp); err != nil {
		return nil, classifyProviderError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return readCapped(resp.Body, maxBytes)
}

func decodeVideoStatus(raw []byte) (RemoteVideo, error) {
	var wire videoStatusWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return RemoteVideo{}, fmt.Errorf("mediagen: decode video status: %w", err)
	}
	return remoteVideoFromWire(wire), nil
}

func remoteVideoFromWire(wire videoStatusWire) RemoteVideo {
	video := RemoteVideo{ID: wire.ID, Status: Status(wire.Status)}
	if wire.Usage != nil {
		video.CostUSD = wire.Usage.Cost
	}
	switch video.Status {
	case StatusFailed, StatusCancelled:
		video.Error = &Error{Code: "job_failed", Message: terminalMessage(wire.Error, "Video generation failed.")}
	case StatusExpired:
		video.Error = &Error{Code: "job_expired", Message: terminalMessage(wire.Error, "Video generation job expired.")}
	}
	return video
}

func terminalMessage(upstream, fallback string) string {
	if msg := boundedMessage(upstream); msg != "" {
		return msg
	}
	return fallback
}
