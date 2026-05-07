package sandbox_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	if len(capture.Request.Packages) != 0 {
		t.Fatalf("packages = %v, want no package load for stdlib-only code", capture.Request.Packages)
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
	if !envContains(capture.Env, "NODE_OPTIONS") {
		t.Fatalf("env missing Node runtime tuning var: %v", capture.Env)
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

func TestPyodideRunner_ExtractXLSXUsesInputFileAndArtifacts(t *testing.T) {
	capturePath := filepath.Join(t.TempDir(), "capture.json")
	runner, _ := newFakePyodideRunner(t, "xlsx-artifact", capturePath)
	body := makeZipWithEntry(t, "xl/worksheets/sheet1.xml", "<worksheet><sheetData/></worksheet>")

	result, err := runner.ExtractXLSX(context.Background(), body)
	if err != nil {
		t.Fatalf("ExtractXLSX() error = %v", err)
	}
	if result.Metadata.ExtractorName != "pyodide_xlsx" || !strings.Contains(result.Markdown, "sandbox") {
		t.Fatalf("result = %+v\n%s", result.Metadata, result.Markdown)
	}

	capture := readFakeRunnerCapture(t, capturePath)
	if len(capture.Request.Code) >= 100_000 {
		t.Fatalf("trusted extractor code length = %d, want below execute_code policy limit", len(capture.Request.Code))
	}
	if strings.Contains(capture.Request.Code, strings.Repeat("x", 100)) {
		t.Fatalf("trusted extractor code embeds workbook bytes")
	}
	if len(capture.Request.InputFiles) != 1 || filepath.Base(capture.Request.InputFiles[0]) != "workbook.xlsx" {
		t.Fatalf("input_files = %v, want workbook.xlsx input", capture.Request.InputFiles)
	}
}

func TestPyodideRunner_ExtractXLSXRejectsOversizedArchiveBeforeRunner(t *testing.T) {
	capturePath := filepath.Join(t.TempDir(), "capture.json")
	runner, _ := newFakePyodideRunner(t, "xlsx-artifact", capturePath)
	body := makeZipWithEntry(t, "xl/worksheets/sheet1.xml", strings.Repeat("x", 21*1024*1024))

	_, err := runner.ExtractXLSX(context.Background(), body)
	if err == nil || !strings.Contains(err.Error(), "uncompressed size exceeds limit") {
		t.Fatalf("ExtractXLSX() error = %v, want uncompressed size limit", err)
	}
	if _, statErr := os.Stat(capturePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("runner capture stat error = %v, want runner not invoked", statErr)
	}
}

func TestPyodideRunner_ExtractDOCXUsesInputFileAndArtifacts(t *testing.T) {
	capturePath := filepath.Join(t.TempDir(), "capture.json")
	runner, _ := newFakePyodideRunner(t, "docx-artifact", capturePath)
	body := []byte(strings.Repeat("d", 160_000))

	result, err := runner.ExtractDOCX(context.Background(), body)
	if err != nil {
		t.Fatalf("ExtractDOCX() error = %v", err)
	}
	if result.Metadata.ExtractorName != "pyodide_docx" || !strings.Contains(result.Markdown, "decisions") {
		t.Fatalf("result = %+v\n%s", result.Metadata, result.Markdown)
	}

	capture := readFakeRunnerCapture(t, capturePath)
	if len(capture.Request.Code) >= 100_000 {
		t.Fatalf("trusted extractor code length = %d, want below execute_code policy limit", len(capture.Request.Code))
	}
	if strings.Contains(capture.Request.Code, strings.Repeat("d", 100)) {
		t.Fatalf("trusted extractor code embeds document bytes")
	}
	for _, want := range []string{"MAX_DOCX_XML_PART_BYTES", "MAX_DOCX_TEXT_BYTES", "MAX_DOCX_COMPRESSION_RATIO", "zf.getinfo(name)", "zf.infolist()"} {
		if !strings.Contains(capture.Request.Code, want) {
			t.Fatalf("trusted DOCX extractor code missing %q", want)
		}
	}
	if len(capture.Request.InputFiles) != 1 || filepath.Base(capture.Request.InputFiles[0]) != "document.docx" {
		t.Fatalf("input_files = %v, want document.docx input", capture.Request.InputFiles)
	}
}

func makeZipWithEntry(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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

func TestPyodideRunner_CheckAvailabilityRequiresNodeRuntime(t *testing.T) {
	runtimeDir := t.TempDir()
	writePyodideBundle(t, runtimeDir, nil)
	runnerPath := filepath.Join(runtimeDir, "runner", "aura-pyodide-runner")
	if err := os.MkdirAll(filepath.Dir(runnerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\nexec node \"$0.mjs\" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")

	runner, err := sandbox.NewPyodideRunner(sandbox.PyodideRunnerConfig{
		RuntimeDir: runtimeDir,
		RunnerPath: runnerPath,
	})
	if err != nil {
		t.Fatalf("NewPyodideRunner() error = %v", err)
	}

	availability := runner.CheckAvailability()
	if availability.Available {
		t.Fatal("Available = true, want false without Node")
	}
	if !strings.Contains(availability.Detail, "requires Node.js") {
		t.Fatalf("Detail = %q, want Node diagnostic", availability.Detail)
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
		Timeout:     time.Second,
		Environment: []string{"PATH=/bin", "TEMP=/tmp", "NODE_OPTIONS=--max-old-space-size=4096", "TELEGRAM_TOKEN=secret", "LLM_API_KEY=secret", "CUSTOM_SECRET=secret"},
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
	InputFiles          []string `json:"input_files"`
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
	case "xlsx-artifact":
		fmt.Print(`{"ok":true,"stdout":"` + strings.Repeat("x", 70000) + `","stderr":"","exit_code":0,"elapsed_ms":9,"artifacts":[{"name":"extract.md","mime_type":"text/markdown; charset=utf-8","size_bytes":47,"content_base64":"fCBpdGVtIHwgY29zdCB8CnwgLS0tIHwgLS0tIHwKfCBzYW5kYm94IHwgMTIgfAo="},{"name":"extract.json","mime_type":"application/json","size_bytes":63,"content_base64":"eyJleHRyYWN0b3JfbmFtZSI6InB5b2RpZGVfeGxzeCIsInNoZWV0X2NvdW50IjoxLCJyb3dfY291bnQiOjF9"}]}`)
		os.Exit(0)
	case "docx-artifact":
		fmt.Print(`{"ok":true,"stdout":"docx extraction complete\n","stderr":"","exit_code":0,"elapsed_ms":9,"artifacts":[{"name":"extract.md","mime_type":"text/markdown; charset=utf-8","size_bytes":40,"content_base64":"IyBNZW1vCgpBdXJhIHNob3VsZCByZW1lbWJlciBkZWNpc2lvbnMuCg=="},{"name":"extract.json","mime_type":"application/json","size_bytes":49,"content_base64":"eyJleHRyYWN0b3JfbmFtZSI6InB5b2RpZGVfZG9jeCIsInRleHRfYnl0ZXMiOjQwfQ=="}]}`)
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
