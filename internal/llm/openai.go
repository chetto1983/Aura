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
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
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
	for _, m := range req.Messages {
		chatReq.Messages = append(chatReq.Messages, chatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
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

	return Response{
		Content: chatResp.Choices[0].Message.Content,
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
		chatReq.Messages = append(chatReq.Messages, chatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
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
