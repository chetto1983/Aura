// Package webui is the single-binary host for the embedded web cockpit. It owns
// the committed Vite build (//go:embed all:dist) and exposes one SPA-fallback
// http.Handler over it (WEB-01) — nothing more.
//
// Handler(apiPrefixes) serves the SPA shell with three outcomes:
//   - an excluded prefix in apiPrefixes (the AG-UI routes /agent/run, /healthz,
//     /readyz, /metrics, /debug/vars, /threads/... and the forward-compat /api/
//     carve-out) → a real 404, so an API client never
//     receives the HTML shell (WEB-01 / SC1);
//   - an unknown client route (no embedded asset) → index.html, so React Router
//     resolves deep links;
//   - a real embedded asset → that asset, MIME-typed by http.FileServerFS.
//
// The exclusion list is NOT hard-coded here: the caller (cmd/aura/serve_webui.go)
// is the single source of truth and passes the set in, so the parent-mux
// registration and the fallback exclusion cannot drift.
//
// This package is intentionally a leaf: it imports only the standard library
// (embed, io/fs, net/http, strings) and never another internal/* package
// (agent, agui, ...). scripts/agui_boundary_check.sh enforces this with a
// dependency-closure assertion (the webui closure must contain no internal/*
// package but itself), so the leaf invariant and the transport boundary (D-17)
// cannot silently regress. Auth (GAP-2 / WEB-02/WEB-03) lives in cmd/aura +
// internal/agui, never here.
package webui
