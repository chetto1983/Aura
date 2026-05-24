package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/storage/sources/store"
)

// ExecuteCodeTool lets the LLM run Python code in Aura's isolated runtime.
type ExecuteCodeTool struct {
	manager     sandbox.Executor
	sender      DocumentSender
	sourceStore source.Writer
	registry    *Registry
}

// ExecuteShellTool lets the LLM run shell commands in Aura's process runtime.
type ExecuteShellTool struct {
	manager sandbox.CommandExecutor
}

// NewExecuteCodeToolWithStoreAndRegistry creates execute_code with optional
// artifact delivery, source persistence, and bounded internal tool orchestration.
func NewExecuteCodeToolWithStoreAndRegistry(manager sandbox.Executor, sender DocumentSender, sourceStore source.Writer, registry *Registry) *ExecuteCodeTool {
	if isNilExecutor(manager) {
		return nil
	}
	return &ExecuteCodeTool{manager: manager, sender: sender, sourceStore: sourceStore, registry: registry}
}

func NewExecuteShellTool(manager sandbox.CommandExecutor) *ExecuteShellTool {
	if isNilCommandExecutor(manager) {
		return nil
	}
	if runtime, ok := manager.(interface{ RuntimeKind() sandbox.RuntimeKind }); ok && runtime.RuntimeKind() != sandbox.RuntimeKindProcess {
		return nil
	}
	return &ExecuteShellTool{manager: manager}
}

func isNilExecutor(manager sandbox.Executor) bool {
	if manager == nil {
		return true
	}
	v := reflect.ValueOf(manager)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func isNilCommandExecutor(manager sandbox.CommandExecutor) bool {
	if manager == nil {
		return true
	}
	v := reflect.ValueOf(manager)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (t *ExecuteCodeTool) Name() string { return "execute_code" }

func (t *ExecuteCodeTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:            t.Name(),
		Description:     t.Description(),
		Parameters:      t.Parameters(),
		DestructiveHint: true, // Python sandbox can write files and run arbitrary side-effects
		VisibilityTier:  VisibilityDeferred,
	}
}

func (t *ExecuteCodeTool) Description() string {
	return `Execute Python code in Aura's Ephemeral sandbox runtime. Read-only stdout, capped, results are INTERNAL — synthesize before replying.

EXAMPLES — copy the shape exactly:

  execute_code({"code":"print(sum(range(1,101)))"})
  execute_code({"code":"import json; print(json.dumps({\"x\":1,\"y\":2}))","timeout":10})

Only "code" is REQUIRED.`
}

func (t *ExecuteCodeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{
				"type":        "string",
				"description": "REQUIRED. Python code to execute in the sandbox.",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Optional. Per-call timeout in seconds (1-600). Defaults to 300s. Raise for long computations.",
				"minimum":     1,
				"maximum":     600,
			},
			"tools_allowed": map[string]any{
				"type":        "array",
				"description": "Optional. Tool names visible to internal orchestration when the script writes aura_tool_calls.json.",
				"items": map[string]any{
					"type": "string",
				},
			},
			"max_calls": map[string]any{
				"type":        "integer",
				"description": "Optional. Max internal tool calls from aura_tool_calls.json. Default 10, cap 20.",
				"minimum":     1,
				"maximum":     20,
			},
		},
		"required": []string{"code"},
		"examples": []any{
			map[string]any{"code": "print(sum(range(1,101)))"},
			map[string]any{"code": "import json; print(json.dumps({\"x\":1,\"y\":2}))", "timeout": 10},
		},
	}
}

func (t *ExecuteCodeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	code, ok := args["code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("code is required and must be a string")
	}

	timeoutSec := intArg(args, "timeout", 0, 0, 600)
	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}

	toolsAllowed := stringSliceArg(args, "tools_allowed")
	maxCalls := intArg(args, "max_calls", 10, 1, 20)

	// The current process runtime shares the Aura container's network namespace,
	// so no per-call network gate exists. Hard-coded to false to make the
	// (currently advisory) runtime arg explicit; future namespaced runtimes can
	// expose the gate back through the tool schema.
	result, err := t.manager.Execute(ctx, code, false)
	if err != nil {
		return "", fmt.Errorf("sandbox execution failed: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("execution failed (exit=%d): %s", result.ExitCode, result.Stderr)
	}
	artifacts, manifest, err := splitInternalToolCallManifest(result.Artifacts)
	if err != nil {
		return "", err
	}

	out := fmt.Sprintf("exit_code: %d\nelapsed_ms: %d\n\n%s", result.ExitCode, result.ElapsedMs, result.Stdout)
	if result.Stderr != "" {
		out += fmt.Sprintf("\n--- stderr ---\n%s", result.Stderr)
	}
	if manifest != nil {
		internalResults, err := t.executeInternalToolManifest(ctx, manifest, toolsAllowed, maxCalls)
		if err != nil {
			return "", err
		}
		out += "\n\ninternal_tool_results:\n" + internalResults
	}
	if len(artifacts) > 0 {
		persisted, err := t.persistArtifacts(ctx, artifacts)
		if err != nil {
			return "", err
		}
		delivered, err := t.deliverArtifacts(ctx, artifacts)
		if err != nil {
			return "", err
		}
		out += "\n\nartifacts:"
		for i, artifact := range artifacts {
			out += fmt.Sprintf("\n- %s (%d bytes, %s, delivered=%t, persisted=%t",
				artifact.Name, artifact.SizeBytes, artifact.MimeType, delivered[i], persisted[i].ok)
			if persisted[i].sourceID != "" {
				out += fmt.Sprintf(", source_id=%s", persisted[i].sourceID)
			}
			if persisted[i].duplicate {
				out += ", duplicate=true"
			}
			out += ")"
		}
	}
	return out, nil
}

const internalToolCallManifestName = "aura_tool_calls.json"

// blockedInternalToolCalls names tools that must never be invokable from an
// execute_code internal manifest, regardless of tools_allowed. The list is
// privilege-escalation-shaped: tools that mint credentials, that can recurse
// into another sandbox call, or that perform irreversible destructive writes.
// CLAUDE.md's "tools the LLM trusted in the active turn" allowlist sits on
// top of this — both must permit the call for it to run.
var blockedInternalToolCalls = map[string]bool{
	"execute_code":  true,
	"execute_shell": true,
	"delete_source": true,
	"forget_memory": true,
}

// perInternalToolCallTimeout caps each call inside the manifest loop. A single
// misbehaving tool that ignores ctx would otherwise hang the remaining calls
// indefinitely — the outer execute_code timeout (1-600s) catches it eventually
// but blocks the manifest from making forward progress.
const perInternalToolCallTimeout = 30 * time.Second

// internalManifestConcurrency caps fan-out when execute_code emits a manifest
// with multiple calls. Aligned with the agent loop's tool-fanout pattern so
// independent calls run in parallel without exhausting downstream services.
const internalManifestConcurrency = 4

type internalToolCallManifest struct {
	Calls []internalToolCall `json:"calls"`
}

type internalToolCall struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args,omitempty"`
}

type internalToolCallResult struct {
	Tool   string `json:"tool"`
	OK     bool   `json:"ok"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type internalToolCallResults struct {
	Calls []internalToolCallResult `json:"calls"`
}

func splitInternalToolCallManifest(artifacts []sandbox.Artifact) ([]sandbox.Artifact, []byte, error) {
	filtered := make([]sandbox.Artifact, 0, len(artifacts))
	var manifest []byte
	for _, artifact := range artifacts {
		if strings.EqualFold(strings.TrimSpace(artifact.Name), internalToolCallManifestName) {
			if manifest != nil {
				return nil, nil, fmt.Errorf("execute_code: multiple %s artifacts emitted", internalToolCallManifestName)
			}
			manifest = append([]byte(nil), artifact.Bytes...)
			continue
		}
		filtered = append(filtered, artifact)
	}
	return filtered, manifest, nil
}

func (t *ExecuteCodeTool) executeInternalToolManifest(ctx context.Context, body []byte, toolsAllowed []string, maxCalls int) (string, error) {
	if t.registry == nil {
		return "", fmt.Errorf("execute_code: internal tool orchestration is unavailable")
	}
	if len(toolsAllowed) == 0 {
		return "", fmt.Errorf("execute_code: tools_allowed is required when %s is emitted", internalToolCallManifestName)
	}

	activeTools := AllowedToolNamesFromContext(ctx)
	if len(activeTools) == 0 {
		return "", fmt.Errorf("execute_code: no active-turn tool allowlist in context")
	}
	for _, name := range toolsAllowed {
		if !toolNameInList(name, activeTools) {
			return "", fmt.Errorf("execute_code: tool %q is not available in the active turn", name)
		}
	}

	var manifest internalToolCallManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", fmt.Errorf("execute_code: invalid %s: %w", internalToolCallManifestName, err)
	}
	if len(manifest.Calls) == 0 {
		return marshalInternalToolResults(internalToolCallResults{Calls: []internalToolCallResult{}})
	}
	if len(manifest.Calls) > maxCalls {
		return "", fmt.Errorf("execute_code: %s requested %d internal calls, max_calls is %d", internalToolCallManifestName, len(manifest.Calls), maxCalls)
	}

	// Validate every call up front so a malformed entry rejects the whole
	// manifest before any side effects fire. Parallel execution below relies
	// on this — partial results from a half-rejected manifest would be hard
	// to reason about from the LLM's side.
	for i, call := range manifest.Calls {
		name := strings.TrimSpace(call.Tool)
		if name == "" {
			return "", fmt.Errorf("execute_code: %s call %d has empty tool", internalToolCallManifestName, i+1)
		}
		if blockedInternalToolCalls[name] {
			return "", fmt.Errorf("execute_code: tool %q cannot be called from internal orchestration", name)
		}
		if !toolNameInList(name, toolsAllowed) {
			return "", fmt.Errorf("execute_code: tool %q was not listed in tools_allowed", name)
		}
		if !toolNameInList(name, activeTools) {
			return "", fmt.Errorf("execute_code: tool %q is not available in the active turn", name)
		}
		if t.registry.Get(name) == nil {
			return "", fmt.Errorf("execute_code: tool %q is not registered", name)
		}
	}

	// Dispatch in parallel with bounded concurrency. Order is preserved by
	// indexing into a pre-sized slice; each goroutine writes its own slot,
	// so no mutex is needed. The per-call timeout bounds any single tool's
	// damage to the manifest's overall latency.
	entries := make([]internalToolCallResult, len(manifest.Calls))
	sem := make(chan struct{}, internalManifestConcurrency)
	var wg sync.WaitGroup
	for i, call := range manifest.Calls {
		i, call := i, call
		name := strings.TrimSpace(call.Tool)
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			callCtx, cancel := context.WithTimeout(ctx, perInternalToolCallTimeout)
			defer cancel()
			result, err := t.registry.Execute(callCtx, name, call.Args)
			entry := internalToolCallResult{Tool: name, OK: err == nil, Result: result}
			if err != nil {
				entry.Error = err.Error()
				entry.Result = ""
			}
			entries[i] = entry
		}()
	}
	wg.Wait()
	return marshalInternalToolResults(internalToolCallResults{Calls: entries})
}

func marshalInternalToolResults(results internalToolCallResults) (string, error) {
	body, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (t *ExecuteShellTool) Name() string { return "execute_shell" }

func (t *ExecuteShellTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:            t.Name(),
		Description:     t.Description(),
		Parameters:      t.Parameters(),
		DestructiveHint: true, // shell commands can modify filesystem, network, and runtime state
		VisibilityTier:  VisibilityDeferred,
	}
}

func (t *ExecuteShellTool) Description() string {
	return `Execute a shell command in Aura's container runtime. Read-only stdout, capped, results are INTERNAL — synthesize before replying.

EXAMPLES — copy the shape exactly:

  execute_shell({"command":"uname -a"})
  execute_shell({"command":"ls /workspace/wiki | head -5"})
  execute_shell({"command":"curl -sSL https://example.com | head -c 200","timeout":15})

Only "command" is REQUIRED.`
}

func (t *ExecuteShellTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "REQUIRED. Shell command to execute in the Aura runtime workspace.",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Optional. Per-call timeout in seconds (1-600). Defaults to 300s.",
				"minimum":     1,
				"maximum":     600,
			},
		},
		"required": []string{"command"},
		"examples": []any{
			map[string]any{"command": "uname -a"},
			map[string]any{"command": "ls /workspace/wiki | head -5"},
			map[string]any{"command": "curl -sSL https://example.com | head -c 200", "timeout": 15},
		},
	}
}

func (t *ExecuteShellTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	command, ok := args["command"].(string)
	if !ok || strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command is required and must be a string")
	}

	timeoutSec := intArg(args, "timeout", 0, 0, 600)
	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}

	// Process runtime shares the Aura container network; hard-code the runtime
	// arg to false. Future namespaced runtimes can re-expose this through the schema.
	result, err := t.manager.ExecuteCommand(ctx, command, false)
	if err != nil {
		return "", fmt.Errorf("shell command failed: %w", err)
	}
	if !result.OK {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		return "", fmt.Errorf("shell command failed (exit=%d): %s", result.ExitCode, detail)
	}

	out := fmt.Sprintf("exit_code: %d\nelapsed_ms: %d\n\n%s", result.ExitCode, result.ElapsedMs, result.Stdout)
	if strings.TrimSpace(result.Stderr) != "" {
		out += fmt.Sprintf("\n--- stderr ---\n%s", result.Stderr)
	}
	if len(result.Artifacts) > 0 {
		out += "\n\nartifacts:"
		for _, artifact := range result.Artifacts {
			out += fmt.Sprintf("\n- %s (%d bytes, %s)", artifact.Name, artifact.SizeBytes, artifact.MimeType)
		}
	}
	return out, nil
}

type persistedArtifact struct {
	ok        bool
	sourceID  string
	duplicate bool
}

func (t *ExecuteCodeTool) persistArtifacts(ctx context.Context, artifacts []sandbox.Artifact) ([]persistedArtifact, error) {
	persisted := make([]persistedArtifact, len(artifacts))
	if t.sourceStore == nil {
		return persisted, nil
	}
	for i, artifact := range artifacts {
		name := strings.TrimSpace(artifact.Name)
		if name == "" {
			name = fmt.Sprintf("artifact-%d.bin", i+1)
		}
		mime := strings.TrimSpace(artifact.MimeType)
		if mime == "" {
			mime = "application/octet-stream"
		}
		src, dup, err := t.sourceStore.Put(ctx, source.PutInput{
			Kind:     source.KindSandboxArtifact,
			Filename: name,
			MimeType: mime,
			Bytes:    artifact.Bytes,
		})
		if err != nil {
			return persisted, fmt.Errorf("execute_code: persist artifact %s: %w", name, err)
		}
		if src.Status != source.StatusIngested {
			src, err = t.sourceStore.Update(src.ID, func(rec *source.Source) error {
				rec.Status = source.StatusIngested
				rec.Error = ""
				return nil
			})
			if err != nil {
				return persisted, fmt.Errorf("execute_code: mark artifact %s ingested: %w", name, err)
			}
		}
		persisted[i] = persistedArtifact{ok: true, sourceID: src.ID, duplicate: dup}
	}
	return persisted, nil
}

func (t *ExecuteCodeTool) deliverArtifacts(ctx context.Context, artifacts []sandbox.Artifact) ([]bool, error) {
	delivered := make([]bool, len(artifacts))
	if t.sender == nil || UserIDFromContext(ctx) == "" {
		return delivered, nil
	}
	userID := UserIDFromContext(ctx)
	for i, artifact := range artifacts {
		name := strings.TrimSpace(artifact.Name)
		if name == "" {
			name = fmt.Sprintf("artifact-%d.bin", i+1)
		}
		caption := "Aura sandbox artifact: " + name
		if err := t.sender.SendDocumentToUser(userID, name, artifact.Bytes, caption); err != nil {
			return delivered, fmt.Errorf("execute_code: artifact %s delivery failed: %w", name, err)
		}
		delivered[i] = true
	}
	return delivered, nil
}
