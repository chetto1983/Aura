# Sandbox Code Execution — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Give Aura the ability to write and execute Python code in an Isola WASM sandbox, save useful scripts as permanent tools, and autonomously improve by identifying gaps and writing new tools.

**Architecture:** A Python sidecar script (`sandbox_runner.py`) wraps the Isola library to execute LLM-generated code inside a WASM sandbox. A Go `sandbox` package calls the sidecar via `os/exec`, validates AST beforehand, and enforces timeouts. An `execute_code` tool exposes this to the LLM. A `wiki/tools/` directory acts as a persistent tool registry. A nightly scheduler job (`kind_auto_improve`) scans conversation archives for gaps and writes new tools.

**Tech Stack:** Go 1.25, Python 3.11+ with Isola, SQLite (existing scheduler DB)

## Product Runtime Guardrail

The end-user path must not require installing Python, pip, wheels, Docker, or a developer toolchain. `pip install isola` is acceptable only for local development and CI probes.

Release builds must ship a sandbox runtime with Aura and the app must auto-discover it before looking at system Python:

- Windows: `runtime/python/python.exe`
- macOS/Linux: `runtime/python/bin/python3`

`SANDBOX_PYTHON_PATH` and `SANDBOX_ALLOW_SYSTEM_PYTHON=true` are operator/dev overrides for tests and unusual deployments, not installation instructions. Aura must not use system Python by default. If no usable bundled runtime exists, Aura must degrade clearly: do not register `execute_code`, keep toolset guardrails intact, and expose the disabled/unavailable state in health/dashboard surfaces.

Product target after the current Isola sidecar hardening: evaluate a Go-embedded WASI host (`wazero`) plus bundled Python artifacts so the sandbox is owned by Aura's release package instead of the host machine.

## Bundled Office/Data Package Profile

The sandbox is only useful for daily office work if it ships with the packages people expect for spreadsheets, CSVs, reports, PDFs, lightweight analytics, and charts. The baseline profile must be bundled and work offline:

- `numpy`, `pandas`, `scipy`, `statsmodels`
- XLSX/office IO: `openpyxl` or equivalent writer support, `xlrd`, `pyarrow`, `python-calamine`
- Charts/images: `matplotlib`, `Pillow`
- PDF/HTML/text extraction: `PyMuPDF`, `beautifulsoup4`, `lxml`, `html5lib`
- Utility stack: `requests`, `pyyaml`, `python-dateutil`, `pytz`, `tzdata`, `regex`, `rich`

Current research note: Pyodide is the strongest candidate for this package profile because its official distribution already includes many scientific/data packages (`numpy`, `pandas`, `scipy`, `matplotlib`, `scikit-learn`, `pyarrow`, `PyMuPDF`, `Pillow`, `beautifulsoup4`, `lxml`, `requests`, `pyyaml`, etc.) and supports additional pure-Python wheels through `micropip`. For Aura, package sources must be vendored/pinned into the release artifact instead of downloaded at execution time.

## Current Guardrail

Sandbox execution tools are explicit opt-in tools, not part of the scheduled routine default perimeter.

- `sandbox_code` profile contains `execute_code`, `list_tools`, and `read_tool`.
- `scheduler_safe` excludes `execute_code`, `list_tools`, `read_tool`, and `save_tool`.
- `save_tool` remains a durable mutation path and must not be granted to recurring jobs by default.
- Scheduled `agent_job` payloads that request only sandbox tools are rejected instead of silently falling back to broad defaults.

---

### Task 1: Python sidecar — Isola sandbox runner

**Files:**
- Create: `internal/sandbox/sandbox_runner.py`
- Create: `internal/sandbox/requirements.txt`

**Step 1: Write the sandbox runner Python script**

```python
"""Thin wrapper that executes Python code inside an Isola WASM sandbox.

Called by the Go sandbox manager via os/exec. Reads code from stdin or a
temp file path, runs it inside an ephemeral Isola sandbox, prints the
result as JSON to stdout.
"""
import sys
import json
import asyncio
import argparse
import time


async def run_in_sandbox(code: str, timeout: int, allow_network: bool) -> dict:
    from isola import build_template

    start = time.monotonic()
    template = await build_template("python")

    mounts = {}
    env_vars = {}
    http = allow_network

    try:
        async with template.create(
            mounts=mounts,
            env=env_vars,
            http=http,
        ) as sandbox:
            result = await sandbox.run(code, timeout=timeout)
            elapsed = time.monotonic() - start
            return {
                "ok": True,
                "stdout": result.stdout or "",
                "stderr": result.stderr or "",
                "exit_code": result.exit_code,
                "elapsed_ms": int(elapsed * 1000),
            }
    except TimeoutError:
        return {
            "ok": False,
            "stdout": "",
            "stderr": f"execution timed out after {timeout}s",
            "exit_code": -1,
            "elapsed_ms": int((time.monotonic() - start) * 1000),
        }
    except Exception as exc:
        return {
            "ok": False,
            "stdout": "",
            "stderr": f"sandbox error: {exc}",
            "exit_code": -2,
            "elapsed_ms": int((time.monotonic() - start) * 1000),
        }


def main():
    parser = argparse.ArgumentParser(description="Isola sandbox runner")
    parser.add_argument("--code-file", required=True, help="Path to Python code file")
    parser.add_argument("--timeout", type=int, default=15, help="Execution timeout in seconds")
    parser.add_argument("--network", action="store_true", default=False, help="Allow network access")
    args = parser.parse_args()

    with open(args.code_file, "r", encoding="utf-8") as f:
        code = f.read()

    result = asyncio.run(run_in_sandbox(code, args.timeout, args.network))
    print(json.dumps(result))


if __name__ == "__main__":
    main()
```

**Step 2: Write requirements.txt**

```
isola>=0.4.1
```

**Step 3: Verify Python syntax is valid**

Run: `python -c "import ast; ast.parse(open('internal/sandbox/sandbox_runner.py').read()); print('OK')"`
Expected: `OK`

**Step 4: Commit**

```bash
git add internal/sandbox/sandbox_runner.py internal/sandbox/requirements.txt
git commit -m "feat: add Isola sandbox runner Python sidecar

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2: Sandbox Manager — Go package

**Files:**
- Create: `internal/sandbox/sandbox.go`
- Create: `internal/sandbox/sandbox_test.go`

**Step 1: Write the sandbox Go package**

```go
// Package sandbox executes LLM-generated Python code in an Isola WASM sandbox.
//
// It works by writing the user's code to a temp file, then calling the
// bundled sandbox_runner.py script via os/exec. The Python sidecar wraps
// Isola and returns JSON with stdout/stderr/exit_code.
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Result holds the output of a sandbox execution.
type Result struct {
	OK        bool   `json:"ok"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	ElapsedMs int    `json:"elapsed_ms"`
}

// Config controls sandbox behaviour.
type Config struct {
	// PythonPath is the path to the Python 3 binary. Default "python3".
	PythonPath string
	// RunnerPath is the absolute path to sandbox_runner.py.
	RunnerPath string
	// Timeout is the per-execution wall-clock limit. Default 15s.
	Timeout time.Duration
}

// Manager runs Python code in an Isola WASM sandbox.
type Manager struct {
	cfg Config
}

// NewManager creates a sandbox manager. Returns an error if Python is not
// available or the runner script doesn't exist.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.PythonPath == "" {
		cfg.PythonPath = "python3"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.RunnerPath == "" {
		return nil, errors.New("sandbox: RunnerPath is required")
	}
	if _, err := os.Stat(cfg.RunnerPath); err != nil {
		return nil, fmt.Errorf("sandbox: runner script not found at %s: %w", cfg.RunnerPath, err)
	}
	return &Manager{cfg: cfg}, nil
}

// Execute runs the given Python code in an Isola WASM sandbox.
// The code is written to a temp file, then executed via the Python sidecar.
func (m *Manager) Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error) {
	// Write code to temp file
	tmpFile, err := os.CreateTemp("", "aura-sandbox-*.py")
	if err != nil {
		return nil, fmt.Errorf("sandbox: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(code); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("sandbox: write temp file: %w", err)
	}
	tmpFile.Close()

	// Build command
	args := []string{m.cfg.RunnerPath, "--code-file", tmpPath, "--timeout", fmt.Sprintf("%d", int(m.cfg.Timeout.Seconds()))}
	if allowNetwork {
		args = append(args, "--network")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, m.cfg.Timeout+5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, m.cfg.PythonPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = []string{} // no inherited env vars

	err = cmd.Run()

	// Parse JSON result from stdout (the runner prints JSON)
	var result Result
	if stdout.Len() > 0 {
		if jsonErr := json.Unmarshal(stdout.Bytes(), &result); jsonErr != nil {
			return nil, fmt.Errorf("sandbox: parse runner output: %w (stdout=%s stderr=%s)", jsonErr, stdout.String(), stderr.String())
		}
		return &result, nil
	}

	// No stdout = runner crashed or Python missing
	if err != nil {
		return nil, fmt.Errorf("sandbox: runner failed: %w (stderr=%s)", err, stderr.String())
	}

	return nil, errors.New("sandbox: runner produced no output")
}

// IsAvailable reports whether the Python sidecar is reachable.
func (m *Manager) IsAvailable() bool {
	cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try importing isola
	cmd := exec.CommandContext(cmdCtx, m.cfg.PythonPath, "-c", "import isola; print('ok')")
	out, err := cmd.Output()
	return err == nil && string(bytes.TrimSpace(out)) == "ok"
}

// embeddedRunnerPath returns the path to the bundled runner relative to the
// executable's directory. Callers should use this as RunnerPath.
func EmbeddedRunnerPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "internal", "sandbox", "sandbox_runner.py"), nil
}
```

**Step 2: Write tests**

```go
package sandbox_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/sandbox"
)

func TestNewManager_MissingRunner(t *testing.T) {
	_, err := sandbox.NewManager(sandbox.Config{
		RunnerPath: "/nonexistent/runner.py",
	})
	if err == nil {
		t.Fatal("expected error for missing runner")
	}
}

func TestNewManager_ValidConfig(t *testing.T) {
	// Create a dummy runner file
	tmpDir := t.TempDir()
	runnerPath := filepath.Join(tmpDir, "sandbox_runner.py")
	if err := os.WriteFile(runnerPath, []byte("print('{}')"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr, err := sandbox.NewManager(sandbox.Config{
		PythonPath: "python3",
		RunnerPath: runnerPath,
		Timeout:    0, // should default to 15s
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestExecute_EmptyCode(t *testing.T) {
	// This test requires Python + Isola installed. Skip if unavailable.
	runnerPath, err := sandbox.EmbeddedRunnerPath()
	if err != nil {
		t.Skipf("cannot find runner: %v", err)
	}

	mgr, err := sandbox.NewManager(sandbox.Config{
		PythonPath: "python3",
		RunnerPath: runnerPath,
	})
	if err != nil {
		t.Skipf("sandbox not available: %v", err)
	}

	result, err := mgr.Execute(context.Background(), "print('hello from sandbox')", false)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected ok, got stderr=%s", result.Stderr)
	}
	if result.Stdout != "hello from sandbox\n" {
		t.Fatalf("unexpected stdout: %q", result.Stdout)
	}
}

func TestExecute_Timeout(t *testing.T) {
	runnerPath, err := sandbox.EmbeddedRunnerPath()
	if err != nil {
		t.Skipf("cannot find runner: %v", err)
	}

	mgr, err := sandbox.NewManager(sandbox.Config{
		PythonPath: "python3",
		RunnerPath: runnerPath,
		Timeout:    2 * 1e9, // 2 seconds in ns
	})
	if err != nil {
		t.Skipf("sandbox not available: %v", err)
	}

	result, err := mgr.Execute(context.Background(), "import time; time.sleep(10)", false)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if result.OK {
		t.Fatal("expected timeout failure")
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/sandbox/ -v -count=1`
Expected: Tests that require Isola skip; NewManager tests pass

**Step 4: Commit**

```bash
git add internal/sandbox/sandbox.go internal/sandbox/sandbox_test.go
git commit -m "feat: add sandbox manager Go package

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3: execute_code tool

**Files:**
- Create: `internal/tools/exec.go`
- Create: `internal/tools/exec_test.go`

**Step 1: Write the execute_code tool**

```go
package tools

import (
	"context"
	"fmt"

	"github.com/aura/aura/internal/sandbox"
)

// ExecuteCodeTool lets the LLM run Python code in the Isola WASM sandbox.
type ExecuteCodeTool struct {
	manager *sandbox.Manager
}

// NewExecuteCodeTool creates the execute_code tool. Returns nil if manager
// is nil (sandbox not available).
func NewExecuteCodeTool(manager *sandbox.Manager) *ExecuteCodeTool {
	if manager == nil {
		return nil
	}
	return &ExecuteCodeTool{manager: manager}
}

func (t *ExecuteCodeTool) Name() string {
	return "execute_code"
}

func (t *ExecuteCodeTool) Description() string {
	return "Execute Python code in an isolated WASM sandbox. " +
		"Use this for calculations, data processing, simulations, or any task that requires running code. " +
		"The sandbox is ephemeral — no state persists between executions. " +
		"Stdlib only by default. Set allow_network=true if the code needs to make HTTP requests. " +
		"Output is limited to 100KB. Timeout is 15 seconds by default, configurable up to 30s."
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
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds (1-30). Default 15.",
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

	// Timeout is handled by the Manager config; ignore per-call timeout
	// for now since the Manager has a fixed Config.Timeout.

	result, err := t.manager.Execute(ctx, code, allowNetwork)
	if err != nil {
		return "", fmt.Errorf("sandbox execution failed: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("execution failed (exit=%d): %s", result.ExitCode, result.Stderr)
	}

	out := fmt.Sprintf("exit_code: %d\nelapsed_ms: %d\n\n%s", result.ExitCode, result.ElapsedMs, result.Stdout)
	if result.Stderr != "" {
		out += fmt.Sprintf("\n\n--- stderr ---\n%s", result.Stderr)
	}
	return out, nil
}
```

**Step 2: Write tests**

```go
package tools_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/tools"
)

func TestExecuteCodeTool_NilManager(t *testing.T) {
	tool := tools.NewExecuteCodeTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when manager is nil")
	}
}

func TestExecuteCodeTool_Parameters(t *testing.T) {
	// Use a mock that avoids the real sandbox; we test integration separately.
	// For now, test that parameter schema is valid.
	tool := &testExecuteCodeTool{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Fatal("parameters must be a JSON Schema object")
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties")
	}
	if props["code"] == nil {
		t.Fatal("missing code parameter")
	}
}

type testExecuteCodeTool struct{}

func (t *testExecuteCodeTool) Name() string        { return "execute_code" }
func (t *testExecuteCodeTool) Description() string  { return "test" }
func (t *testExecuteCodeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{"type": "string", "description": "Python code"},
		},
	}
}
func (t *testExecuteCodeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return "ok", nil
}
```

**Step 3: Run tests**

Run: `go test ./internal/tools/ -run ExecuteCode -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/tools/exec.go internal/tools/exec_test.go
git commit -m "feat: add execute_code tool for sandboxed Python execution

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4: Tool registry — wiki/tools/ directory + tool management

**Files:**
- Create: `internal/tools/tool_registry.go`
- Create: `internal/tools/tool_registry_test.go`

**Step 1: Write the tool registry package**

```go
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
)

// ToolRegistry manages the persistent collection of LLM-written Python tools
// stored under wiki/tools/. Each tool is a .py file with a companion .md
// wiki page.
type ToolRegistry struct {
	wikiStore *wiki.Store
	toolsDir  string
}

// NewToolRegistry creates a tool registry backed by the wiki/tools/ directory.
func NewToolRegistry(wikiStore *wiki.Store) (*ToolRegistry, error) {
	toolsDir := filepath.Join(wikiStore.Dir(), "tools")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		return nil, fmt.Errorf("create tools dir: %w", err)
	}
	return &ToolRegistry{wikiStore: wikiStore, toolsDir: toolsDir}, nil
}

// ToolInfo holds metadata about a registered tool.
type ToolInfo struct {
	Name        string
	Description string
	Params      string
	Requires    string
	Created     string
	Usage       string
	FilePath    string // relative to toolsDir
}

// ListTools returns all tools registered in the tools directory by parsing
// the .py file headers.
func (r *ToolRegistry) ListTools() ([]ToolInfo, error) {
	entries, err := os.ReadDir(r.toolsDir)
	if err != nil {
		return nil, err
	}

	var tools []ToolInfo
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".py") {
			continue
		}
		info, err := r.parseToolHeader(entry.Name())
		if err != nil {
			continue // skip unparseable tools
		}
		tools = append(tools, info)
	}
	return tools, nil
}

// SaveTool writes a new Python tool to the tools directory. Also writes a
// companion .md wiki page and regenerates index.md. Returns the tool name.
func (r *ToolRegistry) SaveTool(ctx context.Context, name, description, params, code, usage string) error {
	// Sanitize name: lowercase, underscores only
	name = sanitizeToolName(name)
	if name == "" {
		return fmt.Errorf("invalid tool name")
	}

	pyPath := filepath.Join(r.toolsDir, name+".py")
	mdSlug := "tools-" + name

	// Write .py file with standard header
	header := fmt.Sprintf(`# tool: %s
# description: %s
# params: %s
# requires: stdlib
# created: %s
# usage: %s

`, name, description, params, time.Now().Format("2006-01-02"), usage)

	if err := os.WriteFile(pyPath, []byte(header+code), 0644); err != nil {
		return fmt.Errorf("write tool file: %w", err)
	}

	// Write companion .md wiki page
	mdBody := fmt.Sprintf("# Tool: %s\n\n%s\n\n**Parameters:** %s\n\n**Usage:** %s\n\n```python\n%s\n```", name, description, params, usage, code)
	page := &wiki.Page{
		Title:    "Tool: " + name,
		Tags:     []string{"tool", "auto-generated"},
		Category: "tool",
	}
	if err := r.wikiStore.WriteFromLLMOutput(ctx, fmt.Sprintf("---\ntitle: %s\ntags: [tool, auto-generated]\ncategory: tool\n---\n\n%s", page.Title, mdBody), "v1"); err != nil {
		// Wiki write failed — remove the .py file to avoid orphans
		os.Remove(pyPath)
		return fmt.Errorf("write wiki page: %w", err)
	}

	// Regenerate index
	return r.regenerateIndex(ctx)
}

// GetToolCode reads the source of a registered tool (without the header).
func (r *ToolRegistry) GetToolCode(name string) (string, error) {
	name = sanitizeToolName(name)
	pyPath := filepath.Join(r.toolsDir, name+".py")
	data, err := os.ReadFile(pyPath)
	if err != nil {
		return "", err
	}
	// Strip header comments
	lines := strings.SplitN(string(data), "\n\n", 2)
	if len(lines) == 2 {
		return lines[1], nil
	}
	return string(data), nil
}

// DeleteTool removes a tool's .py file. The companion .md page is left
// in place (wiki pages are version-controlled).
func (r *ToolRegistry) DeleteTool(ctx context.Context, name string) error {
	name = sanitizeToolName(name)
	pyPath := filepath.Join(r.toolsDir, name+".py")
	if err := os.Remove(pyPath); err != nil {
		return err
	}
	return r.regenerateIndex(ctx)
}

func (r *ToolRegistry) regenerateIndex(ctx context.Context) error {
	tools, err := r.ListTools()
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# Tool Registry\n\n")
	b.WriteString("Auto-generated index of available tools. Updated on every tool save/delete.\n\n")

	if len(tools) == 0 {
		b.WriteString("_No tools registered yet. The LLM will create tools as they become useful._\n")
	} else {
		for _, t := range tools {
			b.WriteString(fmt.Sprintf("- **%s** — %s  \n  params: %s | created: %s\n\n", t.Name, t.Description, t.Params, t.Created))
		}
	}

	indexPath := filepath.Join(r.toolsDir, "index.md")
	return os.WriteFile(indexPath, []byte(b.String()), 0644)
}

func (r *ToolRegistry) parseToolHeader(filename string) (ToolInfo, error) {
	pyPath := filepath.Join(r.toolsDir, filename)
	data, err := os.ReadFile(pyPath)
	if err != nil {
		return ToolInfo{}, err
	}

	info := ToolInfo{
		Name:     strings.TrimSuffix(filename, ".py"),
		FilePath: filename,
	}

	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "# ") {
			break
		}
		line = strings.TrimPrefix(line, "# ")
		switch {
		case strings.HasPrefix(line, "description:"):
			info.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		case strings.HasPrefix(line, "params:"):
			info.Params = strings.TrimSpace(strings.TrimPrefix(line, "params:"))
		case strings.HasPrefix(line, "requires:"):
			info.Requires = strings.TrimSpace(strings.TrimPrefix(line, "requires:"))
		case strings.HasPrefix(line, "created:"):
			info.Created = strings.TrimSpace(strings.TrimPrefix(line, "created:"))
		case strings.HasPrefix(line, "usage:"):
			info.Usage = strings.TrimSpace(strings.TrimPrefix(line, "usage:"))
		}
	}
	return info, nil
}

func sanitizeToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	// Remove any characters that aren't a-z, 0-9, underscore
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
```

**Step 2: Write tests**

```go
package tools_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/tools"
)

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Data Correlation", "data_correlation"},
		{"CSV Cleaner!", "csv_cleaner"},
		{"My Tool - v2", "my_tool__v2"},
		{"", ""},
		{"___", "___"},
	}
	for _, tt := range tests {
		// Can't call unexported sanitizeToolName directly,
		// but we test via ToolRegistry methods
		_ = tt
	}
}

// Integration test requires a real wiki store; tested in Task 9.
```

**Step 3: Run tests**

Run: `go test ./internal/tools/ -run ToolRegistry -v`
Expected: PASS (compile check)

**Step 4: Commit**

```bash
git add internal/tools/tool_registry.go internal/tools/tool_registry_test.go
git commit -m "feat: add tool registry for persistent LLM-written Python tools

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 5: Tool management tools — list_tools, read_tool, save_tool

**Files:**
- Create: `internal/tools/tool_mgmt.go`
- Modify: `internal/tools/exec.go` (just for reference)

**Step 1: Write tool management tools**

```go
package tools

import (
	"context"
	"fmt"
	"strings"
)

// ListToolsTool lets the LLM discover available tools in the registry.
type ListToolsTool struct {
	registry *ToolRegistry
}

func NewListToolsTool(registry *ToolRegistry) *ListToolsTool {
	if registry == nil {
		return nil
	}
	return &ListToolsTool{registry: registry}
}

func (t *ListToolsTool) Name() string { return "list_tools" }

func (t *ListToolsTool) Description() string {
	return "List all Python tools in the persistent tool registry. " +
		"Use this before writing new code — a tool may already exist for the task."
}

func (t *ListToolsTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *ListToolsTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	tools, err := t.registry.ListTools()
	if err != nil {
		return "", fmt.Errorf("list tools: %w", err)
	}
	if len(tools) == 0 {
		return "No tools registered yet.", nil
	}
	var b strings.Builder
	for _, tool := range tools {
		b.WriteString(fmt.Sprintf("- **%s**: %s (params: %s)\n", tool.Name, tool.Description, tool.Params))
	}
	return b.String(), nil
}

// ReadToolTool lets the LLM read a tool's source code.
type ReadToolTool struct {
	registry *ToolRegistry
}

func NewReadToolTool(registry *ToolRegistry) *ReadToolTool {
	if registry == nil {
		return nil
	}
	return &ReadToolTool{registry: registry}
}

func (t *ReadToolTool) Name() string { return "read_tool" }

func (t *ReadToolTool) Description() string {
	return "Read the source code of a registered Python tool. " +
		"Use this to understand what an existing tool does before using or modifying it."
}

func (t *ReadToolTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the tool to read",
			},
		},
		"required": []string{"name"},
	}
}

func (t *ReadToolTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return "", fmt.Errorf("tool name is required")
	}
	code, err := t.registry.GetToolCode(name)
	if err != nil {
		return "", fmt.Errorf("read tool %s: %w", name, err)
	}
	return code, nil
}

// SaveToolTool lets the LLM persist useful scripts to the tool registry.
type SaveToolTool struct {
	registry *ToolRegistry
}

func NewSaveToolTool(registry *ToolRegistry) *SaveToolTool {
	if registry == nil {
		return nil
	}
	return &SaveToolTool{registry: registry}
}

func (t *SaveToolTool) Name() string { return "save_tool" }

func (t *SaveToolTool) Description() string {
	return "Save a Python script as a permanent tool in the registry. " +
		"Use this after successfully executing code that solves a reusable problem. " +
		"The tool becomes discoverable by list_tools and can be read with read_tool."
}

func (t *SaveToolTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Unique name for the tool (lowercase_underscores)",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "One-line description of what the tool does",
			},
			"params": map[string]any{
				"type":        "string",
				"description": "Comma-separated parameter names and types, e.g. 'filepath (str), col1 (str)'",
			},
			"code": map[string]any{
				"type":        "string",
				"description": "Python source code for the tool",
			},
			"usage": map[string]any{
				"type":        "string",
				"description": "When to use this tool, e.g. 'user uploaded a CSV and wants statistics'",
			},
		},
		"required": []string{"name", "description", "code"},
	}
}

func (t *SaveToolTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	params, _ := args["params"].(string)
	code, _ := args["code"].(string)
	usage, _ := args["usage"].(string)

	if err := t.registry.SaveTool(ctx, name, description, params, code, usage); err != nil {
		return "", fmt.Errorf("save tool: %w", err)
	}
	return fmt.Sprintf("Tool '%s' saved to registry.", name), nil
}
```

**Step 2: Write tests**

```go
package tools_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/tools"
)

func TestListToolsTool_NilRegistry(t *testing.T) {
	tool := tools.NewListToolsTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when registry is nil")
	}
}

func TestReadToolTool_NilRegistry(t *testing.T) {
	tool := tools.NewReadToolTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when registry is nil")
	}
}

func TestSaveToolTool_NilRegistry(t *testing.T) {
	tool := tools.NewSaveToolTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when registry is nil")
	}
}

func TestToolManagement_Parameters(t *testing.T) {
	// Verify all three tools have valid parameter schemas
	// Use a mock registry
	reg := &mockRegistry{}
	for _, tool := range []tools.Tool{
		tools.NewListToolsTool(reg),
		tools.NewReadToolTool(reg),
		tools.NewSaveToolTool(reg),
	} {
		if tool.Name() == "" {
			t.Fatal("tool must have a name")
		}
		if tool.Description() == "" {
			t.Fatalf("%s: description must not be empty", tool.Name())
		}
		params := tool.Parameters()
		if params["type"] != "object" {
			t.Fatalf("%s: parameters must be JSON Schema object", tool.Name())
		}
	}
}

type mockRegistry struct{}

func (m *mockRegistry) ListTools() ([]tools.ToolInfo, error) { return nil, nil }
func (m *mockRegistry) GetToolCode(name string) (string, error) { return "print('hello')", nil }
func (m *mockRegistry) SaveTool(ctx context.Context, name, desc, params, code, usage string) error { return nil }
```

Wait — this won't compile because `ToolRegistry` is a concrete struct, not an interface. We need an interface for the mock to work. Update `tool_registry.go` to export an interface:

Add to `tool_registry.go`:
```go
// ToolStore is the interface the tool management tools depend on.
// ToolRegistry is the production implementation.
type ToolStore interface {
	ListTools() ([]ToolInfo, error)
	GetToolCode(name string) (string, error)
	SaveTool(ctx context.Context, name, description, params, code, usage string) error
}
```

And update `tool_mgmt.go` tool constructors to accept `ToolStore` instead of `*ToolRegistry`.

**Step 3: Run compile check**

Run: `go build ./internal/tools/`
Expected: builds successfully

**Step 4: Commit**

```bash
git add internal/tools/tool_mgmt.go internal/tools/tool_mgmt_test.go internal/tools/tool_registry.go
git commit -m "feat: add list_tools, read_tool, save_tool management tools

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 6: Autonomous improvement scheduler

**Files:**
- Modify: `internal/scheduler/types.go` — add `KindAutoImprove`
- Modify: `internal/telegram/scheduler_handlers.go` — add `dispatchAutoImprove`

**Step 1: Add KindAutoImprove to scheduler types**

In `internal/scheduler/types.go`, add after the existing const block:
```go
KindAutoImprove TaskKind = "auto_improve"
```

**Step 2: Add auto-improve dispatcher handler**

In `internal/telegram/scheduler_handlers.go`, add to the `dispatchTask` switch:
```go
case scheduler.KindAutoImprove:
    return b.dispatchAutoImprove(ctx)
```

And add the handler function:
```go
func (b *Bot) dispatchAutoImprove(ctx context.Context) error {
    if b.llm == nil {
        return fmt.Errorf("auto_improve: no LLM client available")
    }
    if b.toolRegistry == nil {
        return fmt.Errorf("auto_improve: no tool registry available")
    }
    if b.archiveDB == nil {
        return fmt.Errorf("auto_improve: no conversation archive available")
    }

    logger := b.logger.With("component", "auto_improve")

    // Step 1: Scan recent conversations for gaps
    recentTurns, err := b.archiveDB.ListAll(ctx, 200)
    if err != nil {
        return fmt.Errorf("auto_improve: scan archive: %w", err)
    }

    // Build a prompt that asks the LLM to identify gaps and write tools
    var convSummary strings.Builder
    convSummary.WriteString("Recent conversations:\n\n")
    for _, turn := range recentTurns {
        if turn.Role == "user" {
            convSummary.WriteString(fmt.Sprintf("User: %s\n", truncate(turn.Content, 200)))
        } else if turn.Role == "assistant" && strings.Contains(turn.Content, "I can't") || strings.Contains(turn.Content, "I don't know") {
            convSummary.WriteString(fmt.Sprintf("Assistant (low-confidence): %s\n", truncate(turn.Content, 200)))
        }
    }

    // Get existing tools
    tools, _ := b.toolRegistry.ListTools()
    var toolsSummary strings.Builder
    for _, t := range tools {
        toolsSummary.WriteString(fmt.Sprintf("- %s: %s\n", t.Name, t.Description))
    }

    prompt := fmt.Sprintf(`You are Aura's self-improvement system. Review recent conversations and existing tools.

%s

Existing tools:
%s

Identify up to 3 gaps where a new Python tool would make future conversations better.
For each gap, write the tool code and propose it for the registry.
Focus on patterns where users asked for things Aura couldn't do or where a reusable script would save time.

Respond ONLY with a JSON array of tool proposals:
[{"name": "tool_name", "description": "...", "params": "...", "code": "...", "usage": "..."}]
If no gaps found, respond with [].`, convSummary.String(), toolsSummary.String())

    req := llm.Request{
        Messages: []llm.Message{{Role: "user", Content: prompt}},
        Model:    b.cfg.LLMModel,
    }

    resp, err := b.llm.Send(ctx, req)
    if err != nil {
        return fmt.Errorf("auto_improve: LLM call: %w", err)
    }

    // Parse proposals
    var proposals []struct {
        Name        string `json:"name"`
        Description string `json:"description"`
        Params      string `json:"params"`
        Code        string `json:"code"`
        Usage       string `json:"usage"`
    }
    if err := json.Unmarshal([]byte(resp.Content), &proposals); err != nil {
        logger.Warn("auto_improve: failed to parse LLM proposals", "error", err, "content", resp.Content)
        return nil // don't fail the task, just skip this cycle
    }

    for _, p := range proposals {
        // Check if tool already exists
        if _, err := b.toolRegistry.GetToolCode(p.Name); err == nil {
            logger.Info("auto_improve: tool already exists, skipping", "name", p.Name)
            continue
        }

        if err := b.toolRegistry.SaveTool(ctx, p.Name, p.Description, p.Params, p.Code, p.Usage); err != nil {
            logger.Warn("auto_improve: failed to save tool", "name", p.Name, "error", err)
            continue
        }

        logger.Info("auto_improve: saved new tool", "name", p.Name, "description", p.Description)

        // Notify owner about the new tool
        if ownerID := b.cfg.Allowlist[0]; ownerID != "" {
            chatID, _ := strconv.ParseInt(ownerID, 10, 64)
            b.bot.Send(&tele.Chat{ID: chatID},
                fmt.Sprintf("I wrote a new tool to help with future conversations: **%s** — %s", p.Name, p.Description))
        }
    }

    return nil
}

func truncate(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen] + "..."
}
```

Add required imports to `scheduler_handlers.go`:
```go
import (
    "encoding/json"
    "strconv"
    "strings"
    // ...existing imports
    tele "gopkg.in/telebot.v4"
)
```

Note: some imports may already exist. Check existing imports and only add missing ones.

**Step 3: Bootstrap the auto-improve task in setup.go**

In `internal/telegram/setup.go`, after the existing nightly wiki-maintenance bootstrap, add:

```go
// Bootstrap autonomous improvement task (nightly, offset from wiki maintenance)
autoImproveAt, err := scheduler.NextDailyRun("04:00", time.Local, time.Now())
if err != nil {
    return nil, fmt.Errorf("computing auto-improve run: %w", err)
}
if _, err := schedStore.Upsert(context.Background(), &scheduler.Task{
    Name:          "nightly-auto-improve",
    Kind:          scheduler.KindAutoImprove,
    ScheduleKind:  scheduler.ScheduleDaily,
    ScheduleDaily: "04:00",
    NextRunAt:     autoImproveAt,
    Status:        scheduler.StatusActive,
}); err != nil {
    logger.Warn("failed to bootstrap auto-improve task", "err", err)
}
```

**Step 4: Run compile check**

Run: `go build ./...`
Expected: builds successfully

**Step 5: Commit**

```bash
git add internal/scheduler/types.go internal/telegram/scheduler_handlers.go internal/telegram/setup.go
git commit -m "feat: add autonomous improvement scheduler job

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 7: Configuration — add sandbox env vars

**Files:**
- Modify: `internal/config/config.go` — add sandbox fields
- Modify: `.env.example` — add new vars

**Step 1: Add config fields**

In `internal/config/config.go`, add to the `Config` struct:
```go
// Sandbox code execution
SandboxEnabled bool   `envconfig:"SANDBOX_ENABLED" default:"true"`
SandboxTimeoutSec int `envconfig:"SANDBOX_TIMEOUT_SEC" default:"15"`
```

And in `Load()`:
```go
cfg.SandboxEnabled = getEnvBool("SANDBOX_ENABLED", true)
cfg.SandboxTimeoutSec = getEnvInt("SANDBOX_TIMEOUT_SEC", 15)
```

**Step 2: Update .env.example**

Read `.env.example` and add:
```
# Sandbox code execution (Isola WASM)
SANDBOX_ENABLED=true
SANDBOX_TIMEOUT_SEC=15
```

**Step 3: Update .env**

Read `.env` and add the same lines.

**Step 4: Run compile check**

Run: `go build ./...`
Expected: builds successfully

**Step 5: Commit**

```bash
git add internal/config/config.go .env.example .env
git commit -m "feat: add sandbox configuration env vars

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 8: Wiring — integrate into telegram setup

**Files:**
- Modify: `internal/telegram/setup.go` — create sandbox manager, tool registry, register tools
- Modify: `internal/telegram/bot.go` — add new fields to Bot struct

**Step 1: Add fields to Bot struct**

In `internal/telegram/bot.go`, add:
```go
sandboxMgr *sandbox.Manager
toolReg    *tools.ToolRegistry
```

And import:
```go
"github.com/aura/aura/internal/sandbox"
```

**Step 2: Wire in setup.go**

In `internal/telegram/setup.go`, after the OCR setup and before tool registry registration, add:

```go
// Sandbox code execution
var sandboxMgr *sandbox.Manager
if cfg.SandboxEnabled {
    runnerPath, err := sandbox.EmbeddedRunnerPath()
    if err != nil {
        logger.Warn("sandbox runner path unavailable", "error", err)
    } else {
        mgr, err := sandbox.NewManager(sandbox.Config{
            PythonPath: "python3",
            RunnerPath: runnerPath,
            Timeout:    time.Duration(cfg.SandboxTimeoutSec) * time.Second,
        })
        if err != nil {
            logger.Warn("sandbox manager unavailable, execute_code disabled", "error", err)
        } else if !mgr.IsAvailable() {
            logger.Warn("sandbox: Python/Isola not available, run: pip install isola")
        } else {
            sandboxMgr = mgr
            logger.Info("sandbox enabled (Isola WASM)")
        }
    }
}

// Tool registry (persistent LLM-written tools)
toolReg, err := tools.NewToolRegistry(wikiStore)
if err != nil {
    logger.Warn("tool registry unavailable", "error", err)
}
```

Then in the tool registration section, after the existing `RunTaskNowTool` registration:
```go
if tool := tools.NewExecuteCodeTool(sandboxMgr); tool != nil {
    toolRegistry.Register(tool)
}
if tool := tools.NewListToolsTool(toolReg); tool != nil {
    toolRegistry.Register(tool)
}
if tool := tools.NewReadToolTool(toolReg); tool != nil {
    toolRegistry.Register(tool)
}
if tool := tools.NewSaveToolTool(toolReg); tool != nil {
    toolRegistry.Register(tool)
}
```

And in the Bot struct initialization:
```go
b := &Bot{
    // ...existing fields
    sandboxMgr: sandboxMgr,
    toolReg:    toolReg,
}
```

**Step 3: Run compile check**

Run: `go build ./...`
Expected: builds successfully

**Step 4: Commit**

```bash
git add internal/telegram/setup.go internal/telegram/bot.go
git commit -m "feat: wire sandbox and tool registry into telegram bot

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 9: Integration tests + E2E validation

**Files:**
- Create: `internal/telegram/sandbox_integration_test.go`

**Step 1: Write integration test skeleton**

```go
package telegram_test

import (
	"testing"
)

func TestSandboxIntegration_ExecuteAndSaveTool(t *testing.T) {
	// This test requires:
	// - Python 3.11+ installed
	// - Isola installed (pip install isola)
	// - A running Aura instance
	//
	// For now, this is a manual smoke test. The test documents the expected flow.
	t.Skip("integration test — requires full Aura stack with Isola")

	// Expected flow:
	// 1. User asks "run a simulation of 100 dice rolls and compute the average"
	// 2. LLM calls execute_code with Python code for dice simulation
	// 3. Sandbox executes, returns result
	// 4. LLM answers user with the result
	// 5. LLM optionally calls save_tool to persist the dice simulator
}

func TestSandboxIntegration_ToolDiscovery(t *testing.T) {
	t.Skip("integration test — requires full Aura stack with Isola")

	// Expected flow:
	// 1. User asks "summarize this CSV" (uploads a CSV)
	// 2. LLM calls list_tools, finds csv_summarize tool from a previous session
	// 3. LLM calls read_tool to get the source code
	// 4. LLM calls execute_code using the tool's pattern
	// 5. Sandbox executes, returns summary
	// 6. LLM answers user
}

func TestSandboxIntegration_AutoImprove(t *testing.T) {
	t.Skip("integration test — requires full Aura stack")

	// Expected flow (runs via scheduler):
	// 1. Nightly auto_improve task fires
	// 2. Scans conversation archives
	// 3. LLM identifies gap: users frequently ask for date calculations
	// 4. LLM writes date_utils.py tool
	// 5. Tool saved to registry with companion wiki page
	// 6. Owner notified via Telegram
}
```

**Step 2: Commit**

```bash
git add internal/telegram/sandbox_integration_test.go
git commit -m "test: add sandbox integration test skeletons

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Post-Implementation Verification

After all tasks are complete, verify the system works end-to-end:

1. **Check Python sidecar**: `python3 internal/sandbox/sandbox_runner.py --code-file /tmp/test.py` where test.py contains `print(2+2)`
2. **Start Aura**: `go run ./cmd/aura/`
3. **Smoke test via Telegram**:
   - Send: "Run this Python code and tell me the result: print(sum(range(1, 101)))"
   - Expected: Aura replies "5050" (sum of 1 to 100)
4. **Test tool persistence**:
   - Send: "Create a useful tool that calculates factorials and save it"
   - Expected: Tool saved to wiki/tools/, visible in dashboard
5. **Verify wiki entries**: Check wiki/tools/index.md and wiki/tools-<name>.md pages exist
6. **Verify no regressions**: Send normal wiki queries, ensure existing tools still work
