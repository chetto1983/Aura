package sandbox_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/sandbox"
)

func TestPyodideRunner_ExecuteSendsProtocolAndSanitizedEnv(t *testing.T) {
	capturePath := filepath.Join(t.TempDir(), "capture.json")
	runner, runtimeDir := newFakePyodideRunner(t, "success", capturePath)

	result, err := runner.Execute(context.Background(), "print(sum(range(4)))", true)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.OK || result.Stdout != "6\n" || result.ExitCode != 0 {
		t.Fatalf("result = %+v", result)
	}

	capture := readFakeRunnerCapture(t, capturePath)
	if capture.Request.Code != "print(sum(range(4)))" {
		t.Fatalf("code = %q", capture.Request.Code)
	}
	if !capture.Request.AllowNetwork {
		t.Fatal("allow_network = false, want true")
	}
	if capture.Request.TimeoutMS <= 0 {
		t.Fatalf("timeout_ms = %d, want positive", capture.Request.TimeoutMS)
	}
	if !slices.Contains(capture.Request.Packages, "numpy") || !slices.Contains(capture.Request.Packages, "rich") {
		t.Fatalf("packages = %v, want baseline package profile", capture.Request.Packages)
	}
	if !slices.Contains(capture.Request.OutputFileAllowlist, "/tmp/aura_out") {
		t.Fatalf("output_file_allowlist = %v, want /tmp/aura_out", capture.Request.OutputFileAllowlist)
	}
	if !argPair(capture.Args, "--runtime-dir", runtimeDir) {
		t.Fatalf("args = %v, want --runtime-dir", capture.Args)
	}
	if envContains(capture.Env, "TELEGRAM_TOKEN") || envContains(capture.Env, "LLM_API_KEY") || envContains(capture.Env, "CUSTOM_SECRET") {
		t.Fatalf("env leaked secret: %v", capture.Env)
	}
	if !envContains(capture.Env, "PATH") || !envContains(capture.Env, "TEMP") {
		t.Fatalf("env missing safe process vars: %v", capture.Env)
	}
}

func TestPyodideRunner_ExecuteReturnsArtifacts(t *testing.T) {
	runner, _ := newFakePyodideRunner(t, "artifact", filepath.Join(t.TempDir(), "capture.json"))

	result, err := runner.Execute(context.Background(), "open('/tmp/aura_out/report.txt','w').write('hello')", false)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1: %+v", len(result.Artifacts), result.Artifacts)
	}
	artifact := result.Artifacts[0]
	if artifact.Name != "report.txt" {
		t.Fatalf("artifact name = %q", artifact.Name)
	}
	if artifact.MimeType != "text/plain; charset=utf-8" {
		t.Fatalf("mime = %q", artifact.MimeType)
	}
	if string(artifact.Bytes) != "hello" {
		t.Fatalf("bytes = %q", string(artifact.Bytes))
	}
	if artifact.SizeBytes != 5 {
		t.Fatalf("size = %d, want 5", artifact.SizeBytes)
	}
}

func TestPyodideRunner_ExecuteRejectsArtifactTraversal(t *testing.T) {
	runner, _ := newFakePyodideRunner(t, "artifact-traversal", filepath.Join(t.TempDir(), "capture.json"))

	_, err := runner.Execute(context.Background(), "open('/tmp/aura_out/../escape.txt','w').write('nope')", false)
	if err == nil || !strings.Contains(err.Error(), "plain filename") {
		t.Fatalf("Execute() error = %v, want plain filename rejection", err)
	}
}

func TestPyodideRunner_ExecuteReturnsRunnerFailure(t *testing.T) {
	runner, _ := newFakePyodideRunner(t, "exit", filepath.Join(t.TempDir(), "capture.json"))

	_, err := runner.Execute(context.Background(), "print('x')", false)
	if err == nil || !strings.Contains(err.Error(), "runner failed") || !strings.Contains(err.Error(), "runner boom") {
		t.Fatalf("expected runner failure with stderr, got %v", err)
	}
}

func TestPyodideRunner_ExecuteRejectsInvalidRunnerJSON(t *testing.T) {
	runner, _ := newFakePyodideRunner(t, "invalid-json", filepath.Join(t.TempDir(), "capture.json"))

	_, err := runner.Execute(context.Background(), "print('x')", false)
	if err == nil || !strings.Contains(err.Error(), "parsing runner response") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestPyodideRunner_ExecuteTimesOut(t *testing.T) {
	capturePath := filepath.Join(t.TempDir(), "capture.json")
	runner, _ := newFakePyodideRunner(t, "sleep", capturePath)

	start := time.Now()
	_, err := runner.Execute(context.Background(), "print('slow')", false)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %v, want prompt kill", elapsed)
	}
}

func TestPyodideRunner_CheckAvailabilityRequiresManifestAndRunner(t *testing.T) {
	runtimeDir := t.TempDir()
	runner, err := sandbox.NewPyodideRunner(sandbox.PyodideRunnerConfig{RuntimeDir: runtimeDir})
	if err != nil {
		t.Fatalf("NewPyodideRunner() error = %v", err)
	}

	availability := runner.CheckAvailability()
	if availability.Available {
		t.Fatal("Available = true, want false without manifest")
	}
	if availability.Kind != sandbox.RuntimeKindPyodide {
		t.Fatalf("Kind = %q, want pyodide", availability.Kind)
	}
	if !strings.Contains(availability.Detail, "manifest missing") {
		t.Fatalf("Detail = %q, want manifest diagnostic", availability.Detail)
	}
}

func TestPyodideRunner_LivePyodideBundle(t *testing.T) {
	if os.Getenv("AURA_SANDBOX_LIVE") != "1" {
		t.Skip("set AURA_SANDBOX_LIVE=1 and SANDBOX_PYODIDE_RUNNER to run the live Pyodide bundle smoke")
	}
	runnerPath := os.Getenv("SANDBOX_PYODIDE_RUNNER")
	if runnerPath == "" {
		t.Fatal("SANDBOX_PYODIDE_RUNNER must point to the bundled runner")
	}
	runtimeDir := os.Getenv("SANDBOX_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = filepath.Join("..", "..", "runtime", "pyodide")
	} else if !filepath.IsAbs(runtimeDir) {
		runtimeDir = filepath.Join("..", "..", runtimeDir)
	}
	if !filepath.IsAbs(runnerPath) {
		runnerPath = filepath.Join("..", "..", runnerPath)
	}
	runner, err := sandbox.NewPyodideRunner(sandbox.PyodideRunnerConfig{
		RuntimeDir: runtimeDir,
		RunnerPath: runnerPath,
		Timeout:    2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewPyodideRunner() error = %v", err)
	}
	if availability := runner.CheckAvailability(); !availability.Available {
		t.Fatalf("CheckAvailability() = %+v, want available", availability)
	}

	code := strings.Join([]string{
		"import numpy, pandas, scipy, statsmodels, matplotlib, PIL, fitz",
		"import bs4, lxml, html5lib, pyarrow, python_calamine, xlrd",
		"import requests, yaml, dateutil, pytz, tzdata, regex, rich",
		"print(sum(range(101)))",
		"print('imports-ok')",
	}, "\n")
	result, err := runner.Execute(context.Background(), code, false)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Stdout, "5050") || !strings.Contains(result.Stdout, "imports-ok") {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func newFakePyodideRunner(t *testing.T, mode, capturePath string) (*sandbox.PyodideRunner, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(t.TempDir(), "runtime", "pyodide")
	runner, err := sandbox.NewPyodideRunner(sandbox.PyodideRunnerConfig{
		RuntimeDir:  runtimeDir,
		RunnerPath:  exe,
		RunnerArgs:  []string{"-test.run=TestPyodideRunnerHelperProcess", "--", mode, capturePath},
		Timeout:     100 * time.Millisecond,
		Environment: []string{"PATH=/bin", "TEMP=/tmp", "TELEGRAM_TOKEN=secret", "LLM_API_KEY=secret", "CUSTOM_SECRET=secret"},
	})
	if err != nil {
		t.Fatalf("NewPyodideRunner() error = %v", err)
	}
	return runner, runtimeDir
}

type fakeRunnerCapture struct {
	Args    []string              `json:"args"`
	Env     []string              `json:"env"`
	Request fakeRunnerRequestBody `json:"request"`
}

type fakeRunnerRequestBody struct {
	Code                string   `json:"code"`
	TimeoutMS           int      `json:"timeout_ms"`
	AllowNetwork        bool     `json:"allow_network"`
	Packages            []string `json:"packages"`
	OutputFileAllowlist []string `json:"output_file_allowlist"`
}

func readFakeRunnerCapture(t *testing.T, path string) fakeRunnerCapture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	var capture fakeRunnerCapture
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatalf("parse capture: %v", err)
	}
	return capture
}

func argPair(args []string, key, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func envContains(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

func TestPyodideRunnerHelperProcess(t *testing.T) {
	args := os.Args
	sep := slices.Index(args, "--")
	if sep == -1 || sep+2 >= len(args) {
		return
	}
	mode := args[sep+1]
	capturePath := args[sep+2]

	var req fakeRunnerRequestBody
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	capture := fakeRunnerCapture{
		Args:    args[sep+3:],
		Env:     os.Environ(),
		Request: req,
	}
	data, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.WriteFile(capturePath, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	switch mode {
	case "success":
		fmt.Print(`{"ok":true,"stdout":"6\n","stderr":"","exit_code":0,"elapsed_ms":7}`)
		os.Exit(0)
	case "artifact":
		fmt.Print(`{"ok":true,"stdout":"made artifact\n","stderr":"","exit_code":0,"elapsed_ms":9,"artifacts":[{"name":"report.txt","mime_type":"text/plain; charset=utf-8","size_bytes":5,"content_base64":"aGVsbG8="}]}`)
		os.Exit(0)
	case "artifact-traversal":
		fmt.Print(`{"ok":true,"stdout":"made artifact\n","stderr":"","exit_code":0,"elapsed_ms":9,"artifacts":[{"name":"../escape.txt","mime_type":"text/plain; charset=utf-8","size_bytes":4,"content_base64":"bm9wZQ=="}]}`)
		os.Exit(0)
	case "exit":
		fmt.Fprint(os.Stderr, "runner boom")
		os.Exit(7)
	case "invalid-json":
		fmt.Print("not-json")
		os.Exit(0)
	case "sleep":
		time.Sleep(5 * time.Second)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q", mode)
		os.Exit(2)
	}
}
