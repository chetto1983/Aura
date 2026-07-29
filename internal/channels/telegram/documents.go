// Package telegram — this file is the document-conversion sidecar client (UX-04).
// It is tiered against the markitdown /convert endpoint by payload size
// (T-13-08-SidecarDoS):
//
//   - ≤5MB  : SYNChronous — convert inline, return the markdown.
//   - 5-50MB: ASYNChronous — convert on a goroutine the client tracks with a
//     WaitGroup that Stop drains (goleak-clean, Pitfall 5); the result is
//     delivered over the per-request callback passed to Convert.
//   - >50MB : REFUSED — return a user-facing message, NO sidecar call.
//
// The converted markdown becomes a text user message fed to runner.Turn
// (handler, 13-09). A2 caveat: the markitdown /convert request shape is ASSUMED —
// it is isolated to postConvert so a real-image mismatch is a one-line change.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"sync"

	"github.com/chetto1983/aura/internal/multimodal"
)

// Tier size boundaries (T-13-08-SidecarDoS). asyncTierMinBytes is the >sync
// threshold (5MB); refuseTierMinBytes is the >async threshold (50MB).
const (
	asyncTierMinBytes  = 5 << 20  // 5 MiB — above this, convert async
	refuseTierMinBytes = 50 << 20 // 50 MiB — above this, refuse
)

// documentRefuseMessage is the user-facing copy for a >50MB document (Italian —
// the output-language directive).
const documentRefuseMessage = "❌ Documento troppo grande (oltre 50MB). Inviane uno più piccolo."

// ConvertStatus classifies a Convert call's outcome so the handler renders the
// right surface (return the markdown / acknowledge async / show the refuse copy).
type ConvertStatus int

const (
	// ConvertSync — the document was converted inline; Markdown is populated.
	ConvertSync ConvertStatus = iota
	// ConvertAsync — the document was accepted for async conversion; the markdown
	// arrives later over the Convert callback.
	ConvertAsync
	// ConvertRefused — the document exceeded the 50MB ceiling; Message is the
	// user-facing copy, no conversion was attempted.
	ConvertRefused
)

// ConvertResult is the immediate outcome of a Convert call. For ConvertSync it
// carries the Markdown; for ConvertAsync the markdown follows over the Convert callback;
// for ConvertRefused it carries the user-facing Message.
type ConvertResult struct {
	Status   ConvertStatus
	Markdown string
	Message  string
}

type documentAsyncCallback func(fileName, markdown string, err error)

// documentsClient converts documents via the markitdown sidecar with the size
// tiers. The async tier's goroutines are tracked by wg so Stop drains them
// (goleak). Thin OpenAI-compat-style HTTP client (zero Go ML).
type documentsClient struct {
	cfg        MultimodalConfig
	httpClient *http.Client

	wg       sync.WaitGroup
	stopOnce sync.Once
}

// newDocumentsClient builds a tiered markitdown client over the multimodal config.
func newDocumentsClient(cfg MultimodalConfig) *documentsClient {
	return &documentsClient{cfg: cfg, httpClient: multimodal.HTTPClient()}
}

// Convert routes a document to the right size tier. ≤5MB converts synchronously
// and returns the markdown; 5-50MB returns ConvertAsync immediately and converts
// on a tracked goroutine (result over onAsync); >50MB returns ConvertRefused
// with the user-facing message and never touches the sidecar.
func (d *documentsClient) Convert(ctx context.Context, payload []byte, fileName string, onAsync documentAsyncCallback) (ConvertResult, error) {
	switch {
	case len(payload) > refuseTierMinBytes:
		return ConvertResult{Status: ConvertRefused, Message: documentRefuseMessage}, nil

	case len(payload) > asyncTierMinBytes:
		// Detach from the request ctx: the async conversion outlives the inbound
		// handler call. Stop drains the goroutine via wg (goleak-clean).
		d.wg.Go(func() {
			md, err := d.postConvert(context.WithoutCancel(ctx), payload, fileName)
			if onAsync != nil {
				onAsync(fileName, md, err)
			}
		})
		return ConvertResult{Status: ConvertAsync}, nil

	default:
		md, err := d.postConvert(ctx, payload, fileName)
		if err != nil {
			return ConvertResult{}, err
		}
		return ConvertResult{Status: ConvertSync, Markdown: md}, nil
	}
}

// Stop drains any in-flight async conversion goroutines (goleak-clean). It is
// idempotent — a second call (or a call with no async work) is a clean no-op.
func (d *documentsClient) Stop(_ context.Context) {
	d.stopOnce.Do(func() {
		d.wg.Wait()
	})
}

// postConvert builds the multipart /convert body (the document bytes in the
// `file` field) and POSTs it to the markitdown sidecar, decoding the markdown. A
// non-2xx response is a *multimodal.StatusError. (A2: the request shape is ASSUMED
// — isolated here for a one-line change if the real image differs.)
func (d *documentsClient) postConvert(ctx context.Context, payload []byte, fileName string) (string, error) {
	reqCtx, cancel := multimodal.TimeoutContext(ctx, d.cfg.TimeoutSec)
	defer cancel()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		return "", err
	}
	if _, err = fw.Write(payload); err != nil {
		return "", err
	}
	if err = mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, d.cfg.DocumentsBaseURL+"/convert", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return "", &multimodal.StatusError{Endpoint: "markitdown", StatusCode: resp.StatusCode}
	}

	var decoded struct {
		Markdown string `json:"markdown"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("telegram markitdown: decode: %w", err)
	}
	return decoded.Markdown, nil
}
