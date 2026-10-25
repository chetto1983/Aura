package multimodal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// fakeReader stands in for the ChatGPT client: it answers with text or err and records the
// deadline it was given.
type fakeReader struct {
	text     string
	err      error
	deadline time.Time
	calls    int
}

func (r *fakeReader) Describe(ctx context.Context, _ []byte, _, _ string) (string, error) {
	r.calls++
	r.deadline, _ = ctx.Deadline()
	return r.text, r.err
}

func failIfHit(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("an HTTP arm was called while the reader decided")
	}))
	t.Cleanup(srv.Close)
	return srv
}

var errCannotRead = fmt.Errorf("%w: chatgpt: gpt-x does not read images", llm.ErrMandatoryModalityUnavailable)

func TestVisionReaderAnswersAsThePrimaryArm(t *testing.T) {
	primary := failIfHit(t)
	c := NewVisionClient(VisionConfig{
		PrimaryReader:        &fakeReader{text: "ok"},
		PrimaryAcceptsImages: true,
		Model:                "gpt-x",
		PrimaryBaseURL:       primary.URL,
		HTTPClient:           primary.Client(),
	})
	text, model, err := c.Describe(t.Context(), []byte("img"), "image/png", "x")
	if err != nil || text != "ok" || model != "gpt-x" {
		t.Fatalf("Describe = %q, %q, %v; want ok from gpt-x", text, model, err)
	}
}

func TestVisionReaderThatCannotReadFallsToTheSidecar(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"sidecar"}}]}`)
	}))
	defer sidecar.Close()
	c := NewVisionClient(VisionConfig{
		PrimaryReader: &fakeReader{err: errCannotRead},
		Model:         "gpt-x",
		LocalBaseURL:  sidecar.URL,
		LocalModel:    "glm-ocr",
		HTTPClient:    sidecar.Client(),
	})
	text, model, err := c.Describe(t.Context(), []byte("img"), "image/png", "x")
	if err != nil || text != "sidecar" || model != "glm-ocr" {
		t.Fatalf("Describe = %q, %q, %v; want the sidecar's answer", text, model, err)
	}
}

func TestVisionReaderThatCannotReadWithoutSidecarIsNoRoute(t *testing.T) {
	c := NewVisionClient(VisionConfig{PrimaryReader: &fakeReader{err: errCannotRead}, Model: "gpt-x"})
	if _, _, err := c.Describe(t.Context(), []byte("img"), "image/png", "x"); !errors.Is(err, ErrNoVisionRoute) {
		t.Fatalf("err = %v, want ErrNoVisionRoute", err)
	}
}

func TestVisionReaderFailureIsReturnedWithoutTheSidecar(t *testing.T) {
	sidecar := failIfHit(t)
	failure := errors.New("chatgpt: usage_limit_reached")
	c := NewVisionClient(VisionConfig{
		PrimaryReader: &fakeReader{err: failure},
		Model:         "gpt-x",
		LocalBaseURL:  sidecar.URL,
		LocalModel:    "glm-ocr",
		HTTPClient:    sidecar.Client(),
	})
	_, _, err := c.Describe(t.Context(), []byte("img"), "image/png", "x")
	if !errors.Is(err, failure) || errors.Is(err, ErrNoVisionRoute) {
		t.Fatalf("err = %v, want the reader's failure, retried later", err)
	}
}

func TestVisionBoundsTheReaderByTheTimeout(t *testing.T) {
	reader := &fakeReader{text: "ok"}
	c := NewVisionClient(VisionConfig{PrimaryReader: reader, Model: "gpt-x", TimeoutSec: 1})
	start := time.Now()
	if _, _, err := c.Describe(context.Background(), []byte("img"), "image/png", "x"); err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if reader.deadline.IsZero() || reader.deadline.After(start.Add(1100*time.Millisecond)) {
		t.Fatalf("reader deadline = %v, want at most 1 s after %v", reader.deadline, start)
	}
}
