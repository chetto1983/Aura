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
	"github.com/chetto1983/aura/internal/secret"
)

const defaultCommandHookTimeout = 2 * time.Second

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
	decision, err := h.run(ctx, CommandHookEvent{Event: "turn_start", Turn: &turn})
	if err != nil {
		return err
	}
	return h.lifecycleDecisionError("turn_start", decision)
}

// BeforeModel sends a before_model event to the command hook.
func (h *CommandHook) BeforeModel(ctx context.Context, req *llm.Request) (*ModelHookResult, error) {
	decision, err := h.run(ctx, CommandHookEvent{Event: "before_model", Request: req})
	if err != nil || isAllowDecision(decision) {
		return nil, err
	}
	switch normalizedDecision(decision) {
	case "rewrite":
		if decision.Request != nil {
			*req = *decision.Request
			return nil, nil
		}
		return &ModelHookResult{
			Content:      decision.Content,
			FinishReason: decision.FinishReason,
			Usage:        llm.Usage{},
		}, nil
	case "deny":
		return &ModelHookResult{Content: commandDecisionMessage(decision, "command hook denied model call"), FinishReason: "hook_deny"}, nil
	default:
		return nil, h.unknownDecisionError("before_model", decision)
	}
}

// BeforeTool sends a before_tool event to the command hook.
func (h *CommandHook) BeforeTool(ctx context.Context, call llm.ToolCall) (*ToolHookResult, error) {
	decision, err := h.run(ctx, CommandHookEvent{Event: "before_tool", ToolCall: &call})
	if err != nil || isAllowDecision(decision) {
		return nil, err
	}
	switch normalizedDecision(decision) {
	case "rewrite":
		return &ToolHookResult{Call: decision.ToolCall, Result: decision.ToolResult}, nil
	case "deny":
		res := commandDecisionToolResult(decision, "command hook denied tool call")
		return &ToolHookResult{Result: &res}, nil
	default:
		return nil, h.unknownDecisionError("before_tool", decision)
	}
}

// AfterTool sends an after_tool event to the command hook.
func (h *CommandHook) AfterTool(ctx context.Context, call llm.ToolCall, res tools.ToolResult) (*ToolResultHookResult, error) {
	decision, err := h.run(ctx, CommandHookEvent{Event: "after_tool", ToolCall: &call, ToolResult: &res})
	if err != nil || isAllowDecision(decision) {
		return nil, err
	}
	switch normalizedDecision(decision) {
	case "rewrite":
		return &ToolResultHookResult{Result: decision.ToolResult}, nil
	case "deny":
		next := commandDecisionToolResult(decision, "command hook denied tool result")
		return &ToolResultHookResult{Result: &next}, nil
	default:
		return nil, h.unknownDecisionError("after_tool", decision)
	}
}

// OnTurnEnd sends a turn_end event to the command hook.
func (h *CommandHook) OnTurnEnd(ctx context.Context, turn HookTurn) error {
	decision, err := h.run(ctx, CommandHookEvent{Event: "turn_end", Turn: &turn})
	if err != nil {
		return err
	}
	return h.lifecycleDecisionError("turn_end", decision)
}

func (h *CommandHook) run(ctx context.Context, event CommandHookEvent) (*CommandHookDecision, error) {
	if err := h.verifyTrust(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("command hook %q marshal %s event: %w", h.name, event.Event, err)
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
		return nil, fmt.Errorf("command hook %q timed out after %s", h.name, h.timeout)
	}
	decision, parseErr := parseCommandHookDecision(stdout.Bytes())
	if runErr != nil {
		if parseErr == nil && !isAllowDecision(decision) {
			return decision, nil
		}
		return nil, fmt.Errorf("command hook %q failed: %w%s", h.name, runErr, stderrSuffix(stderr.String()))
	}
	if parseErr != nil {
		return nil, fmt.Errorf("command hook %q invalid stdout: %w", h.name, parseErr)
	}
	return decision, nil
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

func resolveHookCommand(command string) (string, error) {
	if filepath.IsAbs(command) || strings.ContainsAny(command, `/\`) {
		return command, nil
	}
	resolved, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("command hook executable %q: %w", command, err)
	}
	return resolved, nil
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
