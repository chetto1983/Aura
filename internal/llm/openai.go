package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIClient implements Client using an OpenAI-compatible HTTP API.
type OpenAIClient struct {
	apiKey         string
	baseURL        string
	model          string
	httpClient     *http.Client
	cacheEnabled   bool // whether to inject cache_control on requests
	cacheSupported bool // whether the provider heuristic confirmed cache support
}

// OpenAIConfig holds configuration for the OpenAI-compatible client.
type OpenAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	// CacheEnabled, when true, injects Anthropic-style cache_control markers
	// on the static system-message prefix. Has no effect when the configured
	// provider is not heuristically recognised as cache-capable.
	CacheEnabled bool
}

// NewOpenAIClient creates a new OpenAI-compatible HTTP client.
func NewOpenAIClient(cfg OpenAIConfig) *OpenAIClient {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	supported := false
	if cfg.CacheEnabled {
		supported = DetectCacheSupport(baseURL, cfg.Model)
		if !supported {
			logCacheSkip(baseURL, cfg.Model)
		}
	}
	return &OpenAIClient{
		apiKey:         cfg.APIKey,
		baseURL:        baseURL,
		model:          cfg.Model,
		httpClient:     &http.Client{},
		cacheEnabled:   cfg.CacheEnabled && supported,
		cacheSupported: supported,
	}
}

type chatRequest struct {
	Model           string             `json:"model"`
	Messages        []chatMessage      `json:"messages"`
	Temperature     *float64           `json:"temperature,omitempty"`
	Stream          bool               `json:"stream,omitempty"`
	StreamOptions   *streamOptionsJSON `json:"stream_options,omitempty"`
	Tools           []toolWrapper      `json:"tools,omitempty"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	Reasoning       *reasoningJSON     `json:"reasoning,omitempty"`
}

// applyReasoning fills both wire shapes from a single user-facing value.
// Accepted forms:
//
//	""                                    → no reasoning fields emitted
//	"true" / "on" / "enabled" / "yes"     → {reasoning: {enabled: true}}
//	"low" / "medium" / "high" / "xhigh"   → top-level reasoning_effort + {reasoning: {enabled: true, effort: <v>}}
//	"minimal"                             → top-level reasoning_effort + {reasoning: {enabled: true, effort: "minimal"}}
//	"none" / "off" / "disabled" / "false" → no reasoning fields emitted
//
// Anything else falls through as an effort string so future provider values
// (e.g. a new "ultra") are forwarded verbatim.
func applyReasoning(req *chatRequest, raw string) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" || v == "none" || v == "off" || v == "disabled" || v == "false" {
		return
	}
	enabledTrue := true
	if v == "true" || v == "on" || v == "enabled" || v == "yes" {
		req.Reasoning = &reasoningJSON{Enabled: &enabledTrue}
		return
	}
	// Effort-level value. Mirror to both wire shapes so the request is
	// portable across providers.
	req.ReasoningEffort = v
	req.Reasoning = &reasoningJSON{Enabled: &enabledTrue, Effort: v}
}

// reasoningJSON is OpenRouter's / Anthropic's nested reasoning object.
// Two shapes per OpenRouter docs:
//
//	{"reasoning": {"enabled": true}}        — turn reasoning on, provider default depth
//	{"reasoning": {"effort": "high"}}        — explicit depth (low/medium/high/xhigh)
//
// DeepSeek V4 Flash accepts the enabled shape (your curl example) and the
// effort shape (high or xhigh only). We emit both alongside the top-level
// reasoning_effort so the same request body works on OpenAI (top-level
// only), OpenRouter (both accepted), and Anthropic-shaped passthroughs.
// Providers without reasoning support ignore unknown fields.
type reasoningJSON struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Effort  string `json:"effort,omitempty"`
}

// streamOptionsJSON enables the OpenAI 1.0+ usage-in-stream feature.
// Without it, streaming responses omit token counts entirely, which
// breaks budget tracking. Providers that don't recognize the field
// ignore it (we degrade to missing-usage in that case).
type streamOptionsJSON struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatMessage is the wire shape for a single message in the conversation.
// CacheBreakpoint controls MarshalJSON: when true the content is emitted as
// a content-block array with a cache_control marker instead of a plain string.
// The json:"-" tag prevents the field from appearing in any fallback marshal path.
type chatMessage struct {
	Role            string         `json:"role"`
	Content         *string        `json:"content,omitempty"`
	ToolCalls       []toolCallJSON `json:"tool_calls,omitempty"`
	ToolCallID      string         `json:"tool_call_id,omitempty"`
	CacheBreakpoint bool           `json:"-"`
}

type chatResponse struct {
	Choices []struct {
		Message      messageResponseJSON `json:"message"`
		FinishReason string              `json:"finish_reason"`
	} `json:"choices"`
	Usage usageJSON `json:"usage"`
}

// usageJSON covers the older OpenAI shape, the reasoning-aware shape, and the
// provider-side prompt-cache shape. prompt_tokens_details.cached_tokens is the
// OpenAI/OpenRouter field; cache_read_input_tokens is the Anthropic field.
type usageJSON struct {
	PromptTokens           int                         `json:"prompt_tokens"`
	CompletionTokens       int                         `json:"completion_tokens"`
	TotalTokens            int                         `json:"total_tokens"`
	CompletionTokensDetail *completionTokensDetailJSON `json:"completion_tokens_details,omitempty"`
	PromptTokensDetails    *promptTokensDetailsJSON    `json:"prompt_tokens_details,omitempty"`
	CacheReadInputTokens   int                         `json:"cache_read_input_tokens,omitempty"`
}

type completionTokensDetailJSON struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// messageResponseJSON carries the assistant's response. Reasoning and
// ReasoningDetails are populated by reasoning-capable providers
// (OpenRouter, OpenAI Responses-API-compatible passthroughs); everywhere
// else they stay empty. Reasoning is a plaintext blob (the model's own
// summary of its thinking), ReasoningDetails an opaque structured slice
// (summary, encrypted_content, text) the model expects to round-trip on
// the next turn — surface them so callers that want full fidelity can
// keep them around.
type messageResponseJSON struct {
	Role             string          `json:"role"`
	Content          string          `json:"content"`
	ToolCalls        []toolCallJSON  `json:"tool_calls,omitempty"`
	Reasoning        string          `json:"reasoning,omitempty"`
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
}

type toolWrapper struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type toolCallJSON struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function toolCallFunctionJSON `json:"function"`
}

type toolCallFunctionJSON struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string              `json:"content"`
			Reasoning string              `json:"reasoning"`
			ToolCalls []toolCallDeltaJSON `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	// Usage chunk arrives at end-of-stream when stream_options.include_usage
	// is set. Empty Choices in that final chunk signals it's the usage
	// summary, not a content delta.
	Usage *struct {
		PromptTokens           int                         `json:"prompt_tokens"`
		CompletionTokens       int                         `json:"completion_tokens"`
		TotalTokens            int                         `json:"total_tokens"`
		CompletionTokensDetail *completionTokensDetailJSON `json:"completion_tokens_details,omitempty"`
		PromptTokensDetails    *promptTokensDetailsJSON    `json:"prompt_tokens_details,omitempty"`
		CacheReadInputTokens   int                         `json:"cache_read_input_tokens,omitempty"`
	} `json:"usage"`
}

// toolCallDeltaJSON is the per-chunk shape OpenAI emits for tool-call
// streaming. The first chunk for a given index typically carries id,
// type, function.name, and a (possibly empty) function.arguments prefix;
// subsequent chunks carry only function.arguments fragments that the
// reader concatenates. Multiple tool calls in the same response are
// distinguished by `index`.
type toolCallDeltaJSON struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// isCacheDecline reports whether a provider HTTP status signals rejection of
// the cache_control content-block format, so the caller can retry without markers.
func isCacheDecline(code int) bool {
	return code == http.StatusBadRequest || code == http.StatusUnprocessableEntity
}

// convertMessages converts a []Message to []chatMessage, allocating a fresh
// slice so callers that inject cache markers don't affect the input.
func convertMessages(msgs []Message) []chatMessage {
	out := make([]chatMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, convertMessage(m))
	}
	return out
}

// buildHTTPRequest marshals chatReq and returns an authenticated POST request
// pointed at the chat completions endpoint.
func (c *OpenAIClient) buildHTTPRequest(ctx context.Context, chatReq chatRequest) (*http.Request, error) {
	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+c.apiKey)
	return r, nil
}

// sendHTTP executes a non-streaming request and fully parses the response,
// including optional prompt-cache token counts.
func (c *OpenAIClient) sendHTTP(ctx context.Context, chatReq chatRequest) (Response, error) {
	httpReq, err := c.buildHTTPRequest(ctx, chatReq)
	if err != nil {
		return Response{}, err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return Response{}, &APIError{StatusCode: resp.StatusCode, Body: redact(string(respBody))}
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return Response{}, fmt.Errorf("decoding response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return Response{}, fmt.Errorf("no choices in response")
	}

	msg := chatResp.Choices[0].Message
	toolCalls, err := parseToolCalls(msg.ToolCalls)
	if err != nil {
		return Response{}, err
	}

	usage := TokenUsage{
		PromptTokens:     chatResp.Usage.PromptTokens,
		CompletionTokens: chatResp.Usage.CompletionTokens,
		TotalTokens:      chatResp.Usage.TotalTokens,
	}
	if chatResp.Usage.CompletionTokensDetail != nil {
		usage.ReasoningTokens = chatResp.Usage.CompletionTokensDetail.ReasoningTokens
	}
	if chatResp.Usage.PromptTokensDetails != nil {
		usage.CacheReadTokens = chatResp.Usage.PromptTokensDetails.CachedTokens
	}
	if chatResp.Usage.CacheReadInputTokens > 0 {
		usage.CacheReadTokens = chatResp.Usage.CacheReadInputTokens
	}
	return Response{
		Content:          msg.Content,
		HasToolCalls:     len(toolCalls) > 0,
		ToolCalls:        toolCalls,
		Reasoning:        msg.Reasoning,
		ReasoningDetails: cloneRawJSON(msg.ReasoningDetails),
		Usage:            usage,
	}, nil
}

// streamHTTP opens a streaming connection and returns the open *http.Response.
// The caller owns the response body and must close it (readSSEStream does this).
func (c *OpenAIClient) streamHTTP(ctx context.Context, chatReq chatRequest) (*http.Response, error) {
	httpReq, err := c.buildHTTPRequest(ctx, chatReq)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &APIError{StatusCode: resp.StatusCode, Body: redact(string(respBody))}
	}
	return resp, nil
}

// Send makes a non-streaming call to the LLM.
// When cacheEnabled, the last system message is annotated with an
// Anthropic-style cache_control marker so the static prompt prefix is cached
// at the provider. A 400 or 422 response triggers one retry without markers
// (graceful fallback for providers that reject the format).
func (c *OpenAIClient) Send(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}
	msgs := convertMessages(req.Messages)
	if c.cacheEnabled {
		msgs = injectCacheControl(msgs)
	}
	chatReq := chatRequest{
		Model:       model,
		Temperature: req.Temperature,
		Messages:    msgs,
	}
	applyReasoning(&chatReq, req.ReasoningEffort)
	chatReq.Tools = convertToolDefinitions(req.Tools)

	result, err := c.sendHTTP(ctx, chatReq)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok && isCacheDecline(apiErr.StatusCode) && c.cacheEnabled {
			chatReq.Messages = convertMessages(req.Messages)
			result, err = c.sendHTTP(ctx, chatReq)
		}
	}
	return result, err
}

// cloneRawJSON copies a json.RawMessage so the caller cannot mutate the
// decoder's backing buffer. Returns nil for empty input.
func cloneRawJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

// Stream makes a streaming call to the LLM, returning a channel of tokens.
// Same cache-inject + 400/422-retry semantics as Send.
func (c *OpenAIClient) Stream(ctx context.Context, req Request) (<-chan Token, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}
	msgs := convertMessages(req.Messages)
	if c.cacheEnabled {
		msgs = injectCacheControl(msgs)
	}
	chatReq := chatRequest{
		Model:         model,
		Temperature:   req.Temperature,
		Stream:        true,
		StreamOptions: &streamOptionsJSON{IncludeUsage: true},
		Messages:      msgs,
	}
	applyReasoning(&chatReq, req.ReasoningEffort)
	chatReq.Tools = convertToolDefinitions(req.Tools)

	resp, err := c.streamHTTP(ctx, chatReq)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok && isCacheDecline(apiErr.StatusCode) && c.cacheEnabled {
			chatReq.Messages = convertMessages(req.Messages)
			resp, err = c.streamHTTP(ctx, chatReq)
		}
		if err != nil {
			return nil, err
		}
	}
	ch := make(chan Token, 64)
	go c.readSSEStream(resp.Body, ch)
	return ch, nil
}

func convertToolDefinitions(defs []ToolDefinition) []toolWrapper {
	if len(defs) == 0 {
		return nil
	}
	tools := make([]toolWrapper, 0, len(defs))
	for _, def := range defs {
		tools = append(tools, toolWrapper{
			Type:     "function",
			Function: functionDef(def),
		})
	}
	return tools
}

func convertMessage(m Message) chatMessage {
	msg := chatMessage{
		Role:       m.Role,
		ToolCallID: m.ToolCallID,
	}
	if m.Content != "" || (m.Role != "assistant" && len(m.ToolCalls) == 0) {
		content := m.Content
		msg.Content = &content
	}
	if len(m.ToolCalls) > 0 {
		msg.ToolCalls = make([]toolCallJSON, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			args, err := json.Marshal(tc.Arguments)
			if err != nil {
				args = []byte("{}")
			}
			msg.ToolCalls = append(msg.ToolCalls, toolCallJSON{
				ID:   tc.ID,
				Type: "function",
				Function: toolCallFunctionJSON{
					Name:      tc.Name,
					Arguments: string(args),
				},
			})
		}
	}
	return msg
}

func parseToolCalls(calls []toolCallJSON) ([]ToolCall, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	result := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		args, err := parseToolCallArguments(call.Function.Name, call.Function.Arguments)
		if err != nil {
			return nil, err
		}
		result = append(result, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: args,
		})
	}
	return result, nil
}

func parseToolCallArguments(name, raw string) (map[string]any, error) {
	args := map[string]any{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return args, nil
	}
	if err := decodeToolCallArguments(raw, &args); err == nil {
		return args, nil
	}

	repaired := repairJSONClosers(raw)
	if repaired != raw {
		args = map[string]any{}
		if err := decodeToolCallArguments(repaired, &args); err == nil {
			return args, nil
		}
	}

	return nil, fmt.Errorf("parsing tool call %s arguments: %w", name, json.Unmarshal([]byte(raw), &args))
}

func decodeToolCallArguments(raw string, out *map[string]any) error {
	if err := json.Unmarshal([]byte(raw), out); err == nil {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	return dec.Decode(out)
}

func repairJSONClosers(raw string) string {
	var b strings.Builder
	stack := make([]rune, 0, 8)
	inString := false
	escaped := false

	for _, r := range raw {
		if inString {
			b.WriteRune(r)
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			b.WriteRune(r)
			inString = true
		case '{':
			b.WriteRune(r)
			stack = append(stack, '}')
		case '[':
			b.WriteRune(r)
			stack = append(stack, ']')
		case '}', ']':
			stack = closeJSONStackForRune(&b, stack, r)
		default:
			b.WriteRune(r)
		}
	}
	if inString {
		b.WriteRune('"')
	}
	for i := len(stack) - 1; i >= 0; i-- {
		b.WriteRune(stack[i])
	}
	return b.String()
}

func closeJSONStackForRune(b *strings.Builder, stack []rune, closer rune) []rune {
	for len(stack) > 0 && stack[len(stack)-1] != closer {
		b.WriteRune(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
	}
	if len(stack) > 0 && stack[len(stack)-1] == closer {
		b.WriteRune(closer)
		return stack[:len(stack)-1]
	}
	b.WriteRune(closer)
	return stack
}
