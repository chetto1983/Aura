# Aura — RAG-Protected Compaction Slice (2026-05-25)

Targeted replacement for "Wave OH2 — History hygiene" in
`docs/aura-graph-tools-plan-2026-05-25.md`. The user pushed back on
shipping generic openhuman-style compaction wholesale: graph/RAG output
is evidence-class and a generic TokenJuice/summarizer/microcompact pass
risks corrupting SEED_CONTENT, SEED_SNIPPET, ground-truth body bytes.

This slice is exactly the four points the user asked for:

1. Bypass/rule: `search(subgraph|read|surprises|gaps|diff|...)` and
   evidence-class tools NEVER get summarised; TokenJuice already
   pass-through-safe on them, but `payload_summarizer.MaybeSummarize`
   currently shoots an LLM at a CAPSULE — that's the active leak.
2. Head-preserving budget on the FRESH tool result as backstop.
3. Compact markdown capsule as the primary RAG/graph output format.
4. Microcompact only on OLD tool results (≥ keep_recent + 1 turns ago),
   never on the fresh result.

---

## 0. Source — openhuman 3-stage pipeline (documented for traceability)

Read directly from `D:/tmp/openhuman/src/openhuman/context/{pipeline,microcompact,tool_result_budget,guard}.rs`.
Concepts only — no code lifted (GPLv3 isolation). Citation format:
`<file>:<lines>` so a future reader can re-read.

### Stage 1 — `tool_result_budget.rs:63-108` — fresh-bytes head-preserving cap

```
budget = 16 KB default, TRAILER_RESERVED = 256 bytes
head_capacity = budget - TRAILER_RESERVED  (cut at UTF-8 boundary)
out = head + "\n\n[… N bytes truncated by tool_result_budget — re-run with a narrower query …]"
```

Crucial invariants:
- Runs INLINE inside `execute_tool_call`, BEFORE the result enters
  history.
- Operates on FRESH bytes (the call that just returned). Never on
  bytes already in history.
- Does NOT mutate previously-sent history → does NOT break KV-cache.
- Cut at UTF-8 char boundary — never splits a multi-byte rune.
- Trailer reservation is fixed (256 bytes) so the marker is never
  itself truncated.

### Stage 2 — `Agent::trim_history` — terminal hard cap (not lifted, Aura has it)

Aura already enforces a 50-message cap per `internal/conversation`.
Same role openhuman delegates to `trim_history`. Nothing to do here.

### Stage 3 — `microcompact.rs:59-100` — OLD-tool-results-only placeholder

```
CLEARED_PLACEHOLDER = "[Old tool result content cleared]"
DEFAULT_KEEP_RECENT_TOOL_RESULTS = 5
```

Algorithm:
1. Walk history, collect indices of every `ToolResults` envelope.
2. Peel off `keep_recent` from the END — those stay intact.
3. For each older envelope: replace `content` with the placeholder;
   leave the envelope+ID pairing intact (so the
   `AssistantToolCalls ⇔ ToolResults` API invariant holds).
4. Idempotent: a second pass on the same history is a no-op because
   already-cleared entries match the placeholder.

Crucial invariants:
- The FRESH tool result is in the most-recent `keep_recent` slice →
  NEVER touched.
- Deliberately invalidates KV-cache once → next turn re-prefills →
  the new smaller prefix becomes the cache target.
- Pipeline orchestrator only fires this when guard says
  `CompactionNeeded` (utilisation ≥ 0.90).

### Stage 4 — `guard.rs:50-164` — pre-call utilisation circuit breaker

```
COMPACTION_TRIGGER_THRESHOLD = 0.90  (soft)
HARD_LIMIT_THRESHOLD         = 0.95
MAX_CONSECUTIVE_FAILURES     = 3     (breaker)
```

Outcome enum: `{Ok | CompactionNeeded | ContextExhausted}`. Breaker
trips after 3 consecutive compaction failures → above 0.95 →
`ContextExhausted`. Aura partially has this in `loop_budget.go` but
post-hoc, not as a pre-call gate.

### Stage 0 — `tokenjuice/tool_integration.rs:69-162` — pattern-matched output compaction

Pre-existing in Aura at `internal/tokenjuice/`. Rule shape:
- `MIN_COMPACT_INPUT_BYTES = 512` — skip tiny outputs.
- `MIN_COMPACT_RATIO = 0.95` — pass-through if compaction doesn't
  shrink by ≥5%.
- Rules match on `tool_name` / `commandIncludes` / `argvIncludes`.
- Failure-preserving when `exit_code != 0`.

**The implicit evidence-preservation in openhuman is**: no TokenJuice
rule matches `search(...)` style tools, so they always pass through.
Aura inherits that — TokenJuice is safe by default on graph/RAG.

The actual leak in Aura is one layer above: the LLM-based payload
summarizer.

---

## 1. Aura's current stack — what's there, what leaks

Per-tool-result pipeline in `internal/agent/executor.go:100-148`
(parallel goroutine per tool call):

```
raw = executeOneTool(ctx, call)
if tokenJuiceEnabled:
    raw = CompactToolOutput(logger, call.Name, call.Arguments, raw)   # passes through on RAG
if payloadSummarizer != nil:
    if sp := payloadSummarizer.MaybeSummarize(ctx, call.Name, "", raw); sp != nil:  # ← LEAK
        raw = sp.Summary
wrapped = WrapUntrustedToolResult(call.Name, raw)
if spillDir != "" && len(wrapped) > SpillThresholdBytes:
    SpillOutput(...)
else:
    limitToolContent(wrapped, e.maxChars)   # ← MIDDLE-truncate, destroys CAPSULE
```

Five concrete problems:

| # | Surface | Failure mode | Evidence |
|---|---|---|---|
| 1 | `payload_summarizer.MaybeSummarize` | LLM shoots at a CAPSULE/SEED block when raw ≥ 16K tokens. Output is a paraphrase — IDs / seeds / scores lose their bit-exact identity | `executor.go:124`, `exec_helpers.go:185` — `parentTaskHint=""`, called for every tool unconditionally |
| 2 | `payload_summarizer.MaybeSummarize` | `parentTaskHint` parameter exists (`payload_summarizer.go:99`) and the prompt template uses it (`payload_summarizer.go:210`), but every CALLER passes `""` | `executor.go:125`, `exec_helpers.go:186` |
| 3 | `limitToolContent` / `TruncateMiddle` | MIDDLE truncation cuts the CAPSULE header AND the closing CONTENT bytes. For RAG output where the CAPSULE header carries SEARCH_SEEDS + PPR_SEEDS context, this destroys the structure the agent needs to reason | `truncate.go:18-39` |
| 4 | `search(action=read)` | Returns JSON `{slug, title, body, frontmatter}` — ~30% of bytes are quoting overhead vs a markdown capsule | `search.go:397-432` |
| 5 | Compaction lifecycle | No microcompact equivalent. Once a tool result is in history it stays full-size forever (until the 50-message cap evicts it whole). Heavy turn-N is paying the heavy-turn-N-K cost on every subsequent turn | grep `microcompact|CleardPlaceholder` → 0 hits in `internal/` |

Plus one positive datapoint that constrains the design:

- TokenJuice rules don't match `search`/`wiki_page`/`source(action=read)`
  → those tools already pass through. We do NOT need to add TokenJuice
  exclusions; we need to make sure NOTHING ABOVE TokenJuice (the
  summarizer + the truncate) corrupts evidence either.

---

## 2. The slice — 5 atomic stories, ~600 LOC, 1 session

Per `feedback_one_module_per_slice` each ships as its own atomic commit.
Per `feedback_per_module_deep_refactor_mandatory` each commit includes
the deep-refactor checklist in the same commit.

### RAG-PROT-1 — Evidence-class predicate + skip payload_summarizer (~80 LOC) [TRIVIAL]

- **Why**: the active leak. `payload_summarizer` LLM-summarises a CAPSULE
  whenever raw ≥ 16K tokens. Even with the contract prompt rewritten,
  this destroys bit-exact IDs/scores/seeds that the next agent
  iteration needs to cite.
- **Touches**: new `internal/agent/governance/evidence_class.go`,
  `internal/agent/executor.go:124-128`, `internal/agent/exec_helpers.go:185-189`,
  `internal/agent/governance/payload_summarizer.go` (no behaviour
  change — predicate consulted at the CALLER).
- **Predicate**:
  ```go
  // IsEvidenceClassTool returns true for tools whose output is
  // ground-truth-dense and must NEVER be LLM-summarised. Compaction
  // would paraphrase identifiers, scores, citations the agent must
  // reproduce verbatim downstream.
  func IsEvidenceClassTool(toolName string, args map[string]any) bool {
      switch toolName {
      case "search":
          // Every action under search is evidence-class: search/list/read/
          // lessons/user_facts/god_nodes/subgraph/path/diff/gaps/surprises/
          // suggest_questions. The shared trait: results cite slugs the
          // agent must spell exactly.
          return true
      case "wiki_page":
          // wiki_page(read) returns frontmatter + body. wiki_page(write/edit)
          // doesn't return content, but the wrapper is consistent.
          return true
      case "source":
          // source(read) returns ocr.md / extract.md. Citations must stay
          // bit-exact.
          if a, _ := args["action"].(string); a == "read" {
              return true
          }
          return false
      case "task":
          // task(list) cites task IDs that the agent must cancel/run-now by id.
          if a, _ := args["action"].(string); a == "list" {
              return true
          }
          return false
      default:
          return false
      }
  }
  ```
- **Wiring**:
  ```go
  if e.payloadSummarizer != nil && !governance.IsEvidenceClassTool(call.Name, call.Arguments) {
      if sp := e.payloadSummarizer.MaybeSummarize(ctx, call.Name, parentTaskHint, raw); sp != nil {
          raw = sp.Summary
      }
  }
  ```
- **Acceptance**:
  - 8 sub-tests covering each evidence-class case + 3 non-evidence
    controls (`execute_code`, `web(search)`, `create_document`).
  - 1 integration test: feed a 20KB CAPSULE through executor with a
    summarizer that records every call — assert ZERO summariser
    invocations for `search(action=subgraph)`.
  - `ctxmetrics.Global.PayloadSummarizationsTotal` no longer
    increments on RAG calls (instrument).

### RAG-PROT-2 — Wire `parent_task_hint` from executor (~50 LOC) [TRIVIAL]

- **Why**: the field exists and the prompt uses it; the wiring passes `""`.
  Today summariser is generic; with the user's question as hint it
  preserves the right fields.
- **Touches**: `internal/agent/executor.go` + `internal/agent/exec_helpers.go`
  (caller sites), `internal/agent/runtask.go` (parent task context),
  `internal/agent/governance/payload_summarizer.go` (no signature change —
  already accepts hint).
- **Source for hint**: the user's CURRENT-turn message (latest
  `role=user` content in the active conversation, OR the parent task
  description when running inside a sub-agent loop). Cap to 512 chars
  to keep summariser prompt small.
- **Acceptance**:
  - 3 tests: hint propagates verbatim (capped to 512) to summariser
    callLLM input; empty hint stays empty (back-compat); sub-agent
    inherits parent's hint via runtask path.
  - The prompt assertion (`payload_summarizer.go:212` "Context: ...")
    now ALWAYS includes the user question when present.

### RAG-PROT-3 — Head-preserving fresh-result budget + trailer (~120 LOC) [SMALL]

- **Why**: `limitToolContent`/`TruncateMiddle` cuts head AND tail. For
  CAPSULE output (CAPSULE / QUERY / TRAVERSAL / SEARCH_SEEDS / PPR_SEEDS /
  CONTENT / NODE / EDGE), the HEAD is the structural context the agent
  needs. Tail-only truncation preserves the most-important bytes.
- **Touches**: new `internal/agent/tools/registry/headbudget.go`,
  replaces `limitToolContent` call site in `executor.go:143`.
  Existing `TruncateMiddle` stays for the other use cases (e.g.
  `read_file` where the file body is the answer and middle-truncate
  is fine).
- **Default budgets**:
  - Evidence-class tools: 32 KB (16K tokens) — RAG/graph needs room.
  - Other tools: 16 KB (4K tokens) — matches openhuman default.
  - Trailer reservation: 256 bytes (matches openhuman exactly).
- **Signature**:
  ```go
  // ApplyFreshToolResultBudget caps raw at budgetBytes preserving the
  // head. If raw fits, returns it unchanged. Otherwise returns a
  // UTF-8-safe head prefix followed by the trailer:
  //   [… N bytes truncated by tool_result_budget — re-run with a narrower query …]
  // Cut is made at a rune boundary at budgetBytes - TrailerReservedBytes.
  // Never mutates history — call site is BEFORE history insertion.
  func ApplyFreshToolResultBudget(raw string, budgetBytes int) (string, BudgetOutcome)
  ```
- **Acceptance**:
  - 6 tests: pass-through (under cap), exact-boundary cut, multibyte
    char near boundary (no rune split), zero-budget → noop, trailer
    contains drop-count, BudgetOutcome reports correct byte deltas.
  - Replaces existing `limitToolContent` only at the executor / exec
    helpers call sites; runtask `truncate.go:43-48` TruncateMiddleByTokens
    stays for legacy / `read_file` paths (audit in same commit per
    deep-refactor rule).

### RAG-PROT-4 — `search(action=read)` returns markdown capsule (~100 LOC) [SMALL]

- **Why**: every other RAG action emits markdown — `search(search)` is
  the slim markdown from today's commit `0bb00697`, `wiki_subgraph`
  emits a CAPSULE, `god_nodes` returns JSON+list. `search(read)`
  returns `{"slug":"x","title":"y","body":"...","frontmatter":{...}}` —
  the JSON envelope wastes ~30% of the tokens on quoting and brackets.
- **Touches**: `internal/agent/tools/registry/search.go:397-432`
  (replace `readWikiPage` JSON marshalling with a markdown renderer).
  Existing JSON shape kept available for the dashboard via a separate
  helper (don't break web API consumers).
- **New format**:
  ```
  PAGE [[slug]] — Title
  category=concept  tags=safety,robot
  updated_at=2026-05-23T10:00Z  related=robot,frame,giunti

  <body>
  ```
- **Acceptance**:
  - 4 tests: round-trip slug/title/body, frontmatter line emitted when
    fields present, empty frontmatter → header line omitted, byte
    delta vs old JSON ≥ 20% smaller on representative pages.
  - Dashboard API path (`internal/api/wiki.go`) still receives JSON via
    the preserved helper — no UI regression.

### RAG-PROT-5 — Microcompact on OLD tool results (~250 LOC) [MEDIUM]

- **Why**: heavy turn cost compounds across the session. After 10 tool
  calls the agent's context carries 10× tool-result bytes forever.
  Microcompact frees the bytes from tool calls older than `keep_recent`
  while preserving the envelope so providers don't 400.
- **Touches**: new `internal/agent/governance/microcompact.go`, hooked
  into `internal/agent/loop.go` between iterations. Re-uses Aura's
  existing utilisation tracking in `loop_budget.go` as the trigger.
- **Algorithm** (1:1 port of openhuman concept, Go-native):
  ```go
  const ClearedPlaceholder = "[Old tool result content cleared]"
  const DefaultKeepRecentToolResults = 5

  // Microcompact clears the body of every tool-result message older
  // than keepRecent. Idempotent. Returns stats.
  func Microcompact(history []llm.Message, keepRecent int) MicrocompactStats
  ```
- **Trigger rule** (the user's #4 constraint):
  - NEVER clears the most-recent `keepRecent` tool-result envelopes.
  - The CURRENT turn's tool result is — by construction — in the
    most-recent slice → fresh is never touched.
  - Fires only when `loop_budget.go` reports utilisation ≥ 70% OR
    iteration index ≥ N (default 6).
- **Acceptance**:
  - 7 tests covering openhuman's test matrix (noop when no tool
    results, noop when all within keep_recent, clears oldest when over,
    envelope invariant preserved, idempotent on second pass, clears all
    entries in multi-entry envelope, fresh result NEVER cleared).
  - 1 integration test: simulate 8-tool-call turn → assert tool result
    from iteration 1 has placeholder body on iteration 7, and
    iterations 4-7 keep their full bodies.
  - `ctxmetrics.Global.MicrocompactRunsTotal` instrumented (new metric).

---

## 3. Wave gate

Live probe (the one that originally triggered "fa cagare il grafo / i
tool"): replay chat 1148481707 turn 263 ("Genera un PDF di riepilogo
delle cose che sai su di me"). Expected after this slice:

| Metric | Pre-slice (turn 263) | After RAG-PROT slice |
|---|---|---|
| Tool calls | 11 | ≤4 |
| LLM rounds | 12 | ≤5 |
| Wall-clock | 62s | ≤30s |
| Tokens in | 112k | ≤40k |
| `search(query="Davide")` count | 4 | 1 |
| Bit-exact slug citations in reply | (n/a — paraphrased) | ≥2 |
| Payload summariser calls on `search(*)` | unbounded | 0 |
| Microcompact runs after iter 6 | 0 | ≥1 |

Hold conditions (do NOT ship slice if any fire):
- Any RAG-PROT-1..5 test flakes ≥ 2/10 runs.
- Replay shows summariser invoked on `search(*)` even once.
- TruncateMiddle still called on evidence-class tools.
- `search(read)` JSON path emits to LLM (only dashboard should see it).

---

## 4. What this slice deliberately does NOT do (vs the old OH2)

Pruned for scope discipline:

| Old OH2 story | Why dropped |
|---|---|
| TOKEN-BUDGET-PRE-DISPATCH (token-aware history trim) | Aura already has 50-message cap; this is upstream of microcompact. Re-evaluate after MCP-heavy installs land 80KB tool results regularly |
| CONTEXT-GUARD pre-call utilisation | Pre-existing in `loop_budget.go`; only the 3-strikes circuit breaker is missing — fold into RAG-PROT-5 if breaker needed in production traces |
| PAYLOAD-CONTRACT extraction prompt rewrite | The contract is real but lower-priority than wiring the hint (RAG-PROT-2). Defer to a follow-up "summariser prompt polish" story |
| TOOLRESULT-BUDGET as a generic store | Folded into RAG-PROT-3 as `ApplyFreshToolResultBudget` |
| MICROCOMPACT as a separate story | Promoted to RAG-PROT-5 |

The principle: every story in this slice traces back to one of the
user's four explicit asks; nothing in this slice exists to "complete
the openhuman wave".

---

## 5. Sequencing + Codex/Ralph/interactive

All 5 stories Codex-eligible. Recommended order:

1. **RAG-PROT-2** (trivial, 50 LOC) — wire the existing hint. Ship first
   to immediately improve summariser quality on the non-evidence path
   while RAG-PROT-1 lands.
2. **RAG-PROT-1** (trivial, 80 LOC) — predicate + skip summariser for
   evidence. Closes the active leak.
3. **RAG-PROT-3** (small, 120 LOC) — head-preserving budget. Closes
   the silent-data-loss path.
4. **RAG-PROT-4** (small, 100 LOC) — markdown capsule for `read`.
   Token-density improvement, independent.
5. **RAG-PROT-5** (medium, 250 LOC) — microcompact. Ships last because
   it's the only one that touches `loop.go` and benefits most from
   the other 4 being settled.

Each commit follows today's hot-fix template (commits `13d004d3` →
`35348bee`): commit body lists files refactored + LOC delta + dead
code removed + tests updated/removed. Lefthook green per commit.

---

## 6. Cross-references

- Master plan this slice replaces wave OH2 in:
  `docs/aura-graph-tools-plan-2026-05-25.md` (commit `5731a125`).
- openhuman concept sources (re-read directly today):
  - `D:/tmp/openhuman/src/openhuman/context/microcompact.rs`
  - `D:/tmp/openhuman/src/openhuman/context/tool_result_budget.rs`
  - `D:/tmp/openhuman/src/openhuman/context/guard.rs`
  - `D:/tmp/openhuman/src/openhuman/context/pipeline.rs`
  - `D:/tmp/openhuman/src/openhuman/tokenjuice/tool_integration.rs`
- Aura existing layer audited:
  - `internal/tokenjuice/` (full TokenJuice port, shipped Phase-TJ)
  - `internal/agent/governance/payload_summarizer.go`
  - `internal/agent/executor.go:100-148`
  - `internal/agent/exec_helpers.go:182-189`
  - `internal/agent/tools/truncate.go:18-48`
  - `internal/agent/tools/registry/search.go:397-432`
- Triggering observation (the bug that started this):
  conversations row chat=1148481707 turn=263-286 (today 10:16:58).

---

## 7. First action

Stage Wave RAG-PROT PRD as a single 5-story queue. Per
`feedback_aura_as_product` max-2-staged: this slice REPLACES OH2 in
the staged slot — Wave G1 stays staged in parallel.

```bash
mkdir -p .planning/phases/wave-rag-prot
# Write prd-wave-rag-prot-staged.json with the 5 stories above
# Drive via Codex, 1 story = 1 atomic commit, lefthook green per commit
```

After RAG-PROT ships, re-run the live probe matrix. If the gate fires
(11 tool calls → ≤4), update `docs/aura-quality-snapshot.md` with the
new strict-pass count and stage Wave G2 (graph signal: surprises,
suggest_questions, IDF exact-match).
