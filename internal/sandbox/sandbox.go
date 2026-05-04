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
func (m *Manager) Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error) {
	if err := m.ValidateCode(code); err != nil {
		return nil, err
	}

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
		m.cfg.RunnerPath,
		"--code-file", tmpPath,
		"--timeout", fmt.Sprintf("%d", int(m.cfg.Timeout.Seconds())),
	}
	if allowNetwork {
		args = append(args, "--network")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, m.cfg.Timeout+5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, m.cfg.PythonPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = []string{}

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

// IsAvailable reports whether the Python sidecar is reachable and Isola
// is installed.
func (m *Manager) IsAvailable() bool {
	cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, m.cfg.PythonPath, "-c", "import isola; print('ok')")
	out, err := cmd.Output()
	return err == nil && string(bytes.TrimSpace(out)) == "ok"
}

// forbiddenPatterns are Python constructs rejected before execution as
// defense-in-depth. The WASM sandbox already isolates the runtime, but
// these rejections fail fast with clear error messages.
var forbiddenPatterns = []string{
	"__import__(",
	"eval(",
	"exec(",
	"compile(",
	"globals()",
	"locals()",
	"getattr(",
	"setattr(",
	"delattr(",
	"input(",
}

// ValidateCode performs defense-in-depth validation before sandbox
// execution. It shells out to Python's compiler for syntax checking, then
// scans for forbidden patterns. Returns nil if code appears safe.
func (m *Manager) ValidateCode(code string) error {
	if strings.TrimSpace(code) == "" {
		return errors.New("sandbox: code must not be empty")
	}
	if len(code) > 100_000 {
		return errors.New("sandbox: code exceeds 100KB limit")
	}
	lowered := strings.ToLower(code)
	for _, pattern := range forbiddenPatterns {
		if strings.Contains(lowered, pattern) {
			return fmt.Errorf("sandbox: forbidden construct %q detected", pattern)
		}
	}
	cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, m.cfg.PythonPath, "-c", "compile(__import__('sys').stdin.read(), '<sandbox>', 'exec')")
	cmd.Stdin = strings.NewReader(code)
	out, err := cmd.CombinedOutput()
	if err != nil {
		stderr := string(out)
		if stderr == "" {
			stderr = err.Error()
		}
		return fmt.Errorf("sandbox: syntax error: %s", strings.TrimSpace(stderr))
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
