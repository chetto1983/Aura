package sandbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPyodideContainerRunnerExecuteSendsProtocol(t *testing.T) {
	var captured pyodideContainerRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/execute" {
			t.Fatalf("path = %q, want /execute", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(pyodideRunnerResponse{
			OK:       true,
			Stdout:   "6\n",
			ExitCode: 0,
		})
	}))
	defer server.Close()

	runner, err := NewPyodideContainerRunner(PyodideContainerRunnerConfig{
		BaseURL: server.URL,
		Timeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPyodideContainerRunner() error = %v", err)
	}
	result, err := runner.Execute(context.Background(), "print(sum(range(4)))", true)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.OK || result.Stdout != "6\n" {
		t.Fatalf("result = %+v", result)
	}
	if captured.Code != "print(sum(range(4)))" {
		t.Fatalf("code = %q", captured.Code)
	}
	if !captured.AllowNetwork {
		t.Fatal("allow_network = false, want true")
	}
	if captured.TimeoutMS <= 0 {
		t.Fatalf("timeout_ms = %d, want positive", captured.TimeoutMS)
	}
	if len(captured.Packages) != 0 {
		t.Fatalf("packages = %v, want no package load for stdlib-only code", captured.Packages)
	}
	if !hasString(captured.OutputFileAllowlist, defaultPyodideOutputDir) {
		t.Fatalf("output_file_allowlist = %v, want %s", captured.OutputFileAllowlist, defaultPyodideOutputDir)
	}
}

func TestPyodideContainerRunnerExecuteLoadsImportedPackages(t *testing.T) {
	var captured pyodideContainerRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(pyodideRunnerResponse{OK: true, ExitCode: 0})
	}))
	defer server.Close()

	runner, err := NewPyodideContainerRunner(PyodideContainerRunnerConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewPyodideContainerRunner() error = %v", err)
	}
	_, err = runner.Execute(context.Background(), "import pandas as pd\nfrom rich.text import Text\nprint('ok')", false)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !hasString(captured.Packages, "pandas") || !hasString(captured.Packages, "rich") {
		t.Fatalf("packages = %v, want imported Pyodide packages", captured.Packages)
	}
	if hasString(captured.Packages, "numpy") {
		t.Fatalf("packages = %v, did not expect unrelated baseline packages", captured.Packages)
	}
}

func TestPyodideContainerRunnerCheckAvailability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q, want /health", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"detail":"ready"}`))
	}))
	defer server.Close()

	runner, err := NewPyodideContainerRunner(PyodideContainerRunnerConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewPyodideContainerRunner() error = %v", err)
	}
	availability := runner.CheckAvailability()
	if !availability.Available {
		t.Fatalf("Available = false, detail=%q", availability.Detail)
	}
	if availability.Kind != RuntimeKindPyodide {
		t.Fatalf("Kind = %q, want pyodide", availability.Kind)
	}
	if !strings.Contains(availability.Detail, "ready") {
		t.Fatalf("Detail = %q, want ready", availability.Detail)
	}
}

func TestPyodideContainerRunnerCheckAvailabilityUnavailable(t *testing.T) {
	runner, err := NewPyodideContainerRunner(PyodideContainerRunnerConfig{BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewPyodideContainerRunner() error = %v", err)
	}
	availability := runner.CheckAvailability()
	if availability.Available {
		t.Fatal("Available = true, want false")
	}
	if !strings.Contains(availability.Detail, "container runner health") {
		t.Fatalf("Detail = %q, want health diagnostic", availability.Detail)
	}
}

func TestPyodideContainerRunnerExtractDOCXSendsInputContent(t *testing.T) {
	var captured pyodideContainerRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(pyodideRunnerResponse{
			OK:       true,
			ExitCode: 0,
			Artifacts: []pyodideRunnerArtifactIn{
				containerArtifact("extract.md", "text/markdown", []byte("hello doc\n")),
				containerArtifact("extract.json", "application/json", []byte(`{"extractor_name":"pyodide_docx","extractor_version":"v","text_bytes":10}`)),
			},
		})
	}))
	defer server.Close()
	runner, err := NewPyodideContainerRunner(PyodideContainerRunnerConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewPyodideContainerRunner() error = %v", err)
	}

	result, err := runner.ExtractDOCX(context.Background(), []byte("doc bytes"))
	if err != nil {
		t.Fatalf("ExtractDOCX() error = %v", err)
	}
	if result.Metadata.ExtractorName != "pyodide_docx" || result.Markdown != "hello doc\n" {
		t.Fatalf("result = %+v markdown=%q", result.Metadata, result.Markdown)
	}
	if len(captured.InputFiles) != 1 {
		t.Fatalf("input files = %+v, want one document", captured.InputFiles)
	}
	if captured.InputFiles[0].Name != "document.docx" {
		t.Fatalf("input name = %q", captured.InputFiles[0].Name)
	}
	decoded, err := base64.StdEncoding.DecodeString(captured.InputFiles[0].ContentBase64)
	if err != nil {
		t.Fatalf("input content base64: %v", err)
	}
	if string(decoded) != "doc bytes" {
		t.Fatalf("input bytes = %q", decoded)
	}
}

func TestPyodideContainerRunnerRejectsArtifactTraversal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(pyodideRunnerResponse{
			OK:       true,
			ExitCode: 0,
			Artifacts: []pyodideRunnerArtifactIn{
				{Name: "../escape.txt", MimeType: "text/plain", SizeBytes: 2, ContentBase64: base64.StdEncoding.EncodeToString([]byte("no"))},
			},
		})
	}))
	defer server.Close()
	runner, err := NewPyodideContainerRunner(PyodideContainerRunnerConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewPyodideContainerRunner() error = %v", err)
	}

	_, err = runner.Execute(context.Background(), "print('x')", false)
	if err == nil || !strings.Contains(err.Error(), "plain filename") {
		t.Fatalf("Execute() error = %v, want traversal rejection", err)
	}
}

func hasString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func containerArtifact(name, mimeType string, body []byte) pyodideRunnerArtifactIn {
	return pyodideRunnerArtifactIn{
		Name:          name,
		MimeType:      mimeType,
		SizeBytes:     int64(len(body)),
		ContentBase64: base64.StdEncoding.EncodeToString(body),
	}
}
