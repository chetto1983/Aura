package sandbox

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aura/aura/internal/source"
)

const (
	defaultProcessPython            = "python3"
	defaultProcessRunnerTimeout     = 15 * time.Second
	defaultProcessRunnerOutputBytes = 1 << 20
	defaultProcessResultOutputBytes = 64 * 1024
)

type ProcessRunnerConfig struct {
	PythonPath            string
	WorkDir               string
	Timeout               time.Duration
	Environment           []string
	MaxProcessOutputBytes int64
	MaxResultOutputBytes  int
}

// ProcessRunner executes Python directly in the Aura process container. It is
// intentionally not a WASM sandbox: code runs with the same filesystem and
// network reachability as the Aura process.
type ProcessRunner struct {
	pythonPath            string
	workDir               string
	timeout               time.Duration
	environment           []string
	maxProcessOutputBytes int64
	maxResultOutputBytes  int
}

func NewProcessRunner(cfg ProcessRunnerConfig) (*ProcessRunner, error) {
	pythonPath := strings.TrimSpace(cfg.PythonPath)
	if pythonPath == "" {
		pythonPath = defaultProcessPython
	}
	workDir := strings.TrimSpace(cfg.WorkDir)
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("sandbox: resolve process workdir: %w", err)
		}
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultProcessRunnerTimeout
	}
	maxProcessOutput := cfg.MaxProcessOutputBytes
	if maxProcessOutput == 0 {
		maxProcessOutput = defaultProcessRunnerOutputBytes
	}
	maxResultOutput := cfg.MaxResultOutputBytes
	if maxResultOutput == 0 {
		maxResultOutput = defaultProcessResultOutputBytes
	}
	if timeout < 0 || maxProcessOutput < 0 || maxResultOutput < 0 {
		return nil, errors.New("sandbox: process runner limits must not be negative")
	}
	return &ProcessRunner{
		pythonPath:            pythonPath,
		workDir:               workDir,
		timeout:               timeout,
		environment:           append([]string(nil), cfg.Environment...),
		maxProcessOutputBytes: maxProcessOutput,
		maxResultOutputBytes:  maxResultOutput,
	}, nil
}

func (r *ProcessRunner) Kind() RuntimeKind {
	return RuntimeKindProcess
}

func (r *ProcessRunner) CheckAvailability() Availability {
	if r == nil {
		return Availability{Available: false, Kind: RuntimeKindProcess, Detail: "process runner not configured"}
	}
	if info, err := os.Stat(r.workDir); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("%s is not a directory", r.workDir)
		}
		return Availability{Available: false, Kind: RuntimeKindProcess, Detail: "process runner workdir unavailable: " + err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.pythonPath, "-c", "import sys; print(sys.version.split()[0])")
	cmd.Env = r.env()
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return Availability{Available: false, Kind: RuntimeKindProcess, Detail: "python availability check timed out"}
	}
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return Availability{Available: false, Kind: RuntimeKindProcess, Detail: detail}
	}
	return Availability{Available: true, Kind: RuntimeKindProcess, Detail: "python " + strings.TrimSpace(string(out)) + " available in Aura container"}
}

// ValidateCode is a no-op for the process runtime. Code runs as the Aura user
// with full container access; static validation here cannot meaningfully sandbox
// it. Real isolation would require a namespaced or VM-backed runtime.
func (r *ProcessRunner) ValidateCode(_ string) error {
	return nil
}

// Execute runs Python in the Aura container's process namespace. The
// allowNetwork argument is accepted for interface compatibility with future
// network-isolated runtimes; the process runtime cannot honor it — code sees
// the same network reachability as Aura itself. Callers that need true network
// isolation must select a different Runtime implementation.
func (r *ProcessRunner) Execute(ctx context.Context, code string, _ bool) (*Result, error) {
	return r.execute(ctx, code, r.workDir)
}

// ExecuteCommand has the same network-isolation semantics as Execute: the
// allowNetwork arg is advisory for the process runtime and ignored here.
func (r *ProcessRunner) ExecuteCommand(ctx context.Context, command string, _ bool) (*Result, error) {
	return r.executeCommand(ctx, command, r.workDir)
}

func (r *ProcessRunner) ExtractXLSX(ctx context.Context, body []byte) (source.ExtractResult, error) {
	if r == nil {
		return source.ExtractResult{}, errors.New("sandbox: process runner not configured")
	}
	if err := validateXLSXArchive(body); err != nil {
		return source.ExtractResult{}, err
	}
	tmpDir, err := os.MkdirTemp("", "aura-xlsx-*")
	if err != nil {
		return source.ExtractResult{}, fmt.Errorf("sandbox: create xlsx input dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "workbook.xlsx"), body, 0o600); err != nil {
		return source.ExtractResult{}, fmt.Errorf("sandbox: write xlsx input: %w", err)
	}
	res, err := r.execute(ctx, trustedXLSXExtractorCode, tmpDir)
	if err != nil {
		return source.ExtractResult{}, err
	}
	return extractMarkdownResult(res, "process")
}

func (r *ProcessRunner) ExtractDOCX(ctx context.Context, body []byte) (source.ExtractResult, error) {
	if r == nil {
		return source.ExtractResult{}, errors.New("sandbox: process runner not configured")
	}
	tmpDir, err := os.MkdirTemp("", "aura-docx-*")
	if err != nil {
		return source.ExtractResult{}, fmt.Errorf("sandbox: create docx input dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "document.docx"), body, 0o600); err != nil {
		return source.ExtractResult{}, fmt.Errorf("sandbox: write docx input: %w", err)
	}
	res, err := r.execute(ctx, trustedDOCXExtractorCode, tmpDir)
	if err != nil {
		return source.ExtractResult{}, err
	}
	return extractMarkdownResult(res, "process")
}

func (r *ProcessRunner) execute(ctx context.Context, code, workDir string) (*Result, error) {
	if r == nil {
		return nil, errors.New("sandbox: process runner not configured")
	}
	timeout := r.timeout
	if timeout == 0 {
		timeout = defaultProcessRunnerTimeout
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Per-execution output directory. The previous package-level mutex
	// existed only to keep concurrent calls from clobbering each other's
	// /tmp/aura_out — give every call its own dir and the serialization
	// disappears, letting parallel tool calls actually run in parallel.
	outputDir, err := os.MkdirTemp("", "aura-out-*")
	if err != nil {
		return nil, fmt.Errorf("sandbox: prepare output dir: %w", err)
	}
	defer os.RemoveAll(outputDir)

	tmpDir, err := os.MkdirTemp("", "aura-python-*")
	if err != nil {
		return nil, fmt.Errorf("sandbox: create process script dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	scriptPath := filepath.Join(tmpDir, "main.py")
	if err := os.WriteFile(scriptPath, []byte(code), 0o600); err != nil {
		return nil, fmt.Errorf("sandbox: write process script: %w", err)
	}

	cmd := exec.CommandContext(runCtx, r.pythonPath, scriptPath)
	cmd.Dir = workDir
	cmd.Env = append(r.env(), "AURA_OUT_DIR="+outputDir, "PYTHONUNBUFFERED=1")

	var stdout, stderr limitedBuffer
	stdout.limit = r.maxProcessOutputBytes
	stderr.limit = r.maxProcessOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start)
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("sandbox: process runner timed out after %v", timeout)
	}

	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	artifacts, artifactErr := collectProcessArtifacts(outputDir)
	if artifactErr != nil {
		return nil, artifactErr
	}
	return &Result{
		OK:        err == nil,
		Stdout:    clipOutput(stdout.String(), r.maxResultOutputBytes),
		Stderr:    clipOutput(stderr.String(), r.maxResultOutputBytes),
		ExitCode:  exitCode,
		ElapsedMs: int(elapsed.Milliseconds()),
		Artifacts: artifacts,
	}, nil
}

func (r *ProcessRunner) executeCommand(ctx context.Context, command, workDir string) (*Result, error) {
	if r == nil {
		return nil, errors.New("sandbox: process runner not configured")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("sandbox: command must not be empty")
	}
	timeout := r.timeout
	if timeout == 0 {
		timeout = defaultProcessRunnerTimeout
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	outputDir, err := os.MkdirTemp("", "aura-out-*")
	if err != nil {
		return nil, fmt.Errorf("sandbox: prepare output dir: %w", err)
	}
	defer os.RemoveAll(outputDir)

	shell, args := processShellCommand(command)
	cmd := exec.CommandContext(runCtx, shell, args...)
	cmd.Dir = workDir
	cmd.Env = append(r.env(), "AURA_OUT_DIR="+outputDir, "PYTHONUNBUFFERED=1")

	var stdout, stderr limitedBuffer
	stdout.limit = r.maxProcessOutputBytes
	stderr.limit = r.maxProcessOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start)
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("sandbox: shell command timed out after %v", timeout)
	}

	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	artifacts, artifactErr := collectProcessArtifacts(outputDir)
	if artifactErr != nil {
		return nil, artifactErr
	}
	return &Result{
		OK:        err == nil,
		Stdout:    clipOutput(stdout.String(), r.maxResultOutputBytes),
		Stderr:    clipOutput(stderr.String(), r.maxResultOutputBytes),
		ExitCode:  exitCode,
		ElapsedMs: int(elapsed.Milliseconds()),
		Artifacts: artifacts,
	}, nil
}

func processShellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func (r *ProcessRunner) env() []string {
	if r.environment != nil {
		return append([]string(nil), r.environment...)
	}
	return os.Environ()
}

func collectProcessArtifacts(outputDir string) ([]Artifact, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("sandbox: read output dir: %w", err)
	}
	artifacts := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if len(artifacts) >= maxArtifacts {
			return nil, fmt.Errorf("sandbox: process emitted more than %d artifacts", maxArtifacts)
		}
		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("sandbox: stat artifact %s: %w", name, err)
		}
		if info.Size() > maxArtifactBytes {
			return nil, fmt.Errorf("sandbox: artifact %s exceeds %d bytes", name, maxArtifactBytes)
		}
		body, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			return nil, fmt.Errorf("sandbox: read artifact %s: %w", name, err)
		}
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		artifacts = append(artifacts, Artifact{Name: name, MimeType: mimeType, Bytes: body, SizeBytes: int64(len(body))})
	}
	return artifacts, nil
}
