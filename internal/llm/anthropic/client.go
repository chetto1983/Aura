// Package anthropic streams Aura turns on Claude through the official SDK, on the house
// key read from Settings at every call.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/packages/ssestream"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

// ErrNoKey refuses a call before it dials: no house key is set in Settings.
var ErrNoKey = errors.New("anthropic: no Claude API key is set; add ANTHROPIC_API_KEY in Settings")

// KeySource reads the house key for one call. It is read on every call, so a key the
// admin rotates in Settings takes effect on the next request.
type KeySource func(context.Context) (string, error)

// Client streams Beta Messages requests on the house key.
type Client struct {
	cfg        llm.Config
	key        KeySource
	httpClient *http.Client
	baseURL    string
	// logger is resolved at each request, so the request line follows the default logger
	// the process installs after the client was built.
	logger func() *slog.Logger
	caps   capabilityCache
}

var _ llm.Client = (*Client)(nil)

// New uses the one permitted endpoint and leaves retries to Aura's agent loop.
func New(cfg llm.Config, key KeySource) *Client {
	return newClient(cfg, key, newHTTPClient(cfg.ConnectTimeoutSec), llm.AnthropicBaseURL)
}

func newClient(cfg llm.Config, key KeySource, hc *http.Client, baseURL string) *Client {
	return &Client{cfg: cfg, key: key, httpClient: hc, baseURL: baseURL, logger: slog.Default}
}

func readHouseKey(ctx context.Context, source KeySource) (string, error) {
	if source == nil {
		return "", ErrNoKey
	}
	key, err := source(ctx)
	if err != nil {
		return "", fmt.Errorf("anthropic: read the Claude API key: %w", err)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ErrNoKey
	}
	return key, nil
}

// Stream sends one request and succeeds only after message_stop.
func (c *Client) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	key, err := readHouseKey(ctx, c.key)
	if err != nil {
		return nil, err
	}
	media, err := c.projectMedia(ctx, key, c.model(req), req)
	if err != nil {
		return nil, err
	}
	built, err := c.buildRequest(req, media)
	if err != nil {
		return nil, err
	}
	return c.open(ctx, key, built)
}

func (c *Client) open(ctx context.Context, key string, built builtRequest) (<-chan llm.Chunk, error) {
	streamCtx, firedIdle, cleanup := openai_compat.StreamIdleContext(ctx, c.cfg.StreamIdleTimeout())
	sdk := newSDK(key, c.httpClient, c.baseURL)
	stream := sdk.Beta.Messages.NewStreaming(streamCtx, built.params)
	if err := stream.Err(); err != nil {
		_ = stream.Close()
		cleanup()
		return nil, providerError(err, key)
	}
	out := make(chan llm.Chunk, 16)
	go func() {
		defer close(out)
		defer cleanup()
		defer func() { _ = stream.Close() }()
		c.consume(ctx, stream, firedIdle, out, key, built)
	}()
	return out, nil
}

type messageStream = ssestream.Stream[anthropic.BetaRawMessageStreamEventUnion]
