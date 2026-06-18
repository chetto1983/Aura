// Package display is the trust boundary for Phase-26's typed-display protocol
// (GAP-1 / HARDEN-08). It is the ONLY component allowed to turn an untrusted
// structured tool result into a rich-renderable typed Payload: every other tier
// (the AG-UI translator, the cockpit DisplayRouter) treats a Payload as already
// trusted. A tool with no normalizer here returns (Payload{}, false) so the caller
// falls back to the raw, escaped tool card (D-FALLBACK) — typed rich rendering is
// reserved for results this package recognized.
//
// The package is PURE DATA: Normalize and the per-tool normalizers do no I/O, hold
// no state across turns (the per-turn source Registry is the caller's to own), and
// have no side effects. The same normalizer feeds the live SSE path AND the replay
// path (D-06): both consume the tool's result-preview shape, so a reopened thread
// re-derives byte-identical displays.
//
// Two safety invariants live here:
//   - system_event (systemevent.go) consumes ONLY the web/errors.go sanitize()-
//     classified reason or the swarm Status enum — never free-form error text, so
//     no SSRF internal (resolved IP / internal host / redirect chain) can reach a
//     user-visible card (D-07).
//   - the source Registry (sources.go) owns the [n]→source map with stable
//     URL-keyed RefIDs, so the model can only cite a number the registry rendered
//     (D-05) — it cannot reference a hallucinated source.
package display
