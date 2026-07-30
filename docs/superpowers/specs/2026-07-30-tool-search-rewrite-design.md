# Tool search rewrite — design

Date: 2026-07-30
Status: approved for planning
Supersedes the ranking decisions of spikes 054/055/056/058 for `tool_search` only
(`internal/semindex` and the reasoning classifier are untouched).

## Why

`tool_search` has a green test suite and bad live behaviour. The suite asserts
mechanism (incremental embedding, guard no-ops, cache-stable ordering); nothing
asserts that the right tool comes back. The one test that looks like a retrieval
gate, [`search_corpus_test.go`](../../../internal/agent/tools/search_corpus_test.go),
uses a fixed-map embedder in which every tool carries a hand-written `concept`
label and every query carries the *same* label — a tautology, run over 10 fixture
tools (`calculator`, `translator`, `send_email`) that Aura does not have.

The subsystem was also tuned against a query distribution that does not exist.
Spikes 054/056 assumed the ranker sees the Italian end-user utterance, concluded
BM25 has no lexical signal, and shipped embedding-primary retrieval with BM25
demoted to a narrowly-gated tiebreak. In production the query is written by the
model, not the user.

## Ground truth

All figures below are measured, not estimated.

**Corpus** — `aura toolpipe --manifest-json` against the deployed stack:
58 tools, **54 deferred**, 4 non-deferred (`ask_user`, `read_tool_output`,
`text_response`, `tool_search`). Namespaces: built-in 22, calendar 14,
whatsapp 14, memory 8.

**Query distribution** — every real invocation, from Postgres:

```sql
SELECT c->'function'->>'arguments'
FROM aura.conversation_turns t, jsonb_array_elements(t.tool_calls) c
WHERE c->'function'->>'name' = 'tool_search';
```

42 calls. **26 (62%) are `select:`** — exact name resolution, no ranking involved.
The remaining 16 free-text calls reduce to 10 distinct queries. They are English
capability phrases, several containing literal tool names. Only one is Italian.

**Retrieval quality** — the 10 distinct real queries against the real 54-tool
deferred corpus, through the real ranking code:

| ranker | top-1 | recall@3 | MRR@10 | latency/query |
|---|---:|---:|---:|---:|
| production today (embedding + guarded BM25) | 50% | 100% | 0.700 | 330–443 ms |
| pure embedding | 50% | 90% | 0.692 | 330–443 ms |
| BM25 over Description | 70% | 100% | 0.817 | ~0 ms |
| **BM25 over Summary** | **80%** | **100%** | **0.900** | **~0 ms** |

The guarded tiebreak altered the order on 3/10 queries and did not improve top-1.

**The dominant failure.** `{"query": "web search for B&R 8LSA35.DB030S300-3
specifications"}` returns `document_search` at rank 1. It occurs **seven times
across seven conversations**, plus `"web search for specifications"` and
`"web search for product specifications"`. Two causes, both fixed by this design:

1. `document_search`'s Description ends `"...NOT the public web (use web search/fetch)"`.
   It contains the string *web search* inside a negation. No bag-of-words or
   embedding retriever can honour negation, so the disclaimer reads as a keyword.
2. BM25 would have matched `web_search` exactly, but the guard only fires when
   BM25 returns ≤5 hits. That query floods BM25, the guard no-ops, and the wrong
   embedding answer stands.

**Systematic contamination.** 22 of 54 deferred descriptions name another tool
(`shell_exec` names 8). Searching for A returns B because B's prose discusses A.

**Wiring defect found while probing.** `ToolSearch.Embed` is wired in exactly one
place, [`runner_wiring.go`](../../../internal/runner/runner_wiring.go). Any other
composition of the production registry leaves it nil. `aura toolpipe` — which
builds the full production registry with MCP mounted — returns
`tool_search: semantic ranking unavailable (embed sidecar): no embedder wired`
for every free-text query. The design removes the field, so the defect class
cannot recur.

## Design

`tool_search` stops being a retrieval engine and becomes name resolution with a
lexical fallback. The model already holds the full name list: `sourceOrientation`
prints every deferred tool name, grouped by namespace, into `tool_search`'s
always-visible Description. That is why 62% of real calls are already `select:`.

### Layer 1 — exact name resolution

`select:a,b` as today, uncapped. **New:** a free-text query whose whitespace- or
comma-separated tokens exactly match registered tool names is treated as a
`select:`. This covers the measured case where the model writes
`"memory__memory_add_entity memory__memory_create_relationship memory__memory_add_fact
memory__memory_add_preference"` as free text and today receives a ranking instead
of the four tools it named.

An unknown name in a `select:` must report *that name is not registered*, listing
the closest registered names. It must never fall through to the capability-gap
message. Real incident: during the MCP mount race,
`select:memory__memory_add_entity,memory__memory_create_relationship` returned
*"no matching tools. If the capability you need is a packaged task family… install
skills"* — actively wrong advice for two tools that exist.

### Layer 2 — BM25 over the retrieval document

[`bm25.go`](../../../internal/agent/tools/bm25.go) unchanged in substance, top-5,
stdlib, no network. `searchDocument` changes its input from the long usage prose
to the short capability line:

```
name + name-with-underscores-as-spaces + Summary + argument names + argument descriptions
```

Description is no longer indexed. This is the description-hygiene fix and it needs
no description rewritten: `Summary` already exists and is already clean for all 54
deferred tools (none empty; `document_search — Search the user's uploaded/indexed
documents and return cited chunks.` contains no reference to the web). The index
shrinks from ~8.8k to ~2.0k tokens of indexed text.

The full Description keeps every cross-reference and routing instruction and is
still returned verbatim when a tool is loaded — that advice is valuable *in
context*, and harmful *in the index*. The split is the point: retrieval document
and usage document are different documents.

The MCP `"untrusted MCP server summary data: "` prefix appears in 36 of 54
summaries, so its terms carry near-zero IDF. Measured harmless; left alone.

### Layer 3 — none

## Removed

| Target | LOC | Reason |
|---|---:|---|
| [`adaptive_search.go`](../../../internal/agent/tools/adaptive_search.go) | 247 | A/B control plane in the hot path of every free-text discovery; its benchmark cannot discriminate arms |
| [`search_fusion.go`](../../../internal/agent/tools/search_fusion.go) | 100 | guard calibrated on a query distribution that does not occur; changes order 3/10 and still loses to plain BM25 |
| embedding bank in [`search.go`](../../../internal/agent/tools/search.go) | ~120 | `ensureBank`, `embedQuery`, `banked`, `searchDocumentHash`, the AG-020 rebuild |
| `wireToolSearchEmbedder` in [`runner_wiring.go`](../../../internal/runner/runner_wiring.go) | 49 | no embedder left to wire |
| `internal/adaptive/orderingcontrol/tool.go` | 318 | consumer disappears; `source.go` (source ordering) stays |
| `AURA_TOOLSEARCH_FUSION` | — | knob for deleted behaviour |

Net ≈ **834 LOC of production code removed**, ~40 added. `search.go` drops
410 → ~290. `ToolSearch` loses both the `Embed` and `Adaptive` fields.

Test code follows its subject: `adaptive_search_test.go` (217),
`orderingcontrol/tool_test.go` (392), `search_corpus_test.go` (273), and the
guarded-tiebreak cases in `search_mount_test.go` go with it, replaced by the gate
below.

`InvalidateIndex` stays — the BM25 index still rebuilds on MCP mount
([`bridge.go`](../../../internal/agent/mcptools/bridge.go)) — but collapses to
dropping one pointer, with no hash bank and no incremental re-embed.

`internal/semindex` stays. The reasoning classifier is its other consumer and is
out of scope.

## Callers to update

`cmd/aura/main.go`, `cmd/aura/chat_adaptive_controls.go`,
`cmd/aura/adaptive_benchmark_run_composition.go`,
`cmd/aura/adaptive_benchmark_run_registry.go`,
`cmd/aura/adaptive_benchmark_run_controls_runtime_static.go`,
`internal/runner/runner_wiring.go`.

## The gate

`search_corpus_test.go` is deleted and replaced by a gate that replays the real
distribution: the 10 distinct production queries and their gold tools, against a
manifest fixture dumped from the live registry, scored on top-1 and recall@3. It
runs free in CI — BM25 needs no sidecar, which is the second reason to prefer it.

Thresholds: **top-1 ≥ 70%, recall@3 = 100%** on the current corpus (measured 80%
and 100%). The gate fails on regression, not on absolute perfection.

The corpus is checked in alongside the SQL that produced it so it can be
regenerated and grown as traffic accumulates.

## Honest limitations

- **The query corpus is 10 distinct queries from 42 calls over two days.** It is
  the real distribution, but it is small. The gate must be re-derived as traffic
  grows; a threshold tuned on 10 queries is a regression guard, not proof of
  generality.
- **BM25 degrades on a purely Italian capability query with no shared token.** On
  the measured distribution this is 1/10, and BM25 still returns `web_search` at
  rank 2 because *web* is shared. The structural reason it stays rare is that the
  model writes the query, not the user, and the model writes English capability
  phrases — with the full name list in front of it. If that ever changes, the
  answer is layer 1, not embeddings.
- **Intra-namespace disambiguation is BM25's weak spot** (8 similar `memory__*`
  tools). Layer 1 covers it whenever the model knows the names, which the measured
  traffic says it usually does.

## Adversarial review (partial)

Five hostile reviews were dispatched; **one returned before the review was closed**.
Its findings are recorded here. The literature, production-repo, methodology and
scaling reviews did **not** deliver verdicts and remain open — in particular a
methodology check that had begun investigating whether the embedder is
deterministic, which if it is not would require re-measuring the 50% figure.

### Confirmed by external evidence

**Retrieval recall is the ceiling, and it is the thing nobody measures.** Stacklok:
*"if the correct tool doesn't appear in the search results, even the best model
cannot select it."* Independent replications of Anthropic's own server-side tool
search score far below the vendor number — Arcade at 4,027 tools: regex 56%,
BM25 64% ([arcade.dev](https://www.arcade.dev/blog/anthropic-tool-search-4000-tools-test/));
Stacklok at 2,792 tools: 34% selection / 48% retrieval
([stacklok.com](https://stacklok.com/blog/stackloks-mcp-optimizer-vs-anthropics-tool-search-tool-a-head-to-head-comparison/)).
Both are vendor-authored and sell a competing retrieval layer — treat the direction
as evidence, not the magnitudes. This makes the gate the most important part of
this design, not the least.

**An empty result must not read as an error-free success.** Anthropic's API returns
an empty `tool_references` array silently on no match. This is the same hazard as
the mount-race incident recorded above, and confirms Layer 1's requirement to
distinguish *name not registered* from *no lexical match* from *registry not
loaded*.

**Any message naming a deferred tool must also name the load step.** Claude Code
shipped this defect twice ([#62372](https://github.com/anthropics/claude-code/issues/62372),
[#78360](https://github.com/anthropics/claude-code/issues/78360)) — a nudge or error
string names a deferred tool, the model calls it directly, and misfires on the
first call every time. Cheap invariant worth adding: lint reminder/error strings
for deferred tool names.

**The endogeneity has a measured cost elsewhere.**
[claude-code#60052](https://github.com/anthropics/claude-code/issues/60052): *"the
model sometimes skips the ToolSearch step (because it 'knows' the tool name from
context), which causes a first-call failure followed by a retry."* This is the same
mechanism that produces Aura's 62% `select:` rate.

### Where Aura is already safe

Tool-name shadowing by a later registration (factor-q#177, ClotoCore#283 — an MCP
server silently replacing a sandboxed built-in) does not apply: `Registry.Register`
panics on a duplicate name. Namespacing is already `<server>__<tool>`.

### Two risks this review surfaced that the design does not yet answer

**1. The always-visible name catalog is a documented footgun, and Layer 1 depends
on it.** `sourceOrientation` embeds the live tool-name list into `tool_search`'s
Description. hermes-agent#72560 reports exactly this shape failing: the search
tool's own description advertises removed tools and omits newly added ones, and
because the description is dynamic it is cache-invalidating. Aura's code argues the
output is byte-stable because the registry is immutable per run — but an MCP mount
changes the registry between runs, which is precisely the drift case. Layer 1's hit
rate is a direct function of this catalog being present and correct. **This needs
resolving before implementation**: either the catalog stays and its staleness and
cache cost are measured, or it moves out of the tool schema into a turn-level
context block.

**2. Indexing `Summary` keeps the index attacker-controlled, and BM25 is trivially
stuffable.** MCP tool summaries come from the server. MCPTox
([arXiv:2508.14925](https://arxiv.org/abs/2508.14925)) measures up to 72.8% attack
success across 45 live servers and 353 real tools, with refusal rates under 3% —
and finds *more capable models are more susceptible*. A malicious server can put
`"web search file read shell execute send message"` in a summary and capture the
top rank for every query, at zero cost, under BM25. The existing `"untrusted MCP
server summary data: "` prefix warns the model but does nothing to the ranker. This
design does not make the exposure worse than the current one (Description is
equally server-controlled), but it does not fix it either, and a lexical ranker is
easier to game than a dense one. Worth an explicit decision rather than silence.

**3. The discovery tool must never be droppable.**
[claude-code#77083](https://github.com/anthropics/claude-code/issues/77083):
post-compaction the manifest collapsed to exactly the last `tool_search` result
set — and `tool_search` itself was dropped, leaving no path to reload anything, with
no error surfaced. Aura keeps `tool_search` non-deferred and `Registry.Validate`
excludes it from the actionable count, so the static case is covered; the
compaction path should be checked against `native ∪ loaded`.

## Explicitly out of scope

The deferred-tool grant still travels as markdown: `Execute` writes
`## name … Parameters:` and [`loadedSchemas`](../../../internal/agent/llm_agent_promote.go)
parses that prose back out to decide what is callable. The industrial contract
uses a typed `tool_reference` block
([Anthropic tool search](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool)).
Aura already emits a typed `MetaActivatedTools`; it just does not survive
Postgres, so rehydration falls back to string scanning. This is the most fragile
remaining part of the subsystem, it is working today, and it is a separate change
with its own persistence question. Not in this rewrite.
