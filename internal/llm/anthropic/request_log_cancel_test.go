package anthropic

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

// streamUntilCanceled sends one request whose context the test controls: cancel is called
// once handler has signalled arrived, and the stream is then drained. It returns what the
// client logged.
func streamUntilCanceled(t *testing.T, ctx context.Context, cancel context.CancelFunc, handler func(w http.ResponseWriter, r *http.Request, arrived chan<- struct{})) string {
	t.Helper()
	arrived := make(chan struct{}, 1)
	client := fixtureClient(t, testConfig(), func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		handler(w, r, arrived)
	})
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	client.logger = func() *slog.Logger { return logger }
	go func() {
		<-arrived
		if cancel != nil {
			cancel()
		}
	}()
	stream, err := client.Stream(ctx, userRequest("hi"))
	if err == nil {
		drainStream(stream)
	} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stream: %v", err)
	}
	return buf.String()
}

func blockBeforeAnyByte(_ http.ResponseWriter, r *http.Request, arrived chan<- struct{}) {
	arrived <- struct{}{}
	<-r.Context().Done()
}

func blockAfterADelta(w http.ResponseWriter, r *http.Request, arrived chan<- struct{}) {
	writeEvents(w, reply(textBlock(0, "partial"))...)
	arrived <- struct{}{}
	<-r.Context().Done()
}

// A cancel is the caller stopping the turn, not a failed request, and the other routes log
// no per-request line at all: whenever it lands, before the stream opens or mid-stream, it
// leaves no line.
func TestACanceledRequestLeavesNoLine(t *testing.T) {
	for name, handler := range map[string]func(http.ResponseWriter, *http.Request, chan<- struct{}){
		"before any byte": blockBeforeAnyByte,
		"mid-stream":      blockAfterADelta,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if line := streamUntilCanceled(t, ctx, cancel, handler); line != "" {
				t.Fatalf("a canceled request logged %q", line)
			}
		})
	}
}

// A deadline is a timeout, not the caller stopping: it still leaves its failure line.
func TestADeadlineStillLeavesAFailureLine(t *testing.T) {
	for name, handler := range map[string]func(http.ResponseWriter, *http.Request, chan<- struct{}){
		"before any byte": blockBeforeAnyByte,
		"mid-stream":      blockAfterADelta,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			line := streamUntilCanceled(t, ctx, nil, handler)
			if !bytes.Contains([]byte(line), []byte(`level=WARN msg="anthropic request failed"`)) || !bytes.Contains([]byte(line), []byte("error_class=timeout")) {
				t.Fatalf("a request past its deadline logged %q, want one timeout failure line", line)
			}
		})
	}
}
