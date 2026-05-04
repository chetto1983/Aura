package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultPyodideRuntimeDir        = "./runtime/pyodide"
	defaultPyodideRunnerTimeout     = 15 * time.Second
	defaultPyodideRunnerOutputBytes = 1 << 20
	defaultPyodideResultOutputBytes = 64 * 1024
	defaultPyodideOutputDir         = "/tmp/aura_out"
	maxPyodideArtifacts             = 10
	maxPyodideArtifactBytes         = 5 << 20
)

// PyodideRunnerConfig controls the bundled Pyodide runner adapter.
type PyodideRunnerConfig struct {
	RuntimeDir            string
	RunnerPath            string
	RunnerArgs            []string
	Timeout               time.Duration
	Environment           []string
	MaxProcessOutputBytes int64
	MaxResultOutputBytes  int
}

// PyodideRunner executes Python through Aura's bundled Pyodide runner process.
type PyodideRunner struct {
	runtimeDir            string
	runnerPath            string
	runnerArgs            []string
	timeout               time.Duration
	environment           []string
	maxProcessOutputBytes int64
	maxResultOutputBytes  int
}

type pyodideRunnerRequest struct {
	Code                string   `json:"code"`
	TimeoutMS           int      `json:"timeout_ms"`
	AllowNetwork        bool     `json:"allow_network"`
	Packages            []string `json:"packages"`
	InputFiles          []string `json:"input_files"`
	OutputFileAllowlist []string `json:"output_file_allowlist"`
}

type pyodideRunnerResponse struct {
	OK        bool                      `json:"ok"`
	Stdout    string                    `json:"stdout"`
	Stderr    string                    `json:"stderr"`
	ExitCode  int                       `json:"exit_code"`
	ElapsedMs int                       `json:"elapsed_ms"`
	Error     string                    `json:"error,omitempty"`
	Artifacts []pyodideRunnerArtifactIn `json:"artifacts,omitempty"`
}

type pyodideRunnerArtifactIn struct {
	Name          string `json:"name"`
	MimeType      string `json:"mime_type"`
	SizeBytes     int64  `json:"size_bytes"`
	ContentBase64 string `json:"content_base64"`
}

// NewPyodideRunner creates a runtime adapter for the bundled Pyodide runner.
func NewPyodideRunner(cfg PyodideRunnerConfig) (*PyodideRunner, error) {
	runtimeDir := strings.TrimSpace(cfg.RuntimeDir)
	if runtimeDir == "" {
		runtimeDir = defaultPyodideRuntimeDir
	}
	runnerPath := strings.TrimSpace(cfg.RunnerPath)
	if runnerPath == "" {
		runnerPath = defaultPyodideRunnerPath(runtimeDir)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultPyodideRunnerTimeout
	}
	maxProcessOutput := cfg.MaxProcessOutputBytes
	if maxProcessOutput == 0 {
		maxProcessOutput = defaultPyodideRunnerOutputBytes
	}
	maxResultOutput := cfg.MaxResultOutputBytes
	if maxResultOutput == 0 {
		maxResultOutput = defaultPyodideResultOutputBytes
	}
	if timeout < 0 {
		return nil, errors.New("sandbox: Pyodide runner timeout must not be negative")
	}
	if maxProcessOutput < 0 || maxResultOutput < 0 {
		return nil, errors.New("sandbox: Pyodide runner output limits must not be negative")
	}
	return &PyodideRunner{
		runtimeDir:            runtimeDir,
		runnerPath:            runnerPath,
		runnerArgs:            append([]string(nil), cfg.RunnerArgs...),
		timeout:               timeout,
		environment:           append([]string(nil), cfg.Environment...),
		maxProcessOutputBytes: maxProcessOutput,
		maxResultOutputBytes:  maxResultOutput,
	}, nil
}

func (r *PyodideRunner) Kind() RuntimeKind {
	return RuntimeKindPyodide
}

func (r *PyodideRunner) CheckAvailability() Availability {
	if r == nil {
		return Availability{
			Available: false,
			Kind:      RuntimeKindPyodide,
			Detail:    "Pyodide runner not configured",
		}
	}
	probe := ProbePyodideBundle(r.runtimeDir)
	if !probe.Valid {
		return Availability{
			Available: false,
			Kind:      RuntimeKindPyodide,
			Detail:    probe.Detail,
		}
	}
	if info, err := os.Stat(r.runnerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Availability{
				Available: false,
				Kind:      RuntimeKindPyodide,
				Detail:    fmt.Sprintf("Pyodide runner missing at %s", r.runnerPath),
			}
		}
		return Availability{
			Available: false,
			Kind:      RuntimeKindPyodide,
			Detail:    fmt.Sprintf("checking Pyodide runner: %v", err),
		}
	} else if info.IsDir() {
		return Availability{
			Available: false,
			Kind:      RuntimeKindPyodide,
			Detail:    fmt.Sprintf("Pyodide runner path is a directory: %s", r.runnerPath),
		}
	}
	return Availability{
		Available: true,
		Kind:      RuntimeKindPyodide,
		Detail:    "Pyodide runner available",
	}
}

func (r *PyodideRunner) ValidateCode(_ string) error {
	return nil
}

func (r *PyodideRunner) Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error) {
	if r == nil {
		return nil, errors.New("sandbox: Pyodide runner not configured")
	}
	timeout := r.timeout
	if timeout == 0 {
		timeout = defaultPyodideRunnerTimeout
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	request := pyodideRunnerRequest{
		Code:                code,
		TimeoutMS:           int(timeout.Milliseconds()),
		AllowNetwork:        allowNetwork,
		Packages:            append([]string(nil), RequiredPyodideImports...),
		InputFiles:          []string{},
		OutputFileAllowlist: []string{defaultPyodideOutputDir},
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("sandbox: encoding Pyodide runner request: %w", err)
	}

	args := append([]string(nil), r.runnerArgs...)
	args = append(args, "--runtime-dir", r.runtimeDir)
	cmd := exec.CommandContext(runCtx, r.runnerPath, args...)
	cmd.Stdin = bytes.NewReader(requestJSON)
	cmd.Env = sanitizedPyodideRunnerEnv(r.environment)

	var stdout, stderr limitedBuffer
	stdout.limit = r.maxProcessOutputBytes
	stderr.limit = r.maxProcessOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start)
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("sandbox: Pyodide runner timed out after %v", timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("sandbox: Pyodide runner failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var response pyodideRunnerResponse
	if err := json.Unmarshal([]byte(stdout.String()), &response); err != nil {
		return nil, fmt.Errorf("sandbox: parsing runner response: %w", err)
	}
	if response.ElapsedMs == 0 {
		response.ElapsedMs = int(elapsed.Milliseconds())
	}
	if response.Error != "" && response.Stderr == "" {
		response.Stderr = response.Error
	}
	artifacts, err := decodePyodideArtifacts(response.Artifacts)
	if err != nil {
		return nil, err
	}
	return &Result{
		OK:        response.OK,
		Stdout:    clipPyodideOutput(response.Stdout, r.maxResultOutputBytes),
		Stderr:    clipPyodideOutput(response.Stderr, r.maxResultOutputBytes),
		ExitCode:  response.ExitCode,
		ElapsedMs: response.ElapsedMs,
		Artifacts: artifacts,
	}, nil
}

func decodePyodideArtifacts(raw []pyodideRunnerArtifactIn) ([]Artifact, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > maxPyodideArtifacts {
		return nil, fmt.Errorf("sandbox: runner returned %d artifacts, max %d", len(raw), maxPyodideArtifacts)
	}
	artifacts := make([]Artifact, 0, len(raw))
	for i, item := range raw {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("sandbox: artifact[%d] missing name", i)
		}
		if name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("sandbox: artifact[%d] name must be a plain filename", i)
		}
		body, err := base64.StdEncoding.DecodeString(item.ContentBase64)
		if err != nil {
			return nil, fmt.Errorf("sandbox: artifact[%d] base64: %w", i, err)
		}
		if len(body) > maxPyodideArtifactBytes {
			return nil, fmt.Errorf("sandbox: artifact[%d] exceeds %d bytes", i, maxPyodideArtifactBytes)
		}
		mimeType := strings.TrimSpace(item.MimeType)
		if mimeType == "" {
			mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		size := item.SizeBytes
		if size == 0 {
			size = int64(len(body))
		}
		if size != int64(len(body)) {
			return nil, fmt.Errorf("sandbox: artifact[%d] size mismatch", i)
		}
		artifacts = append(artifacts, Artifact{
			Name:      name,
			MimeType:  mimeType,
			Bytes:     body,
			SizeBytes: size,
		})
	}
	return artifacts, nil
}

func defaultPyodideRunnerPath(runtimeDir string) string {
	name := "aura-pyodide-runner"
	if runtime.GOOS == "windows" {
		cmdPath := filepath.Join(runtimeDir, "runner", name+".cmd")
		if info, err := os.Stat(cmdPath); err == nil && !info.IsDir() {
			return cmdPath
		}
		name += ".exe"
	}
	return filepath.Join(runtimeDir, "runner", name)
}

func sanitizedPyodideRunnerEnv(env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	keep := map[string]bool{
		"APPDATA":      true,
		"HOME":         true,
		"LANG":         true,
		"LC_ALL":       true,
		"LOCALAPPDATA": true,
		"PATH":         true,
		"PATHEXT":      true,
		"SYSTEMROOT":   true,
		"TEMP":         true,
		"TMP":          true,
		"TMPDIR":       true,
		"USERPROFILE":  true,
		"WINDIR":       true,
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToUpper(kv[:eq])
		if keep[key] {
			out = append(out, kv)
		}
	}
	return out
}

func clipPyodideOutput(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...[truncated]"
}

type limitedBuffer struct {
	data      bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 {
		remaining := b.limit - int64(b.data.Len())
		if remaining <= 0 {
			b.truncated = true
			return len(p), nil
		}
		if int64(len(p)) > remaining {
			_, _ = b.data.Write(p[:remaining])
			b.truncated = true
			return len(p), nil
		}
	}
	_, _ = b.data.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	if b.truncated {
		return b.data.String() + "\n...[truncated]"
	}
	return b.data.String()
}
