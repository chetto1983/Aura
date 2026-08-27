package openai_compat

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const chunkBuffer = 16

var errStreamMissingFinishReason = errors.New("openai_compat: stream ended without finish_reason")

// ErrStreamIdleTimeout marks a stream that opened cleanly but then stalled.
var ErrStreamIdleTimeout = errors.New("openai_compat: stream idle timeout (no chunk within the idle window)")

// Client adapts the official openai-go Chat Completions stream to Aura's neutral
// interface. Only provider-specific request fields remain in this package.
type Client struct {
	cfg         llm.Config
	httpClient  *http.Client
	chat        openai.ChatCompletionService
	contentCaps llm.ContentCapabilitySource

	streamIdleTimeout time.Duration
}

var _ llm.Client = (*Client)(nil)

// New constructs an OpenAI-compatible client with provider retries disabled.
func New(cfg llm.Config) *Client {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: time.Duration(cfg.ConnectTimeoutSec) * time.Second,
			}).DialContext,
			DisableKeepAlives: true,
		},
	}
	opts := []option.RequestOption{
		option.WithBaseURL(cfg.BaseURL),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
		option.WithMiddleware(idleResponseMiddleware),
	}
	if llm.ReasoningTarget(cfg.Provider, cfg.BaseURL) == llm.ReasoningTargetOpenRouter && strings.TrimSpace(cfg.APIKey) != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	for key, value := range cfg.Headers {
		opts = append(opts, option.WithHeader(key, value))
	}
	return &Client{
		cfg:               cfg,
		httpClient:        httpClient,
		chat:              openai.NewChatCompletionService(opts...),
		contentCaps:       llm.NewContentCapabilitySource(cfg, 15*time.Minute),
		streamIdleTimeout: time.Duration(cfg.StreamIdleTimeoutSec) * time.Second,
	}
}

// Stream opens the provider stream synchronously, then translates SDK chunks on
// one goroutine. openai-go retries are explicitly disabled in New.
func (c *Client) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	params, requestOpts, nativeCount, err := c.buildSDKRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	reasoningtrace.Record("openai_compat_request", map[string]any{
		"base_url":           c.cfg.BaseURL,
		"model":              req.Model,
		"session_id":         req.SessionID,
		"tool_choice":        effectiveToolChoice(req.ToolChoice),
		"tools_count":        len(params.Tools),
		"max_tokens":         req.MaxTokens,
		"native_media_count": nativeCount,
		"trace_file":         reasoningtrace.Path(),
	})

	streamCtx, cancelStream := context.WithCancel(ctx)
	idle := &idleRequestControl{window: c.streamIdleTimeout, cancel: cancelStream}
	streamCtx = context.WithValue(streamCtx, idleRequestContextKey{}, idle)
	stream := c.chat.NewStreaming(streamCtx, params, requestOpts...)
	if err := stream.Err(); err != nil {
		_ = stream.Close()
		cancelStream()
		return nil, adaptSDKError(err)
	}

	out := make(chan llm.Chunk, chunkBuffer)
	go func() {
		defer close(out)
		defer cancelStream()
		defer idle.stop()
		defer func() { _ = stream.Close() }()
		consumeSDKStream(ctx, stream, idle, out)
	}()
	return out, nil
}

func effectiveToolChoice(choice string) string {
	if choice == "" {
		return "auto"
	}
	return choice
}
