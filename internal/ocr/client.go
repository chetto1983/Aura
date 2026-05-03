package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout    = 5 * time.Minute
	errorSnippetLimit = 256               // chars from non-2xx body included in the error message
	responseHardCap   = 256 * 1024 * 1024 // 256 MiB read cap on response body
)

// Config holds Mistral OCR connection settings.
type Config struct {
	APIKey  string
	BaseURL string // e.g. https://api.mistral.ai/v1
	Model   string // e.g. mistral-ocr-latest

	// TableFormat / ExtractHeader / ExtractFooter mirror the env vars
	// MISTRAL_OCR_TABLE_FORMAT / MISTRAL_OCR_EXTRACT_HEADER /
	// MISTRAL_OCR_EXTRACT_FOOTER. They are sent on the wire (verified
	// against Mistral basic_ocr docs).
	TableFormat   string
	ExtractHeader bool
	ExtractFooter bool

	// HTTPClient lets tests inject a fake server. Defaults to an http.Client
	// with Timeout = defaultTimeout when nil.
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client posts PDFs to Mistral Document AI OCR.
//
// The HTTP shape mirrors internal/tools/ollama_web.go (Bearer auth, JSON
// post, status check, capped-snippet errors) so Aura has a single canonical
// way to talk to OpenAI-compatible APIs.
type Client struct {
	apiKey        string
	baseURL       string
	model         string
	tableFormat   string
	extractHeader bool
	extractFooter bool
	http          *http.Client
}

// New builds a Client. BaseURL is normalized: trailing slashes stripped, a
// "/v1" suffix is preserved (Mistral expects /v1/ocr).
func New(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	model := cfg.Model
	if model == "" {
		model = "mistral-ocr-latest"
	}
	return &Client{
		apiKey:        cfg.APIKey,
		baseURL:       strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		model:         model,
		tableFormat:   strings.TrimSpace(cfg.TableFormat),
		extractHeader: cfg.ExtractHeader,
		extractFooter: cfg.ExtractFooter,
		http:          httpClient,
	}
}

// ProcessInput is the per-call payload for OCR.
type ProcessInput struct {
	PDFBytes []byte
	// IncludeImages mirrors MISTRAL_OCR_INCLUDE_IMAGES. When true the response
	// pages carry image objects; Aura ignores them today but the wire flag is
	// honored so future slices can pull them.
	IncludeImages bool
}

// ProcessResult bundles the parsed response with the raw JSON body. Callers
// archive RawJSON as ocr.json (PDR §4) for replay/debug without touching the
// model again.
type ProcessResult struct {
	Response OCRResponse
	RawJSON  []byte
}

// Process posts a PDF as base64 in a data URL and returns the parsed pages.
// Errors are deliberately terse: status code + a 256-char body snippet, never
// the request body or API key. PDR §9 forbids logging raw OCR bytes.
func (c *Client) Process(ctx context.Context, in ProcessInput) (*ProcessResult, error) {
	if len(in.PDFBytes) == 0 {
		return nil, errors.New("ocr: empty pdf bytes")
	}
	if c.baseURL == "" {
		return nil, errors.New("ocr: base url not configured")
	}

	b64 := base64.StdEncoding.EncodeToString(in.PDFBytes)
	body := OCRRequest{
		Model: c.model,
		Document: Document{
			Type:        "document_url",
			DocumentURL: "data:application/pdf;base64," + b64,
		},
		IncludeImageBase64: in.IncludeImages,
		TableFormat:        c.tableFormat,
		ExtractHeader:      c.extractHeader,
		ExtractFooter:      c.extractFooter,
	}
	reqJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ocr: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/ocr", bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("ocr: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ocr: request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseHardCap))
	if err != nil {
		return nil, fmt.Errorf("ocr: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ocr: HTTP %d: %s", resp.StatusCode, snippet(raw))
	}

	var parsed OCRResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ocr: decode response: %w", err)
	}
	return &ProcessResult{Response: parsed, RawJSON: raw}, nil
}

// snippet trims an error response body to a small, stripped slice for safe
// inclusion in error messages.
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) <= errorSnippetLimit {
		return s
	}
	return s[:errorSnippetLimit] + "…"
}
