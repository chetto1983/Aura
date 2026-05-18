# QA Triage — 2026-05-18 — git_head: 3652fdf2

Phase 2 synthesis of `qa-coverage-tools.md` (Auditor-1) and `qa-coverage-channel-failure.md` (Auditor-2). Ordered by ROI: P0 first with rationale, then P1, then P2 in appendix. Phase 3 cap is 20 specs; the 27 P0 below are narrowed to top 20 with the remaining 7 deferred.

## Baseline numbers

| Metric | Value |
|---|---|
| Tools (incl. swarm + mcp family) | 24 |
| Cells (channel × failure mode) | 68 |
| Total surface | 92 |
| **probe_chat baseline pass rate** | **18/21 (85.7%)** |
| **Real-bug failures** | 1 (agent-note-roundtrip) |
| **Infra-skipped failures** | 2 (phase07d/e, fixture refuses live-container DB write) |
| Tools COVERED E2E | 5 |
| Tools PARTIAL E2E | 3 |
| Tools MISSING E2E | 16 |
| Cells COVERED | 4 |
| Cells PARTIAL | 14 |
| Cells MISSING | 31 |
| Cells N/A | 12 |
| Cells STATIC-INSUFFICIENT | 7 |
| Lint debt (golangci-lint v2.12.2) | 59 issues |
| Lint debt in tool packages | 2 (both unused in source.go) |
| **P0 gaps** | **27** |
| P1 gaps | 33 |
| P2 gaps | 28 |

## Top-20 P0 (Phase 3 spec queue)

Ordered by ROI = (user-visible impact × likelihood of regression in prod) / (effort to write probe).

### Tools — 8 P0

| # | ID | Gap | Why P0 | Adversarial? |
|---|---|---|---|---|
| 1 | US-QA01 | `agent_note` E2E fails: DB row missing after set (conversation_id='chat-cli') | **Real product bug in baseline** — production wiring | no (this IS the bug) |
| 2 | US-QA02 | `execute_code` (sandbox-exec) — zero probe coverage | Largest sandbox blind spot; code execution risk | yes (malformed code) |
| 3 | US-QA03 | `execute_shell` (sandbox-exec) — zero probe coverage | Same | yes (shell injection attempt) |
| 4 | US-QA04 | `subagent_dispatch` (read-only but sandbox-adjacent) — zero probe coverage | Cross-agent delegation untested | yes (parent context leak) |
| 5 | US-QA05 | `run_aurabot_swarm` (sandbox-exec, capability-gated) — zero probe coverage | Swarm spawn untested | yes (orphan child run) |
| 6 | US-QA06 | `spawn_aurabot` (capability-gated) — zero probe coverage | Same family | yes (delegation deny case) |
| 7 | US-QA07 | `ocr_source` (external-API mistral) — MISSING probe | High-value tool; ext API failure path untested | yes (mistral down) |
| 8 | US-QA08 | `ingest_source` (storage-write, multi-LLM) — PARTIAL only | Composite ingest pipeline untested | yes (corrupt source) |

### Channel × failure — 12 P0 (selected from 19 total)

| # | ID | Cell | Why P0 | Adversarial? |
|---|---|---|---|---|
| 9 | US-QA09 | (telegram, LLM rate-limited 429) — STATIC-INSUFFICIENT | User-visible 429 UX (fallback string?) — needs live probe | yes |
| 10 | US-QA10 | (telegram, MaxElapsed 5min) — STATIC-INSUFFICIENT | Wrap-up wording: cap-acknowledge vs pretend-complete | yes |
| 11 | US-QA11 | (telegram, MaxIterations) — PARTIAL | finalizeAnswerAfterBudget only fires when AllowNoToolFinalization on | yes |
| 12 | US-QA12 | (telegram, empty LLM response) — MISSING | Common LLM degradation path | yes |
| 13 | US-QA13 | (web, LLM 429) — MISSING | API client gets cryptic 503? Need to verify | yes |
| 14 | US-QA14 | (web, empty LLM) — MISSING | Same as telegram but on /api/chat | yes |
| 15 | US-QA15 | (telegram, qdrant outage) — MISSING | Memory tools degrade — UX? | yes |
| 16 | US-QA16 | (telegram, embedding sidecar down) — MISSING | search_memory + tool_search both fail | yes |
| 17 | US-QA17 | (telegram, phantom tool detection) — STATIC-INSUFFICIENT | Edit cleanup of phantom text | yes |
| 18 | US-QA18 | (web, capability deny) — PARTIAL (unit only) | Already covered at unit level; promote to E2E | yes |
| 19 | US-QA19 | (cron, MaxIterations / MaxElapsed) — MISSING | Silent channel must log + record run failure | yes |
| 20 | US-QA20 | (swarm, child delegated authz failure) — MISSING | Parent must escalate vs silent fail | yes |

**Total: 20 specs queued for Phase 3.**

## Deferred to next QA run (7 remaining P0)

These are real P0 but bumped due to Phase 3 cap. Carried over to next run.

- (cron, LLM transient network error) — MISSING
- (swarm, sandbox unavailable) — MISSING
- (telegram, sandbox unavailable) — MISSING
- (web, capability deny on api.runs / api.audit) — MISSING
- (telegram, tool-call arg parse error) — STATIC-INSUFFICIENT
- (web, MaxIterations finalize) — STATIC-INSUFFICIENT
- (cron, duplicate tool call dedup) — MISSING

## Adversarial ratio check (rule 4 — ≥30%)

Of the 20 P0 specs above, **19 are adversarial** (the 1 exception is US-QA01 which IS the bug — not synthetically adversarial but a real failure to verify). Ratio = 19/20 = 95%. **Well above the 30% floor.**

## P1 inventory (33 total — bullet summary)

- 5 P1 in tools (e.g., `tool_search`, `recall_user_memory` E2E missing)
- 26 P1 in channel × failure (cron/swarm dominate — 10 MISSING each — most are operational-visibility gaps)
- All 33 listed in `docs/qa-coverage-tools.md` and `docs/qa-coverage-channel-failure.md`

## P2 inventory (28 total)

- 9 P2 in tools (read-only tools with only-reply assertions)
- 19 P2 in channel × failure (PARTIAL cells or low-impact N/A)
- Appendix only; not queued for Phase 3

## Spot-check evidence (rule 3 — orchestrator re-verifies auditor classifications)

Verified 3 rows from each auditor's matrix against codebase:

**Auditor-1 (tools)**:
- `agent_note` PARTIAL E2E → `cmd/probe_chat/cases.go:746` does `env.DB.QueryRow(SELECT content FROM agent_notes WHERE conversation_id='chat-cli')` — that IS a ground-truth assertion. Classification revised from PARTIAL to COVERED-FAILING (case is COVERED structurally; the FAIL is the bug, not the coverage gap). ✓
- `web` PARTIAL → `cases.go:122-149` Verify only checks `r.Reply` substrings. Confirmed PARTIAL. ✓
- `execute_code` MISSING → grep cmd/probe_chat for "execute_code" returns no Case match. Confirmed MISSING. ✓

**Auditor-2 (channel × failure)**:
- (swarm, capability deny) COVERED → `internal/chat/hub_swarm_test.go:170-198` (`TestHubSwarm_NoGenericGrantDenied`). Opened, confirmed authz_decisions row asserted. ✓
- (web, capability deny) COVERED → `internal/api/chat_test.go:102-120` (`TestChatBearerMissingGrantDenied`). Opened, confirmed HTTP 403 + DB row. ✓
- (cron, capability deny) COVERED → `internal/channels/cron/dispatcher_test.go:215-231`. Opened, confirmed `ErrUnauthorized` test. ✓

All spot-checks pass.

## Phase 3 architect dispatch context (for next phase)

When Phase 3 fires:
- 20 specs to design (≤ rule cap)
- 19 adversarial / 1 bug-reproduction (≥ 30% floor satisfied)
- Mix: 8 tool-focused + 12 channel×failure-focused
- All have `currently_passing: false` or `STATIC-INSUFFICIENT`
- Token-baseline: per-case elapsed_ms from baseline-run.json; for new specs (no baseline), default `NO-BASELINE — set after first green run`

## Recommendation

**Highest-ROI single fix to land FIRST** (before Phase 3 architect run): **US-QA01 / agent-note-roundtrip**. It's the only real product bug in the baseline. Triage with `gsd-debugger` to find the conversation_id mismatch root cause; fix; re-baseline. This unblocks all subsequent agent_note coverage and makes the baseline 19/21 = 90.5%.

Phase 3 can then proceed on the remaining 19 P0 specs.
