package main

// serve_browser_live.go wires the cockpit's live view of an agent-browser session (prd.md §12):
// the relay adapter over the sandbox router, and the two parent-mux mounts. Both routes sit
// behind agentRunCapability like POST /agent/run, because driving the box's browser is running
// the agent's tools.

import (
	"context"
	"io"
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

const (
	browserStreamRoute = "GET /api/browser/sessions/{session}/stream"
	browserInputRoute  = "POST /api/browser/sessions/{session}/input"
	// browserRelayScript is baked into docker/aura-sandbox (browser-relay.mjs).
	browserRelayScript = "/usr/local/lib/aura/browser-relay.mjs"
)

func registerBrowserLiveRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	mux.Handle(browserStreamRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(browserInputRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
}

// sandboxBrowserRelay opens the relay in the box of the identity ctx carries. The session name
// reaches the shell unquoted only because agui admits nothing but [A-Za-z0-9_-]{1,48}.
type sandboxBrowserRelay struct {
	router *usersandbox.SandboxRouter
}

func (r sandboxBrowserRelay) Open(ctx context.Context, session string, in io.ReadCloser, out io.Writer) (agui.BrowserRelayHandle, error) {
	h, err := r.router.Route(ctx)
	if err != nil {
		return nil, err
	}
	touched := touchOnInput{ReadCloser: in, touch: func() { r.router.Touch(h.IdentityID) }}
	return r.router.ExecStream(ctx, h, usersandbox.ExecRequest{
		Command: "node " + browserRelayScript + " " + session,
		Dir:     "/workspace",
	}, touched, out)
}

// touchOnInput keeps the box awake while the operator is using the live view. The relay is
// one long-lived exec, and the idle reaper counts only new ones: without this a login that
// took longer than the idle TTL would have its browser suspended mid-way.
type touchOnInput struct {
	io.ReadCloser
	touch func()
}

func (t touchOnInput) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if n > 0 {
		t.touch()
	}
	return n, err
}
