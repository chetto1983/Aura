package llm

import (
	"bufio"
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
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// OpenAIConfig holds configuration for the OpenAI-compatible client.
type OpenAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

// NewOpenAIClient creates a new OpenAI-compatible HTTP client.
func NewOpenAIClient(cfg OpenAIConfig) *OpenAIClient {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	return &OpenAIClient{
		apiKey:     cfg.APIKey,
		baseURL:    baseURL,
		model:      cfg.Model,
		httpClient: &http.Client{},
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

type chatMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content,omitempty"`
	ToolCalls  []toolCallJSON `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      messageResponseJSON `json:"message"`
		FinishReason string              `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type messageResponseJSON struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []toolCallJSON `json:"tool_calls,omitempty"`
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
			ToolCalls []toolCallDeltaJSON `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	// Usage chunk arrives at end-of-stream when stream_options.include_usage
	// is set. Empty Choices in that final chunk signals it's the usage
	// summary, not a content delta.
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
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

// Send makes a non-streaming call to the LLM.
func (c *OpenAIClient) Send(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}

	chatReq := chatRequest{
		Model:       model,
		Temperature: req.Temperature,
		Stream:      false,
	}
	applyReasoning(&chatReq, req.ReasoningEffort)
	chatReq.Tools = convertToolDefinitions(req.Tools)
	for _, m := range req.Messages {
		chatReq.Messages = append(chatReq.Messages, convertMessage(m))
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return Response{}, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

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

	return Response{
		Content:      msg.Content,
		HasToolCalls: len(toolCalls) > 0,
		ToolCalls:    toolCalls,
		Usage: TokenUsage{
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:      chatResp.Usage.TotalTokens,
		},
	}, nil
}

// Stream makes a streaming call to the LLM, returning a channel of tokens.
func (c *OpenAIClient) Stream(ctx context.Context, req Request) (<-chan Token, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}

	chatReq := chatRequest{
		Model:         model,
		Temperature:   req.Temperature,
		Stream:        true,
		StreamOptions: &streamOptionsJSON{IncludeUsage: true},
	}
	applyReasoning(&chatReq, req.ReasoningEffort)
	chatReq.Tools = convertToolDefinitions(req.Tools)
	for _, m := range req.Messages {
		chatReq.Messages = append(chatReq.Messages, convertMessage(m))
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &APIError{StatusCode: resp.StatusCode, Body: redact(string(respBody))}
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

// readSSEStream reads Server-Sent Events from the response body.
//
// Tool-call streaming protocol (OpenAI-compat): the model emits a series
// of delta chunks where the first chunk for each tool call slot carries
// id/type/function.name and possibly a leading function.arguments
// fragment, and subsequent chunks for the same `index` carry only
// further function.arguments fragments. We accumulate per-index state
// here so the caller never has to reassemble partial argument JSON.
// On terminal chunk (FinishReason set or [DONE]) we materialize the
// accumulated state as fully-parsed []ToolCall and emit it on the final
// Done=true token.
func (c *OpenAIClient) readSSEStream(body io.ReadCloser, ch chan<- Token) {
	defer close(ch)
	defer body.Close()

	type accum struct {
		id      string
		name    string
		argsBuf strings.Builder
	}
	toolBuf := map[int]*accum{}
	var indices []int // preserve insertion order so emitted ToolCalls are stable
	var usage TokenUsage

	finish := func() {
		if len(toolBuf) == 0 {
			ch <- Token{Done: true, Usage: usage}
			return
		}
		calls := make([]ToolCall, 0, len(toolBuf))
		for _, idx := range indices {
			a := toolBuf[idx]
			args, err := parseToolCallArguments(a.name, a.argsBuf.String())
			if err != nil {
				ch <- Token{Err: err, Done: true}
				return
			}
			calls = append(calls, ToolCall{ID: a.id, Name: a.name, Arguments: args})
		}
		ch <- Token{ToolCalls: calls, Usage: usage, Done: true}
	}

	scanner := bufio.NewScanner(body)
	// Default scanner buffer (64KB) is too small for chunked streams that
	// occasionally land on a single oversized line.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			finish()
			return
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		// End-of-stream usage chunk: empty Choices, populated Usage.
		if chunk.Usage != nil {
			usage = TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.Delta.Content != "" {
			ch <- Token{Content: choice.Delta.Content}
		}
		for _, tc := range choice.Delta.ToolCalls {
			a, ok := toolBuf[tc.Index]
			if !ok {
				a = &accum{}
				toolBuf[tc.Index] = a
				indices = append(indices, tc.Index)
			}
			if tc.ID != "" {
				a.id = tc.ID
			}
			if tc.Function.Name != "" {
				a.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				a.argsBuf.WriteString(tc.Function.Arguments)
			}
		}
		// Do not finish on finish_reason alone. OpenAI-compatible streams can
		// send the usage summary in a later empty-choices chunk, followed by
		// [DONE]. Returning here would drop token/cost accounting for providers
		// that honor stream_options.include_usage.
	}

	if err := scanner.Err(); err != nil {
		ch <- Token{Err: fmt.Errorf("stream read: %w", err), Done: true}
		return
	}
	finish()
}
