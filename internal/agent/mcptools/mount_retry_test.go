package mcptools

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
	"go.uber.org/goleak"
)

// fastPolicy keeps the backoff sub-millisecond so the retry tests stay fast and deterministic while
// still exercising the real sleepContext timer path.
func fastPolicy(attempts int) MountRetryPolicy {
	return MountRetryPolicy{Attempts: attempts, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}
}

func transportErr() error { return fmt.Errorf("%w: simulated dial failure", mcp.ErrTransport) }

// closerStub is a no-op closer the success path returns; identity is asserted by calling it.
func closerStub() error { return nil }

// TestMountWithRetry_RetriesTransientThenSucceeds: a transport failure on the first two attempts is
// retried, and the third (successful) attempt's closer + names are returned — the boot-race fix.
func TestMountWithRetry_RetriesTransientThenSucceeds(t *testing.T) {
	defer goleak.VerifyNone(t)
	var calls int
	closer, names, err := MountWithRetry(context.Background(), "memory", fastPolicy(5),
		func(context.Context) (func() error, []string, error) {
			calls++
			if calls < 3 {
				return nil, nil, transportErr()
			}
			return closerStub, []string{"memory_search", "memory_add_fact"}, nil
		})
	if err != nil {
		t.Fatalf("want success after retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("want 3 mount attempts (2 transient + 1 ok), got %d", calls)
	}
	if len(names) != 2 || closer == nil {
		t.Fatalf("success must pass through names+closer, got names=%v closerNil=%v", names, closer == nil)
	}
}

// TestMountWithRetry_PermanentErrorNoRetry: a NON-transport error (config/collision/auth) returns
// immediately on the first attempt — a retry could never fix it, so fail fast.
func TestMountWithRetry_PermanentErrorNoRetry(t *testing.T) {
	defer goleak.VerifyNone(t)
	var calls int
	permanent := errors.New("duplicate tool name collision")
	_, _, err := MountWithRetry(context.Background(), "x", fastPolicy(5),
		func(context.Context) (func() error, []string, error) {
			calls++
			return nil, nil, permanent
		})
	if !errors.Is(err, permanent) {
		t.Fatalf("want the permanent error returned verbatim, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("a permanent error must NOT retry; got %d attempts", calls)
	}
}

// TestMountWithRetry_BudgetExhausted: a server that stays transport-down is retried up to the budget
// and then the last transport error is returned (boot continues fail-soft).
func TestMountWithRetry_BudgetExhausted(t *testing.T) {
	defer goleak.VerifyNone(t)
	var calls int
	_, _, err := MountWithRetry(context.Background(), "dead", fastPolicy(3),
		func(context.Context) (func() error, []string, error) {
			calls++
			return nil, nil, transportErr()
		})
	if !mcp.IsTransportError(err) {
		t.Fatalf("want the transport error after exhaustion, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("want exactly Attempts(3) tries, got %d", calls)
	}
}

// TestMountWithRetry_SingleAttemptDisablesRetry: Attempts<=1 restores the original single-shot
// behavior (no retry), so an operator can opt out via AURA_MCP_MOUNT_RETRY_ATTEMPTS=1.
func TestMountWithRetry_SingleAttemptDisablesRetry(t *testing.T) {
	defer goleak.VerifyNone(t)
	for _, attempts := range []int{1, 0, -3} {
		var calls int
		_, _, err := MountWithRetry(context.Background(), "x", MountRetryPolicy{Attempts: attempts},
			func(context.Context) (func() error, []string, error) {
				calls++
				return nil, nil, transportErr()
			})
		if !mcp.IsTransportError(err) || calls != 1 {
			t.Fatalf("Attempts=%d: want exactly 1 try, got %d (err=%v)", attempts, calls, err)
		}
	}
}

// TestMountWithRetry_ContextCancelledDuringBackoff: a cancelled context short-circuits the backoff
// and returns ctx.Err() promptly instead of burning the whole budget.
func TestMountWithRetry_ContextCancelledDuringBackoff(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	_, _, err := MountWithRetry(ctx, "memory",
		MountRetryPolicy{Attempts: 100, BaseDelay: time.Hour, MaxDelay: time.Hour},
		func(context.Context) (func() error, []string, error) {
			calls++
			cancel() // cancel after the first failed attempt; the long backoff must abort
			return nil, nil, transportErr()
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("cancel must abort before a second attempt, got %d", calls)
	}
}

// TestMountRetryDelay_CappedExponential: backoff doubles per attempt and clamps to MaxDelay; a
// zero-value policy falls back to sane non-zero bounds (never a busy-loop).
func TestMountRetryDelay_CappedExponential(t *testing.T) {
	p := MountRetryPolicy{BaseDelay: time.Second, MaxDelay: 5 * time.Second}
	got := []time.Duration{
		mountRetryDelay(0, p), mountRetryDelay(1, p), mountRetryDelay(2, p),
		mountRetryDelay(3, p), mountRetryDelay(10, p),
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("delay[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if d := mountRetryDelay(0, MountRetryPolicy{}); d <= 0 {
		t.Errorf("zero policy must back off with a positive default, got %v", d)
	}
}

// TestMountWithRetry_RealManagedTransportExhausts wires the REAL MountManagedServer against a dead
// loopback URL: the connection-refused is classified as a transport error and retried to exhaustion,
// proving the end-to-end classification (not just the stub path).
func TestMountWithRetry_RealManagedTransportExhausts(t *testing.T) {
	defer goleak.VerifyNone(t)
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: "http://127.0.0.1:0/mcp"}
	closer, names, err := MountWithRetry(context.Background(), "memory", fastPolicy(2),
		func(c context.Context) (func() error, []string, error) {
			return MountManagedServer(c, c, reg, "memory", server)
		})
	if err == nil {
		t.Fatal("a dead URL must fail to mount")
	}
	if !mcp.IsTransportError(err) {
		t.Fatalf("a refused dial must be a transport error, got %v", err)
	}
	if closer != nil || names != nil {
		t.Fatalf("a failed mount returns no closer/names, got closerNil=%v names=%v", closer == nil, names)
	}
}
