---
phase: 40-security-supply-chain-pack
plan: 02
status: complete
completed: 2026-07-30
requirements: [SEC-04]
commit: 01fbfdc68
---

# Plan 40-02 Summary — Graduated MCP enforcement

## Outcome

SEC-04 is closed with a production-real graduated policy instead of the stale
blanket-denial assumption:

- read-only MCP actions remain `Safe` and available;
- ordinary, reversible connector mutations remain `Normal`, require exact
  operation context plus durable idempotency reservation, and do not prompt;
- destructive or externally irreversible effects become `Destructive` and are
  withheld under `server_production` or routed to approval where an interactive
  approver exists.

## Production divergence and correction

The original plan assumed every mutating MCP action was denyable. The live
bridge only consumed `readOnlyHint`, so all non-read actions became `Normal`;
the gateway approval path activates only for `Destructive`. That made the old
test model false and left sends such as Calendar email and WhatsApp messages
outside the intended gate.

The correction parses MCP `destructiveHint` without losing the missing-versus-
explicit-false distinction, fails closed for unknown/untrusted MCP mutations,
and applies a curated graduated map only to Aura's verified built-in recipe
identities. A remote server cannot claim a recipe source to lower its risk.
Risk is monotone: explicit destructive metadata can raise a recipe action but
never lower it.

## Evidence

- `go test ./internal/gateway -run TestInjectionSuite -count=1 -v`: PASS,
  18 scenarios (9 irreversible denied, 5 ordinary mutations contained and
  allowed without approval, 4 reads allowed).
- `go test ./internal/mcp ./internal/agent/mcptools ./internal/gateway
  ./internal/agent/tools`: PASS.
- `go vet ./...` and `go build ./...`: PASS.
- WSL `go test -race ./internal/mcp ./internal/agent/mcptools
  ./internal/gateway ./internal/agent/tools`: PASS.
- `go build -tags cot_eval ./internal/eval` and
  `go vet -tags cot_eval ./internal/eval`: PASS.
- pre-commit `gofmt`, file-size, vet, and lint gates: PASS, zero issues.

The paid `TestInjectionCoTEval` tier is intentionally not executed
unsolicited. Its build/vet gate is closed; a live score remains an explicit
operator-run evaluation and is not represented as deterministic enforcement
evidence.

## Deviations from the stale plan

Production code changed because a test-only implementation would have encoded
the disproven assumption. `web_fetch` remains a read operation governed by its
SSRF and response-bound controls; it is not falsely classified as destructive.
The enforcement suite therefore tests actual effects and containment rather
than searching payload text for an unverifiable "injected" provenance label.
