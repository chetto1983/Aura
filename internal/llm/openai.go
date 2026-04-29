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

	"github.com/aura/aura/internal/tracing"
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
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	Tools       []toolWrapper `json:"tools,omitempty"`
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
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// Send makes a non-streaming call to the LLM.
func (c *OpenAIClient) Send(ctx context.Context, req Request) (Response, error) {
	ctx, span := tracing.StartSpan(ctx, "llm", "openai.send")
	defer span.End()

	model := req.Model
	if model == "" {
		model = c.model
	}

	chatReq := chatRequest{
		Model:       model,
		Temperature: req.Temperature,
		Stream:      false,
	}
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
		return Response{}, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(respBody))
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
	ctx, span := tracing.StartSpan(ctx, "llm", "openai.stream")
	defer span.End()

	model := req.Model
	if model == "" {
		model = c.model
	}

	chatReq := chatRequest{
		Model:       model,
		Temperature: req.Temperature,
		Stream:      true,
	}
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
		return nil, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(respBody))
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
			Type: "function",
			Function: functionDef{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.Parameters,
			},
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
		args := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("parsing tool call %s arguments: %w", call.Function.Name, err)
			}
		}
		result = append(result, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: args,
		})
	}
	return result, nil
}

// readSSEStream reads Server-Sent Events from the response body.
func (c *OpenAIClient) readSSEStream(body io.ReadCloser, ch chan<- Token) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			ch <- Token{Done: true}
			return
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content
			if content != "" {
				ch <- Token{Content: content}
			}
			if chunk.Choices[0].FinishReason != nil {
				ch <- Token{Done: true}
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- Token{Err: fmt.Errorf("stream read: %w", err), Done: true}
		return
	}
	ch <- Token{Done: true}
}
