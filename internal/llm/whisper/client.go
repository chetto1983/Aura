package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"
)

// TranscribeResult holds the output of a single audio transcription request.
// DurationS is set by the caller from the audio source duration (e.g.
// tele.Voice.Duration); ElapsedMs is measured inside Transcribe.
type TranscribeResult struct {
	Text      string
	DurationS float64
	ElapsedMs int
}

// Client sends audio to a whisper-server /inference endpoint and returns
// the transcript text. Thread-safe.
type Client struct {
	baseURL string
	httpc   *http.Client
	logger  *slog.Logger
}

// New creates a Client that posts audio to baseURL/inference. timeout=0
// disables the client-level deadline (ctx cancellation still applies).
func New(baseURL string, timeout time.Duration, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL: baseURL,
		httpc:   &http.Client{Timeout: timeout},
		logger:  logger,
	}
}

// Transcribe posts audioBytes to <baseURL>/inference using multipart/form-data.
// Returns TranscribeResult{Text, DurationS=0, ElapsedMs}. DurationS is always
// 0 from this method — callers fill it from their audio-source duration field
// (e.g. tele.Voice.Duration). ElapsedMs measures the full HTTP round-trip.
//
// Error on non-200: status code + body excerpt ≤200 chars (no full upstream body leaked).
func (c *Client) Transcribe(ctx context.Context, audioBytes []byte, mimeType string, language string) (TranscribeResult, error) {
	t0 := time.Now()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// File part — whisper-server uses the filename extension to detect codec.
	filename := filenameFromMimeType(mimeType)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", mimeType)
	fw, err := mw.CreatePart(h)
	if err != nil {
		return TranscribeResult{}, fmt.Errorf("whisper: create file part: %w", err)
	}
	if _, err := fw.Write(audioBytes); err != nil {
		return TranscribeResult{}, fmt.Errorf("whisper: write audio bytes: %w", err)
	}

	// Required form fields for whisper-server /inference.
	for _, f := range []struct{ k, v string }{
		{"language", language},
		{"temperature", "0.0"},
		{"response_format", "json"},
	} {
		if err := mw.WriteField(f.k, f.v); err != nil {
			return TranscribeResult{}, fmt.Errorf("whisper: write field %q: %w", f.k, err)
		}
	}
	if err := mw.Close(); err != nil {
		return TranscribeResult{}, fmt.Errorf("whisper: close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/inference", &buf)
	if err != nil {
		return TranscribeResult{}, fmt.Errorf("whisper: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpc.Do(req)
	if err != nil {
		return TranscribeResult{}, fmt.Errorf("whisper: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return TranscribeResult{}, fmt.Errorf("whisper: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		excerpt := string(body)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200]
		}
		return TranscribeResult{}, fmt.Errorf("whisper: server %d: %s", resp.StatusCode, excerpt)
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return TranscribeResult{}, fmt.Errorf("whisper: parse response: %w", err)
	}

	return TranscribeResult{
		Text:      out.Text,
		ElapsedMs: int(time.Since(t0).Milliseconds()),
	}, nil
}

// filenameFromMimeType maps a MIME type to a synthetic filename for the
// multipart file field. whisper-server uses the extension for codec detection.
func filenameFromMimeType(mimeType string) string {
	switch mimeType {
	case "audio/ogg", "audio/ogg; codecs=opus":
		return "voice.ogg"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return "audio.wav"
	case "audio/mpeg", "audio/mp3":
		return "audio.mp3"
	default:
		return "audio.ogg"
	}
}
