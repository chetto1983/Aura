package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
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
