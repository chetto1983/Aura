# Phase-CTX Source Audit

**Role:** source evidence for the Phase-CTX closure slices.
**Status:** source-audited for US-CTX-08 as of 2026-05-24.
**Planning lane:** English technical record.

## Local Aura Evidence

| Source | Decision Supported | Adopt | Reject / Defer | Status |
| --- | --- | --- | --- | --- |
| `PRD.md` Phase-CTX row and context rules | Phase-CTX is the active next phase and context is runtime state, not source of truth. | Keep compaction as runtime prompt-shaping, backed by metrics and artifacts. | Do not treat active context as durable memory or audit history. | adopted |
| `.planning/aura-deep-refactor-decisions.json` ADR-012 context primitives | Clearing, compaction, memory, and swarm context sharding are separate layers. | Benchmark compaction independently from payload summarizer and retrieval. | Do not claim retrieval or memory wins from compaction-only metrics. | adopted |
| `.planning/aura-deep-refactor-decisions.json` ADR-034 observability | Trace metadata and payload artifacts are separate. | US-CTX-08 event rows store metadata and redacted previews only. | Do not store raw prompt/tool payloads as default trace rows. | adopted in US-CTX-08 |
| `scripts/ralph/prd.json` US-CTX-06..08 | Closure queue after substrate stories. | Translate queue requirements into this phase folder. | Do not let `scripts/ralph/prd.json` remain the only executable plan. | adopted |
| `internal/conversation/auto_compact.go` and tests | US-CTX-06 behavior exists in code. | Treat robustness as shipped and focus US-CTX-07 on benchmark evidence. | Do not re-open prefix/hysteresis logic without a failing fixture. | adopted |
| `cmd/quality_bench/main.go` and `docs/quality-bench/README.md` | Existing Aura benchmark artifact style. | Reuse JSON artifact plus snapshot table pattern. | Do not invent a disconnected bench output location. | adopted |
| `docs/aura-quality-snapshot.md` | Product quality rows are append-only. | Add a new Phase-CTX section with immutable run rows. | Do not edit past wave rows. | adopted |
| `internal/conversation/compaction_events.go` and `internal/api/conversations.go` | Per-event compaction observability must stay metadata-only while still debuggable by conversation turn. | Use `conversation_compactions` keyed by `chat_id + turn_index`; API adds the archive `conversation_id` at read time. | Do not block the hot compaction path on raw transcript persistence or conversation-row id availability. | adopted in US-CTX-08 |

## D:/tmp Example Sweep

| Example Path | What Was Inspected | Aura Adopts | Aura Rejects / Defers | Destination |
| --- | --- | --- | --- | --- |
| `D:/tmp/codex/codex-rs/config/src/config_toml.rs` | `model_context_window`, `model_auto_compact_token_limit`, and limit scope config. | Keep model-window and limit-scope explicit in benchmark inputs. | Do not hardcode one threshold without per-model evidence. | `benchmark.md` model matrix |
| `D:/tmp/codex/codex-rs/core/src/session/turn.rs` | Pre-sampling and mid-turn auto-compaction with telemetry fields. | Benchmark both token limit behavior and post-compaction continuation quality. | Do not use "compaction invoked" as success without savings/quality metrics. | US-CTX-07 and US-CTX-08 |
| `D:/tmp/hermes-agent/cli-config.yaml.example` and `agent/agent_init.py` | Compression threshold, target ratio, protected head/tail, model-specific override. | Record threshold recommendations per model in bench JSON. | Do not tune threshold from intuition alone. | US-CTX-07 result schema |
| `D:/tmp/openhuman/tests/tokenjuice_integration.rs` | Deterministic fixture directory scan, sorted fixtures, expected-output comparison. | Build committed fixture JSON plus parser tests before live spend. | Do not rely on a single ad-hoc live conversation as benchmark data. | US-CTX-07A |
| `D:/tmp/codex/justfile` | `bench-smoke` runs benchmark targets once with `--test`. | Use smoke only to prove the bench harness starts. | Do not mark smoke as completion evidence. | `benchmark.md` precheck rows |
| `D:/tmp/nanobot/docs/configuration.md` | Per-model `contextWindowTokens` and fallback context-window behavior. | Model fixtures must name the model context window used for threshold math. | Do not assume fallback models share the primary window. | US-CTX-07 model matrix |

## Current 2026 Practice Sweep

| Source | Date Accessed | Decision Supported | Aura Adopts | Aura Rejects / Defers |
| --- | --- | --- | --- | --- |
| OpenAI, "Evaluation best practices" (`https://platform.openai.com/docs/guides/evaluation-best-practices`) | 2026-05-24 | Evals need objective, dataset, metrics, comparison, and continuous evaluation. | Bench fixtures include explicit objective, expected keyword, savings, latency, and quality fields. | No open-ended "the answer seems fine" grading for the first gate. |
| Anthropic, "Effective context engineering for AI agents" (`https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents`) | 2026-05-24 | Context engineering is iterative curation of all runtime tokens, not prompt text alone. | Treat compaction as a runtime context-management layer and benchmark it per turn/workload. | Do not load every historical artifact into context just because the window is large. |
| Chroma, "Context Rot: How Increasing Input Tokens Impacts LLM Performance" (`https://www.trychroma.com/research/context-rot`) | 2026-05-24 | Long context degrades with irrelevant/distractor content; LongMemEval-style conversations are more realistic than needle-only tests. | Include long-session, tool-heavy, and multimodal fixtures with expected keyword retention. | Do not use only NIAH-style literal retrieval as the compaction quality proxy. |
| NIST CAISI, "Towards Best Practices for Automated Benchmark Evaluations" (`https://www.nist.gov/news-events/news/2026/01/towards-best-practices-automated-benchmark-evaluations`) | 2026-05-24 | Benchmarks should be valid, transparent, and reproducible. | Commit fixture definitions and append snapshot rows; keep raw run JSON as the ground-truth artifact. | Do not report a benchmark result without command, fixture, threshold, and run artifact path. |

## Adopted Decisions

| Decision | Rationale | Owner |
| --- | --- | --- |
| Split US-CTX-07 into fixture harness first, then live snapshot run. | Fixture parser/table tests prevent wasting live OpenRouter calls on broken harness code. | `plan.md` implementation gates |
| JSON benchmark output is the durable artifact. | It can be checked by tests, diffed, and summarized into `docs/aura-quality-snapshot.md`. | `benchmark.md` B-CTX-07 rows |
| Quality gate uses keyword retention plus savings and latency. | Savings alone can destroy task memory; keyword retention alone can hide token waste. | US-CTX-07 |
| `recommended_threshold_pct` is reported, not automatically applied. | Phase-CTX should not mix measurement with threshold policy changes. | US-CTX-07B follow-up note |

## Missing / Blocked Sources

| Question | Status | Handling |
| --- | --- | --- |
| Live OpenRouter credentials for the bench run | resolved for 2026-05-24 run through runtime DB secret discovery; values were never printed | Future live benches must still skip or block cleanly when the key is absent. |
| Exact fixture contents | resolved in US-CTX-07A | Committed fixture JSON and parser tests are the reusable bench dataset. |
| Per-event compaction persistence schema | resolved 2026-05-24 | `conversation_compactions` stores run id, chat id, archive turn index, iteration, message/token counts, threshold/cumulative tokens, elapsed ms, timestamp, and redacted focus preview. |
