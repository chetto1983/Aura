package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agentloop"
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
//
// Streaming asymmetry (F-025): unlike the Telegram-facing conversation loop,
// Runner uses llm.Client.Send and never Stream. Background agents return a
// single Result struct rather than progressive output; there is no chat to
// progressively edit. If a future caller needs streamed tokens (e.g. dashboard
// live view of a swarm worker) it can plumb a streaming overload through Task.
//
// Tool-arg validation (F-017): per-call args (call.Arguments) are forwarded
// to tools.Registry.Execute without schema validation. Each tool is
// responsible for validating its own input — the contract is enforced at the
// tool boundary, not here. The clone in cloneToolArgs protects shared state;
// it does NOT sanitize.
type Runner struct {
	mu              sync.RWMutex
	llm             llm.Client
	tools           *tools.Registry
	model           string
	maxIterations   int
	timeout         time.Duration
	toolTimeout     time.Duration
	reasoningEffort string
	logger          *slog.Logger
}

// Config wires a Runner. ToolRegistry may be nil for text-only tasks.
type Config struct {
	LLM             llm.Client
	Tools           *tools.Registry
	Model           string
	MaxIterations   int
	Timeout         time.Duration
	ToolTimeout     time.Duration
	ReasoningEffort string // forwarded to every llm.Request the runner builds
	Logger          *slog.Logger
}

// Task is one isolated background-agent assignment.
type Task struct {
	SystemPrompt        string
	Prompt              string
	Messages            []llm.Message
	ToolAllowlist       []string
	UserID              string
	Temperature         *float64
	MaxToolCalls        int
	MaxToolResultChars  int
	FinalizationTimeout time.Duration
	CompleteOnDeadline  bool
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

// LimitController is the live runtime tuning surface for a runner.
type LimitController interface {
	UpdateLimits(maxIterations int, timeout time.Duration, toolTimeout time.Duration)
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
		llm:             cfg.LLM,
		tools:           cfg.Tools,
		model:           cfg.Model,
		maxIterations:   maxIterations,
		timeout:         timeout,
		toolTimeout:     toolTimeout,
		reasoningEffort: cfg.ReasoningEffort,
		logger:          logger,
	}, nil
}

// Limits returns the runtime loop/deadline limits currently used for new runs.
func (r *Runner) Limits() (maxIterations int, timeout time.Duration, toolTimeout time.Duration) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.maxIterations, r.timeout, r.toolTimeout
}

// UpdateLimits changes the loop/deadline limits used by subsequent runs.
// Non-positive inputs fall back to the same defaults used by NewRunner.
func (r *Runner) UpdateLimits(maxIterations int, timeout time.Duration, toolTimeout time.Duration) {
	if maxIterations <= 0 {
		maxIterations = defaultMaxIterations
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if toolTimeout <= 0 {
		toolTimeout = defaultToolTimeout
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxIterations = maxIterations
	r.timeout = timeout
	r.toolTimeout = toolTimeout
}

// Run executes one bounded agent turn.
//
// Timeout precedence (F-005):
//   - r.timeout (default 60s) caps the whole Run via context.WithTimeout.
//     Once fired, every in-flight tool's ctx is cancelled and partial outcomes
//     are stamped with FormatToolError(ctx.Err()) per F-001.
//   - r.toolTimeout (default 30s) caps each individual tool call. Multiple
//     tools fan out in parallel within one turn, so the effective wall-clock
//     for a turn is bounded by max(toolTimeouts) + LLM RTT, not the sum.
//   - A misbehaving tool that ignores its ctx still cannot pin Run open beyond
//     r.timeout because the parallel-fanout wait races ctx.Done() (F-001).
func (r *Runner) Run(ctx context.Context, task Task) (Result, error) {
	start := time.Now()
	maxIterations, timeout, toolTimeout := r.Limits()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
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
	maxToolCalls := task.MaxToolCalls
	finalizing := false
	for i := 0; i < maxIterations; i++ {
		turnTools := toolDefs
		if finalizing {
			turnTools = nil
		}
		sendCtx := ctx
		sendCancel := func() {}
		if finalizing && task.CompleteOnDeadline && task.FinalizationTimeout > 0 {
			sendCtx, sendCancel = context.WithTimeout(ctx, task.FinalizationTimeout)
		}
		resp, err := r.llm.Send(sendCtx, llm.Request{
			Messages:        messages,
			Model:           r.model,
			Temperature:     task.Temperature,
			Tools:           turnTools,
			ReasoningEffort: r.reasoningEffort,
		})
		sendCancel()
		result.LLMCalls++
		addUsage(&result.Tokens, resp.Usage)
		if err != nil {
			result.Content = interruptedContent(err, lastToolResult, result)
			result.Messages = messages
			result.Elapsed = time.Since(start).Round(time.Millisecond)
			if task.CompleteOnDeadline && errors.Is(err, context.DeadlineExceeded) && result.ToolCalls > 0 {
				result.Messages = append(result.Messages, llm.Message{Role: "assistant", Content: result.Content})
				return result, nil
			}
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

		toolCalls, skipped := splitToolCalls(resp.ToolCalls, maxToolCalls, result.ToolCalls)
		result.ToolCalls += len(toolCalls)
		toolResults := r.executeToolCalls(ctx, task.UserID, allowlist, toolCalls, task.MaxToolResultChars, toolTimeout)
		toolResults = append(toolResults, skippedToolOutcomes(skipped, maxToolCalls)...)
		// Protocol invariant (F-004): every assistant tool_call announced on
		// the previous line must have a matching tool-role result. The
		// upstream Chat Completions API rejects the next request if any
		// tool_call_id is missing. splitToolCalls + skippedToolOutcomes
		// hand-maintain the pairing; this guard catches a future refactor
		// that breaks it.
		if len(toolResults) != len(resp.ToolCalls) {
			return Result{}, fmt.Errorf("agent: protocol invariant broken — %d tool results for %d announced tool calls", len(toolResults), len(resp.ToolCalls))
		}
		for _, tr := range toolResults {
			messages = append(messages, llm.Message{
				Role:       "tool",
				Content:    tr.content,
				ToolCallID: tr.id,
			})
			lastToolResult = tr.content
		}
		if maxToolCalls > 0 && result.ToolCalls >= maxToolCalls {
			messages = append(messages, llm.Message{Role: "user", Content: toolBudgetFinalInstruction(maxToolCalls)})
			finalizing = true
		}
	}

	// Bound the inlined tail so the caller never sees a multi-KB JSON dump
	// when the loop exhausts iterations (F-028). The natural-language
	// synthesis path (agentloop.finalizeAnswerAfterBudget) is a higher-fidelity
	// alternative; consolidating both runners is the F-036 follow-up.
	content := "Agent loop stopped after reaching the maximum iteration limit."
	if lastToolResult != "" {
		content += "\n\nLast tool result (truncated):\n" + limitToolContent(lastToolResult, 400)
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
		// allowlist is already lowercased by cleanToolList; compare in the
		// same case so canonical lowercase tool names match unconditionally
		// (F-018).
		if slices.Contains(allowlist, strings.ToLower(def.Name)) {
			out = append(out, def)
		}
	}
	return out
}

type toolOutcome struct {
	id      string
	content string
}

func (r *Runner) executeToolCalls(ctx context.Context, userID string, allowlist []string, calls []llm.ToolCall, maxChars int, toolTimeout time.Duration) []toolOutcome {
	// results is a fixed-length slice; each goroutine writes to its own
	// disjoint index. Backing array is allocated up front so no append-race
	// can creep in via a future refactor (F-019).
	results := make([]toolOutcome, len(calls))
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		// Clone arguments so a tool that mutates its input map (an idiomatic
		// mistake) cannot stomp on a sibling goroutine's view (F-002). The
		// outer call.Arguments is built once by parseToolCallArguments and
		// shared across goroutines — without the clone, two parallel tools
		// reading the same map could race.
		argsClone := cloneToolArgs(call.Arguments)
		callCopy := call
		callCopy.Arguments = argsClone
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			raw := r.executeOneTool(ctx, userID, allowlist, call, toolTimeout)
			// Wrap output from untrusted-origin tools (web_fetch, web_search,
			// MCP, etc.) before truncation so the LLM sees a clear
			// data-not-instructions envelope (F-003). Trusted tools are
			// returned unchanged so the model does not learn to ignore every
			// tool result indiscriminately.
			wrapped := agentloop.WrapUntrustedToolResult(call.Name, raw)
			results[i] = toolOutcome{id: call.ID, content: limitToolContent(wrapped, maxChars)}
		}(i, callCopy)
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	// Honor outer ctx cancellation so a stuck tool cannot pin Run() open
	// after the user disconnects or the swarm parent dies (F-001). The
	// in-flight goroutines still race their own timeout to completion in
	// the background; their writes go to a slice we no longer read.
	select {
	case <-done:
		return results
	case <-ctx.Done():
		partial := make([]toolOutcome, len(calls))
		for i, call := range calls {
			if results[i].id == "" {
				partial[i] = toolOutcome{id: call.ID, content: tools.FormatToolError(ctx.Err())}
			} else {
				partial[i] = results[i]
			}
		}
		return partial
	}
}

// cloneToolArgs returns a shallow copy of the LLM-supplied arguments map so
// parallel tool executions cannot race on shared map mutation. The values
// themselves (string/float64/bool/nested map/slice from json.Unmarshal) are
// not deep-copied — but those types are read-only in practice for every
// built-in tool. If a future tool needs to mutate a nested slice, it must
// clone internally.
func cloneToolArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func (r *Runner) executeOneTool(ctx context.Context, userID string, allowlist []string, call llm.ToolCall, toolTimeout time.Duration) string {
	if len(allowlist) == 0 || !slices.Contains(allowlist, strings.ToLower(call.Name)) {
		return tools.FormatFatalToolError(fmt.Errorf("tool %q is not allowed for this agent", call.Name))
	}
	if r.tools == nil {
		return tools.FormatFatalToolError(errors.New("tool registry unavailable"))
	}
	toolCtx := ctx
	var cancel context.CancelFunc
	if toolTimeout > 0 {
		toolCtx, cancel = context.WithTimeout(toolCtx, toolTimeout)
		defer cancel()
	}
	if strings.TrimSpace(userID) != "" {
		toolCtx = tools.WithUserID(toolCtx, userID)
	}
	out, err := r.tools.Execute(toolCtx, call.Name, call.Arguments)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("agent tool call failed", "tool", call.Name, "error", redactToolError(err))
		}
		return tools.FormatToolError(err)
	}
	return out
}

// redactToolError returns a log-safe representation of a tool error. Tool
// errors commonly wrap LLM-controlled values (URLs with tokens, source IDs,
// file paths). CLAUDE.md forbids those in logs. This helper scrubs known
// patterns; the full err still goes to the LLM via FormatToolError (F-038).
//
// Patterns scrubbed:
//   - Query strings on URLs containing token=/key=/secret=/auth= (?token=... -> ?<redacted>)
//   - Long base64-shaped runs (32+ chars of [A-Za-z0-9+/=]) → "<base64>"
func redactToolError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = redactURLCredentialsRe.ReplaceAllString(msg, "$1<redacted>")
	msg = redactBase64BlobRe.ReplaceAllString(msg, "<base64>")
	return msg
}

var (
	redactURLCredentialsRe = regexp.MustCompile(`([?&](?i:token|api[_-]?key|secret|auth|bearer)=)[^&\s"]+`)
	redactBase64BlobRe     = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
)

// cleanToolList trims, lowercases, and dedupes the LLM-or-config-supplied
// tool allowlist. Lowercasing is defense-in-depth: tool names in the registry
// are canonical snake_case, but if a future schema mismatch or operator typo
// produces "Web_Fetch" the allowlist would otherwise silently miss
// "web_fetch" emitted by the LLM (F-018).
func cleanToolList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
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

// splitToolCalls partitions a batch of LLM-emitted tool calls into those the
// runner will execute (kept) and those it must skip because the per-turn
// budget is exhausted (skipped). The signature is positional and easy to
// misread — maxToolCalls is the absolute cap for this Run, alreadyUsed is
// the running total of previously-executed calls (F-026). A future cleanup
// could wrap both in a ToolBudget struct.
func splitToolCalls(calls []llm.ToolCall, maxToolCalls, alreadyUsed int) ([]llm.ToolCall, []llm.ToolCall) {
	if maxToolCalls <= 0 {
		return calls, nil
	}
	remaining := maxToolCalls - alreadyUsed
	if remaining <= 0 {
		return nil, calls
	}
	if len(calls) <= remaining {
		return calls, nil
	}
	return calls[:remaining], calls[remaining:]
}

func skippedToolOutcomes(calls []llm.ToolCall, maxToolCalls int) []toolOutcome {
	if len(calls) == 0 {
		return nil
	}
	out := make([]toolOutcome, 0, len(calls))
	msg := fmt.Sprintf("Tool call skipped: this AuraBot worker reached its tool budget (%d). Return a concise partial result from the evidence already gathered.", maxToolCalls)
	for _, call := range calls {
		out = append(out, toolOutcome{id: call.ID, content: msg})
	}
	return out
}

func toolBudgetFinalInstruction(maxToolCalls int) string {
	return fmt.Sprintf("Tool budget reached (%d calls). Do not call tools again. Finish the assigned work now with a concise final report from the evidence above: answer, evidence/URLs when available, gaps, and next action.", maxToolCalls)
}

func interruptedContent(err error, lastToolResult string, result Result) string {
	// User-visible content stays free of internal metrics. The same metrics
	// already live on the Result struct (LLMCalls, ToolCalls, Tokens) so the
	// caller can decide whether to surface them. Surfacing them here also
	// trips LooksLikeUnsafeFinalAnswer in the terminal finaliser (F-027).
	content := fmt.Sprintf("AuraBot worker interrupted before a final answer: %v.", err)
	if strings.TrimSpace(lastToolResult) != "" {
		content += "\n\nLast tool result:\n" + lastToolResult
	}
	return content
}

func limitToolContent(content string, maxChars int) string {
	if maxChars <= 0 {
		return content
	}
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}
	if maxChars <= 16 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-15]) + "\n...[truncated]"
}
