package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/sandboxagent"
)

type sandboxRunner interface {
	Run(ctx context.Context, req sandboxagent.RunRequest) (sandboxagent.RunResponse, error)
}

// SandboxExec runs one command inside the local aura-sandbox-agent container via
// sandbox-agent's /v1/processes/run API.
type SandboxExec struct {
	Runner sandboxRunner
}

type sandboxExecArgs struct {
	Command        string            `json:"command"`
	Args           []string          `json:"args"`
	Cwd            string            `json:"cwd"`
	Env            map[string]string `json:"env"`
	TimeoutMs      int64             `json:"timeout_ms"`
	MaxOutputBytes int               `json:"max_output_bytes"`
}

func (s *SandboxExec) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "Executable to run inside the local Aura sandbox container, for example python, sh, node, or go."},
    "args": {"type": "array", "items": {"type": "string"}, "description": "Command arguments. Prefer args over shell quoting when possible."},
    "cwd": {"type": "string", "description": "Optional working directory inside the sandbox container, for example /workspace."},
    "env": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional environment variables for this command only."},
    "timeout_ms": {"type": "integer", "minimum": 0, "description": "Optional command timeout in milliseconds. Omit for sandbox default."},
    "max_output_bytes": {"type": "integer", "minimum": 0, "description": "Optional stdout/stderr byte cap. Omit for sandbox default."}
  },
  "required": ["command"]
}`)
	return Spec{
		Name:        "sandbox_exec",
		Summary:     "Run a command in the local Aura sandbox container.",
		Description: "Run one command in the local Aura sandbox container through sandbox-agent. Use this for code execution, calculations, scripts, filesystem work inside the sandbox, and quick verification. The sandbox is local to the machine and should be started with `make sandbox-up`; it is not an online service. Results include stdout, stderr, exit_code, timeout/truncation flags, and duration_ms.",
		Parameters:  params,
		// NOT deferred: the model must see the command/args schema to call this
		// correctly. A live E2E with it deferred had the agent cram the whole command
		// line into `command` ("python3 -c ...") → sandbox-agent 502 "failed to spawn".
		Deferred: false,
	}
}

func (s *SandboxExec) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a sandboxExecArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("sandbox_exec args: %w", err)
	}
	if a.Command == "" {
		return ToolResult{}, fmt.Errorf("sandbox_exec: command is required")
	}
	if s.Runner == nil {
		return sandboxUnavailable(ctx, "sandbox-agent client is not configured")
	}

	req := sandboxagent.RunRequest{
		Command: a.Command,
		Args:    a.Args,
		Cwd:     a.Cwd,
		Env:     a.Env,
	}
	if a.TimeoutMs > 0 {
		req.TimeoutMs = &a.TimeoutMs
	}
	if a.MaxOutputBytes > 0 {
		req.MaxOutputBytes = &a.MaxOutputBytes
	}

	res, err := s.Runner.Run(ctx, req)
	if err != nil {
		return sandboxUnavailable(ctx, err.Error())
	}
	out, err := json.Marshal(map[string]any{
		"stdout":           res.Stdout,
		"stderr":           res.Stderr,
		"exit_code":        res.ExitCode,
		"timed_out":        res.TimedOut,
		"stdout_truncated": res.StdoutTruncated,
		"stderr_truncated": res.StderrTruncated,
		"duration_ms":      res.DurationMs,
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("sandbox_exec result marshal: %w", err)
	}
	return NewResult(ctx, string(out))
}

func sandboxUnavailable(ctx context.Context, message string) (ToolResult, error) {
	out, err := json.Marshal(map[string]string{
		"error":   "sandbox_unavailable",
		"message": message,
		"hint":    "start the local sandbox container with `make sandbox-up`",
	})
	if err != nil {
		return ToolResult{}, err
	}
	return NewResult(ctx, string(out))
}
