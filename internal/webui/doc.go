// Package webui is the Phase-23 static placeholder host for the embedded web
// cockpit. It owns the committed Vite build (//go:embed all:dist) and exposes a
// single static http.Handler over it — nothing more.
//
// Deliberately out of scope here (Phase 24, WEB-01):
//   - the SPA catch-all / index.html-fallback for client-side routes,
//   - the API/agent/health route exclusion that keeps the AG-UI routes
//     (/agent/run, /healthz, /readyz, /metrics, /debug/vars, /threads/...)
//     at priority over the static tree,
//   - any web auth boundary (GAP-2 / WEB-02) or non-loopback boot guard.
//
// Do NOT add fallback or routing logic to this package. It is intentionally a
// leaf: it imports only the standard library (embed, io/fs, net/http) and never
// internal/agent or internal/agui, so scripts/agui_boundary_check.sh stays green
// and the transport boundary (D-17) is preserved.
package webui
