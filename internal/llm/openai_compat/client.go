package openai_compat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// chunkBuffer bounds the channel so the stream goroutine can run a little ahead
// of a momentarily-slow consumer without blocking on every delta. The consumer
// MUST still drain (llm.Client doc-comment) — this is throughput, not safety.
const chunkBuffer = 16

var errStreamMissingFinishReason = errors.New("openai_compat: stream ended without finish_reason")

// ErrStreamIdleTimeout marks a stream that opened cleanly but then stalled — no
// chunk arrived within the per-chunk idle window (B-08). It is a RETRYABLE transport
// stall (the agent's stream classifier treats it like ECONNRESET), distinct from the
// whole-call timeout that rides the request ctx.
var ErrStreamIdleTimeout = errors.New("openai_compat: stream idle timeout (no chunk within the idle window)")

// Client is the handrolled OpenAI-compatible SSE streaming client. It implements
// llm.Client.Stream against an OpenRouter-shaped /chat/completions endpoint. The
// API key lives in cfg and is written ONLY onto the Authorization header at
// request-build time (D-28) — never logged, never serialized, never an attr.
type Client struct {
	cfg        llm.Config
	httpClient *http.Client
	// streamIdleTimeout bounds the gap between successive stream chunks (B-08). >0
	// arms a per-chunk watchdog that aborts a stalled stream with ErrStreamIdleTimeout;
	// 0 disables it. Set from cfg.StreamIdleTimeoutSec in New (tests override directly).
	streamIdleTimeout time.Duration
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
		cfg:               cfg,
		streamIdleTimeout: time.Duration(cfg.StreamIdleTimeoutSec) * time.Second,
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
// usage:{include} — a deprecated no-op on OpenRouter (RESEARCH State of the
// Art). stream_options:{include_usage} follows the same OpenRouter-no-op rule
// EXCEPT on the llama.cpp target: llama.cpp only emits a usage object on the
// final stream chunk when include_usage is explicitly requested, and that
// object is the sole source of the cockpit CONTESTO/CACHE gauges — so
// buildWireRequest sets it there (see the ReasoningTarget branch below), while
// OpenRouter (which always sends usage) keeps the wire byte-unchanged.
type wireRequest struct {
	Model       string        `json:"model"`
	Messages    []wireMessage `json:"messages"`
	Tools       []llm.ToolDef `json:"tools,omitempty"`
	ToolChoice  string        `json:"tool_choice,omitempty"`
	Temperature float64       `json:"temperature"`
	// MaxTokens is omitted when non-positive, which is how a caller asks for the model's
	// own output ceiling instead of one this process invented. Only the summarizer does
	// (internal/conversations/compaction_transcript.go): an output cap there cuts the
	// summary mid-section — a reasoning model spends the cap on reasoning first — the
	// stream ends finish_reason="length", and the compaction that was meant to CONDENSE
	// history hard-drops it instead. Every other caller sets it from config, so their wire
	// is byte-unchanged.
	MaxTokens int            `json:"max_tokens,omitempty"`
	Reasoning *wireReasoning `json:"reasoning,omitempty"`
	// ChatTemplateKwargs and ThinkingBudgetTokens are the llama-server per-request
	// reasoning controls (spike 095). They are populated ONLY on a llama.cpp target
	// (Reasoning is left nil there — llama-server ignores the OpenRouter object); on
	// the OpenRouter path they stay nil and omitempty drops them, so that wire is
	// byte-unchanged.
	ChatTemplateKwargs   map[string]any     `json:"chat_template_kwargs,omitempty"`
	ThinkingBudgetTokens *int               `json:"thinking_budget_tokens,omitempty"`
	SessionID            string             `json:"session_id,omitempty"`
	Stream               bool               `json:"stream"`
	Provider             providerObj        `json:"provider"`
	StreamOptions        *wireStreamOptions `json:"stream_options,omitempty"`
	// Transforms carries OpenRouter's ["middle-out"] overflow belt (fix-plan
	// 1.11): a LOSSY provider-side truncation with no tool-pair awareness, so it
	// is explicitly NOT a compaction mechanism — Aura's own context management
	// (amendment #21 ladder, 1.10 estimator) stays primary. It exists only as a
	// last-resort net against a hard 400 "context length exceeded" when the
	// local trim under-counts on the OpenRouter path. Set ONLY when BOTH the
	// resolved target is OpenRouter AND AURA_LLM_OPENROUTER_MIDDLE_OUT is on
	// (buildWireRequest); everywhere else nil, and omitempty drops the key —
	// llama.cpp and default-config OpenRouter both stay byte-unchanged.
	Transforms []string `json:"transforms,omitempty"`
}

// wireMessage mirrors llm.Message on the wire but GUARANTEES a content field on every
// non-assistant message. llm.Message.Content is `omitempty`, so an empty tool/user/
// system message serializes WITHOUT a content key — which strict OpenAI servers
// (llama.cpp) reject with 400 "All non-assistant messages must contain 'content'"
// (OpenRouter silently tolerates the omission, so the bug is invisible there). The most
// common trigger is a tool that returned an empty result: its persisted tool turn
// rehydrates as a content-less tool message. Assistant messages keep OMITTING empty
// content — they are carried by tool_calls and content is optional for them per the
// OpenAI spec. Content is a *string: nil omits the key, &"" emits "content":"".
type wireMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content,omitempty"`
	ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

// toWireMessages projects caller messages onto the wire, guaranteeing a content field on
// every non-assistant message (empty content becomes "content":"" instead of a dropped
// key). The caller's slice is never mutated.
func toWireMessages(msgs []llm.Message) []wireMessage {
	out := make([]wireMessage, len(msgs))
	for i, m := range msgs {
		wm := wireMessage{Role: m.Role, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, Name: m.Name}
		if m.Role == llm.RoleAssistant && m.Content == "" {
			// Assistant may omit content when tool_calls carry the turn (spec-optional).
			out[i] = wm
			continue
		}
		c := m.Content
		wm.Content = &c
		out[i] = wm
	}
	return out
}

// wireStreamOptions is set ONLY on the llama.cpp target (buildWireRequest) —
// see the wireRequest doc comment for why.
type wireStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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

	// A cancellable child ctx lets the idle watchdog (B-08) abort a stalled read
	// without disturbing the caller's ctx. The caller's ctx still governs delivery
	// (emit selects on it) so the terminal error chunk is delivered even after an
	// idle-cancel; cancelStream is released in the goroutine's defer on every path.
	streamCtx, cancelStream := context.WithCancel(ctx)

	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost,
		c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		cancelStream()
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if strings.TrimSpace(c.cfg.APIKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v) // HTTP-Referer + X-Title attribution — D-20
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		cancelStream()
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		herr := newHTTPError(resp)
		_ = resp.Body.Close()
		cancelStream()
		return nil, herr
	}
	reasoningtrace.Record("openai_compat_response_start", map[string]any{
		"status":       resp.StatusCode,
		"content_type": resp.Header.Get("Content-Type"),
	})

	out := make(chan llm.Chunk, chunkBuffer)
	go func() {
		defer close(out)
		defer cancelStream()
		defer func() { _ = resp.Body.Close() }()
		emit := func(ch llm.Chunk) bool {
			select {
			case out <- ch:
				return true
			case <-ctx.Done():
				return false
			}
		}
		// Idle watchdog (B-08): reset on every byte from the wire (data OR keep-alive)
		// so a long reasoning phase does not trip it; a dead connection does.
		var reader io.Reader = resp.Body
		var watchdog *idleWatchdog
		if c.streamIdleTimeout > 0 {
			watchdog = startIdleWatchdog(c.streamIdleTimeout, cancelStream)
			defer watchdog.stop()
			reader = &idleResettingReader{r: resp.Body, w: watchdog}
		}
		res, parseErr := parseSSE(reader, emit)
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
		case watchdog.firedIdle():
			// The watchdog cancelled the read; parseErr is the resulting
			// context.Canceled. Surface the retryable idle stall, not the raw cancel.
			emit(llm.Chunk{Err: ErrStreamIdleTimeout})
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
	wire := wireRequest{
		Model:       req.Model,
		Messages:    toWireMessages(req.Messages),
		Tools:       tools,
		ToolChoice:  choice,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		SessionID:   req.SessionID,
		Stream:      true,
		Provider:    providerObj{DataCollection: "deny"},
	}
	// The reasoning projection is target-aware. llama-server ignores the OpenRouter
	// reasoning:{...} object (spike 095), so it gets its own per-request fields and
	// leaves Reasoning nil; OpenRouter (and any unrecognized target) keeps today's
	// nested object UNCHANGED — xhigh/max serialize automatically via string(r.Effort).
	target := llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL)
	if target == llm.ReasoningTargetLlamaCpp {
		applyLlamaCppReasoning(&wire, req.Reasoning)
		wire.StreamOptions = &wireStreamOptions{IncludeUsage: true}
	} else {
		wire.Reasoning = buildWireReasoning(req.Reasoning)
	}
	if target == llm.ReasoningTargetOpenRouter && c.cfg.OpenRouterMiddleOut {
		wire.Transforms = []string{"middle-out"}
	}
	return wire
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

// llama.cpp per-request thinking budgets in tokens, spike-095-validated live on
// gemma-4-E2B-it-qat: Low/Mid/High mirror the llama.cpp webui, Extra sits between
// High and unlimited, and Max=-1 is unlimited. They are FIXED consts selected by a
// fixed effort symbol — no request-supplied N ever reaches the wire (T-37E-02-BUDGET)
// — and are promotable to AURA_LLM_LLAMACPP_THINKING_BUDGET_* config later without a
// contract change.
const (
	llamaCppBudgetLow   = 512
	llamaCppBudgetMid   = 2048
	llamaCppBudgetHigh  = 8192
	llamaCppBudgetExtra = 16384
	llamaCppBudgetMax   = -1
)

// applyLlamaCppReasoning translates the provider-neutral effort onto llama-server's
// per-request fields (spike 095) and NEVER sets wire.Reasoning (the OpenRouter object
// is a no-op on llama-server). OFF is the only off-switch — chat_template_kwargs:
// {enable_thinking:false} (needs --jinja); a graduated effort maps to a fixed
// thinking_budget_tokens; an empty effort (auto) emits no reasoning fields at all, so
// llama.cpp's default thinking stays on. An effort outside the 37E set (e.g. minimal)
// falls through untouched — a safe no-op, never a guessed budget.
func applyLlamaCppReasoning(wire *wireRequest, r llm.ReasoningConfig) {
	switch r.Effort {
	case llm.ReasoningEffortNone:
		wire.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	case llm.ReasoningEffortLow:
		wire.ThinkingBudgetTokens = new(llamaCppBudgetLow)
	case llm.ReasoningEffortMedium:
		wire.ThinkingBudgetTokens = new(llamaCppBudgetMid)
	case llm.ReasoningEffortHigh:
		wire.ThinkingBudgetTokens = new(llamaCppBudgetHigh)
	case llm.ReasoningEffortXHigh:
		wire.ThinkingBudgetTokens = new(llamaCppBudgetExtra)
	case llm.ReasoningEffortMax:
		wire.ThinkingBudgetTokens = new(llamaCppBudgetMax)
	}
}
