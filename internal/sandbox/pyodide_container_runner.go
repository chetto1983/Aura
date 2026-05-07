package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aura/aura/internal/source"
)

const defaultPyodideContainerTimeout = 15 * time.Second

// PyodideContainerRunnerConfig controls the HTTP sidecar Pyodide adapter.
type PyodideContainerRunnerConfig struct {
	BaseURL              string
	Timeout              time.Duration
	HTTPClient           *http.Client
	MaxResultOutputBytes int
}

// PyodideContainerRunner executes Python through the containerized Pyodide
// runtime sidecar. It uses the same stable execute_code protocol as the local
// runner, but sends input files inline because the sidecar cannot see host
// temp paths.
type PyodideContainerRunner struct {
	baseURL              string
	timeout              time.Duration
	httpClient           *http.Client
	maxResultOutputBytes int
}

type pyodideContainerRequest struct {
	Code                string                      `json:"code"`
	TimeoutMS           int                         `json:"timeout_ms"`
	AllowNetwork        bool                        `json:"allow_network"`
	Packages            []string                    `json:"packages"`
	InputFiles          []pyodideContainerInputFile `json:"input_files,omitempty"`
	OutputFileAllowlist []string                    `json:"output_file_allowlist"`
}

type pyodideContainerInputFile struct {
	Name          string `json:"name"`
	ContentBase64 string `json:"content_base64"`
}

type pyodideContainerHealth struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// NewPyodideContainerRunner creates a runtime adapter for the Pyodide sidecar.
func NewPyodideContainerRunner(cfg PyodideContainerRunnerConfig) (*PyodideContainerRunner, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("sandbox: SANDBOX_RUNTIME_URL is required for container runtime")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("sandbox: invalid SANDBOX_RUNTIME_URL %q", cfg.BaseURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("sandbox: SANDBOX_RUNTIME_URL scheme %q is not allowed", parsed.Scheme)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultPyodideContainerTimeout
	}
	maxResultOutput := cfg.MaxResultOutputBytes
	if maxResultOutput == 0 {
		maxResultOutput = defaultPyodideResultOutputBytes
	}
	if timeout < 0 || maxResultOutput < 0 {
		return nil, errors.New("sandbox: Pyodide container limits must not be negative")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &PyodideContainerRunner{
		baseURL:              baseURL,
		timeout:              timeout,
		httpClient:           client,
		maxResultOutputBytes: maxResultOutput,
	}, nil
}

func (r *PyodideContainerRunner) Kind() RuntimeKind {
	return RuntimeKindPyodide
}

func (r *PyodideContainerRunner) CheckAvailability() Availability {
	if r == nil {
		return Availability{Available: false, Kind: RuntimeKindPyodide, Detail: "Pyodide container runner not configured"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/health", nil)
	if err != nil {
		return Availability{Available: false, Kind: RuntimeKindPyodide, Detail: err.Error()}
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return Availability{Available: false, Kind: RuntimeKindPyodide, Detail: "Pyodide container runner health: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Availability{Available: false, Kind: RuntimeKindUnavailable, Detail: fmt.Sprintf("Pyodide container runner health returned HTTP %d", resp.StatusCode)}
	}
	var health pyodideContainerHealth
	_ = json.NewDecoder(resp.Body).Decode(&health)
	if !health.OK {
		if strings.TrimSpace(health.Detail) == "" {
			health.Detail = "Pyodide container runner not ready"
		}
		return Availability{Available: false, Kind: RuntimeKindUnavailable, Detail: health.Detail}
	}
	if strings.TrimSpace(health.Detail) == "" {
		health.Detail = "Pyodide container runner available"
	}
	return Availability{Available: true, Kind: RuntimeKindPyodide, Detail: health.Detail}
}

func (r *PyodideContainerRunner) ValidateCode(_ string) error {
	return nil
}

func (r *PyodideContainerRunner) Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error) {
	return r.execute(ctx, code, allowNetwork, nil)
}

func (r *PyodideContainerRunner) ExtractXLSX(ctx context.Context, body []byte) (source.ExtractResult, error) {
	if err := validateXLSXArchive(body); err != nil {
		return source.ExtractResult{}, err
	}
	res, err := r.execute(ctx, trustedXLSXExtractorCode, false, []pyodideContainerInputFile{{
		Name:          "workbook.xlsx",
		ContentBase64: base64.StdEncoding.EncodeToString(body),
	}})
	if err != nil {
		return source.ExtractResult{}, err
	}
	return extractPyodideMarkdownResult(res, "pyodide")
}

func (r *PyodideContainerRunner) ExtractDOCX(ctx context.Context, body []byte) (source.ExtractResult, error) {
	res, err := r.execute(ctx, trustedDOCXExtractorCode, false, []pyodideContainerInputFile{{
		Name:          "document.docx",
		ContentBase64: base64.StdEncoding.EncodeToString(body),
	}})
	if err != nil {
		return source.ExtractResult{}, err
	}
	return extractPyodideMarkdownResult(res, "pyodide")
}

func (r *PyodideContainerRunner) execute(ctx context.Context, code string, allowNetwork bool, inputFiles []pyodideContainerInputFile) (*Result, error) {
	if r == nil {
		return nil, errors.New("sandbox: Pyodide container runner not configured")
	}
	timeout := r.timeout
	if timeout == 0 {
		timeout = defaultPyodideContainerTimeout
	}
	request := pyodideContainerRequest{
		Code:                code,
		TimeoutMS:           int(timeout.Milliseconds()),
		AllowNetwork:        allowNetwork,
		Packages:            PyodidePackagesForCode(code),
		InputFiles:          append([]pyodideContainerInputFile(nil), inputFiles...),
		OutputFileAllowlist: []string{defaultPyodideOutputDir},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("sandbox: encoding Pyodide container request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/execute", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sandbox: Pyodide container request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("sandbox: Pyodide container returned HTTP %d", resp.StatusCode)
	}
	var response pyodideRunnerResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("sandbox: parsing Pyodide container response: %w", err)
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

func extractPyodideMarkdownResult(res *Result, label string) (source.ExtractResult, error) {
	if res == nil {
		return source.ExtractResult{}, errors.New("source: pyodide extraction returned nil result")
	}
	if !res.OK {
		return source.ExtractResult{}, fmt.Errorf("source: %s extraction failed: %s", label, res.Stderr)
	}
	md, ok := artifactBytes(res.Artifacts, "extract.md")
	if !ok || len(md) == 0 {
		return source.ExtractResult{}, fmt.Errorf("source: %s extraction missing extract.md", label)
	}
	metaBytes, ok := artifactBytes(res.Artifacts, "extract.json")
	if !ok || len(metaBytes) == 0 {
		return source.ExtractResult{}, fmt.Errorf("source: %s extraction missing extract.json", label)
	}
	var meta source.ExtractionMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return source.ExtractResult{}, fmt.Errorf("source: parse %s extract metadata: %w", label, err)
	}
	return source.ExtractResult{Markdown: string(md), Metadata: meta}, nil
}
