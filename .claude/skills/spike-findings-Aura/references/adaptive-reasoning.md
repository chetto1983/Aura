# adaptive reasoning

Adaptive reasoning / LLM routing for Aura: pick a reasoning tier (none/low/high)
per user turn and project it to the provider. The shipped design is a local
granite-embedding tier classifier (spike 052) plus an async oracle-labeled
active-learning loop (spike 053). Two upstream research families (AdaptThink,
AutoThink) were audited and explicitly bounded to "do not adopt at runtime"
(spikes 040/041), and an early MaxTokens budget shim (spike 042) was partly
SUPERSEDED in production. This file is the build recipe; it cites real shipped
files and seams.

## Requirements

Binding non-negotiables (MANIFEST Session-10, plus the shipped production
contract that amended spike 042). Each is a hard constraint.

- **Do not conflate AdaptThink with AutoThink.** AdaptThink (THU-KEG, arXiv
  2505.13417) is training/RL prior art and a released-model watch signal only —
  it is NOT an Aura runtime policy dependency. AutoThink (codelion/optillm) is a
  runtime technique whose ONLY portable subset is complexity classification +
  token budget; its activation steering / PTS vector datasets are out of scope.
- **Never route normal OpenAI-compatible providers through OptiLLM expecting full
  AutoThink.** Upstream marks AutoThink "N/A for proxy"; its implementation needs
  local `PreTrainedModel`/`PreTrainedTokenizer` objects plus PyTorch
  `register_forward_hook` for steering. Activation steering is a separate
  heavy-sidecar/live-benchmark decision and a budget shim must NOT be marketed as
  full AutoThink.
- **`messages[0]` byte-stability and `ToolChoice` are invariant.** Any adaptive
  reasoning seam mutates neither the stable Aura system prompt (cache prefix) nor
  the tool-choice. The classifier/policy work on a copy or set only the
  request-side `Reasoning` field. Enforced by `messages[0]`-hash assertions.
- **Reasoning is a provider-neutral `Reasoning` request config projected to
  OpenRouter's `reasoning` object** (`effort`, `max_tokens`, `exclude`,
  `enabled`). Prefer `reasoning.exclude` (legacy `include_reasoning` is dropped).
  Only apply on OpenRouter targets (`IsOpenRouterReasoningTarget`). Reasoning
  tokens are billed output tokens.
- **The reasoning tier NEVER sets `max_tokens`** (production amendment to spike
  042, the "203-turn disaster" 2026-06-14): per-tier output caps truncated
  tool-call arguments mid-JSON. The operator-configured `cfg.MaxTokens` is left
  untouched; the tier sets only reasoning EFFORT (none/low/high) + `exclude`.
- **The classifier is an OPTIMIZATION, never a hard dependency.** Any embed
  failure returns "no verdict" and the caller degrades to static `low` reasoning
  (when a classifier is wired) rather than spending a second LLM round-trip. The
  embedding seam reuses the SAME granite sidecar (`:8081`, 384d) as documents —
  no new sidecar.
- **Self-improvement labeling is async/offline and margin-gated, never inline.**
  Labeling an uncertain turn with the LLM oracle on the hot path would re-add the
  exact round-trip the classifier eliminated. Curated seeds stay authoritative;
  dedup by content-hash; the refresh mechanism is centroid-refresh, not kNN.
- **The Italian-corpus wire-path score gate stays `>=90%`:**
  `internal/llm/openai_compat.TestAdaptiveReasoningItalianCorpusE2E` over 60
  natural Italian queries (first run 100.0%). Before using reasoning models with
  tools, decide explicitly whether Aura preserves `reasoning_details` across
  tool-call turns (distinct from the stream-only display path).

## How to Build It

The whole feature is SHIPPED. Spike 052 shipped at commit `16cb5380`; the
active-learning loop (053) shipped as `internal/activelearn` +
`internal/reasoninglearn` + `internal/reasoningstore`. Files below are the
authoritative seams.

### 1. Provider-neutral reasoning config (the wire seam)

`internal/llm/client.go`:

```go
type ReasoningEffort string
const ( ReasoningEffortXHigh="xhigh"; High="high"; Medium="medium"; Low="low"; Minimal="minimal"; None="none" )

type ReasoningConfig struct {
    Effort    ReasoningEffort
    MaxTokens int      // populated by builder? NO — Effort only today
    Exclude   *bool
    Enabled   *bool
}
func (r ReasoningConfig) Empty() bool { ... }
```

`llm.Request` carries `Reasoning llm.ReasoningConfig`. The OpenAI-compat client
(`internal/llm/openai_compat/client.go`) serializes it to OpenRouter's
`reasoning` object with `omitempty` on `exclude`/`enabled`.

### 2. The tier policy (no max_tokens; OpenRouter-only until 2026-08-31, now every recognized backend)

`internal/agent/prompt/reasoning_policy.go` — `ApplyAdaptiveReasoning`:

```go
func ApplyAdaptiveReasoning(req *llm.Request, provider string, cfg llm.Config, tier ReasoningTier) {
    if !cfg.AdaptiveReasoning || !IsOpenRouterReasoningTarget(provider, cfg.BaseURL) || !tier.Valid() {
        return
    }
    req.Reasoning = tier.reasoning(cfg.ShowReasoning) // sets ONLY effort+exclude, never MaxTokens
}
```

`tier.reasoning(showReasoning)`: High→`{Effort:high, Exclude:!show}`,
Low→`{Effort:low, ...}`, default→`{Effort:none, ...}`. `exclude = !showReasoning`
(default exclude:true redacts CoT from the wire; `exclude:false` streams it).
`ReasoningTier` ∈ {none, low, high} with `.Valid()`.

Live-verified DeepSeek-V4-Flash behavior baked into the comments (probe
`scripts/deepseek_reasoning_probe.py`, guarded by
`adaptive_reasoning_live_e2e_test.go`):
- `effort:"none"` is the ONLY working off-switch — DeepSeek's native
  `thinking:{type:disabled}` toggle is dropped by OpenRouter. None tier → 0
  reasoning tokens.
- DeepSeek collapses `effort` low/medium → high server-side, so Low vs High are
  the same gear on DeepSeek today; keep both labels (provider-neutral,
  forward-compatible with models that don't collapse).
- `exclude:true` redacts the CoT text but does NOT cap it; `max_tokens` bounds
  only the VISIBLE answer (reasoning ran ~8351 tokens past a 4096 cap and still
  answered) — which is exactly why the tier must not set `max_tokens`.

### 3. The granite embedding classifier (spike 052 → shipped)

`internal/agent/prompt/reasoning_classifier.go`. Replaces the per-turn DeepSeek
"router" round-trip with one ~10ms local embed + cosine argmax.

- Embedder seam: `type Embedder = semindex.Embedder` (alias) — so
  `documents.EmbeddingClient` (granite `:8081`, 384d) satisfies it with no
  adapter, and the classifier + the semantic tool ranker share one interface.
- Centroid/cosine/margin math lives in `internal/semindex` (`Classifier`,
  Centroid mode, `Normalize`, `RankVecs` → `{Label, Ok, Margin}`). This type owns
  only tier policy: defs/seeds, greeting pre-filter, soft fallback.
- Anchors = per-tier mean of L2-normalized embeddings of `def + seeds` (spike 052
  variant B, "few-shot/SetFit-style"). Tier defs are the production router's own
  Italian wording; seeds are curated prototypes (8 per tier in production; spike
  used 5). Build order fixed via `classifierTierOrder` = none<low<high.
- Greeting pre-filter: `trivialGreetings` exact-match allowlist (ciao/salve/grazie/
  ok/perfetto/...) → `ReasoningTierNone` with ZERO embed round-trip. Conservative:
  only unambiguous greetings/acks; anything else falls through to embedding.
- `Classify(ctx, userText) (ReasoningTier, bool)`: greeting check → ensureAnchors
  → embed → `Normalize` → `cls.RankVecs(v)`. Returns `("", false)` on any embed
  failure. If a `Learner` is wired, calls `learner.Observe(userText, v, margin)`
  (non-blocking) for self-improvement.
- `ensureAnchors`: builds once, lazily, guarded by `sync.Mutex` +
  `singleflight.Group` + `generation` counter. **Build failure is NOT cached** —
  the next call retries so a transiently-down sidecar self-heals.
- `Refresh()`: drops cached centroids (`cls=nil; built=false; generation++;
  build.Forget("anchors")`) so the next `Classify` re-folds newly stored
  examples. Safe concurrently.
- `buildAnchors`: per tier, embed `def+seeds` → `cls.AddVecs`. Then fold
  `ExampleStore.LoadExamples` oracle-labeled vectors into the matching tier
  centroid. A store error is non-fatal → fall back to seed-only centroids.

### 4. Router wiring + fallback chain

`internal/agent/llm_agent_reasoning.go` — `adaptiveReasoningTier(ctx)`:

1. Bail unless `cfg.AdaptiveReasoning` AND OpenRouter target.
2. `user = prompt.LastGenuineUserContent(a.history)` (skips synthetic
   `<budget>`/`<workspace>`/`<current_time>`/recovery nudges); empty → `low`.
3. If classifier wired: `classifier.Classify(ctx, user)` → tier (trace
   `adaptive_reasoning_classifier_decision`, source=embedding). On miss → static
   `low` (trace `..._classifier_miss`), NOT a second LLM call.
4. Only with NO classifier wired: the legacy LLM-router path runs (system prompt
   `prompt.ReasoningRouterSystemPrompt`, `MaxTokens:32`, `Reasoning.Enabled=false`,
   `ToolChoice:"none"`, 2s cap via `reasoningRouterTimeout`), parsed by
   `ParseReasoningRouterTier`; any error/invalid → `low`.

`resolveClassifier` prefers the shared injected `cfg.Classifier` (anchors built
once for the whole process); else builds a per-agent one from `cfg.Embedder` +
`cfg.ExampleStore`.

### 5. Async active-learning loop (spike 053 → shipped)

Generic mechanism extracted to `internal/activelearn` (label-agnostic):
bounded queue + sha256 content-hash dedup (`sync.Map` seen-set) + margin gate +
non-blocking drop-on-full + ONE bounded goroutine + goleak-clean `Close()`.
Defaults: `defaultMarginFloor = 0.05`, `defaultQueue = 64`. The `Oracle`
interface is opaque (`LabelAndSave(ctx,text,vec) (saved bool)`) — the core never
sees a label type. `saved=false` removes the hash so a transient failure can
retry; `saved=true` keeps the hash so the same text is never relabeled.

Reasoning-specific adapter `internal/reasoninglearn/learner.go`: supplies the
`Oracle` (LLM router as teacher → `prompt.ReasoningTier`) + `Saver`, plus
`Refresh` (= `classifier.Refresh`). `Observe(text,vec,margin)` delegates to the
activelearn core and never blocks the turn.

Neo4j store `internal/reasoningstore/store.go` — rides the existing
`mcp-neo4j-cypher` client (`knowledge.Client`), NO new migration:
```cypher
-- save (idempotent MERGE on content hash):
UNWIND $rows AS row
MERGE (e:ReasoningExample {hash: row.hash})
SET e.tier=row.tier, e.embedding=row.embedding, e.text=row.text, e.source=row.source
-- load:
MATCH (e:ReasoningExample) WHERE e.embedding IS NOT NULL
RETURN e.tier AS tier, apoc.convert.toJson(e.embedding) AS embedding
```
Two mcp-neo4j-cypher transport gotchas baked in: (1) the read tool returns NULL
for list-valued columns → serialize the embedding with `apoc.convert.toJson` and
JSON-parse in Go; (2) a top-level list param `$embedding` is dropped by the write
tool → nest it in an `UNWIND $rows` map (mirrors `documents.Indexer`). `source`
is `"oracle"`; reuse the embedding the classifier already computed.

### 6. Composition root + flags

`cmd/aura/chat.go`: the store/learner are opt-in behind `AURA_LLM_REASONING_LEARNING`
(default OFF → classifier runs seed-only, its validated baseline, zero extra
subprocess). When ON, `knowledge.Open` opens the mcp-neo4j-cypher subprocess; the
SAME graph client backs both `reasoningstore.Store{:ReasoningExample}` and
`toolselectstore.Store{:ToolSelectionExample}` (WR-05: the tool-selection loop has
no independent flag — it rides this one; `AURA_TOOLSELECT_ORACLE` gates only the
paid escalation tier). Best-effort: a missing binary or down Neo4j leaves it
seed-only and never blocks boot.

## What to Avoid

- **AutoThink as a drop-in proxy (spike 041 VALIDATED-against).** OptiLLM's
  README sells "OpenAI-compatible proxy", but its approach matrix lists AutoThink
  as not proxy-compatible; `requirements_proxy_only.txt` omits
  torch/transformers/adaptive-classifier/datasets; `server.py`'s `known_approaches`
  has no `autothink`. The technique needs a resident local PyTorch model,
  `DynamicCache` manual generation, `register_forward_hook` steering, and
  model-specific HF vector datasets. Heavy sidecar only — not near-term.
- **AutoThink's runtime auto-`pip install adaptive-classifier`.** It auto-installs
  at runtime; Aura must NOT inherit that behavior.
- **Per-tier `max_tokens` caps (the spike-042 design that production REVERSED).**
  Spike 042 mapped none/small/deep → 512/2048/4096 `max_tokens`. Production
  `ApplyAdaptiveReasoning` deliberately NEVER touches `max_tokens` — the cap
  truncated tool-call arguments mid-JSON (the "203-turn disaster", 2026-06-14).
  Treat the 512/2048/4096 numbers as historical; ship effort-only tiers.
- **Inline oracle labeling / "fallback to LLM on uncertain" (the literal idea
  spike 053 corrected).** Calling the oracle on the user's turn re-adds the round
  trip the classifier removed. Labeling MUST be async, post-turn, margin-gated.
- **kNN over the example bank for the verdict.** Spike 053 measured kNN(5) flat
  (93%→93%) while centroid-refresh moved 90%→97%. The mechanism that works is
  centroid-refresh from `seeds + stored examples`; centroids are also more robust
  to a single noisy oracle label (diluted by the mean). Do not rely on kNN.
- **The regex/keyword classifier (spike 042's Go shim) as the production
  classifier.** Spike 042's `classify()` used `deepIndicators`/`smallIndicators`
  substring lists + word count. That was a shape probe, NOT shipped; the shipped
  classifier is embedding-cosine. (Cross-ref user-memory: "No regex on natural
  language.") The substring shim survives only as the spike harness.
- **CPU LLM routers as the replacement (spike 052 measured-against).** FunctionGemma-270M
  returned tool-call-only (0% usable as a JSON-tier router); Qwen3-0.6B too slow
  on CPU; Gemma-E2B router was accurate (97%) but 160ms/turn on GPU — the
  granite-embedding classifier beat it on latency at ~10ms while matching accuracy
  after learning. Don't reintroduce a per-turn small-LLM router.
- **Caching the anchor-build failure.** If `buildAnchors` fails it must NOT be
  cached — caching a transient sidecar outage would wedge the classifier; the next
  call must retry.
- **Mutating `messages[0]` or `ToolChoice` to express a tier.** The tier lives in
  the request-side `Reasoning` field only; forcing `ToolChoice:"none"` on the main
  turn would hide tool_search/active tools.

## Constraints

- **Embedding sidecar:** granite-embedding-97m (granite) at
  `http://127.0.0.1:8081/v1/embeddings`, 384d, L2-normalized, ~10ms CPU/query.
  Shared with `documents` and the semantic tool ranker — no new sidecar.
- **Classifier accuracy (spike 052, 60-prompt IT+EN held-out):** few-shot variant
  B = 90% accuracy / 92% none-vs-rest @ ~10ms CPU. Zero-shot (defs only) variant
  A is weaker. Beat Gemma-E2B router (97%/160ms GPU) on latency.
- **Active-learning lift (spike 053, 30 held-out / 45 stream, all disjoint from
  seeds):** centroid 90→97% (+7), none-vs-rest 90→97% (+7), kNN flat 93→93%.
  30/45 stream prompts uncertain (margin < 0.05) → oracle-labeled; 15/45 confident
  → zero cost; self-limiting as the bank grows. Oracle (Gemma-E2B) label noise
  ≈1/30 (~3%) did not break the gain. Classifier converged to the oracle's
  accuracy without the per-turn LLM round-trip.
- **`marginFloor = 0.05`** (top-2 cosine margin) is the uncertainty gate, chosen
  because spike-052 mistakes clustered < 0.035 (0.05 captures them + buffer).
  Tunable: `activelearn` `defaultMarginFloor=0.05`, `defaultQueue=64`.
- **Tiers + projection:** none → `reasoning.effort=none, exclude=true`; low →
  `effort=low, exclude=false`; high → `effort=high, exclude=false`. Reasoning
  tokens are billed output tokens. On DeepSeek-V4-Flash low/medium collapse to
  high server-side and only `effort:none` actually disables thinking.
- **Env vars / defaults:** `AURA_LLM_ADAPTIVE_REASONING` (default true,
  `cfg.AdaptiveReasoning`), `AURA_SHOW_REASONING` (default true, `cfg.ShowReasoning`
  — single master switch for live CoT; controls `exclude`),
  `AURA_LLM_REASONING_LEARNING` (default FALSE — self-improvement + Neo4j
  subprocess opt-in), `AURA_TOOLSELECT_ORACLE` (paid escalation tier of the
  shared loop only). Legacy LLM router cap = 2s; router request `MaxTokens:32`.
- **Score gate (CI/regression):**
  `internal/llm/openai_compat.TestAdaptiveReasoningItalianCorpusE2E` ≥90% over 60
  Italian queries (initial 100.0%). Live DeepSeek behavior guarded by
  `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go`.
- **Upstream pins (audit only, NOT dependencies):** AdaptThink `9e2c0e2`, optillm
  `df018d6`, pts `f5750d6`, adaptive-classifier `e2e819e`. AdaptThink training =
  VeRL/vLLM, H800/A100 class. Activation steering / PTS vector datasets = a
  separate heavy-sidecar/live-benchmark decision, not adopted.
- **License/source notes:** AutoThink = SSRN 5253327 / HF codelion blog (no
  benchmark guarantee carried into Aura); AdaptThink = arXiv 2505.13417. Aura
  carries only the portable classifier/budget idea, re-implemented natively.

## Origin

Synthesized from spikes: 040, 041, 042, 052, 053.
Source files in: `sources/040-adaptive-reasoning-source-truth/`,
`sources/041-optillm-autothink-runtime-fit/`,
`sources/042-adaptive-budget-policy-shim/`,
`sources/052-reasoning-tier-embed-classifier/` (no README in spike — `main.go`
is the head-to-head classifier harness),
`sources/053-reasoning-classifier-active-learning/`.
Verdicts: 040 VALIDATED (separate training-only / heavy-runtime / portable
surfaces); 041 VALIDATED (full AutoThink = heavy local-model sidecar, not a
proxy feature); 042 VALIDATED for the shim shape but its per-tier `max_tokens`
design was SUPERSEDED in production (effort-only, the 2026-06-14 fix); 052
VALIDATED + SHIPPED (commit `16cb5380`, granite few-shot 90%/92% @ ~10ms);
053 VALIDATED + SHIPPED (centroid-refresh 90→97%, async Neo4j `:ReasoningExample`
loop). Shipped seams: `internal/agent/prompt/reasoning_classifier.go`,
`reasoning_policy.go`, `internal/agent/llm_agent_reasoning.go`,
`internal/activelearn`, `internal/reasoninglearn`, `internal/reasoningstore`,
`internal/semindex`, `internal/llm/client.go` (`ReasoningConfig`).
