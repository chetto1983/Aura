# QA Triage - 2026-05-19 - git_head: c3b1f0085533aac961f37ce4d2d362146b4c205c

Phase 2 synthesis of `docs/qa-coverage-tools.md` and `docs/qa-coverage-channel-failure.md`.

Baseline: `.planning/qa/baseline-run.json` is 30/30 PASS. The run used a fresh bearer token minted into `api_tokens` for allowlisted user `1148481707`; the plaintext is stored only in `.planning/qa/token.txt` for this local QA run. `.planning/qa/run-head.txt` is pinned to `c3b1f0085533aac961f37ce4d2d362146b4c205c`.

## Baseline Numbers

| Metric | Value |
|---|---:|
| Probe cases | 30 |
| Passing | 30 |
| Failing | 0 |
| Total tokens | 1,278,025 |
| Sum elapsed_ms | 383,785 |
| Tool P0 gaps | 11 |
| Tool P1 gaps | 1 |
| Channel/failure P0 gaps | 0 |
| Channel/failure P1 gaps | 0 |

## P0 Gaps

All P0s below come from `docs/qa-coverage-tools.md`. The channel/failure matrix has no P0/P1 cells in this run, but many P2 partials are listed in the appendix.

| ROI | Story ID | Gap | Rationale |
|---:|---|---|---|
| 1 | US-QA22 | `request_dashboard_token` E2E missing | Secret issuance path needs hashed DB row, out-of-band delivery, and sanitization ground truth. |
| 2 | US-QA23 | `propose_patch` E2E missing | Memory/wiki proposal writes are high-risk for memory poisoning and governance regressions. |
| 3 | US-QA24 | `ask_user` E2E missing | Pause/resume semantics are core Q&A behavior; unit sentinel coverage is not enough. |
| 4 | US-QA25 | `subagent_dispatch` E2E partial | Baseline green uses infra-skip with zero successful `tool_attempts` rows for `subagent_dispatch`. |
| 5 | US-QA26 | `run_aurabot_swarm` E2E missing | Full one-shot swarm run remains uncovered despite swarm being a major execution surface. |
| 6 | US-QA27 | `list_swarm_tasks` E2E partial | Lifecycle probe asks for it, but ground truth only proves task completion, not list output. |
| 7 | US-QA28 | `read_swarm_result` E2E partial | Lifecycle probe asks for it, but no `tool_attempts`/result-body assertion proves the read path. |
| 8 | US-QA29 | `dev_tool` E2E missing | Tool management can mutate local tool scripts; live safety coverage is absent. |
| 9 | US-QA30 | `tool_search` E2E missing | Tool retrieval/routing is untested through the live agent loop. |
| 10 | US-QA31 | `daily_briefing` E2E missing | Read-only but highly compositional; needs live artifact/section assertions. |
| 11 | US-QA32 | `web` E2E partial | Existing web-fetch probe verifies reply shape, not fetched bytes or durable evidence. |

## P1 Gaps

| Story ID | Gap | Rationale |
|---|---|---|
| US-QA33 | `source` unit PARTIAL | Source E2E is strong, but no unit test directly instantiates unified `SourceTool.Execute`. |

## P2 Appendix

Tool P2 rows are currently covered enough for this QA run: `file`, `doc`, `wiki_page`, `task`, `search_memory`, `recall_operational`, `recall_user_memory`, `agent_note`, `execute_code`, `execute_shell`, and `spawn_aurabot`.

Channel/failure matrix: 72 cells total, 4 COVERED, 59 PARTIAL, 0 MISSING, 9 N/A. The PARTIAL set is not green in a deep sense; it means lower-level unit/probe coverage exists, while channel-specific fault injection is still missing. These are deferred because Phase 3 should first close the P0/P1 tool gaps.

Highest-value deferred P2 families:

- Telegram user-visible failure UX for LLM 429, empty response, phantom tool, MaxElapsed, and MaxIterations.
- Web outage probes for Qdrant, embedding sidecar, SearXNG, Mistral OCR, MCP, and LLM hard-fail paths.
- Cron silent-job observability for budget caps, malformed tool calls, duplicate dedup, and retry exhaustion.
- Swarm child-run failure collection for sidecar outages, 429/transient LLM failures, phantom tool, and dedup.

## Spot Checks

The orchestrator spot-checked representative rows against source and baseline evidence:

- `daily_briefing` P0: `internal/agent/tools/registry/daily_briefing.go:83` has `Execute`; unit coverage exists in `daily_briefing_test.go`, and no `cmd/probe_chat` case references `daily_briefing`.
- `subagent_dispatch` P0: `cmd/probe_chat/cases.go:925` defines the case; `.planning/qa/baseline-run.stderr.log` records `tool_attempts subagent_dispatch ... count=0` and `INFRA-SKIP`.
- `run_aurabot_swarm` P0: unit Execute coverage exists in `internal/agent/tools/swarm/tools_test.go`, while `cmd/probe_chat` only covers `spawn_aurabot` lifecycle.
- `web` P0: `cmd/probe_chat/cases.go:122` defines `web-fetch-summarize-context-engineering`; it asserts reply/tool shape, not fetched source bytes.
- Web capability-deny channel/failure COVERED: `internal/api/chat_test.go:104` plus `cmd/probe_chat/cases.go:1112`; baseline stderr shows HTTP body and `authz_decisions` deny preview.
- Cron capability-deny channel/failure COVERED: `internal/cron/dispatch_test.go:261` verifies a denied `cron.run` authz decision.

## Deferred to Different Harness

| Story ID | Gap | Reason |
|---|---|---|
| US-QA22 | `request_dashboard_token` E2E missing | `cmd/probe_chat` cannot observe the Telegram out-of-band token delivery channel; needs Telegram harness or injectable token sender. |
| US-QA24 | `ask_user` E2E missing | Core pause/resume behavior is Telegram/resume-channel oriented; synchronous `/api/chat` probe cannot complete the user-answer roundtrip. |
| US-QA25 | `subagent_dispatch` E2E partial | Current web probe reaches an infra-skip path with no successful `tool_attempts`; needs Telegram/AURABOT harness or direct runtime fixture. |

## Phase 3 Queue

Proceed to Phase 3 test design for US-QA23, US-QA26 through US-QA33. US-QA22, US-QA24, and US-QA25 are deferred to a different harness. That leaves 9 probe-chat specs, below the Phase 3 cap of 20.

Adversarial ratio target: at least 4 of 12 specs must be adversarial. Recommended adversarial specs: US-QA22 token delivery failure/sanitization, US-QA23 malicious memory proposal, US-QA24 invalid resume answer, US-QA25 missing Telegram/AURABOT context, US-QA29 unsafe dev_tool path, and US-QA32 fetch failure/blocked host.

## Phase 2 Verdict

PASS. Phase 2 stop condition is met: every P0 gap is listed with rationale and story ID, and the live baseline signal was captured before classification.
