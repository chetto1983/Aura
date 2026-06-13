package mcptools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// errReconnectClient is a reconnectingClient double whose ListTools/CallTool
// results are scripted per-call, so the reconnect decision branches (transport
// vs non-transport error, reopen success vs reopen failure) are driven without a
// process. Each call advances an index into the scripted slices.
type errReconnectClient struct {
	listDefs   [][]mcp.ToolDef
	listErrs   []error
	listN      int
	callTexts  []string
	callErrs   []error
	callN      int
	closeErr   error
	closed     bool
	closeCount int
}

func (c *errReconnectClient) ListTools(context.Context) ([]mcp.ToolDef, error) {
	i := c.listN
	c.listN++
	var defs []mcp.ToolDef
	if i < len(c.listDefs) {
		defs = c.listDefs[i]
	}
	var err error
	if i < len(c.listErrs) {
		err = c.listErrs[i]
	}
	return defs, err
}

func (c *errReconnectClient) CallTool(context.Context, string, map[string]any) (string, error) {
	i := c.callN
	c.callN++
	var text string
	if i < len(c.callTexts) {
		text = c.callTexts[i]
	}
	var err error
	if i < len(c.callErrs) {
		err = c.callErrs[i]
	}
	return text, err
}

func (c *errReconnectClient) Close() error {
	c.closed = true
	c.closeCount++
	return c.closeErr
}

// TestReconnectServer_ListToolsTransportErrorReconnectsAndRetries covers the
// reconnect-on-transport-error branch of ListTools: the first tools/list fails
// with a transport error, the server reopens (refreshed client lists clean), and
// the retry returns the refreshed defs. The fresh defs come from the reopened
// client's reconnectLocked ListTools, so reconnectAfterTransport returns defs!=nil
// and ListTools returns them directly without a second list round-trip.
func TestReconnectServer_ListToolsTransportErrorReconnectsAndRetries(t *testing.T) {
	initial := &errReconnectClient{
		listErrs: []error{fmt.Errorf("pipe: %w", mcp.ErrTransport)},
	}
	refreshed := &errReconnectClient{
		listDefs: [][]mcp.ToolDef{{{Name: "fresh", Description: "Fresh tool."}}},
	}
	restore := stubOpenMCPClient(t, refreshed)
	defer restore()

	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, initial)
	defs, err := srv.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools after transport reconnect: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "fresh" {
		t.Fatalf("want refreshed defs from reopened client, got %v", defs)
	}
	if !initial.closed {
		t.Fatal("initial client should be closed on reconnect")
	}
}

// TestReconnectServer_ListToolsReconnectListFailsAfterReopen covers the path where
// the reopened client itself fails tools/list inside reconnectLocked: the error
// propagates and no defs come back.
func TestReconnectServer_ListToolsReconnectListFailsAfterReopen(t *testing.T) {
	initial := &errReconnectClient{
		listErrs: []error{fmt.Errorf("pipe: %w", mcp.ErrTransport)},
	}
	reopenListErr := errors.New("reopened list failed")
	refreshed := &errReconnectClient{
		listErrs: []error{reopenListErr},
	}
	restore := stubOpenMCPClient(t, refreshed)
	defer restore()

	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, initial)
	defs, err := srv.ListTools(context.Background())
	if defs != nil {
		t.Fatalf("want nil defs when reopened list fails, got %v", defs)
	}
	if !errors.Is(err, reopenListErr) {
		t.Fatalf("want the reopened-list error, got %v", err)
	}
}

// TestReconnectServer_ListToolsReopenFails covers reconnectLocked's open-failure
// branch: the transport error triggers a reconnect, but openMCPClient itself fails,
// so the wrapped reconnect error propagates.
func TestReconnectServer_ListToolsReopenFails(t *testing.T) {
	initial := &errReconnectClient{
		listErrs: []error{fmt.Errorf("pipe: %w", mcp.ErrTransport)},
	}
	openErr := errors.New("spawn refused")
	old := openMCPClient
	t.Cleanup(func() { openMCPClient = old })
	openMCPClient = func(context.Context, string, mcp.ServerConfig) (reconnectingClient, error) {
		return nil, openErr
	}

	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, initial)
	_, err := srv.ListTools(context.Background())
	if !errors.Is(err, openErr) {
		t.Fatalf("want the wrapped open error, got %v", err)
	}
	if !strings.Contains(err.Error(), "reconnect mcp") {
		t.Fatalf("error should be wrapped with reconnect context, got %v", err)
	}
}

// TestReconnectServer_ListToolsNonTransportErrorPropagates covers the !IsTransportError
// fast path of ListTools: a plain (non-transport) tools/list error is returned
// verbatim with NO reconnect attempt.
func TestReconnectServer_ListToolsNonTransportErrorPropagates(t *testing.T) {
	plain := errors.New("schema parse error")
	initial := &errReconnectClient{listErrs: []error{plain}}
	old := openMCPClient
	t.Cleanup(func() { openMCPClient = old })
	opened := false
	openMCPClient = func(context.Context, string, mcp.ServerConfig) (reconnectingClient, error) {
		opened = true
		return initial, nil
	}

	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, initial)
	_, err := srv.ListTools(context.Background())
	if !errors.Is(err, plain) {
		t.Fatalf("non-transport list error should propagate verbatim, got %v", err)
	}
	if opened {
		t.Fatal("a non-transport error must NOT trigger a reconnect")
	}
}

// TestReconnectServer_ListToolsOnClosedServer covers ListTools' currentClient
// error branch: a closed server returns the transport "is closed" error before any
// list round-trip.
func TestReconnectServer_ListToolsOnClosedServer(t *testing.T) {
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, &errReconnectClient{})
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := srv.ListTools(context.Background())
	if !mcp.IsTransportError(err) || !strings.Contains(err.Error(), "is closed") {
		t.Fatalf("ListTools on a closed server should be a transport-closed error, got %v", err)
	}
}

// TestReconnectServer_CallToolOnClosedServer covers CallTool's currentClient error
// branch symmetrically.
func TestReconnectServer_CallToolOnClosedServer(t *testing.T) {
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, &errReconnectClient{})
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := srv.CallTool(context.Background(), "send", nil)
	if !mcp.IsTransportError(err) || !strings.Contains(err.Error(), "is closed") {
		t.Fatalf("CallTool on a closed server should be a transport-closed error, got %v", err)
	}
}

// TestReconnectServer_CallToolNonTransportErrorPropagates covers the !IsTransportError
// fast path of CallTool: a plain tool error returns verbatim, no reconnect.
func TestReconnectServer_CallToolNonTransportErrorPropagates(t *testing.T) {
	plain := errors.New("tool says no")
	initial := &errReconnectClient{callErrs: []error{plain}}
	old := openMCPClient
	t.Cleanup(func() { openMCPClient = old })
	opened := false
	openMCPClient = func(context.Context, string, mcp.ServerConfig) (reconnectingClient, error) {
		opened = true
		return initial, nil
	}

	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, initial)
	_, err := srv.CallTool(context.Background(), "send", nil)
	if !errors.Is(err, plain) {
		t.Fatalf("non-transport call error should propagate verbatim, got %v", err)
	}
	if opened {
		t.Fatal("a non-transport call error must NOT trigger a reconnect")
	}
}

// TestReconnectServer_CallToolReconnectFailsReturnsReconnectError covers CallTool's
// branch where the post-send transport error triggers a reconnect that itself
// fails: the reconnect error (not the no-replay marker) is what surfaces.
func TestReconnectServer_CallToolReconnectFailsReturnsReconnectError(t *testing.T) {
	initial := &errReconnectClient{
		callErrs: []error{fmt.Errorf("send broke: %w", mcp.ErrTransport)},
	}
	openErr := errors.New("reopen refused")
	old := openMCPClient
	t.Cleanup(func() { openMCPClient = old })
	openMCPClient = func(context.Context, string, mcp.ServerConfig) (reconnectingClient, error) {
		return nil, openErr
	}

	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, initial)
	_, err := srv.CallTool(context.Background(), "send", nil)
	if !errors.Is(err, openErr) {
		t.Fatalf("want the reconnect (open) error to surface, got %v", err)
	}
	if strings.Contains(err.Error(), "not replayed") {
		t.Fatalf("when the reconnect itself fails the no-replay marker must NOT appear, got %v", err)
	}
}

// TestReconnectServer_CloseIdempotent covers Close's already-closed early return
// (second Close is a no-op returning nil, the underlying client is closed once).
func TestReconnectServer_CloseIdempotent(t *testing.T) {
	client := &errReconnectClient{}
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, client)
	if err := srv.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close must be a no-op nil, got %v", err)
	}
	if client.closeCount != 1 {
		t.Fatalf("underlying client should be closed exactly once, got %d", client.closeCount)
	}
}

// TestReconnectServer_CloseNilClientReturnsNil covers Close's nil-client branch:
// a server whose client was cleared still closes cleanly.
func TestReconnectServer_CloseNilClientReturnsNil(t *testing.T) {
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, nil)
	if err := srv.Close(); err != nil {
		t.Fatalf("Close with nil client should return nil, got %v", err)
	}
}

// TestReconnectServer_ClosePropagatesUnderlyingError covers Close's return of the
// underlying client's Close error.
func TestReconnectServer_ClosePropagatesUnderlyingError(t *testing.T) {
	closeErr := errors.New("pipe close failed")
	client := &errReconnectClient{closeErr: closeErr}
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, client)
	if err := srv.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close should propagate the underlying close error, got %v", err)
	}
}

// TestReconnectServer_ReconnectAfterTransportClientSwappedConcurrently covers
// reconnectAfterTransport's "another goroutine already reconnected" branch: when the
// failed client is no longer s.client, the helper returns the current client without
// reopening, so ListTools retries on the already-swapped client.
func TestReconnectServer_ReconnectAfterTransportClientSwappedConcurrently(t *testing.T) {
	failed := &errReconnectClient{}
	current := &errReconnectClient{
		listDefs: [][]mcp.ToolDef{{{Name: "swapped", Description: "Already reconnected."}}},
	}
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, current)

	defs, retry, err := srv.reconnectAfterTransport(context.Background(), failed)
	if err != nil {
		t.Fatalf("reconnectAfterTransport with a stale failed client: %v", err)
	}
	if defs != nil {
		t.Fatalf("a concurrently-swapped client should yield nil defs (caller retries), got %v", defs)
	}
	if retry != current {
		t.Fatal("should hand back the current (already-swapped) client for the retry")
	}
	if failed.closed {
		t.Fatal("the stale failed client must not be closed by this branch")
	}
}

// swapOnListClient is a reconnectingClient whose ListTools swaps the server's
// client to `next` (simulating a concurrent reconnect by another goroutine) and
// then returns a transport error. This drives ListTools' line-48 retry branch:
// reconnectAfterTransport sees s.client != failed, returns the already-swapped
// client with nil defs, and ListTools retries on it.
type swapOnListClient struct {
	srv  *reconnectingServer
	next reconnectingClient
}

func (c *swapOnListClient) ListTools(context.Context) ([]mcp.ToolDef, error) {
	c.srv.mu.Lock()
	c.srv.client = c.next
	c.srv.mu.Unlock()
	return nil, fmt.Errorf("stale pipe: %w", mcp.ErrTransport)
}
func (c *swapOnListClient) CallTool(context.Context, string, map[string]any) (string, error) {
	return "", nil
}
func (c *swapOnListClient) Close() error { return nil }

// TestReconnectServer_ListToolsRetriesOnConcurrentlySwappedClient covers ListTools'
// line-48 retry: when the transport failure races a reconnect done by another
// goroutine, reconnectAfterTransport returns the new client (nil defs, nil err) and
// ListTools lists again on it rather than reopening a second time.
func TestReconnectServer_ListToolsRetriesOnConcurrentlySwappedClient(t *testing.T) {
	swapped := &errReconnectClient{
		listDefs: [][]mcp.ToolDef{{{Name: "after_swap", Description: "Listed on the swapped client."}}},
	}
	failed := &swapOnListClient{next: swapped}
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, failed)
	failed.srv = srv

	old := openMCPClient
	t.Cleanup(func() { openMCPClient = old })
	openMCPClient = func(context.Context, string, mcp.ServerConfig) (reconnectingClient, error) {
		t.Fatal("line-48 retry must NOT reopen — the client was already swapped")
		return nil, nil
	}

	defs, err := srv.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools should retry on the swapped client, got %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "after_swap" {
		t.Fatalf("want defs from the swapped client, got %v", defs)
	}
}

// TestReconnectServer_ReconnectAfterTransportNilClient covers the defensive
// client==nil branch of reconnectAfterTransport (no client to retry on).
func TestReconnectServer_ReconnectAfterTransportNilClient(t *testing.T) {
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, nil)
	failed := &errReconnectClient{}
	_, _, err := srv.reconnectAfterTransport(context.Background(), failed)
	if !mcp.IsTransportError(err) || !strings.Contains(err.Error(), "no client") {
		t.Fatalf("nil-client reconnect should be a transport 'no client' error, got %v", err)
	}
}

// TestReconnectServer_ReconnectAfterTransportClosed covers the closed-server branch
// of reconnectAfterTransport.
func TestReconnectServer_ReconnectAfterTransportClosed(t *testing.T) {
	client := &errReconnectClient{}
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, client)
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, _, err := srv.reconnectAfterTransport(context.Background(), client)
	if !mcp.IsTransportError(err) || !strings.Contains(err.Error(), "is closed") {
		t.Fatalf("reconnectAfterTransport on a closed server should be transport-closed, got %v", err)
	}
}

// TestReconnectServer_ReconnectLockedClosed covers reconnectLocked's own closed
// guard directly (defensive: the only caller already holds the lock and checks
// closed, but the guard is load-bearing if a future caller forgets).
func TestReconnectServer_ReconnectLockedClosed(t *testing.T) {
	client := &errReconnectClient{}
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, client)
	srv.mu.Lock()
	srv.closed = true
	_, err := srv.reconnectLocked(context.Background())
	srv.mu.Unlock()
	if !mcp.IsTransportError(err) || !strings.Contains(err.Error(), "is closed") {
		t.Fatalf("reconnectLocked on a closed server should be transport-closed, got %v", err)
	}
}
