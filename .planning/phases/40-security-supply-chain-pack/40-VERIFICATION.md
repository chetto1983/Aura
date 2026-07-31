---
phase: 40-security-supply-chain-pack
verified: 2026-07-31
status: passed
score: 7/7 requirements verified
requirements:
  - SEC-01
  - SEC-02
  - SEC-03
  - SEC-04
  - SEC-05
  - SEC-06
  - SEC-09
---

# Phase 40: Security & Supply-Chain Pack — Verification

## Verdict

**PASSED.** All seven Phase 40 requirements are implemented, reachable, and covered
by the validated task map in `40-VALIDATION.md`. The missing per-plan summaries are
an execution-record gap, not an implementation gap: the actual atomic commits,
production wiring, and tests were checked directly.

This verdict does not certify `REL-03` or release publication. Exact-candidate
release evidence and the current audit register remain Phase 41 gates.

## Requirement Evidence

| Requirement | Status | Production evidence |
|---|---|---|
| SEC-01 | VERIFIED | Configured secrets are redacted at turn, spill, and sidecar rest seams (`9831df0b9`); full reasoning records are encrypted and fail closed without a key (`d1073a830`). |
| SEC-02 | VERIFIED | Permissive cross-origin behavior was removed; AG-UI is same-origin only (`9322cdaa1`). |
| SEC-03 | VERIFIED | The integrations console refuses unsafe network exposure unless the explicit token-protected escape hatch is configured (`cd6cd8c61`). |
| SEC-04 | VERIFIED | The gateway denies irreversible external MCP effects while ordinary contained/idempotent mutations remain usable (`01fbfdc68`, `04033d51c`); the injection regression suite passes. |
| SEC-05 | VERIFIED | Workflow actions and Go tool refs are immutable, the pin gate self-tests, and SBOM/release configuration is present (`9a5321594`). |
| SEC-06 | VERIFIED | Privileged JSON handlers reject unknown fields, trailing values, invalid content types, empty disallowed bodies, and oversize bodies through the shared strict decoder (`e89b6d4ef`). |
| SEC-09 | VERIFIED | Recovery lookup tokens use peppered HMAC-SHA-256 with fail-closed key derivation and live Postgres round-trip coverage (`05cc69ff1`). |

## Fresh Verification

Executed from the current checkout on 2026-07-31:

- `go test -count=1 ./internal/gateway ./internal/agui ./internal/secret ./internal/config ./internal/tracesink ./internal/reasoningtrace ./cmd/aura` — PASS.
- `bash scripts/workflow_pin_gate_test.sh` — PASS.
- `bash scripts/workflow_pin_gate.sh` — PASS; all action and Go-tool refs immutable.
- Full WSL `go build ./...`, filtered `go vet` excluding only the Windows
  `web/node_modules` readdir artifact, and `go test -race ./...` — PASS.
- `40-VALIDATION.md` is `status: validated`, `nyquist_compliant: true`, with all
  22 task rows passed.

The paid model-resistance evaluation remains manual by policy and is not used to
substitute for the deterministic production enforcement verdict. Release security
evidence must still be regenerated for the exact final candidate.

## Scope Reconciliation

The gateway policy verified here intentionally does not require confirmation for
every mutation. Reads run normally; reversible contained mutations use idempotency
and containment; only irreversible external effects are denied or escalated under
the strict production profile. This is the current PRD-backed behavior and avoids
the frustrating all-mutations-confirmation model.

