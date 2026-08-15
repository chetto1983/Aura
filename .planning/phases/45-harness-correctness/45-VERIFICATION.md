---
phase: 45-harness-correctness
status: gaps_found
verified_by: orchestrator (inline)
verified_on: 2026-08-15
build_verified: a85627198
score: 9/12 requirements closed by live evidence
nyquist_compliant: false
---

# Phase 45: harness-correctness — Verification

**Verdict: `gaps_found`.** Nine of twelve requirements are closed by live evidence on the
running stack. Three are not, and are recorded as open rather than rounded up: HARN-03's
replay marker was never reachable live, ACC-01 cannot be satisfied while any requirement
lacks live evidence, and HARN-09's same-message half could not be induced.

Written inline by the orchestrator after four consecutive `gsd-verifier` dispatches
stalled on the 600s stream watchdog. That is a process deviation and is disclosed here
rather than hidden: this verification was not produced by an independent agent, and its
principal evidence — `45-VALIDATION.md` — was written by the same orchestrator that drove
the run. The mitigating fact is that every claim below is a command output, a SQL result
or a transport-level count that can be re-executed, not a judgement.

## Requirement status

| ID | Evidence tier | Verdict | Basis |
|---|---|---|---|
| HARN-01 | **LIVE** | closed | SC#1: `2 end rows / 2 distinct ids / 2 distinct previews` for two `shell_exec` executions in one turn, both executed |
| HARN-02 | **LIVE** | closed | Same SQL as HARN-01, plus SC#2's single-execution retry (`1 start / 1 end`) |
| HARN-03 | **UNIT ONLY** | **OPEN** | `replayedMarker` on both layers and `aura.tool.replayed`/`replay_layer` are unit-proven (45-03). The replay layer was never reached live — see Gap 1 |
| HARN-04 | **LIVE** | closed | MEM-05 step g: prose object rejected, then recovered unaided; `fact_key`/`supersedes_fact_key` contract visible at the model boundary |
| HARN-06 | **LIVE** | closed | 45-09 re-drive: 0 deliberation markers in `TEXT_MESSAGE_CONTENT` on the shape that previously leaked 6 |
| HARN-07 | **LIVE** | closed | Same re-drive; the reply stayed in the operator's language end to end and delivered the stated intention in full |
| HARN-08 | **UNIT ONLY** | closed with a named concession | The D-12 invariant it protects HOLDS live (0 orphan calls, 0 executions without a call), but **zero repairs fired** — no `_d<n>`, no `call_<12hex>` in 8 end rows. The repair path itself is unit-proven; a malformed provider batch cannot be requested and must not be hand-crafted |
| HARN-09 | **SPLIT** | partially open | Cross-round half proved LIVE by SC#1. Same-message half **NOT REPRODUCED**: asked twice for a naturally repeated action, the model batched both reads into one call both times. Stays unit-proven — see Gap 3 |
| MEM-04 | **LIVE** | closed | Entity traversal on `Davide` returns 10 facts with `distinct subjects == {'Davide'}`; a fact written with the UUID as subject comes back canonicalized, UUID preserved in `statement` |
| MEM-05 | **LIVE** | closed | Rejection quoted verbatim, naming `statement` as the destination, followed by unaided recovery in the same turn |
| ACC-01 | — | **OPEN** | ACC-01 states a requirement without live evidence is not done. HARN-03 has none, so ACC-01 cannot close while it stands |
| ACC-02 | **LIVE** | closed | Evidence surfaces exercised: `aura.tool_invocations` SQL, persisted `conversation_turns.tool_calls`, and the SSE transport |

## Gaps

### Gap 1 (largest) — HARN-03 has no live evidence

The replay marker and its span attributes are the phase's most load-bearing fix: they are
what stops Aura acting on a result she did not produce. They remain unit-proven.

The only non-destructive induction available — replaying a request with a duplicate
`Idempotency-Key` — is refused at the HTTP layer (`operation outcome is indeterminate; do
not retry automatically`) before the agent loop runs, so the reservation-ledger and
operation-registry layers are never entered. Reaching them live needs a scheduler reclaim
or a container killed mid-tool-execution against the operator's live deployment, which was
not performed without an explicit instruction.

**To close:** induce a reclaim deliberately (kill `aura` mid-`shell_exec` and let the
scheduler reclaim), then read the tool result for `replayedMarker` and the `tool.execute`
span for `aura.tool.replayed=true` with the correct `replay_layer`.

### Gap 2 — SC#4's target fact does not exist

D-23 names the ArcadeDB-orphan-nodes misdiagnosis as the fact to correct. Two recalls
returned `{"facts":[],"retrieval":{"abstained":true,...,"reason":"no_qualified_candidates"}}`.
The step as written is unrunnable. The mechanism it exists to prove is evidenced by steps
f and g, but SC#4 itself is not closed.

**To close:** either amend D-23 to name a fact that exists, or re-run after seeding an
equivalent one, and correct it via `supersedes_fact_key`.

### Gap 3 — HARN-09's same-message half was not reproduced

Two independent inductions in ordinary operator language both produced a single batched
call rather than two with identical `(name, arguments)`. This looks like a property of
`deepseek/deepseek-v4-flash-0731`, not of the harness.

**To close:** either accept the unit proof with this measurement recorded as the reason, or
re-attempt against a provider that repeats rather than consolidates.

## What the live run found that no automated tier did

Both defects below passed every automated gate — 85.8% coverage, MRS 100.00, mutation
above floor, lint 0 issues, `-race` green — and were caught only by driving the real agent:

1. **A reply carrying invented identifiers** (SC#5). Deliberation leaked into user-facing
   text, in English to an operator writing Italian, presenting `fact_key` values no tool
   returned, with the model stating in the same visible text that it was inventing them.
   The D-21 prompt rule was verified PRESENT in the shipped binary and did not hold. Fixed
   in 45-09 and re-driven clean (6 markers → 0, 0 unsourced keys).
2. **MEM-04 unreachable by construction.** `compose.yaml` never declared
   `AURA_MEMORY_OPERATOR_DISPLAY_NAME` on the `arcadedb-mcp` service, so the knob Phase 45
   shipped a reader for could not be set from the deployment at all. Fixed in `f104d2dc2`;
   MEM-04 then passed.

A third was caught by Task 1's gate matrix: 45-03's `replayedMarker` broke five
`db_integration` gateway tests, invisible to 45-03's own verification because that ran
under WSL without the tag.

## Automated gates

| Gate | Result |
|---|---|
| `make quality` | green, 0 lint issues |
| `govulncheck` | green after the go1.26.6 bump (was 7 stdlib CVEs) |
| `coverage_docker.sh` | 85.8% (floor 85%) |
| `make agent-memory-eval` | PASS, MRS 100.00 |
| Mutation spot-check | 100% / 100% / 77.8% (floor 70%) |
| `go test -race ./internal/agent/` | green (WSL, 29.8s) |
| Quality snapshot gate | `ok: … checked 3 row(s)` |
| Pre-push hooks | build, deadcode, tagged-tier-compile all green |

## CI

`ci.yml` triggers only on `master`/`main`/`tabula-rasa`, so the push to
`gsd/phase-45-harness-correctness` started no run. **`master` is currently RED**
(run `31875818698`): `TestTwoIdentityCrossDeny/documents_cross_deny` fails in `cmd/aura`.
Attribution checked — that test file's recent history is entirely document-plane work
(`53391eb6d`, `4426c4b6a`, `7dd7ca6ac`) and Phase 45 touched no document-plane code. It is
NOT this phase's regression, but it does mean the branch cannot be validated by CI in its
current state, and phase-45 work is already merged into that red master.

## Recommendation

Do not mark Phase 45 complete. Close Gap 1 (HARN-03) first — it is the phase's central
claim and the only one whose absence undermines the rest. Gaps 2 and 3 are candidates for
a documented waiver rather than more work, since one target does not exist and the other
depends on provider behaviour, but that is the operator's call, not this document's.
