package openai_compat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// chunkBuffer bounds the channel so the stream goroutine can run a little ahead
// of a momentarily-slow consumer without blocking on every delta. The consumer
// MUST still drain (llm.Client doc-comment) — this is throughput, not safety.
const chunkBuffer = 16

var errStreamMissingFinishReason = errors.New("openai_compat: stream ended without finish_reason")

// Client is the handrolled OpenAI-compatible SSE streaming client. It implements
// llm.Client.Stream against an OpenRouter-shaped /chat/completions endpoint. The
// API key lives in cfg and is written ONLY onto the Authorization header at
// request-build time (D-28) — never logged, never serialized, never an attr.
type Client struct {
	cfg        llm.Config
	httpClient *http.Client
}

var _ llm.Client = (*Client)(nil)

// New builds a Client from the resolved llm.Config. The HTTP client carries a
// connect timeout on the dialer but NO http.Client.Timeout: the total timeout
// rides the request ctx (D-19 — Client.Timeout counts body-read time and would
// abort a long healthy stream). DisableKeepAlives keeps goleak order-independent
// (the default transport's persistConn goroutines otherwise linger until
// IdleConnTimeout and trip goleak on short test subsets — PATTERNS / Req#3).
func New(cfg llm.Config) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: time.Duration(cfg.ConnectTimeoutSec) * time.Second,
				}).DialContext,
				DisableKeepAlives: true,
			},
		},
	}
}

// wireRequest is the OpenAI chat-completions request body (D-16/D-20). It sends
// tool_choice:"auto" and provider.data_collection:"deny"; it does NOT send
// usage:{include} or stream_options:{include_usage} — both are deprecated no-ops
// on OpenRouter (RESEARCH State of the Art).
type wireRequest struct {
	Model       string         `json:"model"`
	Messages    []llm.Message  `json:"messages"`
	Tools       []llm.ToolDef  `json:"tools,omitempty"`
	ToolChoice  string         `json:"tool_choice,omitempty"`
	Temperature float64        `json:"temperature"`
	MaxTokens   int            `json:"max_tokens"`
	Reasoning   *wireReasoning `json:"reasoning,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Stream      bool           `json:"stream"`
	Provider    providerObj    `json:"provider"`
}

type wireReasoning struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Exclude   *bool  `json:"exclude,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

type providerObj struct {
	DataCollection string `json:"data_collection"`
}

// Stream opens a streamed chat-completion and returns a channel of llm.Chunk.
// The caller MUST drain the channel (or cancel ctx) — an undrained channel keeps
// the read goroutine and the HTTP connection alive (llm.Client doc-comment).
//
// On a non-2xx response Stream returns (nil, *HTTPError) with the body read and
// Retry-After parsed; the wire does ZERO retries (Req#4). A request-build or
// transport error is returned synchronously. Once the request is in flight, the
// parse runs in ONE goroutine that closes the channel on [DONE], EOF, or
// ctx-cancel; cancellation rides http.NewRequestWithContext so resp.Body.Read
// unblocks within ~100ms and the goroutine returns with zero leak (Req#3).
func (c *Client) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	wire := c.buildWireRequest(req)
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	reasoningtrace.Record("openai_compat_request", map[string]any{
		"base_url":       c.cfg.BaseURL,
		"model":          wire.Model,
		"session_id":     wire.SessionID,
		"tool_choice":    wire.ToolChoice,
		"tools_count":    len(wire.Tools),
		"max_tokens":     wire.MaxTokens,
		"reasoning":      wire.Reasoning,
		"wire_body_json": string(body),
		"trace_file":     reasoningtrace.Path(),
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey) // set ONLY here — D-28
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v) // HTTP-Referer + X-Title attribution — D-20
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		herr := newHTTPError(resp)
		_ = resp.Body.Close()
		return nil, herr
	}
	reasoningtrace.Record("openai_compat_response_start", map[string]any{
		"status":       resp.StatusCode,
		"content_type": resp.Header.Get("Content-Type"),
	})

	out := make(chan llm.Chunk, chunkBuffer)
	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()
		emit := func(ch llm.Chunk) bool {
			select {
			case out <- ch:
				return true
			case <-ctx.Done():
				return false
			}
		}
		res, parseErr := parseSSE(resp.Body, emit)
		errString := ""
		if parseErr != nil {
			errString = parseErr.Error()
		}
		reasoningtrace.Record("openai_compat_stream_done", map[string]any{
			"finish_reason":     res.finishReason,
			"has_usage":         res.hasUsage,
			"prompt_tokens":     res.usage.PromptTokens,
			"completion_tokens": res.usage.CompletionTokens,
			"cached_tokens":     res.usage.CachedTokens,
			"parse_error":       errString,
		})
		// Surface the captured usage to the agent through the provider-neutral
		// channel as a trailing Usage chunk (Req#12 — the llm.Client interface
		// has no other way to carry the final token+cost summary). Omitted when
		// the provider sent no usage object.
		if res.hasUsage {
			u := res.usage.toLLMUsage()
			emit(llm.Chunk{Usage: &u})
		}
		switch {
		case parseErr != nil:
			emit(llm.Chunk{Err: parseErr})
		case res.finishReason == "":
			emit(llm.Chunk{Err: errStreamMissingFinishReason})
		}
	}()
	return out, nil
}

// buildWireRequest projects an llm.Request onto the OpenAI wire body. Messages
// pass through by value (the wire layer never mutates the caller's slice — the
// fuller immutability assertion lives in Plan 04, Req#13).
func (c *Client) buildWireRequest(req llm.Request) wireRequest {
	choice := req.ToolChoice
	if choice == "" {
		choice = "auto" // byte-identical default for every existing caller (landmine #8)
	}
	tools := req.Tools
	if choice == "none" {
		tools = nil // omitempty drops the tools key → forces prose, no phantom tool-call-in-text
	}
	return wireRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Tools:       tools,
		ToolChoice:  choice,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Reasoning:   buildWireReasoning(req.Reasoning),
		SessionID:   req.SessionID,
		Stream:      true,
		Provider:    providerObj{DataCollection: "deny"},
	}
}

func buildWireReasoning(r llm.ReasoningConfig) *wireReasoning {
	if r.Empty() {
		return nil
	}
	return &wireReasoning{
		Effort:    string(r.Effort),
		MaxTokens: r.MaxTokens,
		Exclude:   r.Exclude,
		Enabled:   r.Enabled,
	}
}
