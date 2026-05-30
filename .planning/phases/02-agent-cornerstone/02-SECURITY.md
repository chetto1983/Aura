---
phase: 02
slug: agent-cornerstone
status: verified
threats_open: 0
asvs_level: 1
created: 2026-05-30
register_authored_at_plan_time: true
---

# Phase 02 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register authored at plan time → verification-only audit (no new-threat scan).
> Implementation files treated as READ-ONLY; nothing was patched.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| env → process | `AURA_LOOP_*` parsed at boot (`NewBudgetFromEnv`); malformed input must fail-fast, not crash mid-loop | step/wallclock/timeout limits (untrusted operator input) |
| CLI flags → Budget | `--request-id` / `--max-*` parsed at the `aura agent` entry point | run identity + caps (untrusted CLI input) |
| tool args → dedup fingerprint | tool-call arguments flow into `canonicaljson.Marshal` → `sha256` fingerprint that drives loop termination | tool name + JSON args (LLM-influenced) |
| agent tree → shared step counter | every node mutates one `*atomic.Int32`; a fresh-per-child budget reintroduces depth³ | concurrent counter (in-process) |
| in-process Run scope | `InvocationContext` is single-Run-scoped (D-24); no cross-invocation sharing | per-run state |
| iterator frame ↔ child goroutines | `iter.Seq2` yields must be serial + guarded; channels need exit paths or goroutines leak | Event stream / control signals |
| future trace export | SpanID/TraceID widths must match W3C/OTel exactly or future correlation silently breaks | 8-byte SpanID / 16-byte TraceID |
| supply chain | `go get` of `google/uuid` + `pgregory.net/rapid` at build time | third-party module trust |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-02-01 | DoS | Budget ConsumeStep over-spend (TOCTOU) / depth³ | mitigate | `budget.go:213-223` decrement-then-check-then-restore (D-11) on single shared `*atomic.Int32` (`:51`, Child shares pointer `:269`); tests `budget_test.go:58,94`, `parallel_test.go:138` SC#3 total ≤25 (`:165`) | closed |
| T-02-02 | DoS | fail-open dedup / runaway loop | mitigate | `budget_dedup.go:122-162` pre-execute fingerprint + result-as-veto (`:170-192`); LoopAgent shared ring via `WithSubAgent` (`loop.go:69`); tests `budget_dedup_test.go:72,174`, `loop_test.go:460,489` | closed |
| T-02-03 | DoS | malformed `AURA_LOOP_*` / `--max-*` crashing entry | mitigate | `budget.go:99-137` fail-fast verbatim `errMalformed` (`:72-74`); CLI `-1` sentinel + clean exit 1 (`cmd/aura/agent.go:46-66,167-172`); tests `budget_test.go:265,277,288` | closed |
| T-02-04 | Tampering | `canonicaljson.Marshal` fingerprint collision | mitigate | `canonicaljson.go:32-110` strict-reject NaN/Inf/func/chan + UseNumber so `1 != 1.0` (D-08); fuzz + distinct-literal tests | closed |
| T-02-05 | Spoofing | SpanID/ParentSpanID trace forgery | mitigate | 8-byte SpanID OTel shape (`event.go:39`, `agent.go:51`) + crypto-random UUIDv7 TraceID (`cmd/aura/agent.go:135-148`); no `math/rand` in `internal/agent`; minting deferred (WR-04); test `event_test.go:134` | closed |
| T-02-06 | Tampering | InvocationContext cross-invocation leakage | mitigate | `agent.go:47-74` named `Ctx`, copy-returning `WithContext`/`WithSubAgent` (D-24) | closed |
| T-02-07 | Info disclosure | lossy SpanID truncation (future OTel) | mitigate | `[8]byte` SpanID (`event.go:39,108`) → drop-in OTel, no 16→8 truncation | closed |
| T-02-08 | DoS | in-flight call overruns wallclock | mitigate | `budget.go:300-302` `WithDeadline` (D-13) + `AURA_LOOP_NODE_TIMEOUT_SEC` (`:304-306`); wallclock-first in ConsumeStep (`:214-216`); test `budget_test.go:323` | closed |
| T-02-09 | DoS (latent) | mock fresh budget reintroduces depth³ | mitigate | `agenttest/mocks.go:165-210` CountingAgent consumes `ic.Budget` only, never `NewBudgetFromEnv`; SC#3 `parallel_test.go:165,177` | closed |
| T-02-10 | Tampering | inline mock duplication drift | mitigate | single shared `agenttest` package (D-07) + compile-time `agent.Agent` asserts (`mocks.go:16,27-32`) | closed |
| T-02-11 | DoS/panic | bare `yield` after false (iter.Seq2 footgun) | mitigate | guarded yields `loop.go:81,191`, `sequential.go:62`, `mocks.go:66,102,152,205` (D-22); goleak across `parallel_test.go` + `workflow_test.go:18` | closed |
| T-02-12 | Tampering | termination via error-slot pollution | mitigate | Event-only termination `loop.go:228-243`; `ErrBudgetExhausted` sentinel `errors.go:10` (D-04); test `event_test.go:245` | closed |
| T-02-13 | DoS (slow) | goroutine leak on early break / cancel | mitigate | `parallel.go:80,117,96` defer cancel/close + multi-arm selects + spawn guard (D-23, Go #61611); test `parallel_test.go:243` + goleak | closed |
| T-02-14 | Tampering | fake sentinel error pollutes errgroup | mitigate | captured `context.CancelFunc` escalate `parallel.go:78-80,124` (D-03); siblings drain `nil` (`:151-160`, D-05) | closed |
| T-02-15 | DoS | unbounded buffer under slow consumer | mitigate | synchronous ack-channel backpressure `parallel.go:66-70,148-162`; test `parallel_test.go:297` | closed |
| T-02-16 | Spoofing | predictable/forgeable request_id | mitigate | `cmd/aura/agent.go:135-148` `uuid.NewV7()` (crypto/rand) for auto / validated literal | closed |
| T-02-17 | Tampering | skip-as-green smoke test | mitigate | `scripts/loop_budget_smoke.sh:22-86` runs real binary, asserts 26-line + grep + ≥85% coverage gates under `set -euo pipefail` | closed |
| T-02-SC | Tampering (supply chain) | `go get` of uuid + rapid | accept | `go.mod:7,13` exact pins (uuid v1.6.0 / rapid v1.3.0), canonical paths, `go.sum` hashes, `go mod verify` clean — see Accepted Risks Log | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-02-01 | T-02-SC | Supply-chain trust in `github.com/google/uuid` v1.6.0 + `pgregory.net/rapid` v1.3.0. Both registry-verified canonical packages, pinned at exact versions in `go.mod` with matching `go.sum` checksums; `go mod verify` reports all modules verified. `uuid` is the de-facto Go UUID library (OTel/W3C TraceID); `rapid` is a test-only property-based dependency. Canonical, pinned, verified. | gsd-security-auditor / Davide | 2026-05-30 |

---

## Deferred (documented, not a Phase-2 gap)

- **Per-node SpanID minting (WR-04).** `event.go:24-31` and `agent.go:51-52` document that per-node `crypto/rand` SpanID generation + ParentSpanID chaining is intentionally DEFERRED to the future OTel-integration slice. Phase 02 ships the OTel-compatible 8-byte *shape* (the T-02-05/T-02-07 mitigation), leaving SpanID at the all-zero not-yet-minted sentinel. The forgery/truncation surface the register targets (predictable or lossily-truncated span width, `math/rand`) is closed by the width contract + the crypto-random UUIDv7 TraceID.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-05-30 | 18 | 18 | 0 | gsd-security-auditor (opus), verification-only |

---

## Unregistered Flags

None. SUMMARY files 02-00 through 02-07 introduce no new attack surface: `02-00-SUMMARY.md` Threat Flags = "None"; 02-01 through 02-07 map every threat to an existing register ID with no orphan flag.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-05-30
