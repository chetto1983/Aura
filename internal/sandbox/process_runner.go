package sandbox

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/source"
)

const (
	defaultProcessPython        = "python3"
	defaultProcessRunnerTimeout = 15 * time.Second
)

var processOutputMu sync.Mutex

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
		maxProcessOutput = defaultPyodideRunnerOutputBytes
	}
	maxResultOutput := cfg.MaxResultOutputBytes
	if maxResultOutput == 0 {
		maxResultOutput = defaultPyodideResultOutputBytes
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

func (r *ProcessRunner) ValidateCode(_ string) error {
	return nil
}

func (r *ProcessRunner) Execute(ctx context.Context, code string, _ bool) (*Result, error) {
	return r.execute(ctx, code, r.workDir)
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
	return extractPyodideMarkdownResult(res, "process")
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
	return extractPyodideMarkdownResult(res, "process")
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

	processOutputMu.Lock()
	defer processOutputMu.Unlock()

	outputDir := processOutputDir()
	_ = os.RemoveAll(outputDir)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("sandbox: prepare output dir: %w", err)
	}

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
		Stdout:    clipPyodideOutput(stdout.String(), r.maxResultOutputBytes),
		Stderr:    clipPyodideOutput(stderr.String(), r.maxResultOutputBytes),
		ExitCode:  exitCode,
		ElapsedMs: int(elapsed.Milliseconds()),
		Artifacts: artifacts,
	}, nil
}

func (r *ProcessRunner) env() []string {
	if r.environment != nil {
		return append([]string(nil), r.environment...)
	}
	return os.Environ()
}

func processOutputDir() string {
	if filepath.IsAbs(defaultPyodideOutputDir) {
		if filepath.VolumeName(defaultPyodideOutputDir) != "" || os.PathSeparator == '/' {
			return defaultPyodideOutputDir
		}
	}
	return filepath.Join(os.TempDir(), "aura_out")
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
		if len(artifacts) >= maxPyodideArtifacts {
			return nil, fmt.Errorf("sandbox: process emitted more than %d artifacts", maxPyodideArtifacts)
		}
		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("sandbox: stat artifact %s: %w", name, err)
		}
		if info.Size() > maxPyodideArtifactBytes {
			return nil, fmt.Errorf("sandbox: artifact %s exceeds %d bytes", name, maxPyodideArtifactBytes)
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
