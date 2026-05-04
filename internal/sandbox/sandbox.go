// Package sandbox executes LLM-generated Python code in an isolated runtime.
//
// Manager owns policy, health, and the stable execute_code boundary. Runtime
// adapters own the actual backend implementation so the legacy Isola sidecar
// can be replaced by the bundled Pyodide runtime without changing callers.
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
	"runtime"
	"strings"
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
	// Runtime is the execution adapter. Nil uses the temporary legacy Isola
	// sidecar configured below.
	Runtime Runtime
	// PythonPath is the path to the Python 3 binary. Defaults to an
	// auto-detected interpreter that can import Isola when available.
	PythonPath string
	// AllowSystemPython lets development and CI fall back to python/python3.
	// Product builds should leave this false and ship runtime/python instead.
	AllowSystemPython bool
	// RunnerPath is the absolute path to sandbox_runner.py.
	RunnerPath string
	// Timeout is the per-execution wall-clock limit. Default 15s.
	Timeout time.Duration
}

type RuntimeKind string

const (
	RuntimeKindPyodide     RuntimeKind = "pyodide"
	RuntimeKindIsolaLegacy RuntimeKind = "isola_legacy"
	RuntimeKindUnavailable RuntimeKind = "unavailable"
)

// Runtime is the adapter boundary for sandbox execution backends.
type Runtime interface {
	Kind() RuntimeKind
	Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error)
	CheckAvailability() Availability
	ValidateCode(code string) error
}

// Availability describes whether the configured runtime can execute code.
type Availability struct {
	Available  bool
	Kind       RuntimeKind
	PythonPath string
	Detail     string
}

// Manager runs Python code through a configured sandbox runtime.
type Manager struct {
	cfg     Config
	runtime Runtime
}

// NewManager creates a sandbox manager. Until the Pyodide adapter lands, nil
// Runtime means "use the legacy Isola sidecar".
func NewManager(cfg Config) (*Manager, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.Runtime == nil {
		if cfg.PythonPath == "" {
			cfg.PythonPath = defaultPythonPath(cfg.AllowSystemPython)
		}
		if cfg.RunnerPath == "" {
			return nil, errors.New("sandbox: RunnerPath is required")
		}
		if _, err := os.Stat(cfg.RunnerPath); err != nil {
			return nil, fmt.Errorf("sandbox: runner script not found at %s: %w", cfg.RunnerPath, err)
		}
		cfg.Runtime = newIsolaLegacyRuntime(cfg)
	}
	return &Manager{cfg: cfg, runtime: cfg.Runtime}, nil
}

func defaultPythonPath(allowSystem bool) string {
	candidates := defaultPythonCandidates(allowSystem)
	for _, candidate := range candidates {
		if pythonHasIsola(candidate) {
			return candidate
		}
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if runtime.GOOS == "windows" {
		return filepath.Join("runtime", "python", "python.exe")
	}
	return filepath.Join("runtime", "python", "bin", "python3")
}

func defaultPythonCandidates(allowSystem bool) []string {
	var candidates []string
	add := func(path string) {
		if path != "" {
			candidates = append(candidates, path)
		}
	}

	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if runtime.GOOS == "windows" {
			add(filepath.Join(exeDir, "runtime", "python", "python.exe"))
			add(filepath.Join(exeDir, "python", "python.exe"))
		} else {
			add(filepath.Join(exeDir, "runtime", "python", "bin", "python3"))
			add(filepath.Join(exeDir, "python", "bin", "python3"))
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if runtime.GOOS == "windows" {
			add(filepath.Join(cwd, "runtime", "python", "python.exe"))
			add(filepath.Join(cwd, ".venv", "Scripts", "python.exe"))
		} else {
			add(filepath.Join(cwd, "runtime", "python", "bin", "python3"))
			add(filepath.Join(cwd, ".venv", "bin", "python3"))
		}
	}

	if allowSystem {
		if runtime.GOOS == "windows" {
			add("python")
			add("python3")
		} else {
			add("python3")
			add("python")
		}
	}

	seen := make(map[string]bool, len(candidates))
	unique := candidates[:0]
	for _, candidate := range candidates {
		if !seen[candidate] {
			seen[candidate] = true
			unique = append(unique, candidate)
		}
	}
	return unique
}

func pythonHasIsola(path string) bool {
	cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, path, "-c", "import isola")
	return cmd.Run() == nil
}

// RuntimeKind reports the configured backend kind.
func (m *Manager) RuntimeKind() RuntimeKind {
	if m == nil || m.runtime == nil {
		return RuntimeKindUnavailable
	}
	return m.runtime.Kind()
}

// Execute runs the given Python code in the configured runtime.
func (m *Manager) Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error) {
	if err := m.ValidateCode(code); err != nil {
		return nil, err
	}
	return m.runtime.Execute(ctx, code, allowNetwork)
}

// IsAvailable reports whether the configured runtime can execute code.
func (m *Manager) IsAvailable() bool {
	return m.CheckAvailability().Available
}

// CheckAvailability runs the runtime probe used to decide whether
// execute_code should be registered.
func (m *Manager) CheckAvailability() Availability {
	if m == nil || m.runtime == nil {
		return Availability{
			Available: false,
			Kind:      RuntimeKindUnavailable,
			Detail:    "sandbox runtime unavailable",
		}
	}
	return normalizeAvailability(m.runtime.Kind(), m.runtime.CheckAvailability())
}

// ValidateCode performs defense-in-depth validation before sandbox execution.
// The configured runtime owns the concrete validation mechanism.
func (m *Manager) ValidateCode(code string) error {
	if strings.TrimSpace(code) == "" {
		return errors.New("sandbox: code must not be empty")
	}
	if len(code) > 100_000 {
		return errors.New("sandbox: code exceeds 100KB limit")
	}
	return m.runtime.ValidateCode(code)
}

func normalizeAvailability(kind RuntimeKind, availability Availability) Availability {
	if availability.Kind == "" {
		availability.Kind = kind
	}
	if availability.Kind == "" {
		availability.Kind = RuntimeKindUnavailable
	}
	return availability
}

type isolaLegacyRuntime struct {
	cfg Config
}

func newIsolaLegacyRuntime(cfg Config) Runtime {
	return &isolaLegacyRuntime{cfg: cfg}
}

func (r *isolaLegacyRuntime) Kind() RuntimeKind {
	return RuntimeKindIsolaLegacy
}

func (r *isolaLegacyRuntime) Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error) {
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

	args := []string{
		r.cfg.RunnerPath,
		"--code-file", tmpPath,
		"--timeout", fmt.Sprintf("%d", int(r.cfg.Timeout.Seconds())),
	}
	if allowNetwork {
		args = append(args, "--network")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout+5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, r.cfg.PythonPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// The trusted sidecar needs the host Python environment to locate Isola.
	// User code still receives an empty environment inside sandbox_runner.py.
	cmd.Env = os.Environ()

	runErr := cmd.Run()

	if stdout.Len() > 0 {
		var result Result
		if jsonErr := json.Unmarshal(stdout.Bytes(), &result); jsonErr != nil {
			return nil, fmt.Errorf("sandbox: parse runner output: %w (stdout=%s stderr=%s)",
				jsonErr, stdout.String(), stderr.String())
		}
		return &result, nil
	}

	if runErr != nil {
		return nil, fmt.Errorf("sandbox: runner failed: %w (stderr=%s)", runErr, stderr.String())
	}

	return nil, errors.New("sandbox: runner produced no output")
}

// CheckAvailability runs the same host-side template probe used to decide
// whether execute_code should be registered.
func (r *isolaLegacyRuntime) CheckAvailability() Availability {
	cmdCtx, cancel := context.WithTimeout(context.Background(), r.cfg.Timeout+5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, r.cfg.PythonPath, "-c", checkIsolaRuntimeScript)
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return Availability{
			Available:  false,
			Kind:       RuntimeKindIsolaLegacy,
			PythonPath: r.cfg.PythonPath,
			Detail:     detail,
		}
	}
	return Availability{
		Available:  true,
		Kind:       RuntimeKindIsolaLegacy,
		PythonPath: r.cfg.PythonPath,
		Detail:     "Isola Python runtime template available",
	}
}

const validateCodeScript = `
import ast
import sys

FORBIDDEN_CALLS = {
    "__import__",
    "eval",
    "exec",
    "compile",
    "globals",
    "locals",
    "getattr",
    "setattr",
    "delattr",
    "input",
}

try:
    tree = ast.parse(sys.stdin.read(), filename="<sandbox>", mode="exec")
except SyntaxError as exc:
    if exc.lineno is not None:
        print(f"syntax error: line {exc.lineno}, column {exc.offset}: {exc.msg}", file=sys.stderr)
    else:
        print(f"syntax error: {exc.msg}", file=sys.stderr)
    sys.exit(2)


class Validator(ast.NodeVisitor):
    def visit_Call(self, node):
        name = ""
        if isinstance(node.func, ast.Name):
            name = node.func.id
        elif isinstance(node.func, ast.Attribute):
            name = node.func.attr
        if name in FORBIDDEN_CALLS:
            print(f"forbidden construct {name!r} detected", file=sys.stderr)
            sys.exit(3)
        self.generic_visit(node)


Validator().visit(tree)
`

const checkIsolaRuntimeScript = `
import asyncio
from isola import build_template


async def main():
    await build_template("python")


asyncio.run(main())
`

// ValidateCode shells out to Python's AST parser so comments and strings are
// not treated as executable code, while spaced call variants like eval (...)
// are still rejected. Returns nil if code appears safe.
func (r *isolaLegacyRuntime) ValidateCode(code string) error {
	cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, r.cfg.PythonPath, "-c", validateCodeScript)
	cmd.Stdin = strings.NewReader(code)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if cmdCtx.Err() != nil {
			return fmt.Errorf("sandbox: validation timed out: %w", cmdCtx.Err())
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if strings.HasPrefix(msg, "syntax error:") {
			return fmt.Errorf("sandbox: %s", msg)
		}
		return fmt.Errorf("sandbox: validation failed: %s", msg)
	}
	return nil
}

// FindRunnerPath locates sandbox_runner.py. Tries multiple locations:
// executable-relative (production), repo-root-relative, and
// package-directory-relative (development / tests).
func FindRunnerPath() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "internal", "sandbox", "sandbox_runner.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Try from CWD (repo root during development, package dir during tests).
	// Check repo-root-relative path first.
	candidates := []string{
		filepath.Join("internal", "sandbox", "sandbox_runner.py"),
		"sandbox_runner.py",
	}
	if cwd, err := os.Getwd(); err == nil {
		for _, rel := range candidates {
			p := filepath.Join(cwd, rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}
