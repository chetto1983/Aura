package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type llmClient struct {
	baseURL string
	client  *http.Client
}

type llmOptions struct {
	System         string
	Tools          []map[string]any
	Thinking       bool
	ThinkingBudget int
	MaxTokens      int
}

type llmResult struct {
	Content          string
	Reasoning        string
	ToolName         string
	ToolArguments    string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	LatencyMS        float64
}

func (c *llmClient) complete(ctx context.Context, prompt string, opts llmOptions) (llmResult, error) {
	messages := make([]map[string]any, 0, 2)
	if strings.TrimSpace(opts.System) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": opts.System})
	}
	messages = append(messages, map[string]any{"role": "user", "content": prompt})
	body := map[string]any{
		"model":       "qwen",
		"messages":    messages,
		"temperature": 0,
		"max_tokens":  opts.MaxTokens,
	}
	if len(opts.Tools) > 0 {
		body["tools"] = opts.Tools
		body["tool_choice"] = "auto"
	}
	if !opts.Thinking {
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	} else if opts.ThinkingBudget != 0 {
		body["thinking_budget_tokens"] = opts.ThinkingBudget
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return llmResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return llmResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := c.client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return llmResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return llmResult{}, fmt.Errorf("llama.cpp HTTP %d", resp.StatusCode)
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return llmResult{}, fmt.Errorf("decode llama.cpp response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return llmResult{}, fmt.Errorf("llama.cpp returned no choices")
	}
	msg := decoded.Choices[0].Message
	toolName := ""
	toolArguments := ""
	if len(msg.ToolCalls) > 0 {
		toolName = msg.ToolCalls[0].Function.Name
		toolArguments = msg.ToolCalls[0].Function.Arguments
	}
	return llmResult{
		Content:          msg.Content,
		Reasoning:        msg.ReasoningContent,
		ToolName:         toolName,
		ToolArguments:    toolArguments,
		PromptTokens:     decoded.Usage.PromptTokens,
		CompletionTokens: decoded.Usage.CompletionTokens,
		TotalTokens:      decoded.Usage.TotalTokens,
		LatencyMS:        float64(elapsed.Microseconds()) / 1000,
	}, nil
}

func functionTool(item catalogItem) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        item.Name,
			"description": item.Description,
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"request": map[string]any{"type": "string"},
				},
				"required": []string{"request"},
			},
		},
	}
}

func skillTool(items []catalogItem) map[string]any {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "activate_skill",
			"description": "Activate exactly one skill that best matches the user's request.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "enum": names},
				},
				"required": []string{"name"},
			},
		},
	}
}
