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

	"github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/agent/tools/attempts"
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

// agentExecutor adapts tools.Registry to ToolExecutor.
// It applies allowlist filtering, per-tool timeouts, and untrusted-content
// wrapping, then adds results to the shared state so subsequent LLM rounds
// see them in the message history.
type agentExecutor struct {
	tools             *tools.Registry
	state             State
	logger            *slog.Logger
	allowlist         []string
	userID            string
	runID             string
	maxChars          int
	toolTimeout       time.Duration
	repo              attempts.Repo // Phase-6 US-J03: synchronous observation persistence; nil = disabled
	tokenJuiceEnabled bool
	// spillDir is the runtime-workspace base directory for spilled tool results
	// (US-OUT-02). Empty string disables spill; routing falls back to inline
	// truncation via limitToolContent.
	spillDir string
	// repeatedLookup tracks per-turn external lookup calls for web_search,
	// web_fetch, and search_memory (US-OUT-03). After MaxRepeats identical
	// targets, subsequent calls are short-circuited with a blocked error.
	repeatedLookup *governance.RepeatedLookupCounter
}

var _ ToolExecutor = (*agentExecutor)(nil)

func newAgentExecutor(
	reg *tools.Registry,
	state State,
	logger *slog.Logger,
	allowlist []string,
	userID string,
	runID string,
	maxChars int,
	toolTimeout time.Duration,
	repo attempts.Repo,
	tokenJuiceEnabled bool,
	spillDir string,
) *agentExecutor {
	return &agentExecutor{
		tools:             reg,
		state:             state,
		logger:            logger,
		allowlist:         allowlist,
		userID:            userID,
		runID:             runID,
		maxChars:          maxChars,
		toolTimeout:       toolTimeout,
		repo:              repo,
		tokenJuiceEnabled: tokenJuiceEnabled,
		spillDir:          spillDir,
		repeatedLookup:    governance.NewRepeatedLookupCounter(),
	}
}

type toolOutcome struct {
	id      string
	content string
	// awaitingUser is set when the tool returned ErrAwaitingUserInput;
	// the loop handles the pause, no tool_result is added to state.
	awaitingUser *tools.ErrAwaitingUserInput
}

// ExecuteToolCalls runs each call in parallel, wraps results, adds them to
// state (so the next LLM round sees them), and returns an ExecutionSummary.
// Mirrors the fan-out pattern in internal/telegram/conversation_tool_exec.go.
func (e *agentExecutor) ExecuteToolCalls(ctx context.Context, calls []llm.ToolCall) ExecutionSummary {
	outcomes := make([]toolOutcome, len(calls))
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		// Clone arguments so parallel goroutines cannot race on a shared map
		// (F-002). The clone is shallow — see cloneToolArgs.
		argsClone := cloneToolArgs(call.Arguments)
		callCopy := call
		callCopy.Arguments = argsClone
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			raw, _, execErr := e.executeOneTool(ctx, call) // observation consumed by US-J03
			var awaitErr *tools.ErrAwaitingUserInput
			if errors.As(execErr, &awaitErr) {
				awaitErr.ToolCallID = call.ID
				outcomes[i] = toolOutcome{id: call.ID, awaitingUser: awaitErr}
				return
			}
			if e.tokenJuiceEnabled {
				raw = CompactToolOutput(e.logger, call.Name, call.Arguments, raw)
			}
			wrapped := WrapUntrustedToolResult(call.Name, raw)
			// Route to spill when payload exceeds SpillThresholdBytes and a
			// spill directory is configured (US-OUT-02). Falls back to inline
			// truncation when spill is disabled or the write fails.
			if e.spillDir != "" && len(wrapped) > tools.SpillThresholdBytes {
				sessionID := e.runID
				if sessionID == "" {
					sessionID = e.userID
				}
				if envelope, spillErr := tools.SpillOutput(e.spillDir, sessionID, call.ID, wrapped); spillErr == nil {
					outcomes[i] = toolOutcome{id: call.ID, content: envelope}
					return
				}
			}
			outcomes[i] = toolOutcome{id: call.ID, content: limitToolContent(wrapped, e.maxChars)}
		}(i, callCopy)
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	// Honor outer-ctx cancellation so a stuck tool cannot pin Run open after
	// the swarm parent dies or the user disconnects (F-001).
	select {
	case <-done:
	case <-ctx.Done():
		for i, call := range calls {
			if outcomes[i].id == "" {
				outcomes[i] = toolOutcome{id: call.ID, content: tools.FormatToolError(ctx.Err())}
			}
		}
	}

	summary := ExecutionSummary{Results: make(map[string]string, len(outcomes))}
	for _, o := range outcomes {
		if o.awaitingUser != nil {
			// ask_user pause: do NOT add a tool_result to state.
			// The pending tool_call entry already in the assistant message
			// serves as the marker; the resume path injects the tool_result.
			summary.AwaitingUserInput = o.awaitingUser
			continue
		}
		// Add to state so the next LLM round sees the result in message
		// history — the loop only adds stubs for skipped/duplicate calls,
		// not fresh ones (mirrors conversation_tool_exec.go:112).
		e.state.AddToolResultMessage(o.id, o.content)
		summary.LastResult = o.content
		summary.Results[o.id] = o.content
	}
	return summary
}

func (e *agentExecutor) executeOneTool(ctx context.Context, call llm.ToolCall) (string, tools.ToolObservation, error) {
	startedAt := time.Now()

	// buildObs constructs a ToolObservation for each code path.
	// execErr is the underlying Go error (nil on success); class is the
	// pre-classified label (empty string means OutcomeOK).
	buildObs := func(class string, execErr error) tools.ToolObservation {
		argsHash, argKeys := tools.ObservationArgsHash(call.Arguments)
		errRedacted := ""
		if execErr != nil {
			errRedacted = redactToolError(execErr)
		}
		return tools.ToolObservation{
			RunID:         e.runID,
			ToolName:      call.Name,
			ToolKind:      tools.ToolKindOf(call.Name),
			AttemptN:      1,
			Outcome:       tools.BucketOf(class),
			Class:         class,
			ArgsHash:      argsHash,
			ArgKeys:       argKeys,
			ErrorRedacted: errRedacted,
			StartedAt:     startedAt,
			ElapsedMS:     time.Since(startedAt).Milliseconds(),
		}
	}

	// recordObs persists the observation synchronously before the function
	// returns. Write failures are logged but never propagated — the tool
	// result must still reach the user. A nil repo is skipped silently
	// (feature-flag-ready: set repo=nil to disable persistence).
	recordObs := func(obs tools.ToolObservation) {
		if e.repo == nil {
			return
		}
		if err := e.repo.Record(ctx, obs); err != nil && e.logger != nil {
			e.logger.Error("tool_attempts_write_failed", "err", err, "tool_name", obs.ToolName)
		}
	}

	if len(e.allowlist) == 0 || !slices.Contains(e.allowlist, strings.ToLower(call.Name)) {
		obs := buildObs("permission", nil)
		recordObs(obs)
		return tools.FormatFatalToolError(fmt.Errorf("tool %q is not allowed for this agent", call.Name)), obs, nil
	}
	if e.tools == nil {
		obs := buildObs("error", nil)
		recordObs(obs)
		return tools.FormatFatalToolError(errors.New("tool registry unavailable")), obs, nil
	}
	toolCtx := ctx
	var cancel context.CancelFunc
	if e.toolTimeout > 0 {
		toolCtx, cancel = context.WithTimeout(toolCtx, e.toolTimeout)
		defer cancel()
	}
	if strings.TrimSpace(e.userID) != "" {
		toolCtx = tools.WithUserID(toolCtx, e.userID)
	}
	// US-OUT-03: short-circuit repeated external lookups before dispatch.
	if e.repeatedLookup != nil {
		if blockErr := e.repeatedLookup.Check(call.Name, call.Arguments); blockErr != nil {
			obs := buildObs("blocked", blockErr)
			recordObs(obs)
			return tools.FormatFatalToolError(blockErr), obs, nil
		}
	}
	out, err := e.tools.Execute(toolCtx, call.Name, call.Arguments)
	elapsedMs := time.Since(startedAt).Milliseconds()
	if err == nil && isExecTool(call.Name) {
		out = wrapExecOutput(out, elapsedMs)
	}
	if err != nil {
		var awaitErr *tools.ErrAwaitingUserInput
		if errors.As(err, &awaitErr) {
			obs := buildObs("cancelled", nil)
			recordObs(obs)
			return "", obs, awaitErr // propagate sentinel without formatting
		}
		if e.logger != nil {
			e.logger.Warn("agent tool call failed", "tool", call.Name, "error", redactToolError(err))
		}
		class := tools.ClassifyToolError(err)
		obs := buildObs(class, err)
		recordObs(obs)
		return tools.FormatToolError(err), obs, nil
	}
	obs := buildObs("", nil)
	recordObs(obs)
	return out, obs, nil
}

// cloneToolArgs returns a shallow copy of the LLM-supplied arguments map so
// parallel tool executions cannot race on shared map mutation (F-002).
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

// redactToolError scrubs known credential patterns from a tool error before
// logging. The full error still goes to the LLM via FormatToolError (F-038).
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

// isExecTool reports whether toolName is an execution tool that benefits from
// the structured Exit code / Wall time / Lines envelope.
func isExecTool(name string) bool {
	return name == "execute_code" || name == "execute_shell"
}

// wrapExecOutput reformats the raw output from an exec tool into a structured
// envelope that preserves key metadata even when the body is later truncated
// by TruncateMiddle. execute_code and execute_shell already prefix their output
// with "exit_code: N\nelapsed_ms: M\n\n"; those lines are parsed, stripped, and
// re-expressed in the canonical capitalized form.
func wrapExecOutput(raw string, elapsedMs int64) string {
	exitCode := 0
	body := raw

	// Parse and strip the leading "exit_code: N" line.
	if firstLine, rest, found := strings.Cut(body, "\n"); found && strings.HasPrefix(firstLine, "exit_code:") {
		after, _ := strings.CutPrefix(firstLine, "exit_code:")
		if _, err := fmt.Sscanf(strings.TrimSpace(after), "%d", &exitCode); err != nil {
			exitCode = 0
		}
		body = rest
		// Strip the "elapsed_ms: M" line that follows.
		if secondLine, rest2, found2 := strings.Cut(body, "\n"); found2 && strings.HasPrefix(secondLine, "elapsed_ms:") {
			body = rest2
		}
		// Strip the blank separator line.
		body = strings.TrimPrefix(body, "\n")
	}

	lines := strings.Count(body, "\n") + 1
	if strings.TrimSpace(body) == "" {
		lines = 0
	}
	return fmt.Sprintf(
		"Exit code: %d\nWall time: %dms\nTotal output lines: %d\nOutput:\n%s",
		exitCode, elapsedMs, lines, body,
	)
}
