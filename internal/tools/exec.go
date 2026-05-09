package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/source"
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

// NewExecuteCodeTool creates the execute_code tool. Returns nil if manager
// is nil (sandbox not available).
func NewExecuteCodeTool(manager sandbox.Executor) *ExecuteCodeTool {
	return NewExecuteCodeToolWithStore(manager, nil, nil)
}

// NewExecuteCodeToolWithSender creates execute_code with optional artifact
// delivery. The sender is used only when sandbox code emits artifacts.
func NewExecuteCodeToolWithSender(manager sandbox.Executor, sender DocumentSender) *ExecuteCodeTool {
	return NewExecuteCodeToolWithStore(manager, sender, nil)
}

// NewExecuteCodeToolWithStore creates execute_code with optional artifact
// delivery and source persistence. The store is used only when sandbox code
// emits artifacts.
func NewExecuteCodeToolWithStore(manager sandbox.Executor, sender DocumentSender, sourceStore source.Writer) *ExecuteCodeTool {
	return NewExecuteCodeToolWithStoreAndRegistry(manager, sender, sourceStore, nil)
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

func (t *ExecuteCodeTool) Description() string {
	return "Execute Python code in Aura's configured runtime. In Docker this runs directly inside the Aura container with the same mounted workspace access as Aura. " +
		"Use this for calculations, data processing, simulations, or any task that requires running code. " +
		"For multi-step workflows, first use tool_search, then pass tools_allowed and have the script write /tmp/aura_out/aura_tool_calls.json with calls [{\"tool\":\"name\",\"args\":{...}}]; Aura executes only those active-turn tools after the script exits. " +
		"Use loops, transforms, retries, and structured JSON output inside one script instead of many model/backend round trips. " +
		"The execution process is ephemeral; durable state should be written through workspace tools or emitted as artifacts. " +
		"Use create_xlsx/create_docx/create_pdf for simple documents; use this for computed artifacts, plots, custom data exports, or workflows that genuinely need code. " +
		"To return files, write them under /tmp/aura_out; Aura collects plain files from that directory, persists them as sandbox_artifact sources, and delivers them to Telegram when possible. The internal tool-call manifest is control data and is not persisted as a user artifact. " +
		"Set allow_network=true only when HTTP access is explicitly needed; process runtimes may already share the container network. " +
		"Timeout is configurable up to the server limit (default 120s)."
}

func (t *ExecuteCodeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{
				"type":        "string",
				"description": "Python code to execute in the sandbox",
			},
			"allow_network": map[string]any{
				"type":        "boolean",
				"description": "Allow network access from the sandbox. Default false.",
			},
			"tools_allowed": map[string]any{
				"type":        "array",
				"description": "Optional tool names returned by tool_search and visible in this turn. Required when the script writes aura_tool_calls.json for internal tool orchestration.",
				"items": map[string]any{
					"type": "string",
				},
			},
			"max_calls": map[string]any{
				"type":        "integer",
				"description": "Maximum internal tool calls to execute from aura_tool_calls.json. Defaults to 10 and is capped at 20.",
				"minimum":     1,
				"maximum":     20,
			},
		},
		"required": []string{"code"},
	}
}

func (t *ExecuteCodeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	code, ok := args["code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("code is required and must be a string")
	}

	allowNetwork := false
	if v, ok := args["allow_network"].(bool); ok {
		allowNetwork = v
	}
	toolsAllowed := stringSliceArg(args, "tools_allowed")
	maxCalls := intArg(args, "max_calls", 10, 1, 20)

	result, err := t.manager.Execute(ctx, code, allowNetwork)
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

var blockedInternalToolCalls = map[string]bool{
	"execute_code":  true,
	"execute_shell": true,
	"tool_search":   true,
}

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
			return "", fmt.Errorf("execute_code: tool %q is not available in the active turn; use tool_search first", name)
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

	results := internalToolCallResults{Calls: make([]internalToolCallResult, 0, len(manifest.Calls))}
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
			return "", fmt.Errorf("execute_code: tool %q is not available in the active turn; use tool_search first", name)
		}
		if t.registry.Get(name) == nil {
			return "", fmt.Errorf("execute_code: tool %q is not registered", name)
		}
		result, err := t.registry.Execute(ctx, name, call.Args)
		entry := internalToolCallResult{Tool: name, OK: err == nil, Result: result}
		if err != nil {
			entry.Error = err.Error()
			entry.Result = ""
		}
		results.Calls = append(results.Calls, entry)
	}
	return marshalInternalToolResults(results)
}

func marshalInternalToolResults(results internalToolCallResults) (string, error) {
	body, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (t *ExecuteShellTool) Name() string { return "execute_shell" }

func (t *ExecuteShellTool) Description() string {
	return "Execute a shell command inside Aura's configured process runtime. In Docker this runs inside the Aura container, in the configured workspace, with the same filesystem, network, Python, pip, git, and CLI access as Aura. " +
		"Use this for repository inspection, tests, builds, package checks, pip installs, git status/diff/log, sqlite/jq/rg/curl diagnostics, and runtime smoke checks when file tools or execute_code are not enough. " +
		"Commands are bounded by the server timeout and output limits. Prefer narrow, reversible commands; avoid destructive commands unless the user explicitly asked for them. " +
		"Set allow_network=true only when the command intentionally needs network access."
}

func (t *ExecuteShellTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute in the Aura runtime workspace.",
			},
			"allow_network": map[string]any{
				"type":        "boolean",
				"description": "Allow network access from the command. Default false.",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecuteShellTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	command, ok := args["command"].(string)
	if !ok || strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command is required and must be a string")
	}

	allowNetwork := false
	if v, ok := args["allow_network"].(bool); ok {
		allowNetwork = v
	}

	result, err := t.manager.ExecuteCommand(ctx, command, allowNetwork)
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
