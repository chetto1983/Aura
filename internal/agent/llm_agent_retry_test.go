package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"syscall"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

// flakyTool fails for its first failFor Execute calls (returning err), then
// succeeds. calls records the total number of Execute invocations so a test can
// assert how many attempts execTool made.
type flakyTool struct {
	mutating bool
	failFor  int
	err      error
	calls    int
}

func (f *flakyTool) Spec() tools.Spec {
	return tools.Spec{Name: "flaky", Summary: "test", Mutating: f.mutating}
}

func (f *flakyTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	f.calls++
	if f.calls <= f.failFor {
		return tools.ToolResult{}, f.err
	}
	return tools.ToolResult{Preview: "ok"}, nil
}

// timeoutErr is a typed net.Error that reports a timeout — the transient class
// execTool retries.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func withFastBackoff(t *testing.T) {
	t.Helper()
	prev := toolRetryBaseDelay
	toolRetryBaseDelay = time.Microsecond
	t.Cleanup(func() { toolRetryBaseDelay = prev })
}

func TestExecTool_RetriesTransientNonMutating(t *testing.T) {
	withFastBackoff(t)
	tool := &flakyTool{failFor: 2, err: timeoutErr{}}
	res, err := (&LlmAgent{}).execTool(context.Background(), tool, false, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if res.Preview != "ok" {
		t.Fatalf("preview = %q, want ok", res.Preview)
	}
	if tool.calls != 3 {
		t.Fatalf("calls = %d, want 3 (2 transient failures + 1 success)", tool.calls)
	}
}

func TestExecTool_DoesNotRetryMutating(t *testing.T) {
	withFastBackoff(t)
	tool := &flakyTool{mutating: true, failFor: 1, err: timeoutErr{}}
	_, err := (&LlmAgent{}).execTool(context.Background(), tool, true, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("a mutating tool must not be retried — expected the error to surface")
	}
	if tool.calls != 1 {
		t.Fatalf("calls = %d, want 1 (mutating side effects stay at-most-once)", tool.calls)
	}
}

func TestExecTool_DoesNotRetryPermanent(t *testing.T) {
	withFastBackoff(t)
	tool := &flakyTool{failFor: 9, err: errors.New("validation: bad arg")}
	_, err := (&LlmAgent{}).execTool(context.Background(), tool, false, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected the permanent error to surface")
	}
	if tool.calls != 1 {
		t.Fatalf("calls = %d, want 1 (a non-transient error is not retried)", tool.calls)
	}
}

func TestExecTool_StopsAtBudget(t *testing.T) {
	withFastBackoff(t)
	tool := &flakyTool{failFor: 99, err: timeoutErr{}}
	_, err := (&LlmAgent{}).execTool(context.Background(), tool, false, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected the error to surface after the retry budget is spent")
	}
	if want := maxToolRetries + 1; tool.calls != want {
		t.Fatalf("calls = %d, want %d (initial + maxToolRetries)", tool.calls, want)
	}
}

func TestExecTool_StopsOnCancelledParentCtx(t *testing.T) {
	withFastBackoff(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tool := &flakyTool{failFor: 99, err: timeoutErr{}}
	_, err := (&LlmAgent{}).execTool(ctx, tool, false, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected the error to surface")
	}
	if tool.calls != 1 {
		t.Fatalf("calls = %d, want 1 (a dead parent ctx stops retrying immediately)", tool.calls)
	}
}

func TestIsTransientToolErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"net-timeout", timeoutErr{}, true},
		{"deadline", context.DeadlineExceeded, true},
		{"wrapped-deadline", errors.Join(errors.New("fetch"), context.DeadlineExceeded), true},
		{"permanent", errors.New("validation"), false},
		{"cancelled", context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientToolErr(tc.err); got != tc.want {
				t.Fatalf("isTransientToolErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestRetryableStreamOpenError_GoldenParity is the STRICT golden table for the
// stream-open retry classifier (Pitfall 2 / T-32-06-STR). It pins the exact bool
// output for every input class BEFORE the isTransientNetworkErr extraction so the
// refactor can be proven output-identical. The context.*->false guard MUST stay
// FIRST: context.DeadlineExceeded is itself a net.Error{Timeout}, so without the
// leading guard the shared typed-network subset would flip it to true (the exact
// stream-path regression Pitfall 2 warns about).
func TestRetryableStreamOpenError_GoldenParity(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context.Canceled", context.Canceled, false},
		{"context.DeadlineExceeded", context.DeadlineExceeded, false},
		{"http 429", &openai_compat.HTTPError{StatusCode: 429}, true},
		{"http 500", &openai_compat.HTTPError{StatusCode: 500}, true},
		{"http 503", &openai_compat.HTTPError{StatusCode: 503}, true},
		{"http 400", &openai_compat.HTTPError{StatusCode: 400}, false},
		{"net timeout", timeoutErr{}, true},
		{"url.Error timeout", &url.Error{Op: "Get", URL: "http://x", Err: timeoutErr{}}, true},
		{"url.Error retryable text", &url.Error{Op: "Get", URL: "http://x", Err: errors.New("connection reset")}, true},
		{"url.Error non-retryable text", &url.Error{Op: "Get", URL: "http://x", Err: errors.New("bad certificate")}, false},
		{"ErrStreamIdleTimeout", openai_compat.ErrStreamIdleTimeout, true},
		{"io.EOF", io.EOF, true},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, true},
		{"syscall.ECONNRESET", syscall.ECONNRESET, true},
		{"syscall.ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"syscall.ETIMEDOUT", syscall.ETIMEDOUT, true},
		{"wsarecv text marker", errors.New("wsarecv: connection reset by peer"), true},
		{"bare eof text", errors.New("eof"), true},
		{"wrapped sentinel marker-free message", opaqueWrapErr{msg: "upstream stream interrupted", inner: syscall.ECONNRESET}, true},
		{"plain non-network error", errors.New("invalid api key configuration"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryableStreamOpenError(tc.err); got != tc.want {
				t.Fatalf("retryableStreamOpenError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsTransientNetworkErr characterizes the shared typed-network subset extracted
// in QUAL-03. It matches only typed sentinels (a net.Error that timed out + the
// io/syscall connection sentinels) and excludes context.*, HTTP status, url.Error,
// ErrStreamIdleTimeout, and the substring fallback — those stay caller-specific.
func TestIsTransientNetworkErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"net timeout", timeoutErr{}, true},
		{"io.EOF", io.EOF, true},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, true},
		{"syscall.ECONNRESET", syscall.ECONNRESET, true},
		{"syscall.ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"syscall.ETIMEDOUT", syscall.ETIMEDOUT, true},
		{"wrapped sentinel marker-free message", opaqueWrapErr{msg: "stream interrupted", inner: syscall.ECONNRESET}, true},
		{"plain non-network error", errors.New("validation: bad arg"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientNetworkErr(tc.err); got != tc.want {
				t.Fatalf("isTransientNetworkErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsTransientToolErr_WidenedNetworkSubset documents the QUAL-03 INTENTIONAL
// widening of the tool-retry classifier. The tool path now retries the full typed
// network subset (ECONNRESET/ECONNREFUSED/ETIMEDOUT, io.EOF/io.ErrUnexpectedEOF),
// which the original timeout-only rule treated as permanent (was FALSE, see the
// wasOld column). context.DeadlineExceeded and net-timeout stay retryable (kept);
// validation/cancelled stay permanent. The stream path does NOT share this widening.
func TestIsTransientToolErr_WidenedNetworkSubset(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		want   bool
		wasOld bool // result under the pre-QUAL-03 timeout-only classifier
	}{
		{"deadline kept", context.DeadlineExceeded, true, true},
		{"net timeout kept", timeoutErr{}, true, true},
		{"ECONNRESET widened", syscall.ECONNRESET, true, false},
		{"ECONNREFUSED widened", syscall.ECONNREFUSED, true, false},
		{"ETIMEDOUT widened", syscall.ETIMEDOUT, true, false},
		{"io.ErrUnexpectedEOF widened", io.ErrUnexpectedEOF, true, false},
		{"io.EOF widened", io.EOF, true, false},
		{"validation permanent", errors.New("validation: bad arg"), false, false},
		{"cancelled permanent", context.Canceled, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientToolErr(tc.err); got != tc.want {
				t.Fatalf("isTransientToolErr(%v) = %v, want %v (pre-widening was %v)", tc.err, got, tc.want, tc.wasOld)
			}
		})
	}
}
