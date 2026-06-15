package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
	"github.com/chetto1983/aura/internal/secret"
)

const defaultCommandHookTimeout = 2 * time.Second

// Rewrite bounds (AG-003): a hook may rewrite live request/tool state, but the
// replacement must stay within these caps so a buggy or compromised hook cannot
// inject an unbounded prompt/tool set.
const (
	maxHookRewriteMessages    = 512
	maxHookRewriteTools       = 256
	maxHookRewriteContentByte = 1 << 20 // 1 MiB per content/preview field
)

// CommandHookConfig describes a trust-gated out-of-process hook.
type CommandHookConfig struct {
	Name           string
	Command        string
	Args           []string
	Env            []string
	ExpectedSHA256 string
	Timeout        time.Duration
}

// CommandHookEvent is the JSON envelope sent to a command hook on stdin.
type CommandHookEvent struct {
	Event      string            `json:"event"`
	Turn       *HookTurn         `json:"turn,omitempty"`
	Request    *llm.Request      `json:"request,omitempty"`
	ToolCall   *llm.ToolCall     `json:"tool_call,omitempty"`
	ToolResult *tools.ToolResult `json:"tool_result,omitempty"`
}

// CommandHookDecision is the JSON response read from a command hook's stdout.
type CommandHookDecision struct {
	Decision     string            `json:"decision"`
	Message      string            `json:"message,omitempty"`
	Content      string            `json:"content,omitempty"`
	FinishReason string            `json:"finish_reason,omitempty"`
	Request      *llm.Request      `json:"request,omitempty"`
	ToolCall     *llm.ToolCall     `json:"tool_call,omitempty"`
	ToolResult   *tools.ToolResult `json:"tool_result,omitempty"`
}

// CommandHook implements Hook by executing a trusted local command per event.
type CommandHook struct {
	name           string
	command        string
	args           []string
	env            []string
	expectedSHA256 string
	timeout        time.Duration
}

// NewCommandHook constructs a command hook. Execution is still gated by a fresh
// executable hash check on every event.
func NewCommandHook(cfg CommandHookConfig) (*CommandHook, error) {
	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		return nil, fmt.Errorf("command hook command cannot be empty")
	}
	expected := strings.ToLower(strings.TrimSpace(cfg.ExpectedSHA256))
	if expected == "" {
		return nil, fmt.Errorf("command hook %q has no trusted sha256", cfg.Name)
	}
	resolved, err := resolveHookCommand(command)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultCommandHookTimeout
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = filepath.Base(resolved)
	}
	return &CommandHook{
		name:           name,
		command:        resolved,
		args:           append([]string(nil), cfg.Args...),
		env:            append([]string(nil), cfg.Env...),
		expectedSHA256: expected,
		timeout:        timeout,
	}, nil
}

// OnTurnStart sends a turn_start event to the command hook.
func (h *CommandHook) OnTurnStart(ctx context.Context, turn HookTurn) error {
	decision, _, err := h.run(ctx, CommandHookEvent{Event: "turn_start", Turn: &turn})
	if err != nil {
		return err
	}
	return h.lifecycleDecisionError("turn_start", decision)
}

// BeforeModel sends a before_model event to the command hook.
func (h *CommandHook) BeforeModel(ctx context.Context, req *llm.Request) (*ModelHookResult, error) {
	decision, crashed, err := h.run(ctx, CommandHookEvent{Event: "before_model", Request: req})
	if err != nil || isAllowDecision(decision) {
		return nil, err
	}
	switch normalizedDecision(decision) {
	case "rewrite":
		if cerr := h.rejectCrashedRewrite("before_model", crashed, decision); cerr != nil {
			return nil, cerr
		}
		if decision.Request != nil {
			if verr := validateHookRequest(decision.Request); verr != nil {
				return nil, fmt.Errorf("command hook %q rejected before_model rewrite: %w", h.name, verr)
			}
			h.recordRewrite("before_model", "request")
			*req = *decision.Request
			return nil, nil
		}
		h.recordRewrite("before_model", "content")
		return &ModelHookResult{
			Content:      decision.Content,
			FinishReason: decision.FinishReason,
			Usage:        llm.Usage{},
		}, nil
	case "deny":
		h.recordRewrite("before_model", "deny")
		return &ModelHookResult{Content: commandDecisionMessage(decision, "command hook denied model call"), FinishReason: "hook_deny"}, nil
	default:
		return nil, h.unknownDecisionError("before_model", decision)
	}
}

// BeforeTool sends a before_tool event to the command hook.
func (h *CommandHook) BeforeTool(ctx context.Context, call llm.ToolCall) (*ToolHookResult, error) {
	decision, crashed, err := h.run(ctx, CommandHookEvent{Event: "before_tool", ToolCall: &call})
	if err != nil || isAllowDecision(decision) {
		return nil, err
	}
	switch normalizedDecision(decision) {
	case "rewrite":
		if cerr := h.rejectCrashedRewrite("before_tool", crashed, decision); cerr != nil {
			return nil, cerr
		}
		if verr := validateHookToolRewrite(decision.ToolCall, decision.ToolResult); verr != nil {
			return nil, fmt.Errorf("command hook %q rejected before_tool rewrite: %w", h.name, verr)
		}
		h.recordRewrite("before_tool", "tool_call")
		return &ToolHookResult{Call: decision.ToolCall, Result: decision.ToolResult}, nil
	case "deny":
		h.recordRewrite("before_tool", "deny")
		res := commandDecisionToolResult(decision, "command hook denied tool call")
		return &ToolHookResult{Result: &res}, nil
	default:
		return nil, h.unknownDecisionError("before_tool", decision)
	}
}

// AfterTool sends an after_tool event to the command hook.
func (h *CommandHook) AfterTool(ctx context.Context, call llm.ToolCall, res tools.ToolResult) (*ToolResultHookResult, error) {
	decision, crashed, err := h.run(ctx, CommandHookEvent{Event: "after_tool", ToolCall: &call, ToolResult: &res})
	if err != nil || isAllowDecision(decision) {
		return nil, err
	}
	switch normalizedDecision(decision) {
	case "rewrite":
		if cerr := h.rejectCrashedRewrite("after_tool", crashed, decision); cerr != nil {
			return nil, cerr
		}
		if verr := validateHookToolResult(decision.ToolResult); verr != nil {
			return nil, fmt.Errorf("command hook %q rejected after_tool rewrite: %w", h.name, verr)
		}
		h.recordRewrite("after_tool", "tool_result")
		return &ToolResultHookResult{Result: decision.ToolResult}, nil
	case "deny":
		h.recordRewrite("after_tool", "deny")
		next := commandDecisionToolResult(decision, "command hook denied tool result")
		return &ToolResultHookResult{Result: &next}, nil
	default:
		return nil, h.unknownDecisionError("after_tool", decision)
	}
}

// OnTurnEnd sends a turn_end event to the command hook.
func (h *CommandHook) OnTurnEnd(ctx context.Context, turn HookTurn) error {
	decision, _, err := h.run(ctx, CommandHookEvent{Event: "turn_end", Turn: &turn})
	if err != nil {
		return err
	}
	return h.lifecycleDecisionError("turn_end", decision)
}

// recordRewrite audits a hook rewrite/deny without raw secrets — only the hook
// name, point, and rewrite kind are recorded (AG-003).
func (h *CommandHook) recordRewrite(point, kind string) {
	reasoningtrace.Record("hook_rewrite", map[string]any{
		"hook":  h.name,
		"point": point,
		"kind":  kind,
	})
}

// run executes the hook and returns its decision plus a crashed flag: crashed
// is true when the process exited non-zero but still emitted a parseable
// non-allow decision. The caller honors a crashed `deny` (security-safe) but
// rejects a crashed `rewrite` (AG-030): a hook that crashed after emitting a
// rewrite must not have attacker-influenced state applied as success.
func (h *CommandHook) run(ctx context.Context, event CommandHookEvent) (decision *CommandHookDecision, crashed bool, err error) {
	if verr := h.verifyTrust(); verr != nil {
		return nil, false, verr
	}
	payload, merr := json.Marshal(event)
	if merr != nil {
		return nil, false, fmt.Errorf("command hook %q marshal %s event: %w", h.name, event.Event, merr)
	}
	hookCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	cmd := exec.CommandContext(hookCtx, h.command, h.args...) //nolint:gosec // resolved command is hash-verified immediately before execution.
	cmd.Env = commandHookEnv(h.env)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if hookCtx.Err() == context.DeadlineExceeded {
		return nil, false, fmt.Errorf("command hook %q timed out after %s", h.name, h.timeout)
	}
	parsed, parseErr := parseCommandHookDecision(stdout.Bytes())
	if runErr != nil {
		if parseErr == nil && !isAllowDecision(parsed) {
			return parsed, true, nil
		}
		return nil, true, fmt.Errorf("command hook %q failed: %w%s", h.name, runErr, stderrSuffix(stderr.String()))
	}
	if parseErr != nil {
		return nil, false, fmt.Errorf("command hook %q invalid stdout: %w", h.name, parseErr)
	}
	return parsed, false, nil
}

// rejectCrashedRewrite returns an error when a non-zero-exit hook attempted a
// rewrite. Denial on non-zero exit is allowed (security-safe); rewrite is not.
func (h *CommandHook) rejectCrashedRewrite(event string, crashed bool, decision *CommandHookDecision) error {
	if crashed && normalizedDecision(decision) == "rewrite" {
		return fmt.Errorf("command hook %q rejected %s rewrite: hook exited non-zero after emitting a rewrite", h.name, event)
	}
	return nil
}

func commandHookEnv(explicit []string) []string {
	env := make([]string, 0, len(explicit)+4)
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !allowedCommandHookParentEnv(k) || secret.IsSecretEnvVar(k, v) {
			continue
		}
		env = append(env, kv)
	}
	for _, kv := range explicit {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(k) == "" || secret.IsSecretEnvVar(k, v) {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func allowedCommandHookParentEnv(key string) bool {
	switch strings.ToUpper(key) {
	case "PATH", "SYSTEMROOT", "WINDIR":
		return true
	default:
		return false
	}
}

// validateHookRequest bounds a hook-supplied request replacement (AG-003): a
// rewrite may not inflate the message/tool counts or per-field content beyond
// the configured caps before it replaces live request state.
func validateHookRequest(req *llm.Request) error {
	if req == nil {
		return fmt.Errorf("nil request")
	}
	if len(req.Messages) > maxHookRewriteMessages {
		return fmt.Errorf("rewrite messages %d exceed cap %d", len(req.Messages), maxHookRewriteMessages)
	}
	if len(req.Tools) > maxHookRewriteTools {
		return fmt.Errorf("rewrite tools %d exceed cap %d", len(req.Tools), maxHookRewriteTools)
	}
	for i := range req.Messages {
		if len(req.Messages[i].Content) > maxHookRewriteContentByte {
			return fmt.Errorf("rewrite message %d content %d bytes exceeds cap %d", i, len(req.Messages[i].Content), maxHookRewriteContentByte)
		}
	}
	return nil
}

// validateHookToolRewrite bounds a before_tool rewrite payload (AG-003).
func validateHookToolRewrite(call *llm.ToolCall, res *tools.ToolResult) error {
	if call != nil && len(call.Function.Arguments) > maxHookRewriteContentByte {
		return fmt.Errorf("rewrite tool arguments %d bytes exceed cap %d", len(call.Function.Arguments), maxHookRewriteContentByte)
	}
	return validateHookToolResult(res)
}

// validateHookToolResult bounds a hook-supplied tool result preview (AG-003).
func validateHookToolResult(res *tools.ToolResult) error {
	if res != nil && len(res.Preview) > maxHookRewriteContentByte {
		return fmt.Errorf("rewrite tool result preview %d bytes exceeds cap %d", len(res.Preview), maxHookRewriteContentByte)
	}
	return nil
}

func (h *CommandHook) verifyTrust() error {
	got, err := fileSHA256(h.command)
	if err != nil {
		return fmt.Errorf("command hook %q hash %s: %w", h.name, h.command, err)
	}
	if got != h.expectedSHA256 {
		return fmt.Errorf("command hook %q trust hash mismatch: got %s want %s", h.name, got, h.expectedSHA256)
	}
	return nil
}

func (h *CommandHook) lifecycleDecisionError(event string, decision *CommandHookDecision) error {
	if isAllowDecision(decision) {
		return nil
	}
	if normalizedDecision(decision) == "deny" {
		return fmt.Errorf("command hook %q denied %s: %s", h.name, event, commandDecisionMessage(decision, "denied"))
	}
	return h.unknownDecisionError(event, decision)
}

func (h *CommandHook) unknownDecisionError(event string, decision *CommandHookDecision) error {
	return fmt.Errorf("command hook %q returned unsupported %s decision %q", h.name, event, normalizedDecision(decision))
}

func parseCommandHookDecision(stdout []byte) (*CommandHookDecision, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return &CommandHookDecision{Decision: "allow"}, nil
	}
	var decision CommandHookDecision
	if err := json.Unmarshal(trimmed, &decision); err != nil {
		return nil, err
	}
	return &decision, nil
}

func isAllowDecision(decision *CommandHookDecision) bool {
	d := normalizedDecision(decision)
	return d == "" || d == "allow"
}

func normalizedDecision(decision *CommandHookDecision) string {
	if decision == nil {
		return "allow"
	}
	return strings.ToLower(strings.TrimSpace(decision.Decision))
}

func commandDecisionToolResult(decision *CommandHookDecision, fallback string) tools.ToolResult {
	if decision != nil && decision.ToolResult != nil {
		res := *decision.ToolResult
		if res.Bytes == 0 && res.Preview != "" {
			res.Bytes = len(res.Preview)
		}
		return res
	}
	msg := commandDecisionMessage(decision, fallback)
	return tools.ToolResult{Preview: msg, Bytes: len(msg)}
}

func commandDecisionMessage(decision *CommandHookDecision, fallback string) string {
	if decision != nil {
		if strings.TrimSpace(decision.Content) != "" {
			return decision.Content
		}
		if strings.TrimSpace(decision.Message) != "" {
			return decision.Message
		}
	}
	return fallback
}

// resolveHookCommand requires an absolute path (AG-054). Bare command names
// resolved against the runtime $PATH are rejected: a hook binary must be pinned
// to a known location so the SHA-256 trust gate is meaningful (a PATH lookup
// could resolve to an attacker-shadowed binary earlier on the path).
func resolveHookCommand(command string) (string, error) {
	if !filepath.IsAbs(command) {
		return "", fmt.Errorf("command hook executable %q must be an absolute path; bare command names resolved via $PATH are rejected", command)
	}
	return command, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is an operator-configured hook executable verified by sha256.
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func stderrSuffix(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}
