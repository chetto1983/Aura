package mcptools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

// scriptedOpen is the redial seam driven directly against real in-memory SDK
// sessions: each open() call either fails with errs[i] (if scripted) or dials a
// brand-new server/client pair over a fresh net.Pipe — a genuine "restarted
// peer", not a hand-rolled double. No hand-written mcptools test double exists
// anywhere in this package (D-103).
type scriptedOpen struct {
	t     *testing.T
	build func() *sdkmcp.Server

	mu    sync.Mutex
	dials int
	errs  []error
}

func (f *scriptedOpen) open(_, handshakeCtx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
	f.mu.Lock()
	i := f.dials
	f.dials++
	var scriptedErr error
	if i < len(f.errs) {
		scriptedErr = f.errs[i]
	}
	f.mu.Unlock()
	if scriptedErr != nil {
		return nil, scriptedErr
	}

	server := f.build()
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		return nil, err
	}
	f.t.Cleanup(func() { _ = serverSession.Close() })
	return connectClient(handshakeCtx, clientTransport, o)
}

func (f *scriptedOpen) dialCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials
}

func readOnlyLookupServer() *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(mustTool("lookup", "Look something up.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}), trivialToolHandler)
	return server
}

// attachedMountedServer builds a MountedServer with an initial session over a
// fixture built by build(), wired for redial through a scriptedOpen. The initial
// connect deliberately bypasses the fixture's dial counter/error script — those
// track REDIAL attempts only, so a test asserting "exactly N redial dials" isn't
// off by one from the initial connect.
func attachedMountedServer(t *testing.T, build func() *sdkmcp.Server, errs ...error) (*MountedServer, *scriptedOpen) {
	t.Helper()
	fixture := &scriptedOpen{t: t, build: build, errs: errs}
	srv := NewMountedServer("fixture", fixture.open)

	server := build()
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("initial server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := connectClient(ctx, clientTransport, mcp.SessionOptions{ToolListChanged: srv.onToolListChanged})
	if err != nil {
		t.Fatalf("initial client.Connect: %v", err)
	}
	srv.Attach(session)
	return srv, fixture
}

func TestMountedServer_CallToolText_Success(t *testing.T) {
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	text, err := srv.CallToolText(context.Background(), "sandbox_exec", map[string]any{"container_id": "abc"})
	if err != nil {
		t.Fatalf("CallToolText: %v", err)
	}
	if !strings.HasPrefix(text, "sandbox_exec:") {
		t.Fatalf("text = %q, want routed to sandbox_exec", text)
	}
}

// TestMountedServer_CallToolText_ToolErrorIsTyped covers the domain-outcome
// chain's single call site: a tool reporting isError=true surfaces as a
// *mcp.ToolCallError, matched by errors.As exactly as llm_agent_retry.go does.
func TestMountedServer_CallToolText_ToolErrorIsTyped(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(mustTool("fail", "Always fails.", nil, nil), func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "boom"}}}, nil
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	session, err := connectClient(ctx, clientTransport, mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	_, callErr := srv.CallToolText(ctx, "fail", nil)
	var toolCallErr *mcp.ToolCallError
	if !errors.As(callErr, &toolCallErr) {
		t.Fatalf("CallToolText error = %v, want *mcp.ToolCallError", callErr)
	}
}

// TestMountedServer_DeathObservedWithoutCall proves the property bridge_ping.go
// existed to provide: a dead server is noticed by Wait(), not by making a call
// first.
func TestMountedServer_DeathObservedWithoutCall(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	srv := NewMountedServer("fixture", nil)
	session, err := connectClient(ctx, clientTransport, mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	if srv.isDead() {
		t.Fatal("precondition: server must not already be dead")
	}
	if err := serverSession.Close(); err != nil {
		t.Fatalf("close server session: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.isDead() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("watch() never observed the peer's death — no tool call was made")
}

// TestMountedServer_ReadOnlyRedialsAndReissues covers the redial-then-reissue
// path for a read-only tool: the next call after death succeeds against a
// restarted peer.
func TestMountedServer_ReadOnlyRedialsAndReissues(t *testing.T) {
	srv, fixture := attachedMountedServer(t, readOnlyLookupServer)
	bridged := bridgeTools("catalog", srv, []*sdkmcp.Tool{
		mustTool("lookup", "Look something up.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
	}, time.Second)
	srv.trackBridgedTools(bridged)

	// Kill the live session's underlying connection directly (not through the
	// fixture) so the FIRST CallToolText observes the transport failure itself.
	killLiveSession(t, srv)

	text, err := srv.CallToolText(context.Background(), "lookup", map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("CallToolText after redial: %v", err)
	}
	if !strings.HasPrefix(text, "lookup:") {
		t.Fatalf("text = %q, want served by the redialed session", text)
	}
	if fixture.dialCount() != 1 {
		t.Fatalf("want exactly 1 redial dial, got %d", fixture.dialCount())
	}
}

// killLiveSession closes the CURRENT session's underlying connection from
// outside MountedServer's own bookkeeping, simulating a peer that vanished
// mid-run without MountedServer having initiated the close.
func killLiveSession(t *testing.T, srv *MountedServer) {
	t.Helper()
	session, err := srv.currentSession()
	if err != nil {
		t.Fatalf("currentSession: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("kill session: %v", err)
	}
}

// TestMountedServer_MutatingNotReplayedAfterDeath covers the no-replay
// guarantee: a mutating tool whose transport failed after send redials but does
// NOT reissue.
func TestMountedServer_MutatingNotReplayedAfterDeath(t *testing.T) {
	buildMutating := func() *sdkmcp.Server {
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
		server.AddTool(mustTool("send", "Send.", nil, nil), trivialToolHandler)
		return server
	}
	srv, fixture := attachedMountedServer(t, buildMutating)
	bridged := bridgeTools("mail", srv, []*sdkmcp.Tool{mustTool("send", "Send.", nil, nil)}, time.Second)
	srv.trackBridgedTools(bridged)

	killLiveSession(t, srv)

	_, err := srv.CallToolText(context.Background(), "send", map[string]any{"x": "y"})
	if err == nil || !mcp.IsTransportError(err) || !strings.Contains(err.Error(), "reconnected but not replayed") {
		t.Fatalf("error = %v, want the reconnected-but-not-replayed transport error", err)
	}
	if fixture.dialCount() != 1 {
		t.Fatalf("want exactly 1 redial dial, got %d", fixture.dialCount())
	}
}

// TestMountedServer_IdempotencyScopedOperationNeverRedials covers the strongest
// no-replay signal: an idempotency operation scoped ScopeMCPTool must not
// redial-and-retry at all — byte-identical semantics to bridge_reconnect.go's
// equivalent guard.
func TestMountedServer_IdempotencyScopedOperationNeverRedials(t *testing.T) {
	buildMutating := func() *sdkmcp.Server {
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
		server.AddTool(mustTool("send", "Send.", nil, nil), trivialToolHandler)
		return server
	}
	srv, fixture := attachedMountedServer(t, buildMutating)
	killLiveSession(t, srv)

	op := idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: idempotency.ScopeMCPTool, Key: "mcp-mutation"},
		Fingerprint: [32]byte{1},
	}
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), op)
	if err != nil {
		t.Fatal(err)
	}

	_, callErr := srv.CallToolText(ctx, "send", map[string]any{"message": "hello"})
	if callErr == nil || !mcp.IsTransportError(callErr) || !strings.Contains(callErr.Error(), "not replayed or reconnected") {
		t.Fatalf("error = %v, want the not-replayed-or-reconnected transport error", callErr)
	}
	if fixture.dialCount() != 0 {
		t.Fatalf("an idempotency-scoped mutation must never redial, got %d dials", fixture.dialCount())
	}
}

// TestMountedServer_RedialBreakerOpensAfterThreeFailures pins the exact error
// text and the fact that a 4th attempt during the cooldown never dials at all.
func TestMountedServer_RedialBreakerOpensAfterThreeFailures(t *testing.T) {
	buildReadOnly := readOnlyLookupServer
	failOpen := errors.New("dial refused")
	srv, fixture := attachedMountedServer(t, buildReadOnly, failOpen, failOpen, failOpen)
	bridged := bridgeTools("catalog", srv, []*sdkmcp.Tool{
		mustTool("lookup", "Look something up.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
	}, time.Second)
	srv.trackBridgedTools(bridged)

	killLiveSession(t, srv)
	for i := range mcpRedialBreakerAfter {
		if _, err := srv.CallToolText(context.Background(), "lookup", nil); !errors.Is(err, failOpen) {
			t.Fatalf("failure %d: want the scripted dial error to surface, got %v", i+1, err)
		}
		// Clear the backoff delay set after each failure so the next attempt hits
		// the redial path immediately instead of waiting out the schedule — the
		// breaker THRESHOLD, not the backoff SCHEDULE, is what this test pins.
		srv.mu.Lock()
		srv.nextRedialAfter = time.Time{}
		srv.mu.Unlock()
	}

	_, err := srv.CallToolText(context.Background(), "lookup", nil)
	if !mcp.IsTransportError(err) || !strings.Contains(err.Error(), "breaker open") {
		t.Fatalf("post-threshold call should fail fast with breaker-open error, got %v", err)
	}
	if got := fixture.dialCount(); got != mcpRedialBreakerAfter {
		t.Fatalf("breaker-open call must not attempt a dial, dials=%d want %d", got, mcpRedialBreakerAfter)
	}
}

// TestMountedServer_ToolSetDriftMarksDeadSticky covers D-104: a server-pushed
// tools/list_changed whose name SET differs from the set accepted at mount marks
// the server dead with the shared error text, and the failure is sticky — a
// later successful re-list does not clear it.
func TestMountedServer_ToolSetDriftMarksDeadSticky(t *testing.T) {
	srv, server := newInMemoryMounted(t, mustTool("send", "Send.", nil, nil))
	advertised, err := srv.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	srv.trackAcceptedTools(advertised)

	server.AddTool(mustTool("archive", "Archive.", nil, nil), trivialToolHandler)

	waitFor(t, func() bool {
		_, err := srv.currentSession()
		return err != nil && strings.Contains(err.Error(), "tool set changed; restart required")
	}, "tool-set drift was never observed")

	// Sticky: removing the added tool again (restoring the ORIGINAL set) must
	// NOT clear the drift error — the server already told a different story once.
	server.RemoveTools("archive")
	time.Sleep(20 * time.Millisecond)
	if _, err := srv.currentSession(); err == nil || !strings.Contains(err.Error(), "restart required") {
		t.Fatalf("drift error was cleared by a later re-list, got %v", err)
	}
}

// TestMountedServer_RefreshInPlaceOnSameNameSet covers the OTHER branch: a
// same-name-set push (description/schema only) refreshes the bridgedTool's
// stored spec in place and fires the refresh hook; the registry is never
// touched (there is no registry in this test at all — proving the point).
func TestMountedServer_RefreshInPlaceOnSameNameSet(t *testing.T) {
	srv, server := newInMemoryMounted(t, mustTool("send", "Old description.", nil, nil))
	advertised, err := srv.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	srv.trackAcceptedTools(advertised)
	bridged := bridgeTools("mail", srv, advertised, time.Second)
	srv.trackBridgedTools(bridged)

	var hookCalls int
	var mu sync.Mutex
	srv.setRefreshHook(func() {
		mu.Lock()
		hookCalls++
		mu.Unlock()
	})

	server.AddTool(mustTool("send", "New description.", nil, nil), trivialToolHandler)

	waitFor(t, func() bool {
		return strings.Contains(bridged[0].Spec().Description, "New description.")
	}, "in-place refresh never observed the new description")

	mu.Lock()
	calls := hookCalls
	mu.Unlock()
	if calls == 0 {
		t.Fatal("refresh hook must fire on an in-place spec change")
	}
	if _, err := srv.currentSession(); err != nil {
		t.Fatalf("a same-name-set change must not mark the server dead, got %v", err)
	}
}

// TestMountedServer_CloseIsIdempotent covers Close's idempotent contract across
// repeated calls; TestMain's goleak.VerifyTestMain independently catches any
// leaked watch() goroutine at suite end.
func TestMountedServer_CloseIsIdempotent(t *testing.T) {
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	for range 3 {
		if err := srv.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
}

// TestMountedServer_ConcurrentCallsSingleFlightRedial covers single-flight: 8
// concurrent CallToolText calls racing the SAME dead session collapse onto one
// dial attempt.
func TestMountedServer_ConcurrentCallsSingleFlightRedial(t *testing.T) {
	srv, fixture := attachedMountedServer(t, readOnlyLookupServer)
	bridged := bridgeTools("catalog", srv, []*sdkmcp.Tool{
		mustTool("lookup", "Look something up.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
	}, time.Second)
	srv.trackBridgedTools(bridged)
	killLiveSession(t, srv)

	const callers = 8
	errs := make(chan error, callers)
	for range callers {
		go func() {
			_, err := srv.CallToolText(context.Background(), "lookup", nil)
			errs <- err
		}()
	}
	for range callers {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent CallToolText: %v", err)
		}
	}
	if got := fixture.dialCount(); got != 1 {
		t.Fatalf("concurrent transport failures should single-flight one dial, got %d", got)
	}
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestRedialBackoff_Schedule(t *testing.T) {
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{-1, 0},
		{0, 0},
		{1, mcpRedialBackoffMin},
		{2, 1 * time.Second},
		{3, 2 * time.Second},
		{7, mcpRedialBackoffMax},
		{50, mcpRedialBackoffMax},
	}
	for _, c := range cases {
		if got := redialBackoff(c.failures); got != c.want {
			t.Errorf("redialBackoff(%d) = %v, want %v", c.failures, got, c.want)
		}
	}
}

// TestIsSessionTransportFailure table-tests the classifier directly, per the
// plan's own requirement — a misclassification here either replays a mutation
// or fails to retry a transient boot race.
func TestIsSessionTransportFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled excluded", context.Canceled, false},
		{"wrapped context canceled excluded", fmt.Errorf("call: %w", context.Canceled), false},
		{"io.EOF", io.EOF, true},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, true},
		{"io.ErrClosedPipe", io.ErrClosedPipe, true},
		{"net.ErrClosed", net.ErrClosed, true},
		{"unrelated error", errors.New("tool says no"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSessionTransportFailure(c.err); got != c.want {
				t.Errorf("isSessionTransportFailure(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
