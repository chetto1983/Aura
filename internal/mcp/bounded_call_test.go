package mcp

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bounded_call_test.go closes a coverage gap the profile named directly:
// closeSession and ReleaseSession (the release funcs BoundedCall's callers pass in
// sdkclient.go) had no test anywhere — TestOpenSDKSessionHTTPHonoursCallerDeadline
// exercises BoundedCall's abandoned-goroutine path, but the httptest fixture there
// answers the late connect with an invalid empty body, so the release func is never
// actually reached. These tests call both functions directly against a real
// in-memory session.

func newTestSession(t *testing.T) *closingClientSession {
	t.Helper()
	server := newSDKFixtureServer(1, 0)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "aura-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	return &closingClientSession{session: session}
}

// closingClientSession tracks whether the session's Wait() has observed a close,
// so a test can assert closeSession/ReleaseSession actually tore the session down
// rather than merely returning without panicking.
type closingClientSession struct {
	session *sdkmcp.ClientSession
	waited  chan error
}

func (c *closingClientSession) watch(t *testing.T) {
	t.Helper()
	c.waited = make(chan error, 1)
	go func() { c.waited <- c.session.Wait() }()
}

func TestCloseSessionClosesANonNilSession(t *testing.T) {
	cs := newTestSession(t)
	cs.watch(t)

	closeSession(cs.session)

	select {
	case <-cs.waited:
		// The session's own Wait() unblocked -- closeSession genuinely closed it,
		// not merely returned.
	case <-timeoutChan(t):
		t.Fatal("closeSession did not actually close the session (Wait() never unblocked)")
	}
}

func TestCloseSessionNilIsANoop(t *testing.T) {
	// Must not panic. This is the guard BoundedCall's abandoned-goroutine path relies
	// on when the call itself errored (release is never invoked in that case, but a
	// defensive nil-check protects any other caller too).
	closeSession(nil)
}

func TestReleaseSessionClosesWithoutBlockingTheCaller(t *testing.T) {
	cs := newTestSession(t)
	cs.watch(t)

	ReleaseSession(cs.session)
	// ReleaseSession must not make the caller wait for the close to land -- the
	// tracked background goroutine does the work, drained by WaitForAbandonedCalls.
	WaitForAbandonedCalls()

	select {
	case <-cs.waited:
	case <-timeoutChan(t):
		t.Fatal("ReleaseSession did not close the session even after WaitForAbandonedCalls drained")
	}
}

func TestReleaseSessionNilIsANoop(t *testing.T) {
	ReleaseSession(nil)
	WaitForAbandonedCalls()
}

// timeoutChan is a small helper so the two Wait()-race assertions above have a
// bounded fallback instead of hanging the whole test binary on a regression.
func timeoutChan(t *testing.T) <-chan time.Time {
	t.Helper()
	return time.After(5 * time.Second)
}
