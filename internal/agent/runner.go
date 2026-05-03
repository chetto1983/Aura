package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/tools"
)

const (
	defaultMaxIterations = 5
	defaultTimeout       = 60 * time.Second
	defaultToolTimeout   = 30 * time.Second
)

// Runner executes a bounded LLM/tool loop without Telegram coupling. It is the
// small reusable core future AuraBot workers can use inside SwarmManager.
type Runner struct {
	llm           llm.Client
	tools         *tools.Registry
	model         string
	maxIterations int
	timeout       time.Duration
	toolTimeout   time.Duration
	logger        *slog.Logger
}

// Config wires a Runner. ToolRegistry may be nil for text-only tasks.
type Config struct {
	LLM           llm.Client
	Tools         *tools.Registry
	Model         string
	MaxIterations int
	Timeout       time.Duration
	ToolTimeout   time.Duration
	Logger        *slog.Logger
}

// Task is one isolated background-agent assignment.
type Task struct {
	SystemPrompt  string
	Prompt        string
	Messages      []llm.Message
	ToolAllowlist []string
	UserID        string
	Temperature   *float64
}

// Result captures the final response and enough telemetry for SwarmManager to
// persist/audit the worker.
type Result struct {
	Content   string
	Messages  []llm.Message
	LLMCalls  int
	ToolCalls int
	Tokens    llm.TokenUsage
	Elapsed   time.Duration
}

func NewRunner(cfg Config) (*Runner, error) {
	if cfg.LLM == nil {
		return nil, errors.New("agent runner: llm client required")
	}
	maxIterations := cfg.MaxIterations
	if maxIterations <= 0 {
		maxIterations = defaultMaxIterations
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	toolTimeout := cfg.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = defaultToolTimeout
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		llm:           cfg.LLM,
		tools:         cfg.Tools,
		model:         cfg.Model,
		maxIterations: maxIterations,
		timeout:       timeout,
		toolTimeout:   toolTimeout,
		logger:        logger,
	}, nil
}

func (r *Runner) Run(ctx context.Context, task Task) (Result, error) {
	start := time.Now()
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	messages, err := initialMessages(task)
	if err != nil {
		return Result{}, err
	}

	allowlist := cleanToolList(task.ToolAllowlist)
	toolDefs := r.toolDefinitions(allowlist)

	var result Result
	var lastToolResult string
	for i := 0; i < r.maxIterations; i++ {
		resp, err := r.llm.Send(ctx, llm.Request{
			Messages:    messages,
			Model:       r.model,
			Temperature: task.Temperature,
			Tools:       toolDefs,
		})
		result.LLMCalls++
		addUsage(&result.Tokens, resp.Usage)
		if err != nil {
			result.Messages = messages
			result.Elapsed = time.Since(start).Round(time.Millisecond)
			return result, fmt.Errorf("agent runner: llm send: %w", err)
		}

		if !resp.HasToolCalls {
			content := strings.TrimSpace(resp.Content)
			if content == "" {
				if lastToolResult != "" {
					content = lastToolResult
				} else {
					content = "Task completed."
				}
			}
			messages = append(messages, llm.Message{Role: "assistant", Content: content})
			result.Content = content
			result.Messages = messages
			result.Elapsed = time.Since(start).Round(time.Millisecond)
			return result, nil
		}

		messages = append(messages, llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})
		result.ToolCalls += len(resp.ToolCalls)

		toolResults := r.executeToolCalls(ctx, task.UserID, allowlist, resp.ToolCalls)
		for _, tr := range toolResults {
			messages = append(messages, llm.Message{
				Role:       "tool",
				Content:    tr.content,
				ToolCallID: tr.id,
			})
			lastToolResult = tr.content
		}
	}

	content := "Agent loop stopped after reaching the maximum iteration limit."
	if lastToolResult != "" {
		content += "\n\nLast tool result:\n" + lastToolResult
	}
	messages = append(messages, llm.Message{Role: "assistant", Content: content})
	result.Content = content
	result.Messages = messages
	result.Elapsed = time.Since(start).Round(time.Millisecond)
	return result, nil
}

func initialMessages(task Task) ([]llm.Message, error) {
	if len(task.Messages) > 0 {
		cp := make([]llm.Message, len(task.Messages))
		copy(cp, task.Messages)
		return cp, nil
	}
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		return nil, errors.New("agent runner: prompt or messages required")
	}
	messages := make([]llm.Message, 0, 2)
	if system := strings.TrimSpace(task.SystemPrompt); system != "" {
		messages = append(messages, llm.Message{Role: "system", Content: system})
	}
	messages = append(messages, llm.Message{Role: "user", Content: prompt})
	return messages, nil
}

func (r *Runner) toolDefinitions(allowlist []string) []llm.ToolDefinition {
	if r.tools == nil || len(allowlist) == 0 {
		return nil
	}
	defs := r.tools.Definitions()
	out := make([]llm.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		if slices.Contains(allowlist, def.Name) {
			out = append(out, def)
		}
	}
	return out
}

type toolOutcome struct {
	id      string
	content string
}

func (r *Runner) executeToolCalls(ctx context.Context, userID string, allowlist []string, calls []llm.ToolCall) []toolOutcome {
	results := make([]toolOutcome, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			results[i] = toolOutcome{id: call.ID, content: r.executeOneTool(ctx, userID, allowlist, call)}
		}(i, call)
	}
	wg.Wait()
	return results
}

func (r *Runner) executeOneTool(ctx context.Context, userID string, allowlist []string, call llm.ToolCall) string {
	if len(allowlist) == 0 || !slices.Contains(allowlist, call.Name) {
		return tools.FormatFatalToolError(fmt.Errorf("tool %q is not allowed for this agent", call.Name))
	}
	if r.tools == nil {
		return tools.FormatFatalToolError(errors.New("tool registry unavailable"))
	}
	toolCtx := ctx
	var cancel context.CancelFunc
	if r.toolTimeout > 0 {
		toolCtx, cancel = context.WithTimeout(toolCtx, r.toolTimeout)
		defer cancel()
	}
	if strings.TrimSpace(userID) != "" {
		toolCtx = tools.WithUserID(toolCtx, userID)
	}
	out, err := r.tools.Execute(toolCtx, call.Name, call.Arguments)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("agent tool call failed", "tool", call.Name, "error", err)
		}
		return tools.FormatToolError(err)
	}
	return out
}

func cleanToolList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func addUsage(total *llm.TokenUsage, usage llm.TokenUsage) {
	total.PromptTokens += usage.PromptTokens
	total.CompletionTokens += usage.CompletionTokens
	total.TotalTokens += usage.TotalTokens
}
