# Aura Adaptive Intelligence — Design Specification

**Date:** 2026-07-24  
**Status:** Approved architecture; written specification awaiting operator review  
**Audience:** Aura maintainers, reviewers, and implementation planners  
**Evidence:** Spikes 095–101  
**Initial domains:** reasoning effort, tool routing, skill routing, knowledge retrieval  
**Primary stores:** PostgreSQL durable outbox, Neo4j outcome graph, in-memory policy snapshots  

---

## 1. Purpose

Aura already learns reasoning tiers and tool-selection examples, but each learner
optimizes a local classifier from oracle labels. The system does not yet learn from
the result of the action it actually took. It also lacks a shared policy lifecycle,
cross-domain outcome records, production shadow comparison, and automatic rollback.

This design adds a provider-neutral adaptive control plane. It chooses among safe
execution strategies, records the chosen strategy and its probability, observes the
result, and improves later choices without changing Aura's authorization boundaries.

The first release covers four decisions:

- how much reasoning to request;
- how many and which tools to expose to the model;
- how many and which skills to expose;
- whether knowledge retrieval uses no context, vector retrieval, or graph expansion.

The same contracts can later support model/provider routing, memory retrieval,
multimodal backends, document-search depth, and scheduler strategy. Those extensions
do not belong to the first implementation plan.

## 2. Evidence and decision

The design follows five completed spikes:

- **097:** 44 contexts × 3 actions × 2 executions on the exact
  `unsloth/Qwen3.5-2B-GGUF` `Qwen3.5-2B-Q4_K_M.gguf`, with live Granite embeddings
  and Neo4j. No fixed action won every context.
- **098:** 4,800 bandit-only policy replays. Graph kNN reduced balanced stationary
  regret from 88.39 for the static baseline to 40.60. LinUCB reached 56.78.
  Graph-prior LinUCB tied graph kNN when outcomes were immediate but lost
  significantly under delayed feedback.
- **099:** Neo4j stored decisions, alternatives, propensities, delayed outcomes,
  snapshots, promotion, and rollback idempotently. Neighbor queries measured
  3.71 ms p50 and 5.30 ms p95.
- **100:** 2,400 adversarial replays over 1.92 million decisions. A governed
  graph-kNN policy preserved nominal behavior and reduced abrupt-drift regret by
  55%. A global absolute-reward circuit and a short-window median were falsified.
- **101:** Eight live cross-domain Qwen turns passed through Aura's real
  `EmbeddingClient`, `semindex.Ranker`, `activelearn` mechanism, and Neo4j.
  Shadow recommendations disagreed on half the turns but changed zero served actions.

The selected initial policy is therefore:

> top-5 cosine-neighbor mean over at most 256 chosen-action outcomes, with a
> versioned in-memory snapshot, bounded exploration, clipped rewards, and
> action-local relative drift quarantine.

LinUCB and graph-prior LinUCB remain shadow challengers. They are not the initial
production policy.

## 3. Goals

1. Learn from outcomes across reasoning, tools, skills, and knowledge retrieval.
2. Keep the request path provider-neutral and fail-safe.
3. Preserve the current static and centroid policies as explicit fallbacks.
4. Keep Neo4j and remote teachers off the request hot path.
5. Record enough information for unbiased offline policy evaluation.
6. Promote policies through shadow and canary stages with atomic rollback.
7. Prevent exploration from bypassing the ToolGateway, user overrides, or risk policy.
8. Isolate each identity's raw contexts and learning history.
9. Expose quality, latency, cost, drift, fallback, and rollback metrics.
10. Reuse Aura's current Granite, `semindex`, runner, Postgres, Neo4j, OpenTelemetry,
    Prometheus, gateway, and tool-invocation ledger.

## 4. Non-goals

- Training or fine-tuning the underlying LLM.
- Unrestricted reinforcement learning.
- Letting a learned policy authorize a tool call.
- Automatically sharing raw user learning across identities.
- Replacing `semindex`, the current reasoning classifier, or `tool_search`.
- Querying Neo4j on every user request.
- Activating a learned policy in the first delivery increment.
- Adding a new Python service, vector database, or model-serving framework.
- Implementing the future memory, provider, multimodal, or scheduler adapters in v1.

## 5. Invariants

These rules hold for every adaptive domain.

1. **Policy chooses strategy, not permission.** The ToolGateway remains the sole
   authorization and side-effect gate.
2. **Explicit intent wins.** A user-selected reasoning effort or operator policy
   bypasses adaptation.
3. **Only the chosen action learns.** Unchosen actions receive no fabricated live
   reward.
4. **Every stochastic choice records its propensity.**
5. **Raw outcome components remain stored.** Scalar utility is derived and can be
   recomputed when weights change.
6. **No graph dependency on the hot path.** Requests use an immutable in-memory
   snapshot plus a bounded recent overlay.
7. **Cold start is deterministic.** Missing, corrupt, stale, or unsupported snapshots
   select the static fallback.
8. **Shadow cannot mutate the served choice.** The types and call path keep served
   and shadow decisions separate.
9. **Risk constrains exploration.** Risky or destructive actions receive zero
   exploratory traffic.
10. **Identity data stays identity-scoped.** Raw prompts and embeddings never train a
    different identity's policy automatically.
11. **Model self-reported success is never authoritative for a mutating tool.**
12. **Policy publication is atomic and reversible.**

## 6. Architecture

```text
                         REQUEST PATH

 user request
      │
      ├── explicit override / gateway constraints / available actions
      │
      ├── Granite context embedding
      │
      ├── static fallback decision
      │
      ├── active in-memory snapshot + recent overlay
      │       └── governed graph-kNN scores allowed actions
      │
      ├── safe exploration gate
      │
      ├── served decision ────────────────┐
      │                                   │
      ├── shadow challenger              │
      │       └── never returned          │
      │                                   │
      └── domain adapter executes strategy│
                                          │
                         OUTCOME PATH      │
                                          ▼
 tool result / final answer / correction / latency / tokens / cost
      │
      ├── typed OutcomeEvent
      ├── Postgres adaptive outbox
      ├── async projector
      ├── Neo4j decision/outcome graph
      ├── local bounded recent overlay
      └── snapshot builder
              ├── offline replay + OPE
              ├── drift checks
              ├── shadow candidate
              └── atomic promote / rollback
```

### 6.1 Request plane

The request plane performs only bounded local work:

1. Build the allowed action set.
2. Reuse or request the Granite embedding.
3. Compute the current static fallback.
4. Score actions from the active in-memory policy.
5. Apply the exploration and support gates.
6. Return a typed `ServedDecision`.
7. Optionally compute one or more `ShadowDecision` values.

It performs no Neo4j query, remote teacher call, snapshot training, or policy
promotion.

### 6.2 Outcome plane

The outcome plane runs after a result exists. It converts domain evidence into a typed
event, persists that event through the Postgres outbox, and projects it to Neo4j.
Projection is asynchronous and idempotent. A graph outage delays learning but cannot
delay or fail the user turn.

### 6.3 Policy plane

The policy plane builds candidates from durable outcomes. It replays policies with
chosen-action feedback, performs off-policy evaluation, and publishes immutable
snapshots. Only the policy head changes during promotion or rollback.

## 7. Core contracts

The core package owns no reasoning, tool, skill, knowledge, LLM, runner, or gateway
types. Domain adapters translate at the boundary.

```go
type Domain string

const (
    DomainReasoning Domain = "reasoning"
    DomainTool      Domain = "tool"
    DomainSkill     Domain = "skill"
    DomainKnowledge Domain = "knowledge"
)

type DecisionContext struct {
    ID              string
    OwnerID         string
    Domain          Domain
    Embedding       []float64
    Provider        string
    Model           string
    RewardProfile   RewardProfile
    Risk            scoring.RiskTier
    Available       []Action
    StaticActionID  string
}

type Action struct {
    ID          string
    Domain      Domain
    Strategy    string
    SafeToExplore bool
    Metadata    map[string]string
}

type ServedDecision struct {
    ID             string
    PolicyVersion  string
    Action         Action
    Propensity     float64
    Explored       bool
    FallbackReason string
    CandidateScores []CandidateScore
}

type ShadowDecision struct {
    DecisionID    string
    PolicyVersion string
    Action        Action
    CandidateScores []CandidateScore
}

type OutcomeEvent struct {
    ID          string
    DecisionID  string
    OwnerID     string
    Domain      Domain
    Source      OutcomeSource
    Quality     float64
    Tokens      int64
    Latency     time.Duration
    CostMicros  int64
    RiskPenalty float64
    Confidence  float64
    ErrorClass  string
    ObservedAt  time.Time
}
```

Maps must not carry load-bearing fields. Before implementation, stable metadata keys
must become typed fields or bounded enums.

### 7.1 Domain adapter

Each adaptive surface implements:

```go
type DomainAdapter interface {
    Domain() Domain
    Context(ctx context.Context, input TurnInput) (DecisionContext, error)
    Execute(ctx context.Context, decision ServedDecision) (ExecutionResult, error)
    Outcome(ctx context.Context, decision ServedDecision, result ExecutionResult) []OutcomeEvent
}
```

The runner owns orchestration. An adapter owns only domain translation.

### 7.2 Policy

```go
type Policy interface {
    Version() string
    Decide(DecisionContext) (ServedDecision, error)
}
```

`Policy.Decide` is pure after the context embedding exists. It performs no I/O.

### 7.3 Recorder

```go
type Recorder interface {
    RecordDecision(context.Context, DecisionRecord) error
    RecordOutcome(context.Context, OutcomeEvent) error
}
```

The production recorder writes the Postgres outbox. A no-op recorder supports disabled
adaptation. Tests use an in-memory recorder.

`Policy.Decide` does not call the recorder. The runner carries decision records in the
invocation context and flushes them through the recorder at its existing turn/tool
persistence boundary. This avoids a new synchronous database round-trip during policy
selection. Delayed feedback uses the same recorder in a later transaction.

## 8. Initial action catalog

Action IDs are stable policy data. Provider-specific fields do not appear in them.

### 8.1 Reasoning

| Action ID | Meaning |
|---|---|
| `reasoning/direct` | Disable model reasoning when the backend supports it. |
| `reasoning/bounded` | Use the backend's low bounded-reasoning mode. |
| `reasoning/deep` | Use the strongest supported reasoning mode. |

The provider capability adapter translates these actions:

- llama.cpp: `enable_thinking:false` or `thinking_budget_tokens`;
- OpenRouter/DeepSeek: reliable `off` versus `on`; low/medium/high are advisory because
  the tested model does not honor them monotonically;
- unsupported backend: deterministic static mapping.

An explicit cockpit effort selection bypasses the adaptive policy.

### 8.2 Tool routing

| Action ID | Meaning |
|---|---|
| `tool/semantic_top1` | Use semantic top-1 when confidence and task risk allow it. |
| `tool/model_top3` | Give the model the three best semantic candidates. |
| `tool/full_catalog` | Give the model every allowed tool in the domain catalog. |

This action selects the discovery strategy. It does not authorize or execute the
selected tool. Every actual invocation still crosses the ToolGateway.

### 8.3 Skill routing

| Action ID | Meaning |
|---|---|
| `skill/semantic_top1` | Activate a high-confidence semantic top-1 skill. |
| `skill/model_top3` | Let the model choose from the three best skills. |
| `skill/full_catalog` | Let the model choose from every eligible skill. |

Catalog eligibility applies before adaptation. Installed state, scope, and policy
remain deterministic filters.

### 8.4 Knowledge routing

| Action ID | Meaning |
|---|---|
| `knowledge/none` | Do not retrieve private context. |
| `knowledge/vector_top1` | Retrieve the best vector result. |
| `knowledge/graph_expand` | Retrieve vector seeds and expand relevant graph relations. |

Identity filtering and document authorization apply before results reach the model.
The policy can choose retrieval depth but cannot broaden access.

## 9. Context and policy scope

Every policy key is:

```text
owner_id + domain + reward_profile
```

`owner_id` may be a reserved system scope for curated seed policies. User events update
only that user's overlay. Aura never promotes one user's raw contexts into a global
policy automatically.

Context features include:

- normalized Granite embedding;
- domain;
- provider and model capability class;
- available-action mask;
- risk tier;
- selected reward profile;
- coarse task and channel metadata with bounded cardinality.

The policy does not need raw prompt text. The durable graph stores a content hash and
an optional redacted preview only when audit capture is enabled. Embeddings remain
sensitive identity-scoped data.

## 10. Reward model

Aura stores raw outcome components and derives utility:

```text
utility =
    clipped_quality
    - token_weight   × normalized_tokens
    - latency_weight × normalized_latency
    - cost_weight    × normalized_cost
    - risk_penalty
```

Initial reward profiles:

| Profile | Token weight | Latency weight | Cost weight |
|---|---:|---:|---:|
| `quality_first` | 0.02 | 0.01 | 0.01 |
| `balanced` | 0.08 | 0.04 | 0.04 |
| `economy` | 0.18 | 0.08 | 0.12 |

Normalization bounds come from the policy snapshot. The candidate builder recomputes
utility from raw components using one fixed set of bounds, then stores those bounds in
the candidate. Publishing new weights or bounds creates a new snapshot; it never
mutates the raw components or utility recorded by an earlier snapshot.

### 10.1 Evidence hierarchy

| Source | Default confidence | Examples |
|---|---:|---|
| `human_explicit` | 1.00 | correction, approval, rejection, rating |
| `deterministic_check` | 0.95 | test result, exact answer, schema validation |
| `tool_result` | 0.90 | structured success/error, HTTP status, persisted artifact |
| `grounded_eval` | 0.80 | citation coverage, retrieval relevance, domain evaluator |
| `behavioral` | 0.40 | retry, immediate reformulation, abandonment |
| `model_self_report` | 0.10 | model says its answer succeeded |

Confidence is stored, not hidden inside the utility. Model self-report alone cannot
produce a positive outcome for a mutating action.

### 10.2 Delayed and corrected outcomes

A decision can receive multiple outcome events. Each event is immutable. A correction
links to the prior event and supersedes its derived aggregate without deleting the
original evidence. The projector recomputes the decision's current aggregate
idempotently.

## 11. Champion graph-kNN policy

For each allowed action:

1. Read up to 256 observations from the active snapshot and recent overlay.
2. Select the five contexts with highest non-negative cosine similarity.
3. Compute their mean utility with `similarity × evidence confidence` as the weight.
4. Choose the highest-scoring supported action.

If an action has no support, it receives the static prior. If every action lacks
support, the static action wins.

The initial support gate requires five observations for an action before its learned
score can override the static prior. This prevents a single outcome from moving the
served policy.

### 11.1 Exploration

Exploration applies after constraints and before execution:

| Condition | Epsilon |
|---|---:|
| Safe internal strategy, adequate support | 0.04 |
| Normal-risk strategy | 0.01 |
| Risky or destructive strategy | 0.00 |
| Explicit user override | 0.00 |
| Cold start, stale snapshot, or degraded infrastructure | 0.00 |

The sampled action probability is recorded exactly. Exploration never adds a tool,
skill, document, or capability that deterministic eligibility removed.

### 11.2 Drift quarantine

The initial detector uses the validated spike-100 rule per action:

- prior window: 16 outcomes;
- recent window: 8 outcomes;
- prior mean must exceed `0.65`;
- recent mean must fall below `0.15`;
- quarantine duration: 100 eligible decisions.

Only outcomes with confidence of at least `0.80` enter the drift detector. Weak
behavioral and model-self-report signals can inform ordinary ranking but cannot
quarantine an action.

When the detector fires:

1. Quarantine only the degraded action.
2. Clear its recent learned bank.
3. Continue serving other actions.
4. Preserve all durable evidence.
5. Emit a drift and rollback metric.
6. Reintroduce the action only through safe exploration after quarantine.

Aura must not implement a global absolute-reward circuit. Spike 100 falsified that
design because legitimate low-cost rewards triggered broad fallback.

## 12. Snapshot lifecycle

Snapshots have these states:

```text
draft → shadow → canary → active → retired
                     └──────→ rolled_back
```

An `AdaptivePolicyHead` identifies the active snapshot for one policy key. Promotion
and rollback update the head in one Neo4j transaction. Serving processes poll for a
head-version change every 30 seconds and swap snapshots atomically.

### 12.1 Snapshot contents

Each snapshot contains:

- immutable ID and schema version;
- owner scope, domain, and reward profile;
- algorithm and hyperparameters;
- action catalog version;
- provider-capability version;
- normalization bounds;
- observation IDs or an immutable packed observation bank;
- creation time and evidence window;
- offline and shadow metrics;
- predecessor ID and checksum.

Snapshots never contain API keys, raw tool arguments, or unredacted prompts.

### 12.2 Local recent overlay

Chosen outcomes received after snapshot creation enter a bounded in-memory overlay.
The overlay uses the same 256-per-action cap and drift rules. Snapshot replacement
atomically resets the overlay to events newer than the new snapshot's evidence
watermark.

On restart, Aura hydrates the active snapshot and the post-watermark outcomes before it
enables adaptive serving. Until hydration succeeds, it serves the static fallback.

### 12.3 Promotion gates

A candidate may enter canary only when all gates pass:

1. At least seven days of shadow operation.
2. At least 1,000 eligible decisions overall and 100 per enabled domain.
3. Complete propensities for every evaluated stochastic decision.
4. For `candidate utility − champion utility`, the paired or off-policy 95% lower
   confidence bound is at least `−1%` overall.
5. No domain regresses more than 2%.
6. Harmful-action rate does not increase.
7. Adaptive decision overhead adds less than 2 ms p95 after the embedding exists.
8. No authorization, identity-isolation, or served/shadow invariant violation.
9. Drift, graph-outage, restart, and rollback tests pass.
10. Operator explicitly approves promotion.

Canary stages are 1%, 5%, 20%, 50%, and 100% of eligible safe decisions. Each stage
requires at least 200 outcomes and 24 hours without an SLO violation. Low-volume
deployments may take longer; they may not waive the outcome minimum.

### 12.4 Automatic rollback

Rollback restores the predecessor snapshot when any condition holds:

- harmful-action rate exceeds the champion by 2 percentage points;
- error rate exceeds the champion by 2 percentage points;
- p95 adaptive latency exceeds its budget for 15 minutes;
- action-local drift fires in more than half of one domain's actions;
- snapshot checksum or schema validation fails;
- operator requests rollback.

Rollback changes policy selection only. It does not undo completed tool side effects.

## 13. Persistence

### 13.1 PostgreSQL outbox

The runner already persists turn state in PostgreSQL. The same transaction writes
adaptive events to `aura.adaptive_outbox`.

Required columns:

```text
id UUID PRIMARY KEY
owner_id TEXT NOT NULL
event_kind TEXT NOT NULL
decision_id TEXT NOT NULL
payload JSONB NOT NULL
created_at TIMESTAMPTZ NOT NULL
projected_at TIMESTAMPTZ
attempts INTEGER NOT NULL DEFAULT 0
next_attempt_at TIMESTAMPTZ NOT NULL
last_error_class TEXT
```

The payload is versioned and validated before insertion. The projector uses bounded
retry with jitter and a dead-letter state after ten failures. Replaying an event is
safe.

### 13.2 Neo4j outcome graph

```text
(:AdaptiveDecision)-[:FOR_CONTEXT]->(:AdaptiveContext)
(:AdaptiveDecision)-[:CONSIDERED {score, probability, rank}]->(:AdaptiveAction)
(:AdaptiveDecision)-[:CHOSEN]->(:AdaptiveAction)
(:AdaptiveDecision)-[:FROM_POLICY]->(:AdaptivePolicySnapshot)
(:AdaptiveDecision)-[:OBSERVED]->(:AdaptiveOutcome)
(:AdaptiveOutcome)-[:SUPERSEDES]->(:AdaptiveOutcome)
(:AdaptivePolicyHead)-[:ACTIVE]->(:AdaptivePolicySnapshot)
(:AdaptivePolicySnapshot)-[:PREVIOUS]->(:AdaptivePolicySnapshot)
```

Unique constraints:

- `AdaptiveDecision.id`;
- `AdaptiveContext.id`;
- `AdaptiveAction.id`;
- `AdaptiveOutcome.id`;
- `AdaptivePolicySnapshot.id`;
- `AdaptivePolicyHead.scope`.

`MERGE` keys use immutable IDs. Mutable fields are set separately. The projector
groups one event's writes in one managed transaction.

Neo4j is the durable analytical graph and snapshot registry. It is not queried by
`Policy.Decide`.

### 13.3 Retention

- Raw identity-scoped decision contexts and embeddings: 30 days.
- Outcome events: 90 days.
- Aggregated policy evidence and snapshot metrics: 365 days.
- Active and predecessor snapshots: retained until both are retired for 90 days.
- Audit records for promotion and rollback: follow Aura's existing audit retention.

Deletion of an identity removes its raw contexts, overlays, outcomes, and private
snapshots from both stores. Global curated seeds are unaffected.

## 14. Integration with existing Aura systems

### 14.1 `semindex`

Use `semindex.Ranker` for exact cosine top-k over bounded banks. Add no ANN dependency.
The 256-per-action cap keeps brute-force scoring small and deterministic.

### 14.2 `activelearn`

Keep `activelearn` for uncertain-example oracle labeling. Outcome events need a typed
recorder rather than encoding a decision ID as learner text.

The implementation may extract the existing bounded, non-blocking worker mechanics
into a reusable typed queue, but it must preserve current `reasoninglearn` and
`toolselectlearn` behavior.

### 14.3 Reasoning learner

The existing reasoning classifier remains the static prior and fallback.

Fix two current lifecycle weaknesses:

1. Build a replacement classifier in the background and swap it atomically. Do not
   invalidate the cache and force the next user request to rebuild anchors.
2. Cap learned contribution per tier so stored examples cannot overwhelm curated
   anchors. For each tier, the total learned-example weight is capped at the number of
   curated definition and seed vectors in that tier.

### 14.4 Tool-selection learner

The current runner enables learned boost but does not hydrate stored examples during
startup. The composition root must load and fold existing examples before reporting
the learned bank ready. A load failure leaves the static ranker available, marks
learning degraded, and schedules an off-path retry; it does not block daemon startup.

The current centroid boost remains the static prior. The adaptive policy learns which
catalog strategy to use; it does not replace the ranker or the ToolGateway.

### 14.5 Skills

The skill adapter consumes the installed and policy-eligible catalog after normal
scope checks. It records the routing strategy separately from the skill the model
ultimately activates.

### 14.6 Knowledge and documents

The knowledge adapter operates inside the authorized retrieval path. It records
retrieval strategy, result IDs, citation coverage, latency, and grounded evaluator
signals. It cannot remove identity filters or expand into unauthorized documents.

### 14.7 Gateway and tool ledger

The gateway verdict and the actual tool-invocation result are authoritative outcome
inputs. Adaptive decisions cannot bypass reservation, approval, retry, sandbox, or
capability rules.

### 14.8 Memory learning

The existing memory-learning fusion design remains authoritative for proactive recall,
write-worthiness, and temporal supersession. A later adapter may publish its
recall-depth decisions and outcomes through this control plane without moving memory
storage into the adaptive graph.

## 15. Configuration and operator control

The initial configuration surface is intentionally small:

| Variable | Values | Default |
|---|---|---|
| `AURA_ADAPTIVE_MODE` | `off`, `shadow`, `canary`, `active` | `off` |
| `AURA_ADAPTIVE_DOMAINS` | comma-separated initial domains | empty |
| `AURA_ADAPTIVE_REWARD_PROFILE` | `quality_first`, `balanced`, `economy` | `balanced` |
| `AURA_ADAPTIVE_SNAPSHOT_POLL` | positive duration | `30s` |
| `AURA_ADAPTIVE_CAPTURE_PREVIEW` | `on`, `off` | `off` |

Unknown enum values fail configuration validation. They never select a permissive
mode.

Mode behavior:

- `off`: preserve current behavior and write no new adaptive events;
- `shadow`: serve the static policy, record outcomes, and compute challengers;
- `canary`: serve the active candidate only for the audited canary percentage;
- `active`: serve the active snapshot for every eligible decision.

The first operator surface is:

```text
aura adaptive status
aura adaptive promote --snapshot <id>
aura adaptive rollback --scope <owner/domain/profile>
```

Status requires `adaptive.read`. Promotion and rollback require `adaptive.manage` and
write the existing audit ledger. The future cockpit surface must call the same service;
it must not implement separate policy logic.

## 16. Failure behavior

| Failure | Behavior |
|---|---|
| Granite unavailable | Use static policy; record bounded infrastructure metric when possible. |
| Snapshot absent or invalid | Use static policy; refuse adaptive activation. |
| Neo4j unavailable | Continue from in-memory snapshot and overlay; queue projection in Postgres. |
| Projector backlog | Continue serving; alert on age and size; do not drop durable outbox rows. |
| Postgres unavailable | Follow the existing turn-persistence failure path; do not create graph-only evidence. |
| Unsupported provider action | Map to deterministic supported fallback before policy scoring. |
| Outcome missing | Keep decision unevaluated; never invent reward. |
| Corrupt outcome | Reject it, increment poison metric, retain the raw outbox event for audit. |
| Drift quarantine | Exclude only the affected action and keep the predecessor snapshot available. |
| Snapshot poll failure | Keep the last valid in-memory snapshot. |
| Queue saturation | Preserve the durable outbox event; skip only the immediate local overlay update. |

## 17. Security and privacy

1. Adaptive policy is not an authorization system.
2. Raw prompts are excluded from the graph by default.
3. Embeddings, hashes, and outcomes carry `owner_id`.
4. Adaptive graph labels are not exposed through the general LLM-facing Neo4j MCP.
5. Logs and spans contain IDs and bounded enums, not prompts, tool arguments, or
   retrieved private text.
6. Global policies use curated or explicitly anonymized evidence only.
7. Outcome values are clipped to `[-0.25, 1]`.
8. One event cannot promote a policy; minimum support and confidence gates apply.
9. Human corrections supersede weak behavioral and model-self-report evidence.
10. Destructive and risky actions receive zero exploration.
11. Snapshot payloads are checksummed and schema-versioned.
12. Promotion and rollback require audited identity and capability checks.

## 18. Observability

### 18.1 Traces

Add spans:

- `adaptive.decide`;
- `adaptive.shadow`;
- `adaptive.outcome`;
- `adaptive.project`;
- `adaptive.snapshot.build`;
- `adaptive.snapshot.promote`;
- `adaptive.snapshot.rollback`.

Bounded attributes:

```text
adaptive.domain
adaptive.policy.version
adaptive.action.id
adaptive.fallback.reason
adaptive.explored
adaptive.propensity_bucket
adaptive.outcome.source
adaptive.snapshot.state
adaptive.error_class
```

Do not attach raw prompt, embedding, tool arguments, or private retrieved content.

### 18.2 Metrics

Required metrics:

- decisions by domain, action, policy, and fallback reason;
- shadow disagreement;
- chosen-action reward components and derived utility;
- decision and projection latency;
- token and monetary cost;
- exploration and harmful-action rates;
- outcome delay and missing-outcome age;
- drift detections and quarantines;
- snapshot build, promotion, and rollback;
- outbox depth, oldest age, retries, and dead letters;
- snapshot hydration success and age.

Current `learning_write` queue metrics remain, but they are not evidence of improved
quality.

### 18.3 Operator surface

The first implementation exposes read-only status through existing diagnostics and
metrics. It does not require a new cockpit page. Promotion and rollback may initially
use an audited CLI command. A later UI must call the same service and capability gate.

## 19. Testing and evaluation

### 19.1 Unit tests

- graph-kNN scoring and deterministic tie-breaking;
- static fallback and provider mapping;
- propensity calculation;
- reward clipping and profile scalarization;
- delayed outcome aggregation and supersession;
- action-local drift quarantine;
- snapshot checksum, schema validation, and atomic swap;
- identity and action eligibility filters;
- outbox retry and dead-letter classification.

### 19.2 Property and fuzz tests

- probability distribution sums to one;
- disallowed actions are never selected;
- explicit override is invariant under policy data;
- duplicate events do not change graph counts or aggregates;
- any malformed snapshot fails closed;
- reward remains bounded for arbitrary numeric inputs;
- served choice is independent from shadow output.

### 19.3 Concurrency tests

- concurrent decision reads during snapshot swap;
- outcome append during hydration;
- close while recorder/projector queues contain work;
- graph outage and recovery;
- multi-instance polling of one policy head;
- WSL `CGO_ENABLED=1 go test -race`.

### 19.4 Integration tests

- Postgres outbox to Neo4j projection;
- idempotent replay;
- active snapshot hydration after restart;
- graph outage with continued cached decisions;
- atomic promotion and rollback;
- identity deletion across both stores;
- live Granite embeddings;
- agent-memory health before and after graph writes.

Integration tests must fail under CI when required infrastructure is missing. They may
skip only in local runs outside CI.

### 19.5 Scientific policy gate

Every candidate comparison must:

1. Use identical seeded context streams.
2. Expose only chosen-action outcomes to learners.
3. Retain per-seed results.
4. Report paired bootstrap 95% confidence intervals.
5. Include nominal, delayed, noisy, drift, outage, restart, and combined conditions.
6. Report utility, quality, tokens, latency, cost, harmful actions, fallback, and
   adaptation lag.
7. Preserve failed policy variants as evidence.

### 19.6 Live acceptance score

The final implementation must score at least **49/50 = 9.8/10**:

| Gate | Points |
|---|---:|
| Four domains execute against the configured live model | 5 |
| Every served decision and propensity persists idempotently | 5 |
| Every chosen-action outcome projects to Neo4j | 5 |
| Shadow changes zero served actions | 5 |
| Static fallback survives cold start and corrupt snapshot | 5 |
| Cached policy survives Neo4j outage | 5 |
| Risky/destructive exploration remains zero | 5 |
| Restart hydration restores the active version | 5 |
| Promotion and rollback are atomic and audited | 5 |
| Race, coverage, mutation, privacy, and retention gates pass | 5 |

A partial point is allowed only for a non-safety performance threshold. Any failure in
identity isolation, authorization, served/shadow separation, destructive exploration,
or rollback caps the total below 9.8.

## 20. Delivery increments

### Increment 1 — Correct current learning lifecycle

- Hydrate tool-selection examples at startup.
- Replace reasoning cache invalidation with background build-and-swap.
- Cap learned-versus-curated anchor weight.
- Distinguish duplicate, capacity, failure, and success metrics.

### Increment 2 — Typed control plane in shadow mode

- Add core types, static policy, governed graph-kNN, and shadow separation.
- Add the Postgres outbox and Neo4j schema.
- Add projector, snapshot hydration, and observability.
- Serve no learned action.

### Increment 3 — Reasoning and tool adapters

- Record served and shadow decisions.
- Add provider capability translation.
- Attach reasoning and structured tool outcomes.
- Run the seven-day shadow gate.

### Increment 4 — Skill and knowledge adapters

- Add installed-skill eligibility and skill outcomes.
- Add authorized retrieval strategies and grounded outcomes.
- Re-run the scientific comparison across all four domains.

### Increment 5 — Governed activation

- Add OPE, candidate publication, audited promotion, canary stages, drift quarantine,
  and automatic rollback.
- Activate only after the 9.8/10 live gate and operator approval.

Each increment is independently reversible. Increment 2 must ship before any domain can
activate learned choices.

## 21. Rejected designs

### Static or centroid-only learning

It is simple but cannot optimize context-dependent quality and cost. The measured
static regret was more than twice graph kNN.

### LinUCB as the initial champion

It improved on static routing but trailed graph kNN. The linear reward assumption was
too restrictive for the measured surface.

### Graph-prior LinUCB as the initial champion

It tied graph kNN with immediate outcomes and lost significantly under delay. Keep it
as a shadow challenger.

### Neo4j on the request hot path

Measured latency was acceptable, but direct graph serving creates an avoidable outage
dependency. Neo4j remains the durable ledger and snapshot registry.

### Global absolute-reward circuit breaker

Falsified in spike 100. It confused legitimate low-cost rewards with degradation and
caused 48–64% fallback.

### Short-window median kNN

It improved drift response but materially regressed nominal performance. Preserve the
validated top-5 mean estimator.

### Immediate online activation

Chosen-action feedback, model variability, and weak outcome signals make unreviewed
activation unsafe. Learning is online; publication is governed.

### Cross-identity raw learning

It creates privacy, poisoning, and relevance risks. v1 uses identity-local overlays and
curated global seeds.

## 22. Industrial references

- Vowpal Wabbit contextual-bandit contract and exploration:
  <https://vowpalwabbit.org/docs/vowpal_wabbit/python/latest/tutorials/python_Contextual_bandits_and_Vowpal_Wabbit.html>
- Vowpal Wabbit offline policy evaluation:
  <https://vowpalwabbit.org/docs/vowpal_wabbit/python/latest/tutorials/off_policy_evaluation.html>
- AWS SageMaker shadow testing:
  <https://docs.aws.amazon.com/sagemaker/latest/dg/shadow-tests.html>
- Google Vertex AI agent evaluation:
  <https://docs.cloud.google.com/vertex-ai/generative-ai/docs/agent-engine/evaluate>
- MLflow production tracing and asynchronous evaluation:
  <https://mlflow.org/docs/latest/genai/tracing/prod-tracing>
- OpenTelemetry GenAI semantic conventions:
  <https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/>
- NIST AI RMF Core:
  <https://airc.nist.gov/airmf-resources/airmf/5-sec-core/>
- NIST AI 800-4 post-deployment monitoring:
  <https://www.nist.gov/publications/challenges-monitoring-deployed-ai-systems-center-ai-standards-and-innovation>
- Neo4j vector indexes:
  <https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/vector-indexes/>
- Neo4j Go transaction guidance:
  <https://neo4j.com/docs/go-manual/current/transactions/>
- BaRP:
  <https://arxiv.org/abs/2510.07429>
- MixLLM:
  <https://arxiv.org/abs/2502.18482>
- Correlation-Aware Contextual Bandits:
  <https://arxiv.org/abs/2607.09015>

## 23. Source evidence

- `.planning/spikes/095-llama-cpp-reasoning-effort-wire-contract`
- `.planning/spikes/096-openrouter-reasoning-effort-wire-contract`
- `.planning/spikes/097-qwen-multidomain-reward-surface`
- `.planning/spikes/098a-static-centroid-baseline`
- `.planning/spikes/098b-graph-knn-policy`
- `.planning/spikes/098c-linucb-policy`
- `.planning/spikes/098d-graph-prior-linucb`
- `.planning/spikes/099-neo4j-outcome-policy-graph`
- `.planning/spikes/100-adaptive-policy-governance-stress`
- `.planning/spikes/101-aura-adaptive-shadow-e2e`

The JSON artifacts in those directories are the numerical ground truth for this
specification.
