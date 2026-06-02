# Phase 6: KV Cache Builder - Research

**Researched:** 2026-06-02
**Domain:** Go prompt-construction discipline, KV/prompt-cache stability, Postgres metrics (sqlc/pgx), runtime-faithful CI gating
**Confidence:** HIGH (all claims grounded in the live codebase + PRD; the one external claim — Anthropic `cache_control` shape — is CITED from official Anthropic docs)

## Summary

Phase 6 is overwhelmingly an **internal-plumbing + refactor** phase against a codebase whose KV-cache invariant *already holds by construction*. There are no new external packages, no new runtime dependencies, and no algorithmic research needed — the entire value is in (a) extracting a single chokepoint (`PromptBuilder`) without breaking byte-identity, (b) persisting already-shipped `llm.Usage` data to a new Postgres table, (c) building a dormant provider-aware `cache_control` seam, and (d) a runtime-faithful CI gate driving the real `runner.Turn` loop against the importable `agenttest.FakeClient`.

Every "Claude's Discretion" item in CONTEXT.md resolves cleanly against codebase evidence: the import graph **confirms** the cycle (D-01a is correct — `internal/agent/tools` imports `internal/llm`, so `internal/llm` cannot import back; PromptBuilder belongs in `internal/agent`), the migration sequence is verified at **0007**, the per-turn `llm.Usage` already surfaces at one clean seam (`runner_persist.go:persistAssistantAnswer`), and `a.cfg.Provider` is already threaded into `LlmAgent` (so the provider branch reads it directly — no new plumbing).

**Primary recommendation:** Put `PromptBuilder` in a new `internal/agent/prompt` subpackage (or directly in `internal/agent`), record the deviation from the PRD's `internal/llm/prompt.go` as a **PRD-amendment** (alongside the D-02 OQ2 amendment), add `ToolsCacheControl string` to `llm.Request`, persist metrics via a new `aura.cache_metrics` migration `0007` consumed by sqlc, and build the gate as a hidden `aura cache-audit` subcommand that loops `runner.Turn` 20× over deterministic fixtures and prints `SHA-256(canonicaljson(messages[0]))` per turn.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Extract a named `PromptBuilder` type (not inline). Move MUST preserve the existing byte-identity invariant of `messages[0]`.
- **D-01a (planner constraint):** PRD targeted `internal/llm/prompt.go`, but `internal/llm` **cannot import `internal/agent/tools`** (import cycle). `PromptBuilder` belongs in **`internal/agent`** (or a new `internal/agent/prompt` subpackage), NOT `internal/llm`. Record any deviation from PRD file-targets as a PRD-amendment.
- **D-02:** Persist per-turn metrics to Postgres (`aura.cache_metrics`: turn, conv_id, ts, prompt_tokens, cached_tokens, cost). **Overrides PRD Slice 4 OQ2** ("in-memory only") → **requires a PRD-amendment commit BEFORE implementation**. New migration in 0007+ sequence.
- **D-02a:** Source data already shipped — `llm.Usage{PromptTokens, CompletionTokens, CachedTokens, Cost}` populated by the OpenAI-compat client. Tracker consumes the existing trailing Usage chunk; no wire-layer parsing remains.
- **D-03:** Build the no-op provider-aware seam now (`cache_anthropic.go` injecting `cache_control: {"type":"ephemeral"}` on system + tools; add `ToolsCacheControl` field to `llm.Request` per PRD OQ3). **No PRD-amendment needed.** Pure no-op under OpenRouter.
- **D-03a:** Breaks the `internal/llm/client.go` "the wire layer is unaware" design comment — update it in the same commit (DEEP REFACTOR ON TOUCH). Injection lives in the `PromptBuilder`/provider layer, not the raw `Stream` wire path.
- **D-04:** Runtime-faithful gate, not synthetic. Drive a real **20-turn `runner.Turn` loop against a deterministic stub LLM**; assert `SHA-256(messages[0])` constant across all 20 turns.
- **D-05:** Stub LLM = extend `internal/agent/agenttest.FakeClient` (importable, captures `Requests`). Read `FakeClient.Requests[n].Messages[0]` directly. **Explicitly NOT** `cmd/aura/cmdfakes_test.go` (package main, test-only).
- **D-06:** Operator entrypoint = a hidden `aura cache-audit` subcommand running the 20-turn replay, printing per-turn SHA-256 to stdout. `scripts/cache_invariant_audit.sh` is a thin wrapper invoking + diffing. CI-wires into `.github/workflows/ci.yml` from this phase onward.
- **D-06a:** Fixtures = `scripts/fixtures/cache_invariant/turn-{01..20}.json` (growing-history replay turns). Hash must cover *only* `messages[0]` today; design the hash to accept an **index set** so amendment #11 can extend to `{0,1,2}` once Slices 10/11e ship.

### Claude's Discretion
- Exact `aura.cache_metrics` column types / index strategy, and whether `cache-stats` aggregates client-side or via SQL `GROUP BY` — decide against `golang-database` patterns. *(Resolved below — §Standard Stack / §Architecture.)*
- The precise `PromptBuilder` package boundary (new subpackage vs. existing `internal/agent`) — subject to D-01a's cycle constraint. *(Resolved below — recommend `internal/agent/prompt`.)*
- Fixture turn content (synthetic conversation for the 20-turn replay) — must be deterministic and exercise tool-call turns, not just text turns. *(Resolved below — §Architecture Pattern 4.)*

### Deferred Ideas (OUT OF SCOPE)
- `messages[1]` content (Agent.md profile) — seam only this phase; content ships with Slice 10.
- `messages[2]` content (cached AgentInsight) — seam-aware hash only; content ships with Slice 11e (amendment #11).
- Runtime provider selection / activating the Anthropic `ephemeral` path — Slice 13 `LLMRouter`; this phase builds only the dormant seam.
- Throwaway `chat-loop` REPL (PRD smoke) — superseded by the shipped persisted `aura chat`; the 20-turn replay lives in `cache-audit`, not a new REPL.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CAP-04 | KV cache builder stable-prefix + provider-aware. Two system messages: `messages[0]` cache-stable byte-identical, `messages[1]` mutable. CI job `scripts/cache_invariant_audit.sh` asserts SHA-256(`messages[0]`) constant across 20-turn replay. 80% cache-hit target on DeepSeek-V4. [Slice 4 + amendment #16] | §Architecture Patterns 1–4 (PromptBuilder, provider seam, metrics, runtime gate); §Validation Architecture maps each of the 5 ROADMAP SCs. |

**CAP-03 vs CAP-04 numbering drift — RECONCILED:** `.planning/REQUIREMENTS.md` is authoritative and maps **CAP-04 → Phase 6 (KV cache, Slice 4)** (line 35, and traceability table line 113), while **CAP-03 → Phase 9 Swarm** (line 34, line 112). ROADMAP also says CAP-04. The CONTEXT.md note that "PROJECT.md labels it CAP-03" is the drift — **use CAP-04** in all Phase 6 artifacts; flag the PROJECT.md mislabel to the planner for a one-line fix (out of this phase's code scope, doc-only).
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Prompt assembly (`messages[0]` byte-identity) | `internal/agent` (PromptBuilder) | `internal/agent/tools` (manifest) | System prompt + manifest both live in `internal/agent*`; this is where `LlmAgent` already assembles history (`llm_agent.go:68-70, 131-137`). |
| Provider-aware `cache_control` injection | `internal/agent` (PromptBuilder/provider branch) | `internal/llm` (`Request.ToolsCacheControl` field only) | The field is wire-shape (llm), but the *decision* to inject reads `a.cfg.Provider` which is already in `internal/agent`. Wire path (`Stream`) stays caching-unaware of *logic*; it only serializes the field. |
| Per-turn metric persistence | `internal/runner` (persist seam) → `internal/db/sqlc` | Postgres `aura.cache_metrics` | `runner_persist.go:persistAssistantAnswer` already holds the per-turn `llm.Usage`; it is the one write seam that fires once per completed assistant turn. |
| `cache-stats` query/aggregation | `cmd/aura` (subcommand) → `internal/db/sqlc` | Postgres (SQL `GROUP BY` time window) | Mirrors `aura db`/`aura chat` subcommand dispatch. SQL-side aggregation for the `--since` window (see §Pattern 3). |
| Runtime-faithful invariant gate | `cmd/aura` (hidden `cache-audit`) → `internal/runner` + `internal/agent/agenttest` | bash wrapper + CI | Only a real `runner.Turn` loop exercises the cross-slice mutation paths the gate exists to catch (D-04). |

## Standard Stack

### Core (all already in the repo — NO new external packages)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/jackc/pgx/v5` | (repo-pinned) | Postgres driver for the new `cache_metrics` writes/reads | Already the project driver (`sqlc.yaml` `sql_package: pgx/v5`). [VERIFIED: sqlc.yaml] |
| `github.com/sqlc-dev/sqlc` (codegen) | v1.31.1 | Generate typed query funcs for `cache_metrics` | Pinned in `.github/workflows/ci.yml` "Install sqlc" step. [VERIFIED: ci.yml line 148] |
| `golang-migrate` (CLI/lib) | v4.x | New migration `0007_cache_metrics.{up,down}.sql` | Existing migration tool; 0001-0006 shipped. [VERIFIED: internal/db/migrations] |
| `crypto/sha256` + `internal/canonicaljson` | stdlib + repo | Deterministic `SHA-256(canonicaljson(messages[0]))` | `canonicaljson.Marshal` is the project's deterministic serializer, already used for dedup + Phase 4 conv hash + Phase 11 content_hash. [VERIFIED: internal/canonicaljson/canonicaljson.go:5-7] |
| `internal/agent/agenttest.FakeClient` | repo | Deterministic stub LLM for the 20-turn replay | Importable (not `_test.go`), implements `llm.Client`, captures every `Requests[n]` with a *cloned* Messages slice. [VERIFIED: agenttest/fakeclient.go:20-77] |

### Supporting (existing, reused)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `internal/runner.Runner.Turn` | repo | The 20-turn loop driver | The audit subcommand loops this against an in-memory FakeClient-backed Runner. [VERIFIED: runner/runner.go:148] |
| `time.ParseDuration` (stdlib) | go | Parse `--since=1h` → `time.Duration` for the metrics window | `cache-stats` flag parsing. |
| `text/tabwriter` (stdlib) | go | Columnar `cache-stats` output | Mirrors `aura db status` output style. [VERIFIED: cmd/aura/db.go:108] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `internal/agent/prompt` subpackage | Inline `PromptBuilder` in `internal/agent` package | Subpackage gives a clean import + a natural home for `prompt_test.go` + the index-set hash; the package `agent` is already large. Recommend subpackage; either satisfies D-01a. |
| SQL `GROUP BY` aggregation | Client-side aggregation in Go | SQL keeps the `--since` window a single indexed query; client-side pulls all rows. Recommend SQL for the per-turn list + a separate aggregate query (§Pattern 3). |
| New dedicated stub harness | Extend `agenttest.FakeClient` | D-05 locks reuse; FakeClient already captures `Requests` and clones Messages — purpose-built harness is redundant. |

**Installation:** None. No `npm`/`pip`/`go get` of new packages. sqlc regeneration only: `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 && sqlc generate` (already wired in CI's "sqlc generate is in sync" job).

**Version verification:** No new packages to verify on a registry. sqlc pin v1.31.1 confirmed in `.github/workflows/ci.yml:148`. pgx/v5 + golang-migrate are the established project deps (no version bump needed).

## Package Legitimacy Audit

> **Not applicable — this phase installs ZERO external packages.** All code uses stdlib (`crypto/sha256`, `time`, `text/tabwriter`, `encoding/json`) plus already-vendored project deps (`pgx/v5`, `sqlc`-generated code, `golang-migrate`, `internal/canonicaljson`, `internal/agent/agenttest`). slopcheck/registry verification is moot — there is no install step.

**Packages removed due to slopcheck [SLOP] verdict:** none (no installs)
**Packages flagged as suspicious [SUS]:** none (no installs)

## Architecture Patterns

### System Architecture Diagram

```
                          aura cache-audit (hidden subcommand)
                                     │
              builds in-memory Runner (FakeClient + real Registry + fake Stores)
                                     │
   for turn := 1..20:               ▼
   load fixture turn-NN.json ─► Runner.Turn(ctx, &userMsg) ──► LlmAgent.Run
                                     │                              │
                                     │                  req := PromptBuilder.Build(  ◄── NEW chokepoint
                                     │                        history, registry, provider)
                                     │                              │
                                     │                  FakeClient.Stream(req)  ──► captures Requests[n]
                                     ▼                              │
                          read FakeClient.Requests[n].Messages[0]   │ (scripted response: text or tool_call turn)
                                     │                              ▼
                          h := sha256(canonicaljson(msg[0]))   LlmAgent threads tool results, emits final Event
                          print "turn NN: <hex>"                    │
                          assert h == h_prev                        ▼
                                     │                   persistAssistantAnswer(usage)  ──► NEW: cache_metrics INSERT
                                     ▼                                                         (in prod path; FakeStore in audit)
                       exit 0 (all equal) / 1 (mutated) / 2 (fixture corrupt)


   PRODUCTION turn path (unchanged loop, NEW seams marked *):
   runner.Turn ─► LlmAgent.Run ─► *PromptBuilder.Build (was inline llm.Request{}) ─► client.Stream
                                                                                          │
                                          trailing llm.Usage chunk ◄── openai_compat/sse.go (already parses cached_tokens+cost)
                                                                                          │
   runner_persist.go:persistAssistantAnswer(usage) ─► Conv.AppendTurn (existing aggregates)
                                                   └─► *cache_metrics INSERT (NEW)

   aura cache-stats --since=1h ─► sqlc query (time-windowed GROUP BY) ─► tabwriter stdout
```

### Recommended Project Structure
```
internal/agent/prompt/
├── builder.go         # PromptBuilder.Build(history, registry, provider, opts) []llm.Message  (~120 LOC)
├── cache_anthropic.go # injectEphemeral(req *llm.Request) — provider=="anthropic" only (~60 LOC)
├── hash.go            # PrefixHash(msgs []llm.Message, indices []int) string  (index-set, ~40 LOC)
└── builder_test.go    # byte-identity over N turns, monotonic growth, no-mutation, anthropic wire-shape, hash index-set
internal/db/
├── migrations/0007_cache_metrics.up.sql   # + 0007_cache_metrics.down.sql
└── queries/cache_metrics.sql               # InsertCacheMetric, ListCacheMetricsSince, AggregateCacheMetricsSince
cmd/aura/
├── cache.go           # runCacheStats(args) + runCacheAudit(args) dispatch; main.go switch +2 cases
scripts/
├── cache_invariant_audit.sh                # thin wrapper: go run ./cmd/aura cache-audit; diff hashes; explicit failure
└── fixtures/cache_invariant/turn-01..20.json
```
**Note on `internal/llm/prompt.go`:** PRD §Slice 4 file-target (`internal/llm/prompt.go ~140 LOC`) is **unworkable** — see §Common Pitfalls Pitfall 1. The deviation to `internal/agent/prompt/` MUST be a PRD-amendment.

### Pattern 1: PromptBuilder as the single assembly chokepoint
**What:** Replace the inline `llm.Request{Messages: a.history, Tools: a.registry.RenderToolDefs(), ...}` in `llm_agent.go:131-137` with `PromptBuilder.Build(...)`. The builder owns the *order* and *cache_control* injection; it must reproduce today's byte-stable `messages[0]` exactly (system message + alphabetically-sorted manifest are unchanged).
**When to use:** Every LLM call in the agent loop.
**Why a builder when it's constant by construction:** D-01 — a single chokepoint is where the CI gate, the `messages[1]`/`messages[2]` future seams, and the provider branch all hook. The byte-identity is *preserved*, not newly created.
**Example (target shape — derived from existing call-site):**
```go
// Source: derived from internal/agent/llm_agent.go:131-137 (current inline) + D-01/D-06a
// PromptBuilder.Build assembles the wire request. messages[0] = system + manifest
// (byte-identical turn-on-turn). Future: messages[1]=Agent.md (Slice 10),
// messages[2]=cached AgentInsight (Slice 11e). Today both absent.
func (b *PromptBuilder) Build(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config) llm.Request {
    req := llm.Request{
        Model:       cfg.Model,
        Messages:    history,                 // history[0] is already the byte-stable system msg (NewLlmAgent prepends it)
        Tools:       reg.RenderToolDefs(),     // alphabetical (manifest.go:39) — cache-load-bearing
        Temperature: cfg.Temperature,
        MaxTokens:   cfg.MaxTokens,
    }
    injectCacheControl(&req, provider)         // no-op unless provider=="anthropic"
    return req
}
```

### Pattern 2: Provider-aware `cache_control` seam (dormant under OpenRouter)
**What:** Add `ToolsCacheControl string` to `llm.Request` (PRD OQ3, line 1531). For `provider == "anthropic"` only, mark the system block and the tools array with `cache_control: {"type":"ephemeral"}`. Under `provider == "openrouter"` (the day-1 default, `llm/config.go:19`) this is a pure no-op. `a.cfg.Provider` is **already threaded** into `LlmAgent` and read by `setSpanAttrs` (`llm_agent.go:147`) — no new plumbing.
**When to use:** Built now, activated by Slice 13 `LLMRouter`. Never hardcode the provider (Seam D00 / amendment #30).
**Anthropic wire shape (CITED — do NOT inline-cache history):**
```jsonc
// Source: docs.anthropic.com/en/docs/build-with-claude/prompt-caching  [CITED]
// (1) tools array: cache_control on the LAST tool definition marks the whole
//     tools+system prefix as a cache breakpoint.
// (2) system: array of blocks, cache_control on the system block.
// (3) NEVER on per-turn history messages — that would shift the breakpoint every turn.
{
  "system": [{ "type": "text", "text": "<SystemPrompt>", "cache_control": {"type": "ephemeral"} }],
  "tools":  [ /* ... */ { "name": "z_last_tool", "cache_control": {"type": "ephemeral"} } ]
}
```
**Critical:** Aura's `llm.Message.Content` is a plain string and the wire client is OpenAI-compat (`client.go:24-30`). The Anthropic block-array shape is a *different wire format* — under the current single-provider (OpenAI-compat) client the field is serialized but ignored. The seam is the `ToolsCacheControl` field + a provider branch in `PromptBuilder`; the actual Anthropic-native wire translation is Slice 13's job. This phase only proves the branch exists and is a no-op (fixture/wire test asserts OpenRouter requests carry no `cache_control`).

### Pattern 3: Per-turn metrics → Postgres (the existing persist seam)
**What:** `runner_persist.go:persistAssistantAnswer` (line 58-78) already computes the per-turn `u := usageFromStateDelta(...)` and writes `Conv.AppendTurn(...)`. Add a sibling `cache_metrics` INSERT in the same function (one extra write per completed assistant turn) — no wire-path touch (D-02a satisfied; usage is already in hand).
**Schema (against existing migration conventions — `0005_conversations.up.sql`):**
```sql
-- Source: 0007_cache_metrics.up.sql (NEW) — conventions mirror 0005_conversations.up.sql
-- numeric(10,4) for USD, timestamptz DEFAULT now(), aura. schema, grants to aura_app/aura_migrate.
CREATE TABLE aura.cache_metrics (
    conversation_id uuid        NOT NULL REFERENCES aura.conversations (id) ON DELETE CASCADE,
    seq             integer     NOT NULL,                          -- the turn within the conversation
    ts              timestamptz NOT NULL DEFAULT now(),
    prompt_tokens   integer     NOT NULL DEFAULT 0,
    cached_tokens   integer     NOT NULL DEFAULT 0,
    cost_usd        numeric(10, 4) NOT NULL DEFAULT 0,
    PRIMARY KEY (conversation_id, seq)
);
-- Index strategy: the --since window query filters on ts → a ts index serves it.
CREATE INDEX cache_metrics_ts_idx ON aura.cache_metrics (ts DESC);
GRANT SELECT, INSERT ON aura.cache_metrics TO aura_app;
GRANT ALL            ON aura.cache_metrics TO aura_migrate;
COMMENT ON TABLE aura.cache_metrics IS
    'Per-turn KV-cache hit metrics (Slice 4 / Phase 6). One row per completed assistant turn; aura cache-stats --since windows on ts.';
```
**sqlc queries (against `internal/db/queries/` conventions, `sqlc.arg` style from conversations.sql):**
```sql
-- name: InsertCacheMetric :exec
INSERT INTO aura.cache_metrics (conversation_id, seq, prompt_tokens, cached_tokens, cost_usd)
VALUES ($1, $2, $3, $4, $5);

-- name: ListCacheMetricsSince :many   -- per-turn rows for the window (cache-stats detail lines)
SELECT conversation_id, seq, ts, prompt_tokens, cached_tokens, cost_usd
FROM aura.cache_metrics
WHERE ts >= sqlc.arg(since)::timestamptz
ORDER BY ts ASC;

-- name: AggregateCacheMetricsSince :one  -- session summary (avg hit-rate, totals)
SELECT count(*)                                                   AS turns,
       coalesce(sum(prompt_tokens), 0)                            AS total_prompt_tokens,
       coalesce(sum(cached_tokens), 0)                            AS total_cached_tokens,
       coalesce(sum(cost_usd), 0)                                 AS total_cost_usd
FROM aura.cache_metrics
WHERE ts >= sqlc.arg(since)::timestamptz;
```
**Aggregation decision (Claude's Discretion resolved):** Use **SQL** for the window. The detail list (`ListCacheMetricsSince`) feeds the per-turn `tabwriter` lines; the aggregate (`AggregateCacheMetricsSince`) computes `avg hit-rate = total_cached / total_prompt` in one indexed pass. Client-side would pull every row to do arithmetic Postgres does for free — SQL keeps `--since=1h` a single bounded query. Compute the *hit-rate ratio* client-side from the summed integers (avoid a SQL float divide that could divide-by-zero; guard `total_prompt_tokens == 0`).
**`--since` parsing:** `since := time.Now().Add(-d)` where `d, _ := time.ParseDuration(arg)` (e.g. `1h`, `24h`). Pass `since` as a `timestamptz` arg.

### Pattern 4: Runtime-faithful invariant gate via hidden `aura cache-audit`
**What:** A shipped (non-test) subcommand that constructs a `Runner` with `Client: &agenttest.FakeClient{...}` (scripted 20 turns, mixing `agenttest.TextChunks` and `agenttest.ToolCallTurn`), a real `tools.Registry` (`buildRegistry()`), and **in-memory fake Stores** for Conv/Pause/Identity (so the audit needs no Postgres). It calls `Runner.Turn` 20× feeding `turn-NN.json` user messages, then reads `fakeClient.Requests[n].Messages[0]`, hashes each with the index-set hash, prints `turn NN: <hex>`, and asserts all 20 equal.
**Why runtime-faithful (D-04):** `messages[0]` is constant *by construction* today, so a synthetic `Build()` hash is trivially green. The gate's job (amendment #16) is catching a *future* slice (1.8b microcompact, 7e, 10, 11e) that mutates the assembled prefix at runtime. Only replaying the real `runner.Turn → LlmAgent.Run → Build` path exercises that.
**Index-set hash (D-06a forward-compat):**
```go
// Source: NEW internal/agent/prompt/hash.go — uses internal/canonicaljson (deterministic)
// indices is {0} today; amendment #11 extends to {0,1,2} once Slices 10/11e ship.
func PrefixHash(msgs []llm.Message, indices []int) (string, error) {
    h := sha256.New()
    for _, i := range indices {
        if i >= len(msgs) { continue } // [1]/[2] absent until Slices 10/11e
        b, err := canonicaljson.Marshal(msgs[i])
        if err != nil { return "", err }
        h.Write(b)
    }
    return hex.EncodeToString(h.Sum(nil)), nil
}
```
**Fixtures (Claude's Discretion resolved):** `scripts/fixtures/cache_invariant/turn-{01..20}.json` — each file holds the *user message* for that turn AND the *scripted FakeClient response* (so the replay is deterministic + self-contained). MUST include tool-call turns (e.g. turns that script a `current_time` or `tool_search` call + a `tool` result, then a `text_response`), not just text turns — a tool round is exactly where a future slice could poison the prefix. Keep content fixed (no clock, no UUIDs in fixture text).
**Wrapper + exit codes (PRD §amendment #16 line 1618):** `0` pass, `1` mutation detected (explicit `messages[0] mutated at turn N — diff: <prev> vs <cur>`), `2` fixture corrupt. The bash wrapper invokes `go run ./cmd/aura cache-audit`, captures stdout, and `diff`s consecutive hash lines; the Go subcommand itself can also assert and exit non-zero (belt-and-suspenders, mirrors `loop_budget_smoke.sh`).

### Anti-Patterns to Avoid
- **Putting PromptBuilder in `internal/llm`** — import cycle (Pitfall 1). PRD's file-target is wrong here.
- **Marking history messages with `cache_control`** — moves the Anthropic breakpoint every turn, the exact poisoning the gate prevents.
- **Synthetic `Build()`-only hash in the gate** — trivially green, catches nothing (D-04 rationale).
- **Threshold-gating cache hit-rate in CI** — hit-rate is provider-dependent and flaky (PRD OQ4). CI gates the *invariant* (byte-identity); the *percentage* is measured by `cache-stats`, never gated. (Mirrors the project's "latency env-tunable, correctness hard-gated" memory.)
- **Reaching into runner internals for the captured request** — read `FakeClient.Requests[n]` (D-05); it already clones Messages so a later in-place mutation can't corrupt the snapshot (`fakeclient.go:57-59`).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Deterministic prefix hashing | Custom `json.Marshal` + manual key-sort | `internal/canonicaljson.Marshal` | Already the project's deterministic serializer (sorted keys, json.Number, strict-reject); reused by dedup + Phase 4 + Phase 11. [VERIFIED: canonicaljson.go:5-7] |
| Stub LLM for the replay | New fake client | `agenttest.FakeClient` | Importable, captures `Requests`, clones Messages, goleak-clean by construction. [VERIFIED: fakeclient.go] |
| Token/cost parsing from the wire | Re-parse SSE usage | The trailing `llm.Usage` chunk | `openai_compat/sse.go`+`usage.go` already parse `prompt_tokens_details.cached_tokens` + `usage.cost`. [VERIFIED: CONTEXT D-02a + sse.go] |
| Time-window metric aggregation | Pull-all + Go loop | SQL `GROUP BY`/`sum()` with a `ts` index | One bounded indexed query vs. full-table scan into Go. (§Pattern 3) |
| Subcommand dispatch | New CLI framework | The existing `case "<sub>"` switch in `main.go` | `db.go`/`neo4j.go`/`identity.go` establish the pattern; +2 cases. [VERIFIED: main.go:35-61, db.go:19-42] |

**Key insight:** This phase is almost entirely *reuse + one refactor*. The temptation to "build a cache system" is the trap — the cache discount is the provider's; Aura's job is the *invariant* (stable prefix) + *measurement* (persist already-emitted usage) + *enforcement* (the gate).

## Common Pitfalls

### Pitfall 1: The PRD's `internal/llm/prompt.go` location causes an import cycle
**What goes wrong:** Following the PRD file-target literally, `PromptBuilder` in `internal/llm` needs `tools.Registry.RenderToolDefs()`, but `internal/agent/tools` already imports `internal/llm` (`manifest.go:7`). `internal/llm` importing `internal/agent/tools` = cycle → build break.
**Why it happens:** PRD was written before the package layout settled; the system prompt + manifest live in `internal/agent*`.
**How to avoid:** Put `PromptBuilder` in `internal/agent/prompt` (or `internal/agent`). `internal/agent` already imports both `tools` and `llm` (`llm_agent.go:21-23`) — no cycle. **Record as a PRD-amendment.** [VERIFIED: grep import directions — `tools`→`llm` exists, `llm`→`tools` does not.]

### Pitfall 2: Forgetting the D-02 PRD-amendment-before-code ordering
**What goes wrong:** D-02 (Postgres persistence) overrides PRD OQ2 ("in-memory only"). PRD-first principle (CLAUDE.md) forbids code that contradicts the PRD without an amendment commit first.
**Why it happens:** The decision is locked in CONTEXT, but the PRD still says "in-memory."
**How to avoid:** First commit of the phase is a PRD-amendment editing §Slice 4 OQ2 (in-memory → Postgres `cache_metrics`) AND the file-target table (`internal/llm/prompt.go` → `internal/agent/prompt/`). Then the code. (D-03 needs NO amendment — it aligns with OQ3 "Proposto: SÌ".)
**Warning signs:** A `cache_metrics` migration appearing before any `prd:` amendment commit in `git log`.

### Pitfall 3: A "green" gate that exercises nothing (skip-as-green / synthetic)
**What goes wrong:** A synthetic `Build()` hash, or a gate whose fixtures never drive a tool round, passes without testing the runtime mutation paths the gate exists for.
**Why it happens:** `messages[0]` is constant by construction → easy to write a trivially-true test.
**How to avoid:** Runtime-faithful replay (D-04) + fixtures that include tool-call turns + the `loop_budget_smoke.sh` discipline (assert real output lines, fail loudly on empty). The gate must actually invoke `go run ./cmd/aura cache-audit` and count 20 hash lines. [VERIFIED pattern: loop_budget_smoke.sh:30-42]

### Pitfall 4: Migration `0006` used `CREATE INDEX CONCURRENTLY` as its sole statement
**What goes wrong:** golang-migrate v4 runs each migration in an implicit transaction; `CONCURRENTLY` cannot run in a tx block. If `0007` adds a plain index it's fine, but if anyone later adds CONCURRENTLY it must be the sole statement (Pitfall noted in `0005` comments line 70-73).
**How to avoid:** `0007_cache_metrics` uses a plain `CREATE INDEX` (no CONCURRENTLY needed — fresh table, no rows). Keep it multi-statement-safe. [VERIFIED: 0005 comment line 70-73, 0006 is the CONCURRENTLY migration.]

### Pitfall 5: Coverage floor 85% across the full tag matrix (CLAUDE.md, overrides PRD 75/60)
**What goes wrong:** New files (`prompt/builder.go`, `cmd/aura/cache.go`, sqlc consumers) drop combined coverage below 85%.
**How to avoid:** `builder_test.go` covers byte-identity/monotonic-growth/no-mutation/anthropic-wire/hash-index-set; a `cache_metrics` integration test (db_integration tag) covers the INSERT + window queries; the audit subcommand is covered behaviorally by the smoke script + a focused unit test on `PrefixHash`. Report the **combined** figure (unit + integration + smoke), not unit-only.

## Runtime State Inventory

> This is a refactor that touches the prompt-assembly path. A grep audit finds files; runtime state is below.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `aura.conversations` already aggregates `total_cached_tokens`/`total_cost_usd` (0005). New `aura.cache_metrics` is *additive* per-turn detail — no migration of existing rows. | New migration 0007 only; no backfill. |
| Live service config | None — no external service stores any Phase-6 string. The OpenRouter endpoint is config-driven (`AURA_LLM_BASE_URL`), unchanged. | None — verified by grep (no n8n/Datadog/Tailscale analogue in this Go repo). |
| OS-registered state | None — no OS-level task/service registers a KV-cache string. | None — verified (Aura is a single Go binary; no scheduler/launchd entries this phase). |
| Secrets/env vars | No new secrets. Possible new tunable env (e.g. a `cache-audit` turn count) MUST follow `AURA_<DOMAIN>_<UNIT>` if added; provider stays `AURA_LLM_*` config-driven (no hardcode). | None mandatory; if a knob is added, document it in the PRD env index. |
| Build artifacts | sqlc-generated `internal/db/sqlc/*.go` will gain `cache_metrics.sql.go` + model — must be regenerated (`sqlc generate`) and committed; CI "sqlc generate is in sync" job will fail otherwise. | Run `sqlc generate`, commit the generated file. [VERIFIED: ci.yml job line 135.] |

**Nothing found in categories Live service config / OS-registered state — verified by repo grep; this is a self-contained Go monorepo with no external runtime registrations touched by prompt assembly.**

## Code Examples

### Subcommand dispatch (mirror the existing pattern)
```go
// Source: cmd/aura/main.go:35-61 (existing) — add two cases
case "cache-stats":
    runCacheStats(os.Args[2:])
case "cache-audit": // hidden — not advertised in usage()
    runCacheAudit(os.Args[2:])
```

### The one-line persist addition (no wire-path touch)
```go
// Source: internal/runner/runner_persist.go:58-78 (persistAssistantAnswer) — add a sibling INSERT.
// u (llm.Usage) and cost are ALREADY computed here for the AppendTurn aggregate call.
if err := r.cacheMetrics.Insert(ctx, sqlc.InsertCacheMetricParams{
    ConversationID: convUUID, Seq: int32(seq),
    PromptTokens: int32(u.PromptTokens), CachedTokens: int32(u.CachedTokens),
    CostUsd: cost, // numeric(10,4)
}); err != nil {
    return fmt.Errorf("persist cache metric: %w", err)
}
```
*(Inject a narrow `CacheMetricStore` interface into `runner.Deps` mirroring `ConversationStore`, so the audit can pass a no-op fake — `runner/interfaces.go` already establishes narrow consumer-side Store interfaces, D-A2-02.)*

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Inline `llm.Request{}` construction in `LlmAgent.Run` | Named `PromptBuilder.Build(...)` chokepoint | This phase (Slice 4) | Single hook for CI gate + future `messages[1]/[2]` tiers |
| PRD: cache stats in-memory only (OQ2) | Postgres `aura.cache_metrics` per-turn | D-02 (amendment this phase) | `cache-stats --since` becomes a real time-windowed query |
| `client.go` comment "wire layer is unaware [of caching]" | Provider-aware `cache_control` seam exists (dormant) | D-03/D-03a | Slice 13 `LLMRouter` activates it for Anthropic-direct |

**Deprecated/outdated:**
- PRD `aura chat-loop` REPL smoke → superseded by shipped `aura chat`; replay lives in `cache-audit` (CONTEXT Deferred).
- PRD file-target `internal/llm/prompt.go` → `internal/agent/prompt/` (cycle; amendment).
- PRD file-targets `cache_deepseek.go` (~50 LOC) — the DeepSeek usage parsing it describes is ALREADY shipped in `openai_compat/sse.go`+`usage.go` (D-02a), so this file is likely unnecessary; fold any residual into the metrics path. Flag to planner.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Anthropic `cache_control: {"type":"ephemeral"}` goes on the system block + last tool def, never on history. | Pattern 2 | LOW — CITED from Anthropic docs; and it's a *dormant no-op* under OpenRouter this phase, so even if the exact block placement differs, Slice 13 (which actually activates it) re-verifies. The phase-6 assertion is only "OpenRouter requests carry NO cache_control." |
| A2 | `numeric(10,4)` is the right USD precision for `cost_usd`. | Pattern 3 | LOW — copied verbatim from the shipped `0005_conversations.total_cost_usd` convention. |
| A3 | A `CacheMetricStore` can be injected into `runner.Deps` without disturbing the existing narrow-interface pattern. | Code Examples | LOW — `runner/interfaces.go` already defines Conv/Pause/Identity narrow Stores (D-A2-02); this is one more of the same shape. Planner should read `interfaces.go` to confirm constructor wiring. |

**Note:** No `[ASSUMED]` package names exist (zero installs). All structural claims are `[VERIFIED]` against the codebase; the single external claim (A1) is `[CITED]` and rendered low-risk by the dormant-seam design.

## Open Questions

1. **Is `internal/llm/cache_deepseek.go` (PRD ~50 LOC) needed at all?**
   - What we know: D-02a says the DeepSeek/OpenRouter usage parsing (`cached_tokens`, `cost`) is already shipped in `openai_compat/sse.go`+`usage.go`.
   - What's unclear: whether the PRD intended any *additional* DeepSeek logic beyond parsing.
   - Recommendation: Treat the parsing as done; do NOT create `cache_deepseek.go`. If the planner finds a residual need, fold it into the metrics/PromptBuilder path. Note the file-target drop in the PRD-amendment.

2. **Should the audit's in-memory Stores be new fakes or reuse `runner/fakes_test.go`?**
   - What we know: `runner/fakes_test.go` exists but is `_test.go` (unreachable from a shipped subcommand — same constraint that ruled out `cmdfakes_test.go` in D-05).
   - What's unclear: whether to promote a minimal in-memory store to a non-test package or build a tiny one inside `cmd/aura/cache.go`.
   - Recommendation: A tiny in-memory `CacheMetricStore`/`ConversationStore` no-op in the `cmd/aura` (or a small `internal/agent/agenttest`-adjacent) non-test file, mirroring how `agenttest.FakeClient` is a shipped non-test fake. Planner decides exact home; must be importable by `cmd/aura`.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all | ✓ (assumed dev env) | 1.25+ | — |
| Postgres (compose) | `cache_metrics` integration tests + `aura cache-stats` live | ✓ via `make db-up` (CI job exists) | 15+ | The `cache-audit` gate needs NO Postgres (in-memory Stores) — only the metrics integration tier + live `cache-stats` need it |
| sqlc | regenerate `cache_metrics.sql.go` | ✓ (CI installs v1.31.1) | v1.31.1 | — |
| golang-migrate | apply 0007 | ✓ (project lib) | v4.x | — |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** the invariant gate (the phase's headline deliverable) is deliberately Postgres-free, so it runs in any CI job without the DB stack.

## Validation Architecture

> nyquist_validation assumed enabled (no `.planning/config.json` override read as false). This section derives VALIDATION.md / Nyquist Dimension 8.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + table-driven (`golang-testing` skill); `goleak` + `-race` per project discipline |
| Config file | none (Go convention) — tags: `db_integration` for the metrics tier |
| Quick run command | `go test ./internal/agent/prompt/...` |
| Full suite command | `go test -race -count=1 ./... && go test -tags db_integration -race ./internal/db/... ./internal/runner/... && bash scripts/cache_invariant_audit.sh` |

### Phase Requirements → Test Map (the 5 ROADMAP Success Criteria)
| SC | Behavior | Test Type | Automated Command | File Exists? |
|----|----------|-----------|-------------------|-------------|
| SC#1 | SHA-256(messages[0]) constant across 20-turn replay, printed to stdout | smoke (runtime-faithful) | `bash scripts/cache_invariant_audit.sh` (drives `go run ./cmd/aura cache-audit`) | ❌ Wave 0 |
| SC#1 (unit) | `PromptBuilder.Build` byte-identical [0] over N turns + monotonic history growth + no in-place mutation | unit | `go test ./internal/agent/prompt/ -run TestBuildPrefixStable` | ❌ Wave 0 |
| SC#2 | `usage.prompt_cache_hit_tokens` (CachedTokens) rises from turn 2 — provider-side, NOT CI-gated | manual-only (live DeepSeek) | manual `aura chat send` ×3 + `aura cache-stats` (documented in VALIDATION Manual-Only; flaky/provider-dependent per PRD OQ4) | ❌ Wave 0 (manual) |
| SC#3 | Anthropic-provider build carries `cache_control` on system+tools, NOT history; OpenRouter build carries none | unit (wire-shape) | `go test ./internal/agent/prompt/ -run TestCacheControlSeam` | ❌ Wave 0 |
| SC#4 | `aura cache-stats --since=1h` returns the window; hit-rate ≥80% target is *measured* not gated | integration (query) + manual (the 80% number) | `go test -tags db_integration ./internal/db/... -run TestCacheMetrics` + manual cache-stats read | ❌ Wave 0 |
| SC#5 | CI fails with explicit "messages[0] mutated at <site>" on a poisoning PR | smoke (negative) | `scripts/cache_invariant_audit.sh` returns exit 1 + the explicit message when a fixture/build mutates [0] | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/agent/prompt/... && go vet ./... && go build ./...`
- **Per wave merge:** full unit `-race` + `db_integration` metrics tier + `bash scripts/cache_invariant_audit.sh`
- **Phase gate:** full suite green + the new CI "cache invariant gate" step green + combined coverage ≥85% (CLAUDE.md floor, full tag matrix).

### Wave 0 Gaps
- [ ] `internal/agent/prompt/builder_test.go` — covers SC#1 (unit) + SC#3 (byte-identity, monotonic growth, no-mutation, cache_control seam, index-set hash)
- [ ] `scripts/cache_invariant_audit.sh` + `scripts/fixtures/cache_invariant/turn-{01..20}.json` — covers SC#1 (smoke) + SC#5 (negative); fixtures MUST include tool-call turns
- [ ] `internal/db/.../cache_metrics_integration_test.go` (build tag `db_integration`) — covers SC#4 (INSERT + `--since` window + aggregate)
- [ ] `cmd/aura/cache_test.go` — `cache-stats` flag parsing (`--since` → duration), `cache-audit` exit-code contract (0/1/2)
- [ ] CI wiring: `.github/workflows/ci.yml` step `name: cache invariant gate` invoking the wrapper (Postgres-free job)
- [ ] **Negative test discipline (SC#5):** a test that deliberately mutates a fixture's `messages[0]` and asserts the gate exits 1 with the explicit message — without this, SC#5 is unproven (the gate could be silently green).

*Mutation spot-check (CLAUDE.md): run `go-mutesting` on `internal/agent/prompt/builder.go` + `hash.go` (≥70% killed) — these are the cache-load-bearing files.*

## Security Domain

> `security_enforcement` assumed enabled (no config override). This phase is internal plumbing with a narrow surface.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — (no auth surface) |
| V3 Session Management | no | — |
| V4 Access Control | yes (DB) | Postgres role separation: `cache_metrics` GRANTs SELECT/INSERT to `aura_app`, ALL to `aura_migrate` (mirrors 0005; migrations gated via `AURA_DB_MIGRATE_URL`, amendment #17). |
| V5 Input Validation | yes | `--since` parsed via `time.ParseDuration` (reject unparseable); sqlc parameterized queries (no string concat). |
| V6 Cryptography | yes (hashing) | `crypto/sha256` for the invariant hash — a content fingerprint, NOT a security primitive; no key handling. |
| V7 Error Handling/Logging | yes | Never log `llm.Config` (D-28 structural redaction); cache metrics carry no secrets (tokens are counts, not content). |

### Known Threat Patterns for {Go prompt assembly + Postgres metrics}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Cache-prefix poisoning by a future slice mutating `messages[0]` | Tampering | The runtime-faithful CI gate (this phase's headline deliverable) — SHA-256 invariant on the real loop. |
| SQL injection via `--since` | Tampering | sqlc-generated parameterized query + `time.ParseDuration` validation; never interpolate the arg. |
| Secret leakage through metrics | Information Disclosure | `cache_metrics` stores token *counts* + cost only — no message content, no API key; `llm.Config` never logged. |
| Privilege escalation via migration role | Elevation | `aura_app` gets SELECT/INSERT only (no DDL); DDL requires `aura_migrate` + `AURA_DB_MIGRATE_URL` (amendment #17). |

## Sources

### Primary (HIGH confidence)
- Codebase (read this session): `internal/agent/prompt.go`, `internal/agent/tools/manifest.go`, `internal/agent/agenttest/fakeclient.go`, `internal/llm/client.go`, `internal/llm/config.go`, `internal/runner/runner.go`, `internal/runner/runner_persist.go`, `internal/agent/llm_agent.go`, `internal/db/migrations/0005_conversations.up.sql`, `internal/db/queries/conversations.sql`, `sqlc.yaml`, `cmd/aura/main.go`, `cmd/aura/db.go`, `scripts/loop_budget_smoke.sh`, `.github/workflows/ci.yml` — import-graph, persist seam, conventions all VERIFIED.
- `prd.md` §"Slice 4 — KV cache builder" (L1469-1549), §"KV cache invariant CI amendment #16" (L1599-1626), §"Context rot mitigation policy amendment #21" L0 (L1559), OQ3 (L1531), amendment #11, amendment #30/Seam D00.
- `.planning/ROADMAP.md` §Phase 6 (5 success criteria, L176-188); `.planning/REQUIREMENTS.md` (CAP-04 mapping, L35/L113).
- `.planning/phases/06-kv-cache-builder/06-CONTEXT.md` + `06-DISCUSSION-LOG.md` (locked decisions).

### Secondary (MEDIUM confidence)
- `claude-api` project skill (`.claude/skills/claude-api/`) — Anthropic API Go/curl examples for the `cache_control` wire shape.

### Tertiary (CITED, external)
- Anthropic prompt-caching docs (`docs.anthropic.com/en/docs/build-with-claude/prompt-caching`) — `cache_control: {"type":"ephemeral"}` placement on system block + last tool definition, not on history. [CITED — rendered low-risk by the dormant-seam design; Slice 13 re-verifies on activation.]

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new packages; every reuse VERIFIED in the codebase this session.
- Architecture: HIGH — import-cycle confirmed by grep (D-01a correct); persist seam + provider threading VERIFIED at exact file:line.
- Pitfalls: HIGH — each grounded in a read file (migration 0006 CONCURRENTLY note, coverage floor, PRD-first ordering).
- Anthropic wire shape: MEDIUM-HIGH — CITED from docs + claude-api skill; low-risk because dormant this phase.

**Research date:** 2026-06-02
**Valid until:** ~2026-07-02 (stable — internal codebase + a settled external API; re-check only if the package layout or `llm.Request` shape changes before planning).
