# Deep cross-analysis of the 7 scouts — 2026-05-21

Reading the 7 deliverables back-to-back surfaced things the per-scout summaries did not. This document captures: conflicts, hidden dependencies, critical bugs, items the master synthesis under/overestimated, and gaps the scouts did not address.

It is NOT a sequencing proposal — that comes after the user weighs in. It is the missing context for choosing.

---

## 1. Conflicts and tensions between scouts

### 1.1 Tool surface — collapse vs namespacing

- **picobot scout** (pattern #1, -1100 LOC): collapse `source_*` (6 tools) + `scheduler_*` (3 tools) + `wiki_*` (3 tools) into ~3 enum-dispatched tools via `action: enum`. Anchored in picobot's `filesystem.go` (read/write/list action), `cron.go` (add/list/cancel action), `exec.go`.
- **Online scout** (§2, Anthropic "Writing tools for agents"): "**Namespacing matters** — `wiki_search` and `web_search` beat a single overloaded `search`." Anti-pattern: wrap every API endpoint as a tool, but ALSO anti-pattern: one overloaded enum that does everything.

**Resolution**: not contradictory if read carefully. picobot's action-enum is for genuinely related operations on the same resource (filesystem ops on files, cron ops on cron jobs). Anthropic's namespacing rule is against erasing semantic distinctions (search-the-wiki vs search-the-web). The right cut:

- ✅ Collapse `source_store + source_read + source_list + source_delete + source_ocr + source_ingest` → ONE `source` tool with `action: enum`. Same resource (sources), same lifecycle.
- ✅ Collapse `schedule_task + list_tasks + cancel_task` → ONE `task` tool. Same.
- ❌ Do NOT collapse `web_search + search_memory + wiki_search` into one `search`. Different stores, different semantics, different latency/cost classes.
- 🤔 `read_memory + list_memory + search_memory + forget_memory` — borderline. Same store, distinct lifecycle ops. picobot's pattern would say YES; Anthropic's would say maybe. **Defer until measured: bench LLM tool selection accuracy on both shapes.**

### 1.2 Tool result overflow — spill-to-disk vs compaction vs summarization

Three scouts independently proposed different overflow strategies:

| Scout | Pattern | Mechanism | Storage | LOC |
|---|---|---|---|---|
| nanobot #1 | Spill-to-disk + reference envelope | Full payload on disk + path + preview | filesystem | ~180 |
| Codex #5 | Centralized middle-truncate + exec wrapper | Head+tail preserve, marker, lose middle | none — drop | ~110 |
| Codex #7 / Hermes #8 | Inline auto-compaction | LLM summarizes mid-conversation | system prompt | ~400-600 |
| openhuman #5 (payload_summarizer) | Per-tool-result summarization | LLM compresses single tool output | inline | ~300 |

**These are complementary, not competing.** Different layers:

- Truncate (Codex #5) is the floor — even with spill or compaction you cap raw bytes that hit the LLM.
- Spill (nanobot #1) is the data-preservation layer — you keep what you'd otherwise lose.
- Per-tool-result summarization (openhuman #5) is the smarter middle — when the full result is too big BUT you want the LLM to act on it now.
- Inline auto-compaction (Codex #7 / Hermes #8) is conversation-level — different timescale.

**The bug in the master synthesis**: I listed them as separate Tier-2 picks. The truth is they form a stack:

```
LLM sees: [structured preview] from openhuman summarizer (when applicable)
            OR [head + tail + truncation marker] from Codex #5 (when summarizer off)
            OR [path + small preview] from nanobot #1 (when result is reference-able)
            
Conversation-level: Codex #7 / Hermes #8 compaction kicks in at 70% context
```

Implementation order: Codex #5 first (always-on floor) → nanobot #1 (for re-referenceable results) → openhuman #5 (for orchestrator-only summarization) → Codex #7 (last, requires substrate from Hermes ContextEngine ABC).

### 1.3 Single system message (picobot #3) vs Anthropic cache layout

- **picobot #3** wants ONE system-role message at index 0 (`-50 LOC`, friendlier to local LLMs like llama.cpp).
- **Online #1.1 + Codex #4** want prompt-cache breakpoints inserted BETWEEN content blocks: `[soul+agent+tools] → CACHE → [history] → CACHE → [user]`.

**Tension**: Anthropic's cache_control breakpoint syntax is per-content-block. If you have ONE giant system message, you can only put ONE breakpoint at its end. If you have multiple, you can place breakpoints between them.

**Resolution**: For OpenAI-compat endpoints (DeepSeek, OpenAI, most proxies), the cache is automatic and prefix-based — one giant system message is FINE (Anthropic's breakpoint syntax is provider-specific). For an Anthropic adapter, keep the layout multi-block. The right shape:

```go
// internal/llm/client.go
func buildRequest(...) {
    if provider.SupportsCacheControl() {
        // emit multi-block layout with explicit breakpoints
    } else {
        // emit single system message with concatenated parts
    }
}
```

Provider-adapter pattern. Both scouts can ship; the if-branch hides the difference.

### 1.4 MaxIterations cap=20 (current) vs server-driven end_turn (Codex #2)

- **Aura today**: MaxIterations=20 is the primary termination signal.
- **Codex #2**: server-driven `end_turn` becomes primary; iteration cap becomes emergency brake.

Not a conflict — Codex #2 ADDS a signal, doesn't remove the cap. But the master synthesis implied "drop iteration anxiety". Reality: most self-hosted OpenAI-compat endpoints (llama.cpp, vllm) don't emit `end_turn`. Aura would keep the cap; it just stops being the only signal. Implementation: `OR` semantics, not `REPLACE`.

### 1.5 hermes per-platform tool config — TWO different lessons

- **Hermes scout §9.6**: per-platform tool config (cron jobs default-off expensive tools after $4.63 incident).
- **Web-telegram-consolidation scout**: web and telegram must have SAME feature level.

Tension: "same feature level" vs "different defaults per transport". 

**Resolution**: "same level" means the same FEATURES are available; per-transport DEFAULTS can differ. Cron should default-off expensive tools, telegram and web should both default-on. The consolidation mandate is about CAPABILITY parity, not configuration uniformity.

---

## 2. Hidden dependencies the master synthesis missed

### 2.1 prompt_cache_key requires stable prefix order

If Aura adds `prompt_cache_key=thread_id` (Codex #4, 15 LOC) BEFORE the web-telegram consolidation (CONS-01..05) stabilizes the prompt layout, the provider's cache will repeatedly miss as CONS commits restructure the prefix. → **Land Codex #4 AFTER CONS-01 (overlay fix) AND CONS-04 (builder collapse).**

### 2.2 Stream-time parallel dispatch + web SSE share the same layer

- Codex #1 (FuturesOrdered, 130-180 LOC) restructures `llm.Client.Stream` to emit per-tool-call events as they parse.
- Web SSE (CONS-07, +280 LOC) needs `llm.Client.Stream` to emit per-token events.

Both touch the same `internal/llm/client.go`. → **Bundle: land them in one wave OR land Codex #1 first as foundation, then CONS-07 piggybacks.**

### 2.3 os.Root sandboxing + kitchen-sink collapse touch same files

- picobot #2 (os.Root, -150 LOC) refactors `wiki_path.go`, `source_*.go`, `workspace_files.go`.
- picobot #1 (kitchen-sink collapse, -1100 LOC) DELETES `source_*.go` and consolidates wiki path tools.

If picobot #1 lands first, picobot #2's refactor target shrinks (-150 LOC becomes -80 LOC because fewer files). If picobot #2 lands first, picobot #1 inherits clean os.Root-rooted code. → **Order: #2 first (smaller, isolated), then #1 (large, depends on clean substrate).**

### 2.4 hermes ContextEngine ABC is upstream of EVERYTHING context-related

- hermes #8 ContextCompressor (~500 LOC) IS the substrate.
- Codex #7 inline auto-compaction is built on the ContextEngine interface.
- openhuman #5 payload_summarizer plugs into the ContextEngine for token estimation.

The master synthesis treated these as parallel Tier-4 picks. They're a stack. → **Sequence: ContextEngine interface (small) → default compressor impl → payload_summarizer plugs in → auto-compaction trigger as new engine.**

### 2.5 Reranker sidecar requires init-models extension

Online #6 (BGE-reranker-v2-m3) and #5 (Qwen3-Reranker-0.6B) need:
- Docker compose entry (new sidecar)
- `aura-init-models` extension to fetch the GGUF (~400 MB)
- New llm client adapter (`internal/llm/reranker`)
- Wire in `internal/storage/search/sqlite.go` and `internal/storage/qdrant.go`

The reranker is NOT a 1-day add. Plan it as a 3-5 day mini-phase. → **Same shape as Wave 2.10 init-models work; reuse pattern.**

### 2.6 web/telegram consolidation BLOCKS several other items

CONS-04 collapses both InvocationBuilders. Any work that touches `internal/channels/telegram/invocation_builder.go` between now and CONS-04 will conflict. → **Either FREEZE that file until CONS-04 ships OR sequence everything-else-first then CONS-04. Strongly recommend the latter — most other picks DON'T touch this file.**

---

## 3. Critical bugs surfaced (must fix immediately per CLAUDE.md "bugs are fixed when found")

### 3.1 Web /api/chat skips operator overlays

**Source**: web-telegram-consolidation scout, finding #5 Cluster A.

`/api/chat` builds the system prompt via `conversation.RenderSystemPrompt(now, loc)` — base + runtime block only. It NEVER loads AGENT.md / SOUL.md / USER.md / TOOLS.md.

**Consequence**: Web users see a slim base prompt; Telegram users see the full agent personality. Bench runs through `/api/chat` are measuring a DIFFERENT system than production Telegram. **All web bench data prior to the fix is partially invalid.**

**Fix**: CONS-01 (~200 LOC touch). Ship first.

### 3.2 `internal/logging` imports `internal/api` — broken leaf contract

**Source**: codebase-cleanup-audit §5.1.

`internal/logging` shows 590 transitive deps because it imports `internal/api` to satisfy a health-shape interface. Logging should be a LEAF — depends only on `zap`+`slog`+stdlib.

**Consequence**: any package that imports `internal/logging` (almost all of them) drags `internal/api` along. Build times inflate, dep graph confused, test isolation harder.

**Fix**: Invert dep — `api` provides its own logging adapter. M effort, blocks nothing else but worth doing.

### 3.3 webToolExecutor diverges from agent.ExecuteToolCalls on wrapping

**Source**: web-telegram-consolidation scout Cluster C.

`webToolExecutor.ExecuteToolCalls` wraps results with `agent.WrapUntrustedToolResult` + `limitToolContent`. Telegram path uses the registry's standard wrapper. → silent divergence between transports for the SAME tool. Test/prod skew not caught by any test today.

**Fix**: CONS-03 collapses this. Until then, NO new behavior should be added to either wrapper without also adding to the other.

### 3.4 errcheck noise hides 2 real bugs

**Source**: codebase-cleanup-audit §2.5 + pick #9.

50 unchecked `defer Close()` / `Encode()` flagged. Most are noise, BUT:
- `internal/api/health_server.go` — silent JSON-write failure on `/health` (3 sites)
- `internal/backup/export.go` — silent gzip-flush + tar-writer failure → corrupted backup on disk error

**Fix**: pick #9 cleanup-audit, M effort. The two real ones are the priority; the noise can be addressed in the same commit.

### 3.5 Three chat.Hub instances in one process

**Source**: web-telegram-consolidation scout Cluster D.

Hub #1 telegram, Hub #2 web, Hub #3 cron. The cron one is genuinely separate (different lifecycle). The telegram + web should be ONE.

**Consequence**: each has its own `priorityCaches`, its own AgentLoopAdapter. Cross-channel thread state never reconciles. If a user starts on telegram then asks "what did we just talk about" on web, the answer is from a different Hub's cache. Subtle, but real.

**Fix**: CONS-05.

### 3.6 1 confirmed `unused` symbol (clean code base, not a bug strictly)

`appendUniqueSorted` in `internal/wiki/memory_hygiene.go:735` — golangci-lint U1000. Quick win.

---

## 4. Items the master synthesis under/overestimated

### Underestimated effort

- **CONS-04** (Web/Telegram InvocationBuilder collapse): master said "8 stories net -90 LOC". Reality: CONS-04 alone is -360 LOC in ONE commit, the highest-risk single change. Mitigation is the feature flag (`AURA_AGENTCORE_BUILDER`) for 1 week of live traffic. The scout warned about this; the master one-line-summary did not.
- **Hermes ContextEngine ABC + ContextCompressor**: master said "~400-600 LOC". Closer to truth, but it's also a foundational substrate; once it lands, 3-4 other features can plug in. The LOC is honest, the strategic weight is bigger.
- **Reranker sidecar deploy**: master said "+27% nDCG@10". True. But the deploy plumbing (Docker, init-models, env vars, health check) is 3-5 days of work BEFORE you even measure the quality bump. Not free.
- **Sub-agents (openhuman §3+4+6)**: master said "~600 LOC for TOML registry + builtins". True for the registry alone. But to USE it, you also need spawn primitives (~500 LOC §6), tier validator (~80 LOC §4), and 3-4 builtin agent.toml files + their prompt.md siblings (~50 LOC each + ~500 LOC of prompt text per agent). Realistic total: ~1500-2000 LOC if you ship it usefully.

### Overestimated effort

- **prompt_cache_key=thread_id**: master said 10-15 LOC, no risk. Confirmed. Truly trivial.
- **end_turn termination signal**: master said 25-40 LOC. Probably <30 because most of the parsing exists.
- **Tool description ≤200 char audit**: master said 5 min. Probably accurate.
- **lastToolResult empty-reply fallback (picobot #7)**: master listed but understated — it's 3 LOC and prevents a known UX failure mode. Should ship NOW.

### Wrong category in master

- **Single consolidated system message (picobot #3)**: master called it Tier 1. Actually mid-tier because it interacts with cache layout (§1.3 above). Land AFTER prompt_cache_key.
- **read_only / concurrency_safe per-tool flags (nanobot #12)**: not in top tiers but should be — it's the safety primitive for parallel dispatch (Codex #1). Without it, the kitchen-sink action-enum tools become race-prone.

---

## 5. Gaps the scouts did NOT address

These came up in my reading but no scout had them as primary scope.

### 5.1 Embedding cache invalidation on wiki edits

When a wiki page is edited (via `wiki` tool or manual edit), the SHA-keyed embedding cache for that page must invalidate. Confirm by reading `internal/storage/embedcache/`. If not invalidated, search returns stale vectors → silent quality regression. **Spike priority: 1h to verify.**

### 5.2 Concurrent wiki writes — file-level mutex coverage

The wiki has a per-page file mutex. But MULTIPLE chat sessions can hold mutexes for DIFFERENT pages simultaneously and produce inconsistent `[[wiki-link]]` graphs if both reference each other. Not race-condition-as-corruption, but race-condition-as-incoherent-state. Confirm: does Aura serialize wiki writes globally or per-page? **Spike: 30 min code read.**

### 5.3 Telegram session restoration after restart

`b.NotifySoftBudget`, ongoing typing indicator, pending ask_user state — what survives a container restart? If a question is asked and the user replies AFTER restart, does the chat hub still match the reply to the question? Confirm via `internal/telegram/pending_questions.go` (if exists) + SQLite. **Spike: 1h.**

### 5.4 Rate-limit handling for hosted LLM

Anthropic's Claude Sonnet 4.6 / Opus 4.7 have rate limits per token-bucket. Does `internal/llm/client.go` handle `429 Too Many Requests` with backoff? If not, a burst of agent activity could fail catastrophically. **Spike: 30 min.**

### 5.5 MCP server reconnection logic

If an MCP server (stdio child OR HTTP endpoint) drops mid-conversation, does Aura reconnect or surface a tool error to the LLM? The `mcp.json` reconciler (2.10.b) handles reload, but not mid-conversation drop. **Spike: 1h.**

### 5.6 BoundOutput byte cap interacts with truncation in odd ways

Currently `DefaultOutputMaxBytes=8192` triggers truncation. The nanobot #1 spill-to-disk pattern, if added, must run BEFORE BoundOutput cap (otherwise the spill loses the bytes before they're saved). Sequencing critical. **Trivial fix if remembered; silent bug if forgotten.**

### 5.7 Wiki TOC injection — token budget at runtime

`InjectWikiTOC` (already in `internal/conversation/system_prompt.go`) appends the TOC. With ~100 wiki pages * ~80 char each = ~8 KB = ~2000 tokens. If wiki grows to 500 pages → 10000 tokens just for TOC. No truncation today. **Phase-WIKI-B should address this with selective TOC injection (only pages relevant to current query, via embedding pre-filter on titles).**

### 5.8 Probe coverage of /api/chat vs Telegram

`cmd/probe_chat` exercises Telegram via the same hub layer. Does it ALSO exercise `/api/chat`? If web has 7 missing features (CONS-08), probes that pass on Telegram silently regress web. **Confirm in `cmd/probe_chat/`** — if no web cases, add them as CONS-06/07/08 ship.

### 5.9 Cron-vs-interactive context shape

Cron Hub stays separate post-CONS-05. But it shares `agent.ExecuteToolCalls` etc. If cron agent's system prompt diverges from telegram's, where does that drift live? Likely `internal/cron/dispatch.go::agentjob_prompt`. Audit for parity AFTER CONS-04 stabilizes the shared builder. **Half-day.**

### 5.10 LLM provider abstraction is paper-thin

`internal/llm/client.go` is OpenAI-compat HTTP only. Switching to Anthropic native (for `cache_control` breakpoints, `interleaved-thinking-2025-05-14` beta, etc.) means significant refactor. The "provider adapter pattern" (§1.3 resolution) is a 2-3 day add. **Should land in same wave as prompt_cache_key.**

---

## 6. Anti-patterns reaffirmed by multiple scouts

Strong consensus:

- **No fast-path classifier bypassing the loop** — picobot has it (`remember` regex), 4/5 mature systems (Codex, elysia, nanobot, openhuman) reject. Online research distinguishes "replica routing" (OK, vLLM Semantic Router) from "loop bypass" (rejected).
- **No LLM-as-reranker for retrieval** — picobot has it (two-tier memory ranker), online research + Aura's locked decision (`feedback_minillm_cpu_not_viable_for_tool_retrieval`) say cross-encoder reranker.
- **No compaction at 100% context** — wait too long, signal lost. Compact at 70-80%.
- **No "wrap every API endpoint as a tool"** — Anthropic's explicit anti-pattern; MCP tool bloat = 40-50% of context.
- **No silent truncation** — every truncate must have a marker the LLM recognizes; missing marker → hallucinated completeness.
- **No defaults-on for expensive tools in autonomous contexts** — hermes $4.63 cron incident.

---

## 7. What "calma" actually unlocks

Going slow on this revealed three things that were not visible in a single-pass executive summary:

1. **The web/telegram bug (overlay-loading) is more serious than "missing feature"**: all web bench numbers are partially invalid because the system prompt is different. Re-bench AFTER CONS-01, not before.
2. **The compaction story is a stack, not a list**: 4 different scouts converged on layered overflow handling. Implementing them one at a time without the stack picture would result in re-doing work.
3. **The reranker is more than a sidecar swap**: it's a deploy phase. Plan accordingly.

---

## 8. Decision points for the user

These are the choices the analysis doesn't resolve — they require user judgement:

### D1. Tool surface collapse aggressiveness
- Conservative: collapse only `source_*` (clear win, -700 LOC).
- Medium: also `scheduler_*` (-150 LOC) and `wiki_*` (-200 LOC).
- Aggressive: also `*_memory` (high risk of regression on tool selection).

### D2. Cache_control provider adapter
- Now: 2-3 day refactor to provider adapter pattern, unlocks Anthropic native.
- Later: stay OpenAI-compat, lose Anthropic-specific cache placement, gain simplicity.

### D3. CONS-04 risk model
- Feature flag for 1 week as scout proposed (slow, safe).
- Direct merge with byte-parity test (faster, riskier).

### D4. Sub-agents / TOML AgentDefinition
- Phase 8 substrate as planned (~1500-2000 LOC real cost).
- Defer indefinitely — Aura's overlay system is "good enough".

### D5. Reranker model choice
- BGE-reranker-v2-m3 (mature, more docs).
- Qwen3-Reranker-0.6B (newer, better numbers on benches).

### D6. SSE vs WebSocket for web streaming
- SSE (simpler, scout's recommendation).
- WebSocket (handles bidirectional better, more complex auth).

### D7. Order of operations
- Wave-by-wave as master proposed.
- Bug-fix first then everything else (start with CONS-01 + logging + errcheck bugs).
- Cache_first (prompt_cache_key + end_turn + description audit) for fastest measurable win.

---

*This document is intermediate scaffolding. Update master synthesis sequencing after user weighs in on D1-D7.*
