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

type catalogItem struct {
	Name        string
	Description string
}

type qwenClient struct {
	baseURL string
	client  *http.Client
}

type liveOutcome struct {
	Content       string  `json:"content"`
	Selected      string  `json:"selected"`
	Correct       bool    `json:"correct"`
	Tokens        int     `json:"tokens"`
	LatencyMS     float64 `json:"latency_ms"`
	Error         string  `json:"error,omitempty"`
	ReasoningRune int     `json:"reasoning_runes"`
}

type completionOptions struct {
	system         string
	tools          []map[string]any
	thinking       bool
	thinkingBudget int
	maxTokens      int
}

type completion struct {
	content, reasoning, toolName, toolArguments string
	tokens                                      int
	latencyMS                                   float64
}

func (c *qwenClient) complete(ctx context.Context, prompt string, opts completionOptions) (completion, error) {
	messages := make([]map[string]any, 0, 2)
	if opts.system != "" {
		messages = append(messages, map[string]any{"role": "system", "content": opts.system})
	}
	messages = append(messages, map[string]any{"role": "user", "content": prompt})
	body := map[string]any{
		"model": "qwen", "messages": messages, "temperature": 0, "max_tokens": opts.maxTokens,
	}
	if len(opts.tools) > 0 {
		body["tools"], body["tool_choice"] = opts.tools, "auto"
	}
	if !opts.thinking {
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	} else {
		body["thinking_budget_tokens"] = opts.thinkingBudget
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return completion{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return completion{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := c.client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return completion{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return completion{}, fmt.Errorf("llama.cpp HTTP %d", resp.StatusCode)
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
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return completion{}, err
	}
	if len(decoded.Choices) == 0 {
		return completion{}, fmt.Errorf("llama.cpp returned no choice")
	}
	message := decoded.Choices[0].Message
	result := completion{
		content: message.Content, reasoning: message.ReasoningContent,
		tokens: decoded.Usage.TotalTokens, latencyMS: float64(elapsed.Microseconds()) / 1000,
	}
	if len(message.ToolCalls) > 0 {
		result.toolName = message.ToolCalls[0].Function.Name
		result.toolArguments = message.ToolCalls[0].Function.Arguments
	}
	return result, nil
}

func executeLive(ctx context.Context, client *qwenClient, sc sourceScenario, action string) liveOutcome {
	var result completion
	var err error
	switch sc.Domain {
	case "reasoning":
		opts := completionOptions{
			system: "Return only the requested final answer with no explanation.", maxTokens: 1024,
			thinking: action != "off", thinkingBudget: 256,
		}
		result, err = client.complete(ctx, sc.Prompt, opts)
	case "tool":
		items := toolCatalog()
		if action == "qwen_top3" {
			items = filterCatalog(items, sc.Candidates[action])
		}
		tools := make([]map[string]any, 0, len(items))
		for _, item := range items {
			tools = append(tools, functionTool(item))
		}
		result, err = client.complete(ctx, sc.Prompt, completionOptions{
			system: "Call exactly one available function that best fulfills the request. Do not answer in text.",
			tools:  tools, maxTokens: 192,
		})
	case "skill":
		items := skillCatalog()
		if action == "qwen_top3" {
			items = filterCatalog(items, sc.Candidates[action])
		}
		result, err = client.complete(ctx,
			"Available skills:\n"+catalogText(items)+"\nUser request:\n"+sc.Prompt,
			completionOptions{
				system: "Call activate_skill exactly once with the best matching skill. Do not answer in text.",
				tools:  []map[string]any{skillTool(items)}, maxTokens: 192,
			})
	case "knowledge":
		contextText := sc.Contexts[action]
		prompt := sc.Prompt
		if contextText != "" {
			prompt = "Private context:\n" + contextText + "\n\nQuestion:\n" + prompt
		}
		result, err = client.complete(ctx, prompt, completionOptions{
			system:    "Answer only from supplied private context. If it is insufficient, answer UNKNOWN. Return only the short answer.",
			maxTokens: 96,
		})
	default:
		err = fmt.Errorf("unknown domain %s", sc.Domain)
	}
	out := liveOutcome{Content: strings.TrimSpace(result.content), Tokens: result.tokens, LatencyMS: result.latencyMS, ReasoningRune: len([]rune(result.reasoning))}
	if err != nil {
		out.Error = err.Error()
		return out
	}
	switch sc.Domain {
	case "tool":
		out.Selected = result.toolName
	case "skill":
		out.Selected = parseSkill(result.toolName, result.toolArguments)
	default:
		out.Selected = action
	}
	if sc.Domain == "tool" || sc.Domain == "skill" {
		out.Correct = sameAnswer(out.Selected, sc.Expected)
	} else {
		out.Correct = sameAnswer(out.Content, sc.Expected)
	}
	return out
}

func toolCatalog() []catalogItem {
	return []catalogItem{
		{"weather_now", "Get current or forecast weather for a location."},
		{"send_email", "Send an email message to a recipient."},
		{"document_search", "Search the user's private indexed documents and manuals."},
		{"memory_add_preference", "Remember a durable user preference for future conversations."},
		{"calendar_create_event", "Create an event or appointment in the user's calendar."},
		{"finance_quote", "Get the latest market price for a stock or cryptocurrency."},
		{"web_search", "Search the public internet for current information."},
		{"shell_exec", "Execute a command or script in the user workspace."},
	}
}

func skillCatalog() []catalogItem {
	return []catalogItem{
		{"golang-testing", "Design idiomatic Go tests, benchmarks, fuzzing, coverage, and race checks."},
		{"web-research", "Research current information online using primary sources."},
		{"document-pdf", "Read, extract, inspect, or transform PDF documents."},
		{"xlsx", "Create, edit, inspect, or validate Excel workbooks."},
		{"docker-compose-orchestration", "Design, run, debug, and validate Docker Compose services."},
		{"accessibility-a11y", "Audit or implement WCAG accessibility."},
		{"systematic-debugging", "Investigate a reproducible bug using hypothesis-driven debugging."},
		{"copywriting", "Write or improve marketing and product copy."},
	}
}

func filterCatalog(catalog []catalogItem, names []string) []catalogItem {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	var out []catalogItem
	for _, item := range catalog {
		if wanted[item.Name] {
			out = append(out, item)
		}
	}
	return out
}

func functionTool(item catalogItem) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": item.Name, "description": item.Description,
			"parameters": map[string]any{
				"type": "object", "properties": map[string]any{
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
			"name": "activate_skill", "description": "Activate the best matching skill.",
			"parameters": map[string]any{
				"type": "object", "properties": map[string]any{
					"name": map[string]any{"type": "string", "enum": names},
				},
				"required": []string{"name"},
			},
		},
	}
}

func catalogText(items []catalogItem) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "- %s: %s\n", item.Name, item.Description)
	}
	return b.String()
}

func parseSkill(toolName, arguments string) string {
	if toolName != "activate_skill" {
		return ""
	}
	var args struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(arguments), &args) == nil {
		return args.Name
	}
	return ""
}

func sameAnswer(got, expected string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.Trim(value, "`'\".,;:!?$€ ")
		value = strings.ReplaceAll(value, "seven", "7")
		value = strings.ReplaceAll(value, "eighteen", "18")
		value = strings.ReplaceAll(value, "ninety", "90")
		value = strings.Join(strings.Fields(value), " ")
		value = strings.ReplaceAll(value, " at ", " ")
		return value
	}
	g, e := normalize(got), normalize(expected)
	return g == e || strings.Contains(g, e)
}
