// Package chatgpt adapts ChatGPT plan usage to Aura's LLM interface.
package chatgpt

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

// BaseURL is the only supported public endpoint for ChatGPT plan usage.
const BaseURL = "https://api.openai.com/v1"

// TokenSource resolves the selected account's current OAuth access token.
type TokenSource interface {
	AccessToken(context.Context) (string, error)
}

// Client streams Responses requests with account credentials instead of API keys.
type Client struct {
	cfg        llm.Config
	tokens     TokenSource
	httpClient *http.Client
	baseURL    string
	responses  responses.ResponseService
}

var _ llm.Client = (*Client)(nil)

// New uses the fixed public endpoint and leaves retries to Aura's agent loop.
func New(cfg llm.Config, tokens TokenSource) *Client {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:       (&net.Dialer{Timeout: time.Duration(cfg.ConnectTimeoutSec) * time.Second}).DialContext,
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return newClient(cfg, tokens, client, BaseURL)
}

func newClient(cfg llm.Config, tokens TokenSource, client *http.Client, baseURL string) *Client {
	return &Client{cfg: cfg, tokens: tokens, httpClient: client, baseURL: baseURL,
		responses: responses.NewResponseService(
			option.WithBaseURL(baseURL), option.WithHTTPClient(client), option.WithAPIKey(""),
			option.WithMaxRetries(0), option.WithMiddleware(openai_compat.StreamIdleMiddleware)),
	}
}

func accessToken(ctx context.Context, tokens TokenSource) (string, error) {
	if tokens == nil {
		return "", errors.New("chatgpt: connect your ChatGPT account first")
	}
	token, err := tokens.AccessToken(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", errors.New("chatgpt: the connected account has no access token")
	}
	return token, nil
}

// Stream refreshes credentials per request and succeeds only after response.completed.
func (c *Client) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	token, err := accessToken(ctx, c.tokens)
	if err != nil {
		return nil, err
	}
	params, err := c.buildRequest(ctx, req, token)
	if err != nil {
		return nil, err
	}
	streamCtx, firedIdle, cleanup := openai_compat.StreamIdleContext(ctx, time.Duration(c.cfg.StreamIdleTimeoutSec)*time.Second)
	stream := c.responses.NewStreaming(streamCtx, params, option.WithAPIKey(token))
	if err := stream.Err(); err != nil {
		_ = stream.Close()
		cleanup()
		return nil, sanitizeError(err, token)
	}
	out := make(chan llm.Chunk, 16)
	go func() {
		defer close(out)
		defer cleanup()
		defer func() { _ = stream.Close() }()
		consumeStream(ctx, stream, firedIdle, out, token)
	}()
	return out, nil
}
