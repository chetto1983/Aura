package mcp

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/chetto1983/aura/internal/mcpoauth"
)

// fakeSession stands in for a live MCP session. The registry only ever closes it.
type fakeSession struct{ closed bool }

func (f *fakeSession) Close() error { f.closed = true; return nil }

// openerRunningTheFlow imitates what the SDK does inside a mount: it calls the fetcher
// with a consent URL and waits for the code. Everything this file asserts on — replay,
// state invalidation, owner isolation, the callback handoff — happens before any of it
// would reach a provider, which is why no provider is needed.
func openerRunningTheFlow(authURL string, fetched chan<- error) SessionOpener {
	return func(ctx context.Context, _ string, _ ManagedServer, opts SessionOptions) (io.Closer, error) {
		_, err := opts.OAuth.Fetcher(ctx, &auth.AuthorizationArgs{URL: authURL})
		if fetched != nil {
			fetched <- err
		}
		if err != nil {
			return nil, err
		}
		return &fakeSession{}, nil
	}
}

func newTestFlows(t *testing.T, opener SessionOpener) *Flows {
	t.Helper()
	flows, err := NewFlows(&fakeStore{loadErr: mcpoauth.ErrNoGrant}, opener, nil)
	if err != nil {
		t.Fatalf("NewFlows: %v", err)
	}
	// Without this every abandoned flow leaves its fetcher parked until the TTL, which
	// goleak reports — correctly, because production has the same goroutine.
	t.Cleanup(flows.Close)
	return flows
}

func waitForStatus(t *testing.T, flows *Flows, owner, id, want string) Flow {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := flows.Status(owner, id)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if got.Status == want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %q (%s), want %q", got.Status, got.Error, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

const testAuthURL = "https://slack.com/oauth/v2/authorize?state=st4te&code_challenge=abc"

// The whole reason this registry exists: the consent URL comes back on one request and
// the code arrives on a completely different one, minutes later.
func TestFlowSpansTwoRequests(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow(testAuthURL, nil))

	started, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.Status != FlowAuthorizationRequired {
		t.Fatalf("status = %q (%s), want the consent URL published", started.Status, started.Error)
	}
	if started.AuthorizationURL != testAuthURL {
		t.Fatalf("authorization url = %q", started.AuthorizationURL)
	}

	if _, err := flows.Complete("st4te", "the-code", ""); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	done := waitForStatus(t, flows, "id-1", started.ID, FlowApproved)
	// An approved flow must stop advertising a consent URL: rendering one after the
	// fact invites a second authorization that would replace the grant just made.
	if done.AuthorizationURL != "" {
		t.Fatalf("an approved flow still advertises %q", done.AuthorizationURL)
	}
}

// A cockpit reload must not start a second authorization. LibreChat replays the pending
// flow for the same reason: two live flows mean two dynamic client registrations and two
// consent URLs of which only one can work.
func TestReloadReplaysTheSameFlow(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow(testAuthURL, nil))

	first, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	second, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if second.ID != first.ID || second.AuthorizationURL != first.AuthorizationURL {
		t.Fatalf("a reload started a second flow: %+v vs %+v", second, first)
	}
}

// Two identities authorizing the same server must not collide: the second must get its
// own flow, not a replay of somebody else's consent URL.
func TestTwoIdentitiesGetTheirOwnFlow(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow(testAuthURL, nil))

	mine, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	theirs, err := flows.Start(context.Background(), "id-2", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if theirs.ID == mine.ID {
		t.Fatal("two identities were handed the same authorization flow")
	}
}

// A flow id is a handle. One identity polling another's would leak which servers somebody
// else is connecting and when.
func TestStatusIsScopedToItsOwner(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow(testAuthURL, nil))

	mine, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := flows.Status("id-2", mine.ID); !errors.Is(err, ErrNoSuchFlow) {
		t.Fatalf("err = %v, want ErrNoSuchFlow for another identity", err)
	}
}

// A redirect that arrives twice — a double click, a browser prefetch, a refresh of the
// callback tab — must be delivered once. The second is not an error to hide but it must
// not reach the flow.
func TestARedirectIsDeliveredOnlyOnce(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow(testAuthURL, nil))
	if _, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, err := flows.Complete("st4te", "code", ""); err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if _, err := flows.Complete("st4te", "code", ""); !errors.Is(err, ErrFlowNotWaiting) {
		t.Fatalf("err = %v, want ErrFlowNotWaiting on the replayed redirect", err)
	}
}

func TestUnknownStateIsRefused(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow(testAuthURL, nil))
	if _, err := flows.Complete("never-issued", "code", ""); !errors.Is(err, ErrFlowNotWaiting) {
		t.Fatalf("err = %v, want ErrFlowNotWaiting", err)
	}
}

// The provider refused. Without this the human watches a spinner until the TTL expires
// and never learns why.
func TestAProviderRefusalEndsTheFlow(t *testing.T) {
	t.Parallel()
	fetched := make(chan error, 1)
	flows := newTestFlows(t, openerRunningTheFlow(testAuthURL, fetched))

	started, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := flows.Fail("st4te", errors.New("access_denied: the user said no")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	got := waitForStatus(t, flows, "id-1", started.ID, FlowError)
	if !strings.Contains(got.Error, "access_denied") {
		t.Fatalf("error = %q, want the provider's own words", got.Error)
	}
}

// An authorization URL with no state cannot be matched to any redirect, so publishing a
// button that leads nowhere is worse than failing here.
func TestAnAuthorizationURLWithoutStateFailsTheFlow(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow("https://slack.com/oauth/v2/authorize", nil))

	started, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.Status != FlowError {
		t.Fatalf("status = %q, want the flow to fail before publishing a URL", started.Status)
	}
	if started.AuthorizationURL != "" {
		t.Fatal("a URL no redirect can match was published anyway")
	}
}

// An abandoned attempt's state must die with it. LibreChat deletes the mapping on replace
// precisely so an old URL cannot still complete — and completing it would overwrite the
// grant the human is making right now.
func TestReplacingAFlowInvalidatesTheOldRedirect(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow("https://slack.com/oauth/v2/authorize", nil))

	// The first attempt fails (no state in its URL), so it is not replayable and the
	// next Start replaces it.
	if _, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	flows.open = openerRunningTheFlow(testAuthURL, nil)
	replacement, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if replacement.Status != FlowAuthorizationRequired {
		t.Fatalf("replacement status = %q (%s)", replacement.Status, replacement.Error)
	}
	// And the replacement is reachable by ITS state, which is the live one.
	if _, err := flows.Complete("st4te", "code", ""); err != nil {
		t.Fatalf("the replacement's own redirect was refused: %v", err)
	}
}

func TestAnExpiredFlowIsForgotten(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow(testAuthURL, nil))
	started, err := flows.Start(context.Background(), "id-1", "slack", remoteServer(), "https://aura.example/cb")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	flows.now = func() time.Time { return time.Now().Add(2 * flowTTL) }

	if _, err := flows.Status("id-1", started.ID); !errors.Is(err, ErrNoSuchFlow) {
		t.Fatalf("err = %v, want an expired flow to be gone", err)
	}
	if _, err := flows.Complete("st4te", "code", ""); !errors.Is(err, ErrFlowNotWaiting) {
		t.Fatalf("err = %v, want an expired flow's redirect refused", err)
	}
}

// Starting a flow against a server that takes no authorization would open a browser for
// nothing and leave a pending row in the cockpit forever.
func TestStartRefusesAServerWithNoAuthorizationFlow(t *testing.T) {
	t.Parallel()
	flows := newTestFlows(t, openerRunningTheFlow(testAuthURL, nil))
	stdio := ManagedServer{Command: "notes-mcp"}

	if _, err := flows.Start(context.Background(), "id-1", "notes", stdio, "https://aura.example/cb"); err == nil {
		t.Fatal("a stdio server was given an authorization flow")
	}
}

func TestFlowsRefuseToBeBuiltWithoutTheirDependencies(t *testing.T) {
	t.Parallel()
	if _, err := NewFlows(nil, openerRunningTheFlow(testAuthURL, nil), nil); err == nil {
		t.Fatal("flows were built with no grant store, so nothing would be persisted")
	}
	if _, err := NewFlows(&fakeStore{}, nil, nil); err == nil {
		t.Fatal("flows were built with no session opener")
	}
}
