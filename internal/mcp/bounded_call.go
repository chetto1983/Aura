package mcp

import (
	"context"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bounded_call.go holds the one thing the SDK does not do for its caller: end when
// the caller's context does.
//
// Measured 2026-08-16 against go-sdk v1.7.0: OpenSDKSession with a 1s deadline against
// an unresponsive HTTP endpoint returned after 7.0s. The client sent the three requests
// of the dual-era fallback ladder — `server/discover`, legacy `initialize`, then the
// deprecated HTTP+SSE probe, the sequence the 2026-07-28 spec's Versioning §"Backward
// Compatibility" matrix prescribes — and let each run to completion, observing the
// deadline only BETWEEN them. The error it returned was always correct; it just arrived
// seven times later than asked for. The same holds for a paginated tools/list against a
// server that completes its handshake and then stops answering.
//
// D-06/MCPH-04 requires a hung MCP server to cost its caller the configured timeout and
// no more: `aura doctor`, the governance board and mount all dial on a bounded budget,
// and one unresponsive server must never spend everyone else's.
//
// There is no library knob for this. StreamableClientTransport exposes only Endpoint,
// HTTPClient, MaxRetries, DisableStandaloneSSE and OAuthHandler; ClientOptions has no
// call or connect timeout — checked against v1.7.0 and against upstream HEAD (64e454e),
// where both structs are unchanged. http.Client.Timeout is not a substitute: it also
// bounds the response body read, so it would sever the standalone SSE stream the
// tool-list-changed subscription rides on for the whole session.

// abandonedCalls tracks goroutines whose caller stopped waiting.
//
// These are not leaks — each ends when the SDK call it is blocked on ends — but they
// outlive the call that started them, which is exactly what the goroutine-leak checkers
// in this package and in internal/agent/mcptools are built to catch. Tests drain them
// with WaitForAbandonedCalls rather than whitelisting a goroutine in goleak, which would
// hide a real leak later for the sake of a known-benign one now.
var abandonedCalls sync.WaitGroup

// WaitForAbandonedCalls blocks until every abandoned call has finished. It exists for
// tests; production never waits for work it deliberately walked away from.
func WaitForAbandonedCalls() { abandonedCalls.Wait() }

// closeSession is the release func for a session that connects after its caller left.
func closeSession(cs *sdkmcp.ClientSession) {
	if cs != nil {
		_ = cs.Close()
	}
}

// ReleaseSession closes cs without making the caller wait for it.
//
// Closing a session whose peer has stopped answering blocks on that peer — a
// CommandTransport's Close waits for the child process to exit, and a child that hung
// during tools/list is exactly the child that will not. A mount that has already given
// up on its deadline must not then pay an unbounded close, so the close is tracked like
// any other abandoned call and drained by WaitForAbandonedCalls in tests.
func ReleaseSession(cs *sdkmcp.ClientSession) {
	if cs == nil {
		return
	}
	abandonedCalls.Go(func() { _ = cs.Close() })
}

// BoundedCall runs call on its own goroutine and returns as soon as ctx is done,
// whether or not call has finished.
//
// release is invoked on a result that arrives after the caller has left, so an abandoned
// call cannot strand a live resource (a session that connects late is closed, not
// dropped). Pass nil when the result owns nothing.
//
// The abandoned goroutine lives as long as the SDK call does. That is the honest cost of
// bounding a library that does not bound itself, and it is bounded in turn by whatever
// the underlying transport eventually does — a dead TCP peer, a killed child process.
func BoundedCall[T any](ctx context.Context, call func(context.Context) (T, error), release func(T)) (T, error) {
	type outcome struct {
		value T
		err   error
	}
	// Buffered so the single send never blocks, whether or not anyone is left to receive
	// it. Exactly one value is sent and exactly one receive happens — the caller's below,
	// or the release goroutine's after the caller has walked away.
	done := make(chan outcome, 1)

	abandonedCalls.Go(func() {
		value, err := call(ctx)
		done <- outcome{value: value, err: err}
	})

	select {
	case result := <-done:
		return result.value, result.err
	case <-ctx.Done():
		abandonedCalls.Go(func() {
			late := <-done
			if late.err == nil && release != nil {
				release(late.value)
			}
		})
		var zero T
		return zero, ctx.Err()
	}
}
