# Broker MCP hardening and cached startup

The operator authorized implementation after the read-only review of the lab VM conversation
`01a1a1c9-ba15-70f2-9cce-ac701c3d646d` on 2026-11-03, then asked why startup takes roughly
30 seconds despite Aura's cache. Work stays on the existing main branches.

## Measured baseline

- The VM registry launches broker-analysis `v0.3.0` with `uvx --from git+https://github.com/chetto1983/broker-analysis-mcp@v0.3.0`,
  runtime `box`, cwd `/plugins/broker-analysis`, and `initTimeoutSec=120`.
- Logs at 14:22:56, 14:23:06 and 14:23:17 UTC record three 10-second reconnect handshake
  failures; the breaker then opens for 30 seconds. A later call recovers. A worker's first
  catalog call takes 12,491 ms between its ledger timestamps.
- `portfolio_risk` uses the hook-write annotation: read-only false, destructive false,
  open-world true, idempotent unspecified. Aura deliberately escalates that combination.
- Invalid RSI zero, confidence 1.2 and ETF period 1d return generic errors. The custom
  indicator path gives a useful parameter/range error.
- AAPL's WACC/EVA/debt-cost nulls have no explanations; warnings are empty.
- All 13 worker broker results have zero stored duration. The read-only start rows omit
  arguments. `closeUnobservedReservation` and `recordDecisionFact` explain these omissions.

These measurements establish integration and diagnostic defects, not independent financial
formula accuracy. Cache effectiveness and the time spent starting Python still need measurement.

## Required behavior

1. Keep the broker's existing tools and numerical contracts. Distinguish local snapshot writes
   from hooks, while honestly declaring Yahoo interaction and fresh timestamped records.
   Do not label saving read-only, external reads closed-world, or new records idempotent.
   Aura's generic conservative fallback and the operator's existing standing grant remain valid;
   do not introduce a name-based trust exception for an arbitrary installed pack.
2. Validate public parameters before provider I/O; expose ranges in schemas and useful domain
   errors. Distinguish invalid input and insufficient observations without leaking internal errors.
3. Explain unavailable financial metrics and provide consistent retrieval/schema and observation
   metadata while preserving existing fields.
4. Preserve worker arguments and actual call timing in the existing durable invocation ledger,
   including errors/cancellation, without weakening reservations or recording a call twice.
5. Measure the existing uv/source/environment caches and Python startup on the real VM. Repair
   the actual cause using the existing runtime/cache lifecycle. Reconnect must honor the
   configured box initialization budget, preserve cancellation/lifetime rules and never replay
   an already-sent mutation.
6. Validate targeted tests in WSL, real persistence where relevant, and real agent calls on the
   lab VM. Record measured startup times and remaining limits in a dated verification file.

## Implementation boundaries

- Broker code and its Python tests belong in `D:/broker-analysis-mcp`.
- Aura session/cache work belongs in `internal/agent/mcptools`, `internal/mcp` and the existing
  sandbox stdio launcher only where evidence requires it.
- Aura ledger changes belong in `internal/gateway` and corresponding tests.
- No new protocol, custom cache manager, cache deletion, migration, or identity-sharing change.
- Reuse uv's documented cache/offline mechanisms and the MCP SDK transport.

## Acceptance

- Invalid inputs yield actionable errors without Yahoo calls; standard/custom validation agrees.
- Local analysis persistence has its own documented annotations; hook effects and the broker's
  external data interactions remain truthfully declared.
- Representative unavailable metrics carry a reason and missing dependencies.
- A delegated call's persisted args match its actual args; duration reflects start/end, not zero.
- Reconnecting a slow box succeeds within its declared startup budget; short normal HTTP
  reconnect and no-replay behavior remain covered.
- Repeated real broker startup reports cache hit/miss, resolution/import time, and end-to-end
  session latency. Improvement is measured, not inferred from unit tests.
