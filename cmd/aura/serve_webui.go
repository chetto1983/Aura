// serve_webui.go wires the embedded operator SPA into the running daemon (FND-02).
// The single-binary host serves the committed Vite build (internal/webui) at "/",
// additively, on the SAME loopback http.Server that already carries the AG-UI
// gateway — the embed mount adds no new listener and no new bind (T-23-06).
//
// Precedence is the whole design: a Go 1.22 http.ServeMux gives a longer/more
// specific registered pattern priority over the catch-all "/", so registering the
// explicit AG-UI route prefixes (/healthz, /readyz, /debug/vars, /metrics,
// /agent/run, /threads/) to the AG-UI handler and "/" to the static handler keeps
// the AG-UI routes authoritative while everything else falls through to the embed.
//
// Phase-23 scope is the static placeholder ONLY. No SPA catch-all / index.html
// fallback for unknown client routes and no API-404 exclusion is added here — a
// missing asset under "/" simply 404s from http.FileServerFS, which is correct for
// a static placeholder. The real SPA-fallback + route-exclusion is deliberately
// Phase 24 (WEB-01). internal/webui stays leaf-level (it imports no other internal/*
// package), an invariant scripts/agui_boundary_check.sh enforces via a dependency-
// closure assertion on the webui package.
package main

import (
	"fmt"
	"net/http"

	"github.com/chetto1983/aura/internal/webui"
)

// aguiRoutePrefixes are the route patterns the AG-UI gateway owns. Registered on
// the parent mux ahead of the "/" catch-all, Go 1.22 ServeMux precedence keeps them
// authoritative — a request to any of these reaches the AG-UI handler, never the
// embed. The trailing-slash "/threads/" is a subtree pattern (matches
// /threads/{id}/messages); the rest are exact paths.
var aguiRoutePrefixes = []string{
	"/healthz",
	"/readyz",
	"/debug/vars",
	"/metrics",
	"/agent/run",
	"/threads/",
}

// newServeHandler builds the parent http.Handler for the daemon's single loopback
// server: the AG-UI route prefixes delegate to aguiHandler (the agui Server.Mux),
// and the catch-all "/" serves the embedded operator shell. A webui.Handler()
// failure (an embed sub error, which a committed dist makes unreachable) is returned
// so bootServe fails the daemon boot cleanly rather than mounting a half-wired host.
func newServeHandler(aguiHandler http.Handler) (http.Handler, error) {
	static, err := webui.Handler()
	if err != nil {
		return nil, fmt.Errorf("webui handler: %w", err)
	}
	mux := http.NewServeMux()
	for _, prefix := range aguiRoutePrefixes {
		mux.Handle(prefix, aguiHandler)
	}
	mux.Handle("/", static)
	return mux, nil
}
