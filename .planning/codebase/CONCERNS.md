# Codebase Concerns

**Analysis Date:** 2026-05-28

> Aura is a **tabula-rasa rewrite** as of 2026-05-27. The on-disk repository is a ~633 LOC Go skeleton plus a 4401-line PRD (`prd.md`) that specifies 16+ slices. Concerns split into two distinct sources:
> 1. **Current skeleton gaps** — what is stubbed today, blocking the next step.
> 2. **PRD-anticipated concerns** — open questions, pre-merge benchmarks, security/perf design constraints, fragile areas the rewrite explicitly knows about.
>
> Pre-rewrite tech debt (`pre-rewrite-2026-05-27` git tag) is intentionally *not* the working tree; it is referenced only for "patterns we are deliberately not repeating."

---

## Current Implementation Gaps (what's stubbed / missing)

The four-component scope (agent loop / KV cache / sandbox / swarm) is wired with stubs that compile but do nothing meaningful end-to-end.

### LLM client — stub returns canned text

- File: `cmd/aura/main.go` (lines 74-90), `internal/llm/client.go`
- Issue: `stubClient.Stream` emits one hardcoded `text_response` tool call with literal text `"hello from tabula-rasa stub..."`. No SSE parser, no OpenAI-compat wire, no provider routing.
- Impact: `aura chat hello` runs but only echoes the stub. No real model interaction. Blocks all other slices that need an LLM.
- Fix approach: Slice 1 — implement `internal/llm/openai_compat/client.go` (~280 LOC), SSE parser, tool-call delta-merge by `index`, `goleak.VerifyNone` discipline on ctx-cancel (per `prd.md:571`).

### Agent loop — minimal terminal logic, no Slice 0.9 abstraction

- File: `internal/agent/loop.go` (131 LOC)
- Status: `Loop.Turn` implements MaxSteps + tool dispatch + `text_response` terminator. Single `runTool` returns `(string, error)` — no `ToolResult` pattern yet.
- Issue: `Tool.Execute` returns plain `string` (see `internal/agent/tools/spec.go:32`). Slice 1 must change signature to `(ToolResult, error)` with preview+sidecar; touching this file later forces refactor of `text_response.go`, `search.go`, `tool_search`.
- Missing: `Agent` interface + `iter.Seq2[*Event, error]` streaming (Slice 0.9), `LlmAgent` rename, `Loop.Cancel`, `PausedState` machinery (Slice 1.5), `MaxSteps` documentation embedded in system prompt for cache stability.
- Impact: Every downstream slice (1, 1.5, 3, 6, 8, 10) re-touches this file. Refactor-on-touch will be heavy.

### Sandbox — stub returns errors

- File: `internal/sandbox/sandbox.go` (36 LOC)
- Status: `Stub.RunPython` / `Stub.RunShell` return `"sandbox.Runner not yet implemented — see sandbox slice"`.
- Impact: `execute` tool not registered yet in `cmd/aura/main.go::buildRegistry`. No Python/shell snippet execution possible.
- Fix approach: Slice 2a (Docker sidecar, seccomp default-deny, ulimit, `network_mode: none`, tmpfs) → Slice 2b (session-bound + workspace mount + DNS-resolved iptables allowlist).

### Swarm — stub returns errors

- File: `internal/swarm/swarm.go` (42 LOC)
- Status: `Stub.Spawn`, `Stub.Talk`, `Stub.Join` all return "not yet implemented." `MaxSpawnDepth=3` constant declared.
- Missing: shared bus channel, DM-by-ID routing, tier→model mapping (`chat|reasoning|worker`), `RejectingResponder` default for swarm children (so child `ask_user` auto-rejects with `<auto-rejected: child loop has no human responder>`), `SpawnInteractive` for explicit pause propagation, `Coordinator.ResumeChild` (mutex-protected children map per audit round 2 P0).
- Impact: No parallelism, no agent_job sub-loop spawning (blocks Slice 6 `agent_job` handler).

### LLM Client interface — type defined, no impl

- File: `internal/llm/client.go` (78 LOC)
- Status: `Client` interface + `Message`/`ToolCall`/`ToolDef`/`Chunk`/`Request` types defined. Cache-friendliness disclaimer in doc comment (lines 70-72). No concrete impl yet.
- Missing: provider headers injection (OpenRouter `HTTP-Referer` + `X-Title`), connect/total timeout config (10s dial / 120s total per `prd.md:627`), zero-allocation contract on `req.Messages` mutation, `ToolsCacheControl` field for Anthropic (Slice 4 OQ3).

### No persistence layer

- Missing entirely: `internal/db/` (Postgres + sqlc + pgx + golang-migrate). No `aura` schema, no migrations.
- Missing entirely: `internal/db/migrations/neo4j/` (Cypher migrations). No `aura-neo4j` container, no MCP `mcp-neo4j-cypher` config.
- Blocks: Slice 1.5 (`aura.paused_states`), Slice 1.7 (`aura.identities` + `aura.capability_grants`), Slice 1.8 (`aura.conversations` + `aura.conversation_turns`), Slice 6, Slice 7c audit, every memory slice.
- 14 Postgres migrations + 2 Neo4j Cypher migrations expected (0001-0014).

### No tests anywhere

- Zero `*_test.go` files in the tree. No `TestMain` with `goleak.VerifyNone`. No `testdata/`. No build tags `db_integration` / `sandbox_integration` / `multimodal_integration` / `onboarding_integration`.
- Impact: Hard requirements from `prd.md:1447` (Test discipline rigorosa, 10 hard reqs incl. coverage ≥75% unit / ≥60% integration) are not yet meetable. Slice 1 must land `TestMain` + first fixtures.

### Tool registry — only 2 tools

- File: `cmd/aura/main.go::buildRegistry` (lines 48-53)
- Registered: `TextResponse` (non-deferred) + `ToolSearch` (non-deferred hook).
- Missing: `execute` (Slice 2 sandbox), `swarm.spawn`/`swarm.talk`/`swarm.join` (Slice 3), `ask_user` (Slice 1.5), `read_tool_output` (Slice 1 builtin), `web_search`/`web_fetch` (Slice 5), `task.*` (Slice 6), `skill.*` (Slice 7), `ingest.*` (Slice 11), `memory.search` (Slice 11d).

### CLI subcommands stubbed

- File: `cmd/aura/main.go::main` (lines 36-37)
- Status: `aura shell` and `aura serve` print `"TODO: implemented by the agent-loop and CLI slices"`.
- Impact: No long-lived runtime, no REPL. Channel framework (Slice 9) and AG-UI gateway (Slice 8) ungrounded.

---

## Open Questions Pre-Merge (~28 totali distributed by slice)

Each item: `[slice] question — current default | decision-priority`. *CRITICA* = blocking, must close before slice can land. *Default-OK* = decision provisionally chosen, can ratify at DoR gate.

### Slice 0.9 — Agent runtime abstraction (3 OQ)

1. **`InvocationContext.Branch` shape** — string vs nested struct. Default: free-form string. Default-OK.
2. **`Actions.StateDelta` merge semantics** — shallow vs deep merge. Default: deep merge via `jsonpatch` (RFC 6902). Default-OK.
3. **`ParallelAgent` backpressure** — synchronous ackChan vs buffered N. Default: synchronous (rubato da adk-go). Default-OK.

### Slice 2 — Sandbox (3 OQ, lower-criticality after audit 2026-05-28)

1. **Sidecar implementation language** — Python stdlib vs Go single-binary. Default: Python stdlib. Default-OK.
2. **DNS cache TTL allowlist** — `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` DNS resolve cache. Default: 5 min. **Pre-merge validation needed**: `pypi.org` rotates A records — does cache invalidate legitimate calls? (`prd.md:1146`). Pre-merge benchmark.
3. **Sandbox session vs swarm Coordinator child** — riusare il container session della parent conversation o creare nuovo per child. Default: stesso session, isola solo se RISKY tier. Default-OK.

### Slice 4 — KV cache builder (3 OQ)

1. **Cache stats persistence** — in-memory vs flush to file. Default: in-memory. Default-OK.
2. **Tools cache-control breakpoint** — separate `Request.ToolsCacheControl` field for Anthropic. Default: yes (add field). Default-OK.
3. **Threshold cache_hit_rate per CI** — fail under X%? Default: no — assert byte-identity invariant, not percentage (provider-dependent, flaky). Default-OK.

### Slice 7 — Skills (1 OQ aperta)

1. **Skill versioning** — out of scope, audit log allows manual rollback. Default-OK.
   - (OQs 1, 2, 3, 5 closed pre-merge; only versioning aperta.)

### Slice 7e — Snippets + pattern analysis (4 OQ, differite post-benchmark)

1. **Synth LLM call cost** — ~$1.5/mese tier=reasoning. Default: accept. Default-OK.
2. **Cross-identity snippet sharing** — privato per `identity_id` vs library globale. Default: privato. Default-OK.
3. **Snippet versioning on update** — versioning implicito via `content_hash` in `skill_audit`. Default-OK.
4. **Workspace files cleanup** — scope conversation, cascade su `aura chat delete`. Default-OK.

### Slice 8 — AG-UI gateway (4 OQ post-benchmark)

1. **Endpoint path canonical** — `/agent/run`. Default-OK.
2. **WebSocket transport** — SSE only Slice 8; WebSocket future. Default-OK.
3. **Cost streaming via STATE_DELTA** — JSONPatch per turn. Default-OK.
4. **CLI default mode (in-process vs via-agui)** — **Decisione differita post-benchmark**: measure HTTP loopback latency ~50-150 ms. Pre-merge benchmark needed before Slice 9 closes.

### Slice 9c — Multimodal (2 OQ pre-merge)

1. **Variant Gemma 4 finale** — E2B vs E4B vs 26B MoE vs 31B. **Pre-merge benchmark CRITICO** su corpus IT/EN: STT accuracy + image description + latency + RAM. Default baseline E4B Q4_0 (~3 GB RAM).
2. **Vision fallback markitdown OCR** — necessary or not. Pre-merge benchmark decides. If Gemma quality basta → solo vision sidecar; else → markitdown OCR fallback if Gemma down.

### Slice 10 — Onboarding + Agent.md (4 OQ)

1. **`AURA_PROFILE_CERTAINTY_N=3`** — pre-merge calibrate su corpus di test. Default: 3, env override. Pre-merge benchmark light.
2. **Conflicting facts handling** — keep current + log conflict; if 3+ conflict → ask_user. Default-OK.
3. **Schema versioning Agent.md** — `metadata.json.schema_version` int + migrations. Default-OK.
4. **Privacy / `/forget --all` GDPR** — out of scope, future multi-user. Default-OK.

### Slice 11 — Memory (6 OQ)

1. **Chunk size 512 vs 1024 tokens** — **Pre-merge benchmark** su corpus tipo (papers + libri + note). 512 più precise per Q&A; 1024 più context per summarization. Pre-merge benchmark CRITICO.
2. **Entity type taxonomy** — fissa (Person/Org/Location/Concept/Event/Topic) vs dynamic. Default: fissa. Default-OK.
3. **Re-ranker cost** — ~$0.001/query × 100/giorno = $3/mese. Default: accept. Default-OK.
4. **Memify prune threshold** — 90gg + < 3 mention. Default-OK, configurable.
5. **Agent insight injection top-K** — top-3 → +500 token/turn; relevance > 0.7 threshold. Default-OK.
6. **Multi-user refactor cost** — stimato +800 LOC se atterra post-Slice 11. Default: accept single-user MVP. Default-OK.

### Slice 13 — Local LLM fallback (5 OQ, una CRITICA)

1. **⚠️ CRITICA — GPU vs CPU** (`prd.md:3651-3660`): mini-PC target ha GPU dedicata?
   - SE GPU → Slice 13 vLLM+LMCache OK.
   - SE CPU-only → vLLM CPU mode è **5-10x peggio di llama.cpp CPU** per stesso modello. Activate path **13-bis** (riusa `aura-llama-multimodal` Slice 9c per chat fallback, no LMCache).
   - **Pre-merge benchmark CRITICO**: vLLM Gemma 3 12B Q5 latency p50/p95 + tokens/sec vs llama.cpp Gemma 4 E4B Q4 baseline, prompt 1000-token. **SE < 5 tok/sec → switch a 13-bis** (`prd.md:3833`).
2. **Modello fallback** — Gemma 3 12B vs Llama 3.1 8B vs Qwen 2.5 7B. **Pre-merge benchmark** su corpus IT (utente italiana) + EN code. Quality/size trade-off.
3. **LMCache config tuning** — `chunk_size` 256 vs 512 per `max_model_len 8192`. Post-merge test, no block.
4. **Cost threshold default** — `$1/day` per single-user MVP. Default-OK.
5. **STATE_DELTA reactive on offline-switch** — Notifier proactive Telegram message. Default: SÌ via Notifier alert. Default-OK.

### Slice 0.9, 1.7, 5, 6 — minor OQs (all closed or default-OK)

- Slice 5 (Web tools): SearXNG self-hosted vs cloud (default self-hosted), `go-readability` mantenuto (default yes).
- Slice 6: Notifier default impl (stdout printf default), `agent_job_runs` retention (forever, escape hatch CLI `task runs purge`).

---

## Pre-Merge Benchmarks Required

Five benchmarks gate slice merges. Each is a **DoR ✅ checkbox** per `prd.md:1501`.

### Slice 13b — CRITICAL: vLLM CPU vs GPU

- **Decisione**: vLLM Gemma 3 12B Q5 latency p50/p95 + tokens/sec on 1000-token prompt vs llama.cpp Gemma 4 E4B Q4 baseline.
- **Action threshold**: SE vLLM CPU < 5 tokens/sec → **scrap Slice 13b, activate Slice 13-bis** (riusa `aura-llama-multimodal` Slice 9c per chat fallback, no new sidecar, no LMCache, save ~5 GB RAM).
- **Mini-PC scenario impact**: GPU scenario = +1 GB RAM + 7 GB VRAM; CPU scenario = +7 GB RAM (tight su 16 GB, OK su 32 GB); 13-bis scenario = zero new sidecar.
- File: `prd.md:3651-3660`, `prd.md:3833`, `prd.md:3918`.

### Slice 11b — Chunk size 512 vs 1024 tokens

- **Corpus**: papers + libri + note.
- **Metric**: Q&A precision (favors 512) vs summarization context (favors 1024).
- **Default**: 512 (`AURA_MEMORY_CHUNK_SIZE_TOKENS=512`, `AURA_MEMORY_CHUNK_OVERLAP_TOKENS=64`).
- File: `prd.md:3461`.

### Slice 9c — Gemma 4 variant selection

- **Variants to benchmark**: E2B (small), E4B (default baseline), 26B MoE (heavier), 31B (largest).
- **Metric**: STT accuracy IT/EN + image description quality + latency + RAM consumption (~3 GB E4B baseline).
- **Decision impact**: variant influences `MULTIMODAL_MODEL` env + compose service resource limits.
- File: `prd.md:2495`, `prd.md:2723`.

### Slice 11d — Re-ranker cost-quality trade-off

- **Cost stima**: `$0.001/query × 100/giorno = $3/mese` LLM tier=worker.
- **Latency**: 200-500 ms per query at retrieval time.
- **Metric**: NDCG@5 ≥ 0.8 on eval corpus vs no-rerank baseline.
- **Decision**: accept default (single-user MVP) or off-by-default with per-conv flag.
- File: `prd.md:3463`.

### Slice 8 — CLI default mode

- **Latency to measure**: HTTP loopback roundtrip in-process vs via-agui (~50-150 ms expected).
- **Decision**: default in-process (zero overhead) confirmed pre-merge; `--via-agui` opt-in.
- File: `prd.md:2394`.

---

## Security Concerns

### Sandbox seccomp default-deny (Slice 2a)

- Status: not implemented yet — current `Stub` returns errors.
- Requirement: Linux seccomp profile default-deny, ulimit `nofile=64`, `cpus=1.0`, `mem=512m`, `network_mode: none`, tmpfs `/tmp`, read-only rootfs.
- Audit test: `aura exec python "import socket; socket.socket().connect(...)"` must fail with **EPERM on `socket()` syscall** (not deeper — verify seccomp catches at syscall, not at higher level). File: `prd.md:1164`.
- Failure mode: if seccomp profile not loaded, sandbox executes arbitrary network code. Sanity-check post-impl via `nft list` for iptables + seccomp profile load verification.

### Risk-Based Governance (Slice 6 + 7, cross-cutting)

- Pattern: **Hybrid C — System computes tier, agent decides** (`prd.md:3981`).
- 4 tiers: `SAFE` / `NORMAL` / `RISKY` / `DESTRUCTIVE`.
- Mapping hard-coded in `internal/scoring/` (~100 LOC):
  - `reminder | backup_postgres | backup_neo4j` → SAFE
  - `agent_job` → NORMAL (DESTRUCTIVE if payload regex `\b(rm|delete|drop|purge|truncate)\b`)
  - `skill.create | skill.update | skill.install` → RISKY
  - `skill.delete` → DESTRUCTIVE
- Modifiers bump tier UP (saturate at DESTRUCTIVE): `every_minute|every_hour`, `silent:true`, `tier=reasoning` for agent_job, frequency increase > 10x.
- Anti-elevation razionale (`prd.md:4125`): 5 attack vectors covered (cron-costoso-non-rischioso, skill-prompt-injection, cron-irreversible, frequency-escalation, silent-cumulative-damage).
- Risk: if `internal/scoring/` mapping is wrong (false-negative), DESTRUCTIVE action proceeds silently. Audit log + Notifier IMMEDIATE alert + pending_approval state are the structural mitigations.

### Skill injection blocklist (Slice 7)

- Env: `AURA_SKILL_INJECTION_BLOCKLIST` (built-in patterns).
- Patterns: ChatML, Anthropic, Llama, Llama-3, DeepSeek, Gemma, Qwen literal blocklist (`prd.md:1962`).
- Cap: `AURA_SKILL_BODY_CAP_BYTES=32768` (32 KiB write-time refuse) — body > 32 KiB is "quasi certamente garbage o prompt-injection payload nascosto" (`prd.md:4164`).
- Single chokepoint: `internal/skills/paths.go::SanitizeName`. Static-analysis test asserts writer/deleter/installer all use it (`prd.md:1961`).

### Audit immutability via DB trigger

- Tables: `aura.skill_audit` (Slice 7c) + `aura.profile_audit` (Slice 10) + `aura.ingest_audit` (Slice 11).
- Function: `raise_audit_immutable()` + trigger `BEFORE UPDATE OR DELETE` rejects mutation (`prd.md:1931`).
- Constraint coherence (cross-slice symmetric):
  - `approval_source='ask_user'` ⇔ `paused_state_token IS NOT NULL AND gate_taken=true`
  - `approval_source='cli'` ⇔ `paused_state_token IS NULL AND gate_taken=true`
  - `approval_source='auto'` ⇔ `paused_state_token IS NULL AND gate_recommended=false`
- Risk if not enforced: agent or operator could rewrite audit history, masking RISKY/DESTRUCTIVE gate-skips.

### Capability grants — scaffolding only

- Status: Slice 1.7 scaffolds single-user `identity='local'` with wildcard `capability='*'`. Multi-user disabled.
- Risk: hardcoded wildcard is a known stub; production multi-user requires refactor (Slice 11 OQ6 estimates +800 LOC). Documented and accepted pre-merge.

### Telegram setup wizard — NO auth

- File: `prd.md:4216-4219`.
- `AURA_SETUP_BIND=127.0.0.1:9081` default loopback (safe).
- Override `AURA_SETUP_BIND=0.0.0.0:9081` for LAN setup — **no auth on the endpoint**. Headless container scenario only. Documented out-of-scope future slice; risk acknowledged.

### SSRF defense in web_fetch (Slice 5)

- DNS-rebinding protection: `safeDialContext` resolves host → validates IP against blocklist → dials explicitly on resolved IP, **does NOT re-lookup** between resolve and dial (`prd.md:1617`).
- Redirect interception (audit round 1 P0): `http.Client.CheckRedirect` custom re-validates every Location header against blocklist (`prd.md:1618`). Failure mode: `https://safe.example.com/r` → `http://169.254.169.254/` (AWS metadata) must reject at redirect step, NOT at first dial.
- Status: not implemented (skeleton has no `internal/web/`).

### Forensics symmetry across slices

- All audit tables must include identical columns: `approval_source` + `paused_state_token` (FK to `aura.paused_states.token` ON DELETE SET NULL) + `computed_risk_tier` + `gate_recommended` + `gate_taken`.
- Tables affected: `skill_audit`, `profile_audit`, `agent_job_runs`, `ingest_audit`.
- Failure mode: if a table lacks one column, cross-slice forensics queries break ("which RISKY actions had gate_taken=false in the last 24h?"). Audit round 2 P0 (`prd.md:1709`).

---

## Performance Considerations

### KV cache discipline (Slice 4 — foundational)

- Invariant: `Messages[0]` MUST be byte-identical across turns. System prompt baked at `NewLoop` time (`internal/agent/loop.go:42`).
- Manifest stable ordering: alphabetical by Name in `internal/agent/tools/manifest.go:37`. Per doc comment lines 19-21: "any reshuffle invalidates the provider-side prompt cache."
- Risk metric: `usage.prompt_cache_hit_tokens` from DeepSeek auto-cache (OpenAI-compat extension). Test asserts hit-rate > 0.7 from turn 2 onward (`prd.md:1384`).
- Cost amplifier: providers charge cached tokens at 10-90% of input rate. A poisoned cache silently 10x's the spend.
- Threshold: NOT enforced in CI (provider-flaky); only invariant byte-identity enforced (Slice 4 OQ4).

### LMCache disk-tier 50 GB (Slice 13b, GPU scenario)

- Path: `LMCACHE_LOCAL_DISK_PATH=/var/cache/lmcache`.
- Max size: `LMCACHE_MAX_LOCAL_DISK_GB=50`.
- Chunk size: 256 tokens (tunable for long-context).
- Acceptance: KV cache hit ratio > 30% turn 2-5 on long-context (>4K token prompt) (`prd.md:1481`).
- Risk: requires NVMe; HDD spinning rust will not meet TTFT reduction promises.

### Mini-PC RAM peak

- File: `prd.md:3933-3936`.
- Scenario baseline (no Slice 13): ~7 GB on 32 GB target (Neo4j heap ~1.5-2 GB + embed sidecar ~600 MB + Postgres + Go runtime + multimodal sidecar ~3 GB).
- Scenario vLLM CPU (Slice 13b): **~14 GB** (tight on 16 GB, OK on 32 GB).
- Scenario vLLM GPU: +1 GB RAM overhead + 7 GB VRAM on dedicated GPU.
- Scenario 13-bis (CPU-only fallback): invariato, riusa Gemma 4 E4B already in RAM.

### Re-ranker LLM cost

- Tier: `worker` (cheapest).
- Cost: ~$0.001/query.
- Latency: 200-500 ms added to retrieval.
- Volume: 100 queries/day expected single-user → $3/month. Acceptable.
- Knob: `AURA_MEMORY_RERANK_TOP_K_IN=20`, `AURA_MEMORY_RERANK_TOP_K_OUT=5` (`prd.md:4297-4298`).

### Background goroutines (concurrent timers)

All run in `aura serve` process. Total ~50 MB heap aggregate per Slice 11 budget (`prd.md:3470`).

| Interval | Goroutine | Slice | Env |
|---|---|---|---|
| 24h | Memify post-processing (prune/strengthen/derive) | 11e | `AURA_MEMORY_MEMIFY_INTERVAL_HR=24` |
| 24h | Leiden community detection | 11c | `AURA_MEMORY_COMMUNITY_INTERVAL_HR=24` |
| 24h | Skill TTL sweep (90-day idle archive) | 7e | `AURA_SKILL_TTL_SWEEP_INTERVAL_HR=24` |
| 60min | Skill pattern analyzer (cluster autosuggest) | 7e | `AURA_SKILL_PATTERN_ANALYSIS_INTERVAL_MIN=60` |
| 60min | Memory insight analyzer (cross-conv pattern) | 11e | `AURA_MEMORY_INSIGHT_INTERVAL_MIN=60` |
| 30s | Offline detector (TCP probe to `AURA_LLM_BASE_URL`) | 13 | `AURA_LLM_OFFLINE_DETECTION_INTERVAL_SEC=30` |
| 30s | Cron scheduler tick loop (DueTasks dispatch) | 6 | — |
| 1h | Pending-skills stale cleanup (24h TTL) | 7c | — |

Risk: goroutine leak guard required everywhere (`goleak.VerifyNone(t)` in TestMain, per `prd.md:1455` hard req #3). Ctx-cancel propagation required end-to-end (HTTP connection close on Ctrl+C, `prd.md:571`).

### Context budget formula

- File: `prd.md:4187-4195`.
- `hard_cap = Model.ContextWindow - max(MaxOutputTokens, AURA_CONTEXT_MAX_OUTPUT_TOKENS=20000) - AURA_CONTEXT_RESERVE_TOKENS=13000`
- `warn_cap = hard_cap * 0.75`
- DeepSeek-V4 1M context: ~967K hard / 725K warn.
- OpenAI/Anthropic 200K: ~167K hard / 125K warn.
- Above warn: log WARN. Above hard: explicit `Loop.Turn` error (use `chat_compact` tool or `aura chat new`).
- Microcompact L1: `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS=10` — tool results older than N turns replaced with `[evicted, re-fetch via read_tool_output(X)]` pointer (no LLM call).

### Telegram throttle (Slice 9b)

- 2 panes per turn: status pane (1500 ms throttle), content pane (500 ms throttle).
- Hard chat rate: 1000 ms per chat_id queue (respects Telegram 1 msg/sec hard limit).
- 429 backoff: parse `retry_after` header, exponential up to 30s.
- Risk: under-throttle → bot banned per Telegram rate limits.

---

## Fragile Areas (anticipated)

### vLLM multimodal Gemma 4 support TBD

- Open question Slice 13b (`prd.md:3835`).
- vLLM v1+ multimodal support varies per model. Gemma 4 E4B audio+image native support in vLLM may regress between releases.
- Mitigation: 13-bis fallback path (llama.cpp E4B Q4 via `aura-llama-multimodal` sidecar) is a frozen escape hatch.

### iptables OUTPUT rules from DNS resolve (Slice 2b)

- Mechanism: parse `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` CSV → DNS resolve hosts at container exec hook → generate `iptables` rules per resolved IP (`prd.md:1198`).
- DNS cache: 5 minutes. Risk: `pypi.org` has multiple A records that rotate; cache may invalidate legitimate calls after 5 min (Slice 2b OQ DNS TTL).
- Mitigation: re-resolve on cache miss; manual override `AURA_SANDBOX_NETWORK_ALLOW_IPS` (not yet specified) as escape hatch.

### Crash recovery agent_job runs (Slice 6 boot query)

- Boot query (`prd.md:1712-1722`):
  ```sql
  UPDATE aura.agent_job_runs
     SET exit_status='unknown_recovery', finished_at=now(), recovered_at=now()
   WHERE finished_at IS NULL
  ```
- Decision: **never auto-re-execute a job with committed side-effects**. `ReschedulesOnRecovery()==true` (reminder, idempotent) → re-fired via `UPDATE next_run_at=now()`. `ReschedulesOnRecovery()==false` (agent_job, side-effecting) → limbo audit-only.
- Risk: an `agent_job` that crashed mid-write leaves partial state. `unknown_recovery` flag forces operator review.

### Multi-pause FIFO priority sort (Slice 1.5 #4)

- Scenario: parent has N children spawned via `SpawnInteractive`, each pauses simultaneously → parent's `PausedState[]` queue grows.
- Sort: priority + FIFO across children + cross-conversation parallel pauses (`prd.md:1287`).
- Mutex contract: `children map[string]*childState` mutex-protected via `sync.RWMutex` (audit round 2 P0). `ResumeChild + Spawn + Join` all share the RWMutex. Test: N=10 children paused/resumed without data race.
- Risk: if mutex contract broken, N concurrent interactive pauses race on map. Failure mode: deadlock or panic on map write.

### LMCache + vLLM integration version compat

- LMCache 8.4k stars, Apache 2.0, prod in GCP/GMI/CoreWeave (`prd.md:3630`).
- vLLM `--kv-transfer-config '{"kv_connector":"LMCacheConnectorV1", "kv_role":"kv_both"}'` integration.
- Risk: vLLM API breaking changes across releases. Pin to `vllm/vllm-openai:latest` is risky; pin to specific tag pre-merge.

### Skill loader cache TTL

- Pre-rewrite: 1s TTL on loader cache (preserved in Slice 7a, `prd.md:1963`).
- Risk: editing SKILL.md externally while `aura serve` running may take up to 1s to reflect. Manual `Loader.Invalidate()` on `skill.update` resume handler mitigates.

### markitdown sidecar tiered conversion (Slice 9c)

- Tiered: ≤5 MB sync (HTTP 30s timeout), 5-50 MB async background goroutine + "📄 Convertendo..." placeholder + edit-to-done, >50 MB refuse.
- Risk: 50 MB hard cap is Telegram limit; if Telegram raises it, async tier becomes the new hard limit.

### OpenRouter pass-through caveats (Slice 4)

- DeepSeek auto-cache preserved via OpenRouter `usage.prompt_cache_hit_tokens` (OpenAI-compat extension, `prd.md:1367`).
- Anthropic `cache_control: ephemeral` does NOT apply (DeepSeek doesn't support it).
- Risk: provider-side OpenRouter changes the wire format → cache parser break. Mitigation: optional field handling (`prd.md:1367`).

---

## Pre-Rewrite Tech Debt Awareness (acknowledged, addressed)

The `pre-rewrite-2026-05-27` git tag preserves the prior implementation. The rewrite explicitly addresses these patterns so they do not re-emerge.

### God classes split per ≤600 LOC rule

All split in the tabula-rasa.

| Pre-rewrite file | LOC | Status in rewrite |
|---|---|---|
| `internal/agent/tools/registry/search.go` | 562 | Split (Slice 5: `internal/web/fetcher.go` + `internal/web/searcher.go`) |
| `internal/cron/scheduler.go` | 587 | Split (Slice 6: scheduler tick + `internal/cron/handlers/<kind>.go` per kind) |
| `internal/cron/store.go` | **594 GOD CLASS** | Split per Upsert/Cancel/Delete/DueTasks/MarkFired/RecordManualRun/RecordAgentJobResult |
| `internal/agent/tools/registry/skill.go` | 347 | Split (Slice 7a: filesystem/parser/cache/loader 4-way + writer/installer/audit separate files) |
| `internal/web/direct_fetch.go` | 474 | Split (Slice 5: `fetcher.go` + `parser.go` + `safe_dial.go`) |
| `internal/cron/dispatch.go + dispatch_handlers.go` | 244+246 | Split via `Agent` interface (Slice 0.9): each `Handler` = `internal/cron/handlers/<kind>.go` |

### Dispatch switch redundancy

- Pre-rewrite: 5 `dispatchXxx` private functions + 2 inline arms in `Dispatch` switch (no strategy pattern, verified `pre-rewrite-2026-05-27` tag round 1 reality check, `prd.md:1688`).
- Rewrite: unified via `Agent` interface Slice 0.9. Adding a new `TaskKind` = adding 1 file with `Agent` impl, no dispatch switch (`prd.md:1706`).

### 4 separate runtimes → 1 unified runtime

- Pre-rewrite: `Loop`, `Scheduler.Handler`, `Swarm.Coordinator`, `Skill.execute` all had divergent shapes.
- Rewrite (Slice 0.9): 1 `Agent` interface, `iter.Seq2[*Event, error]` streaming, 4+ impls (`LlmAgent` Slice 1, `ParallelAgent` Slice 3, `Handler` = `Agent` Slice 6, `LoopAgent[InterviewStepAgent]` Slice 10).
- Saving: **−400 LOC net** (−680 LOC across slices + 280 LOC for Slice 0.9 itself, `prd.md:513`).

### No TODO comments orphan

- Per CLAUDE.md "NO COMMENTS UNLESS WHY IS NON-OBVIOUS." All TODOs live in `prd.md` slice sections, not in `.go` source (`prd.md:4379`).
- Current skeleton compliance: `cmd/aura/main.go:37` has `"TODO: implemented by the agent-loop and CLI slices"` as printed string (not a comment) — acceptable as stub-marker.

---

## Operational Concerns

### 14 Postgres migrations + 2 Neo4j Cypher migrations

- Order critical, every migration needs paired `down.sql`.
- Numbering (rinumerated per `prd.md:4354`):
  - `0001_init` (identity schema baseline)
  - `0005_conversations` (NEW per Slice 1.8 — was 0006_skill_audit in earlier plan)
  - `0006_scheduler` (Slice 6, was 0005)
  - `0007_skill_audit` (Slice 7, was 0006)
  - `0008_telegram` (Slice 9a)
  - `0010_sandbox_sessions` (Slice 2b)
  - `0011_ingest_audit` (Slice 11b)
  - `0013_local_llm` (Slice 13)
- Neo4j: `internal/db/migrations/neo4j/0001_init.cql` + `0002_memory_schema.cql` (Slice 11a, vector index 768d + fulltext index + Leiden community labels).
- Idempotency required: re-running a migration is a no-op (`prd.md:1507`).

### 60+ env vars catalogati in Caps & Limits indice

- File: `prd.md:4237-4312` (full table).
- Categories: `cap` / `operative` / `path` / `secret`.
- Defaults must be sane per single-user MVP. Production deployment requires reviewing `AURA_RISK_ALERT_THRESHOLD` (default `risky`), `AURA_LLM_LOCAL_FALLBACK_COST_USD_DAY` (default `$1.0`), `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS` (default 5).
- Naming convention: `AURA_<DOMAIN>_<UNIT>` with `_BYTES` / `_SEC` / `_MS` unit suffix. Library/sidecar envs (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `MULTIMODAL_*`, `LLAMA_*`, `LMCACHE_*`) keep canonical naming for compat (`prd.md:4313`).

### 13 sub-slice atomicity (per slice splits)

- Order critical, smoke green prerequisite for the next.
- Slices with sub-slices:
  - **2** → 2a (Docker sidecar stateless), 2b (session-bound + workspace + allowlist)
  - **6** → 6a (reminder + cron infra), 6b (agent_job + ActionRouter)
  - **7** → 7a (loader), 7b (catalog), 7c (mutation governance), 7d (installer), 7e (snippets + pattern + TTL)
  - **9** → 9a (channel framework + setup), 9b (Telegram impl), 9c (multimodal)
  - **11** → 11a (schema), 11b (ingestion), 11c (community), 11d (retrieval+rerank), 11e (agent journal + Memify)
  - **13** → 13a (router + offline + cost), 13b (vLLM + LMCache)
- Each sub-slice = atomic commit. No multi-sub-slice batching.

### CI build tags

- `db_integration`, `sandbox_integration`, `multimodal_integration`, `onboarding_integration`.
- Separate CI runner per tag (no flaky-on-CI mainstream, `prd.md:1458`).
- Risk: tests behind build tags silently rot if CI runners not configured.

### Git push discipline

- CLAUDE.md: never `git push` (or any remote-mutating command) unless explicitly requested in current turn. A previous approval does not carry over.
- Risk: aggressive AI agent that auto-pushes could publish secrets, broken state. Discipline enforced via human approval gate per turn.

### No feature flags

- Per CLAUDE.md / `prd.md:4377`: if a slice removes a stub, the stub is removed. No toggle. No re-export shims. No TODO in source.
- Implication: every refactor is destructive. Pre-rewrite tag is the only escape hatch.

---

## Test Coverage Gaps

Zero tests in current skeleton. The following are required by `prd.md:1447` and not yet meetable:

- **Coverage threshold per package**: ≥75% unit, ≥60% integration. Fail CI under threshold.
- **Goroutine leak check**: `goleak.VerifyNone(t)` in TestMain on slices 1, 3, 6, 8, 9, 11, 13.
- **Property-based**: `gopter` or `rapid` for PromptBuilder invariants (Slice 4), AG-UI translator event types (Slice 8), swarm backpressure (Slice 3).
- **Mutation testing spot-check**: `go-mutesting` killed score ≥ 70% on core files (`llm_agent.go`, `coordinator.go`, `pipeline.go`).
- **No `time.Sleep` non-determinism**: use Go 1.24+ `synctest` or channel sync. Wait with 5s timeout + fail-loud.
- **Fixture realistic**: `testdata/*.{json,csv,md,sse,sql,html,pdf}` realistic content (no `{"foo":"bar"}` placeholder).
- **Failure-driven test**: every bug fixed during implementation gets a reproducing test **before** the fix (TDD reverse, `prd.md:1462`).

Anti-patterns to reject (per `prd.md:1466-1481`):
- `assert reply == "4"` (test passes if model hallucinates the answer without invoking the tool).
- `aura exec python "print(2)" → 2` (no syscall verification, no goroutine leak check).
- `coordinator.Spawn(2) → 2 children` (no wall-clock parallelism timing assertion).
- `messages[0] == messages[0]` (no `usage.prompt_cache_hit_tokens > 0` assertion).

---

*Concerns audit: 2026-05-28*
