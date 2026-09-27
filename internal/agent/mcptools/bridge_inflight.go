package mcptools

import (
	"context"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_inflight.go remembers which of Aura's requests are open on which session,
// so an elicitation can find the run that asked. The SDK runs a multi-round-trip
// elicitation on the context of the request that asked (go-sdk@v1.8.0
// mcp/mrtr.go:73-118, for tools/call and resources/read alike), and callOnSession
// and readLinksOnSession mark that context. A classic elicitation/create arrives on
// the session's connection context instead, and only the calls open on that
// session can say which run it belongs to.
//
// The connection context is never trusted to name a run. It keeps the values of
// whatever context dialled the session (go-sdk@v1.8.0 mcp/streamable.go:2074
// detaches it with xcontext.Detach), and a redial or an identity pool's first dial
// runs on a call's context (bridge_supervisor_redial.go:113,
// bridge_identity_sessions.go:101). So the marker is added around the request
// alone, never before a dial, and a context without it is routed by the calls open
// on its session, never by the asker it happens to carry.

type callToolKey struct{}

func withCallTool(ctx context.Context, tool string) context.Context {
	return context.WithValue(ctx, callToolKey{}, tool)
}

// callToolFrom reports the tool whose request ctx is, if ctx is one of Aura's
// marked requests.
func callToolFrom(ctx context.Context) (string, bool) {
	tool, ok := ctx.Value(callToolKey{}).(string)
	return tool, ok
}

type inFlightCall struct{ ctx context.Context }

// inFlightCalls is process-wide because its keys already are: a *ClientSession
// belongs to exactly one mount, so two mounts never share an entry.
type inFlightCalls struct {
	mu        sync.Mutex
	bySession map[*sdkmcp.ClientSession]map[*inFlightCall]struct{}
	asking    map[*sdkmcp.ClientSession]int
}

var inFlight = &inFlightCalls{
	bySession: map[*sdkmcp.ClientSession]map[*inFlightCall]struct{}{},
	asking:    map[*sdkmcp.ClientSession]int{},
}

func (f *inFlightCalls) enter(ctx context.Context, session *sdkmcp.ClientSession) (leave func()) {
	// A call can return while its run remains alive. Classic questions waiting
	// on a snapshot of this entry must still observe that the call has ended.
	ctx, cancel := context.WithCancel(ctx)
	call := &inFlightCall{ctx: ctx}
	f.mu.Lock()
	calls := f.bySession[session]
	if calls == nil {
		calls = map[*inFlightCall]struct{}{}
		f.bySession[session] = calls
	}
	calls[call] = struct{}{}
	f.mu.Unlock()
	return func() {
		cancel()
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(calls, call)
		if len(calls) == 0 {
			delete(f.bySession, session)
		}
	}
}

// on returns the contexts of the calls open on session, in no order.
func (f *inFlightCalls) on(session *sdkmcp.ClientSession) []context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	open := make([]context.Context, 0, len(f.bySession[session]))
	for call := range f.bySession[session] {
		open = append(open, call.ctx)
	}
	return open
}

// ask takes one of session's elicit.MaxOpenQuestions slots for a question being
// decided, and done gives it back. ok is false when every slot is taken.
func (f *inFlightCalls) ask(session *sdkmcp.ClientSession) (done func(), ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.asking[session] >= elicit.MaxOpenQuestions {
		return nil, false
	}
	f.asking[session]++
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.asking[session]--
		if f.asking[session] == 0 {
			delete(f.asking, session)
		}
	}, true
}

// callOnSession makes one of MountedServer.CallTool's tools/call attempts, with its
// context marked and recorded for as long as it is open.
func callOnSession(ctx context.Context, session *sdkmcp.ClientSession, name string, args map[string]any) (*sdkmcp.CallToolResult, error) {
	ctx = withCallTool(ctx, name)
	defer inFlight.enter(ctx, session)()
	return session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
}

// readLinksOnSession reads a result's links back as part of the call that returned
// them: those resources/read requests are the same run's, and a form a server asks
// inside one reaches the handler on their context (mrtr.go:76).
func readLinksOnSession(ctx context.Context, session *sdkmcp.ClientSession, tool string, payload mcp.ToolPayload) mcp.ToolPayload {
	ctx = withCallTool(ctx, tool)
	defer inFlight.enter(ctx, session)()
	return resolveLinks(ctx, session, payload)
}
