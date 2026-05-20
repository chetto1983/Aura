package pockettts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"
)

// ErrEmptyText is returned when Synthesize is called with an empty text argument.
var ErrEmptyText = errors.New("pockettts: empty text")

// SynthesizeResult holds the output of a single TTS synthesis request.
type SynthesizeResult struct {
	Audio     []byte
	MimeType  string
	ElapsedMs int
	Voice     string
	Language  string
}

// Client sends text to a pocket-tts sidecar /tts endpoint and returns audio bytes.
// Thread-safe.
type Client struct {
	baseURL         string
	httpc           *http.Client
	logger          *slog.Logger
	defaultVoice    string
	defaultLanguage string
}

// New creates a Client that POSTs to baseURL/tts. timeout=0 disables the
// client-level deadline (ctx cancellation still applies).
// Defaults: voice="giovanni", language="italian_24l".
func New(baseURL string, timeout time.Duration, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL:         baseURL,
		httpc:           &http.Client{Timeout: timeout},
		logger:          logger,
		defaultVoice:    "giovanni",
		defaultLanguage: "italian_24l",
	}
}

// Synthesize sends text to <baseURL>/tts using multipart/form-data and returns
// the audio bytes. If voice is empty, the client's defaultVoice is used.
// Caps the response body at 8 MB (~170 seconds of 24 kHz mono audio).
//
// Error on non-200: status code + body excerpt ≤200 chars (no full upstream body leaked).
// Empty text: returns ErrEmptyText before any HTTP call.
// ctx cancellation propagates to the in-flight request.
func (c *Client) Synthesize(ctx context.Context, text string, voice string) (SynthesizeResult, error) {
	if text == "" {
		return SynthesizeResult{}, ErrEmptyText
	}
	if voice == "" {
		voice = c.defaultVoice
	}

	t0 := time.Now()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("text", text); err != nil {
		return SynthesizeResult{}, fmt.Errorf("pockettts: write field text: %w", err)
	}
	if err := mw.WriteField("voice_url", voice); err != nil {
		return SynthesizeResult{}, fmt.Errorf("pockettts: write field voice_url: %w", err)
	}
	if err := mw.Close(); err != nil {
		return SynthesizeResult{}, fmt.Errorf("pockettts: close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tts", &buf)
	if err != nil {
		return SynthesizeResult{}, fmt.Errorf("pockettts: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpc.Do(req)
	if err != nil {
		return SynthesizeResult{}, fmt.Errorf("pockettts: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return SynthesizeResult{}, fmt.Errorf("pockettts: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		excerpt := string(body)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200]
		}
		return SynthesizeResult{}, fmt.Errorf("pockettts: server %d: %s", resp.StatusCode, excerpt)
	}

	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "audio/wav"
	}

	return SynthesizeResult{
		Audio:     body,
		MimeType:  mimeType,
		ElapsedMs: int(time.Since(t0).Milliseconds()),
		Voice:     voice,
		Language:  c.defaultLanguage,
	}, nil
}
