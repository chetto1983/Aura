# Aura Adaptive Intelligence — Production Design Specification

**Date:** 2026-07-24
**Status:** Shadow plumbing validated; adaptive production serving blocked
**Evidence:** Spikes 095–105
**Initial domains:** reasoning, tool routing, skill routing, knowledge retrieval
**Authority:** PostgreSQL
**Projection:** private Neo4j labels in Aura's existing agent-memory deployment
**Serving default:** static Aura behavior

## 1. Decision

Aura should adopt one provider-neutral adaptive control plane for reasoning and every
other safe strategy choice it can measure: tool discovery, skill routing, retrieval
depth, memory recall, context handling, model routing, and recovery strategy.

It must not activate a learned action yet.

Qwen3.5 2B is a spike instrument only. Its llama.cpp runs test local portability,
event flow, action-dependent behavior, and OPE mechanics. It does not represent
Aura's real production model, is not a production candidate, and cannot prove
production answer quality. The held-out Qwen spike improved balanced utility
by `+0.1679`, but its family-bootstrap 95% interval was
`[-0.0168, +0.3198]`. Generalization therefore failed the preregistered gate.

The current production champion remains Aura's static behavior. Graph kNN remains a
shadow challenger until a randomized canary on the real configured model and real
traffic passes the calibrated promotion gate.

## 2. Evidence ledger

| Proof | Result | What it establishes | What it does not establish |
|---|---|---|---|
| Qwen llama.cpp action surface, spike 097 | validated spike | reasoning/tool/skill/knowledge actions produce different quality, cost, and latency | production-model quality |
| Replay policies, spikes 098–100 | graph kNN best in measured replay | a useful challenger and failure modes for drift guards | unseen or live population benefit |
| Shadow E2E, spike 101 | 8/8 events, served action unchanged | provider-neutral shadow mechanics | statistical gain |
| Held-out generalization, spike 102 | **invalidated** | point estimate is promising but uncertain | permission to activate |
| Supported OPE, spike 102 | SNIPS error `.0004`, DR error `.0051`, ESS `5605` | randomized logged propensities can recover known target value | production effect |
| Runner/outbox, spike 103 | validated mechanism | real Runner control points, atomic durable events, ordered projection | better answers |
| Identity/rollback, spike 104 | validated safety mechanism | deletion, tombstones, private graph, distributed kill switch | better answers |
| Canary statistics, spike 105 | validated gate, not model | false-promotion, power, harm, poison, and censor behavior | real-model win |

## 3. Goals

1. Learn only from observed outcomes of actions Aura actually served.
2. Cover reasoning and non-reasoning decisions through one typed contract.
3. Preserve explicit user choices and current static behavior as the fallback.
4. Keep authorization, gateway policy, identity scope, and tool side effects outside
   learned control.
5. Record propensities and evidence provenance for supported OPE and canaries.
6. Make turns and adaptive facts atomic in PostgreSQL.
7. Reuse Aura's Neo4j and agent-memory infrastructure without exposing private
   adaptive nodes through the LLM-facing memory MCP.
8. Fail to the baseline on missing policy state, partitions, invalid snapshots, or
   unsupported provider actions.
9. Promote only with independent, calibrated, sequentially valid quality and safety
   evidence from the real production model.

## 4. Non-goals

- Fine-tuning model weights.
- Letting a policy grant capabilities or bypass the ToolGateway.
- Treating tool completion, HTTP success, token savings, or model self-report as
  answer quality.
- Cross-identity learning from raw prompts, arguments, embeddings, or outcomes.
- Querying Neo4j on the request hot path.
- Writing adaptive evidence through LLM-facing `memory_add_*` tools.
- Claiming Qwen 2B results transfer to a larger or different production model.
- Activating every adaptive surface at once.

## 5. Adaptive surface catalog

All surfaces use the same decision, outcome, policy, rollout, and deletion contracts.
Each surface has its own action catalog and promotion evidence; success in one domain
does not automatically promote another.

| Surface | Safe strategy choices | Authoritative outcome examples | State |
|---|---|---|---|
| Reasoning | direct, bounded, deep | deterministic correctness, calibrated answer rubric, tokens, latency | shadow-ready |
| Tool discovery | semantic top-1, model top-3, full eligible catalog | correct tool, gateway verdict, structured result, independent task result | shadow-ready |
| Skill routing | semantic top-1, model top-3, full eligible catalog | correct skill, artifact rubric, explicit correction | shadow-ready |
| Knowledge retrieval | none, vector, graph expansion, rerank depth | citation support, deterministic answer, retrieval relevance | shadow-ready |
| Memory recall | none, recent, semantic, graph-temporal | explicit correction, preference/entity recall rubric | future adapter |
| Memory write | do not write, candidate, supersede | human confirmation, later contradiction, temporal validity | future adapter |
| Model/provider routing | local/cloud/model capability class | calibrated quality, cost, latency, availability | future adapter |
| Context management | retain, compact, recall, tool-result eviction depth | answer rubric, cache/tokens, lost-fact checks | future adapter |
| Document search | vector depth, graph expansion, rerank | grounded citation and exact-document checks | future adapter |
| Multimodal route | local/cloud/fallback strategy | modality-specific deterministic or human rubric | future adapter |
| Retry/recovery | retry, alternate provider, ask user, stop | terminal correctness, duplicate-side-effect checks | future adapter |
| Scheduler/concurrency | serial, bounded parallel, defer | completion, deadline, cost, conflict rate | research only |

“Future adapter” means the control-plane contract is reusable; it is not permission to
add an action before its risk analysis, evaluator, and canary are specified.

## 6. Non-negotiable invariants

1. A learned policy chooses a strategy, never permission.
2. Explicit user and operator choices win.
3. Unchosen actions receive no fabricated live reward.
4. Every randomized choice records its true non-zero propensity.
5. Raw evidence remains immutable; derived utility is recomputable.
6. Operational success is not quality.
7. Model self-report is ineligible for promotion.
8. Shadow recommendations cannot change the served action.
9. Risky and destructive actions receive zero exploration.
10. Missing authoritative policy state disables adaptation.
11. One identity's raw evidence cannot update another identity's policy.
12. Deletion removes both authoritative adaptive rows and the private projection.
13. Policy transitions are compare-and-swap and stale epochs cannot reactivate.
14. Neo4j or agent-memory failure cannot create graph-only truth.
15. A spike model can validate mechanics but cannot satisfy a production-model gate.
16. Canary or active policy state cannot make a learned action serve until the
    production serving adapter exists and passes its own activation gates.

## 7. Architecture

```text
request
  │
  ├─ explicit override + provider capabilities + gateway eligibility
  ├─ static Aura decision (always available)
  ├─ authoritative PostgreSQL policy epoch gate
  │     └─ unavailable/off/rollback => static decision
  ├─ immutable local challenger snapshot
  ├─ shadow or randomized safe canary assignment
  └─ domain adapter executes an eligible strategy
          │
          ▼
tool result / answer / correction / deterministic evaluator / human rubric
          │
          ├─ typed immutable event
          ├─ conversation turn + event in one PostgreSQL transaction
          ├─ leased ordered outbox
          ├─ private Neo4j projection
          └─ offline OPE + closed-cohort promotion gate
```

### 7.1 Source of truth

PostgreSQL is authoritative for events, ordering, deletion, and the distributed policy
epoch. Neo4j is a replayable analytical projection. A graph outage may delay learning;
it may not lose a turn or permit stale activation.

### 7.2 Request path

The long-term scorer is local and immutable after context features exist. Neo4j,
remote teachers, snapshot building, and evaluation remain off the hot path.

The distributed safety epoch is different: before an adaptive action can affect
execution, the process reads `aura.adaptive_policy_state`. This gives a hard rollback
bound of one authoritative read before the next action. A read error returns the
baseline. An optimized cache may be introduced only if it preserves an explicit,
measured stale-serving bound and still fails closed during a partition.

### 7.3 Outcome path

The current Runner integration records:

- terminal tool results;
- terminal assistant answers;
- actual model reasoning configuration;
- actual selected tool/skill/knowledge call;
- tokens, cost, latency, and bounded operational fields.

Tool and assistant completion events explicitly carry `quality_observed:false`.
Independent or human evidence arrives as later immutable events linked to the same
decision.

## 8. Implemented PostgreSQL contracts

Migrations:

- `0052_adaptive_outbox`
- `0053_adaptive_identity_tombstones`
- `0054_adaptive_policy_state`
- `0055_adaptive_policy_evidence`
- `0056_adaptive_tombstone_privileges`
- `0057_adaptive_runtime_hardening`
- `0058_adaptive_policy_hardening`

### 8.1 Event

Event kinds are `decision`, `outcome`, `correction`, `promotion`, and `rollback`.
Every event has:

```text
id UUID
owner_id UUID
aggregate_id TEXT
sequence BIGINT
decision_id UUID
event_kind ENUM
payload JSONB with schema_version
payload_hash SHA-256
status pending|leased|projected|dead_letter
attempt and lease fields
created_at
```

The event UUID is idempotent only when every immutable field and the canonical payload
hash match. Reusing an UUID with changed content is a hard conflict.

Per-owner/per-aggregate sequencing is atomic and gap-free. Projector workers use
`SKIP LOCKED`, but may claim sequence `n+1` only after earlier events in that aggregate
are projected. Different aggregates may progress concurrently. A stale worker cannot
acknowledge an expired and reclaimed lease.

### 8.2 Atomic Runner persistence

`adaptive.TurnCommitter` uses the same owner-scoped PostgreSQL transaction for:

- the adaptive event;
- conversation sequence allocation;
- the tool or assistant turn;
- the assistant cache metric.

An exact retry writes neither a duplicate event nor a duplicate turn. An immutable
event conflict rolls back the companion turn.

Oversized turn content is staged through Aura's spill path before commit. A successful
transaction retains the staged blob and turn reference; a transaction failure removes
the staged blob. Live tests cover successful spill, event-conflict rollback, and blob
cleanup, so large content no longer bypasses the atomic adaptive commit contract.

### 8.3 Identity deletion

Adaptive rows reference `aura.identities` with `ON DELETE CASCADE`. A permanent
tombstone is written on identity deletion. Recreating the same UUID cannot append new
adaptive events.

Deprovisioning first writes the permanent PostgreSQL identity tombstone in a distinct
journaled `adaptive_fence` leg, then runs the `adaptive_graph` purge before the
identity row is hard-deleted. A Neo4j purge failure stops the saga and is retried.

The projector checks the tombstone both before and after its graph write. If deletion
races between those checks, the post-write check purges the late graph projection and
does not acknowledge the outbox row. A controlled live interleaving test exercises
this exact time-of-check/time-of-use race.

### 8.4 Distributed policy state

The singleton policy row contains:

```text
scope = global
epoch
policy_version
mode = off|shadow|canary|active|rollback
rollout_bps
config JSONB
updated_at
```

All application policy mutation goes through `adaptive.PolicyService`. The service
requires the actor's `adaptive.manage` capability (or wildcard capability), locks the
current row, validates the legal state transition, and increments `epoch` with
compare-and-swap semantics. A stale expected epoch returns `ErrPolicyChanged`.

Transitions to `canary` or `active` require a gate report that the service evaluates
itself, a configured production-model ID match, and immutable
`adaptive_promotion_evidence`. Every transition writes
`adaptive_policy_transitions`; evidence and transition rows are protected by
append-only triggers. The legacy direct compare-and-swap application method is
deleted; `PolicyService` is the only application mutation API.

Canary assignment hashes a point-specific decision ID and policy version into 10,000
stable buckets. Reasoning, each tool call, and other adaptive points derive different
decision IDs from one request ID, preventing multi-assignment collisions.

### 8.5 Production composition boundary

Aura's chat boot path now composes the decision hook and atomic turn committer only
when the authoritative boot policy is `shadow`. The hook observes challengers while
the static action remains served. Before every observation or adaptive commit, a fresh
policy read gates the path; `off`, `rollback`, or a policy-store error falls back to
the existing non-adaptive persistence path.

`canary` and `active` learned-action serving are deliberately not implemented. If
either state is encountered at boot, Aura logs the unsupported state and forces the
static baseline. This makes the present production boundary executable rather than a
documentation-only convention.

## 9. Neo4j and agent-memory reuse

Aura reuses the running Neo4j deployment and agent-memory's existing
`(:User {identifier})` node:

```text
(:User)-[:HAS_ADAPTIVE_EPISODE]->(:AdaptiveEpisode)
(:AdaptiveEpisode)-[:HAS_ADAPTIVE_EVENT {sequence}]->(:AdaptiveEvent)
```

Rules:

1. Only Aura's internal projector writes these labels.
2. The LLM-facing agent-memory MCP does not receive general Cypher access.
3. Existing memory add/search/context tools continue to query their own labels.
4. Adaptive payloads never enter memory message, fact, preference, or reasoning-trace
   labels.
5. Adaptive purge deletes only `AdaptiveEpisode` and `AdaptiveEvent`; the shared
   `User` and memory subgraph remain.
6. The existing Aura agent-memory fork remains authoritative for memory semantics.
   Adaptive storage does not restore removed reasoning-trace MCP tools.

The live test projected an adaptive marker through an existing user, called the real
agent-memory `memory_get_context`, confirmed the marker and event ID were absent, then
purged the adaptive nodes while retaining the shared user.

Neo4j Community does not provide Enterprise property-based access control. Therefore
Community deployment is acceptable only while untrusted actors and the LLM have no
general Cypher endpoint. If general untrusted Cypher is exposed, activation is blocked
unless Neo4j Enterprise property privileges or an equivalent isolated database/service
boundary is deployed.

## 10. Policy algorithm

The static policy is the champion.

The first challenger remains:

> per action, top-5 non-negative cosine-neighbor mean from at most 256 chosen-action
> outcomes, with a versioned local snapshot, bounded recent overlay, clipped raw
> components, and action-local drift quarantine.

Spike 102 invalidated promotion of that challenger on unseen families. It may continue
in shadow while Aura collects stronger real-model evidence. LinUCB and graph-prior
LinUCB remain research challengers, not production fallbacks.

No algorithm may compensate for missing overlap. An action with no supported evidence
uses the static prior.

## 11. Off-policy evaluation

Production logs must include the actual probability of the chosen action and non-zero
support for every candidate action being evaluated.

Required diagnostics:

- overlap and minimum logging probability;
- maximum importance weight;
- effective sample size;
- IPS as a high-variance diagnostic;
- SNIPS and doubly robust estimates as required estimators;
- uncertainty intervals and known negative controls.

Deterministic logs without support are rejected, not scored. Spike 102 proved this
behavior and showed why IPS alone is insufficient: IPS missed truth while SNIPS and DR
covered it.

OPE may qualify a candidate for a production canary. It cannot directly activate one.

## 12. Calibrated outcome and promotion gate

### 12.1 Eligible evaluators

| Evaluator | Promotion eligible | Requirement |
|---|---|---|
| deterministic checker | yes | versioned benchmark/rubric ID |
| human rubric | yes | rubric ID and reviewer provenance |
| calibrated judge model | yes | calibration dataset/version and drift monitoring |
| behavioral proxy | no by itself | diagnostic only |
| operational tool success | no by itself | diagnostic only |
| model self-report | no | never promotion evidence |

Aura stores provenance, not an arbitrary confidence constant. Calibration quality is
measured on held-out labeled examples and versioned.

### 12.2 Closed production cohort

The implemented gate requires:

- environment `production_canary`;
- real model ID, policy version, closed cohort ID;
- one unique decision ID per assignment;
- logged randomization propensity in `(0,1)`;
- outcomes from an eligible evaluator with calibration ID;
- minimum raw and effective sample sizes;
- bounded total and differential censoring;
- preregistered number of statistical looks;
- quality-uplift lower bound above the declared threshold;
- harm-increase upper bound at or below the declared margin.

Five looks divide total alpha `0.05` by Bonferroni correction. Independent-arm
Wilson/Newcombe bounds are intentionally conservative.

Spike 105 used a 2-point harm margin and 2,500 assignments per arm:

- equal quality/equal harm: `0/1000` false promotions;
- quality `65% -> 78%`, equal harm: `1000/1000` promotions;
- the same quality gain with harm `1% -> 8%`: `0/1000` promotions.

These seeded trials validate gate behavior, not model quality. Before a real canary,
Aura must run a power analysis using the real baseline rate, expected minimum effect,
harm rate, randomization ratio, and planned looks. Low traffic extends the experiment;
it does not lower the evidence requirement.

### 12.3 Poisoning and delayed outcomes

- Duplicate decision IDs invalidate the batch.
- Missing outcomes remain as censored assignments; they cannot disappear.
- Differential censoring beyond the preregistered bound invalidates the batch.
- Corrections append and supersede; they never rewrite raw evidence.
- One identity cannot contribute to another identity's raw policy bank.
- Abnormal evaluator/calibration drift stops promotion and rolls back canary mode.

## 13. Rollout and rollback

Stages:

```text
off -> shadow -> canary -> active
          \         \        \
           ----------> rollback
```

`shadow` records real decisions and outcomes but serves the static choice. `canary`
uses stable randomized assignment only for eligible safe actions. `active` is allowed
only after every domain-specific gate passes. `rollback` disables observing and
adapting until an audited operator transition.

Automatic rollback triggers include:

- quality/harm bound breach at a preregistered look;
- authorization or identity-isolation invariant violation;
- invalid policy schema/checksum;
- evaluator calibration drift;
- operator kill switch.

Rollback changes future strategy only. It cannot undo completed tool side effects.

## 14. Failure behavior

| Failure | Required behavior |
|---|---|
| PostgreSQL policy read fails | baseline only; no stale adaptive action |
| PostgreSQL turn transaction fails | follow existing turn-fatal persistence behavior |
| Neo4j unavailable | keep authoritative outbox; retry projection |
| projector lease expires | another worker reclaims; stale ack rejected |
| dead-letter threshold reached | retain row, alert, do not skip aggregate ordering silently |
| owner deleted during projection | purge private graph and refuse recreation |
| agent-memory unavailable | adaptive evidence remains in PostgreSQL; no direct memory write |
| model/provider lacks action | deterministic supported fallback |
| outcome missing | censored/unevaluated; never invent quality |
| evaluator uncalibrated | exclude from promotion |
| corrupt or duplicate evidence | invalidate gate and emit poison diagnostic |
| Qwen spike wins | no production promotion |

## 15. Security and privacy

- No raw prompt is written by the current decision hook.
- Tool arguments are represented only by byte count and SHA-256.
- Logs and spans carry bounded IDs/enums, not arguments or retrieved text.
- RLS/FK owner scope is enforced in PostgreSQL.
- Adaptive Neo4j labels are private to the internal projector.
- Destructive/risky actions receive zero exploration.
- Promotion/rollback use the capability-gated, evidence-bound `PolicyService`. No
  production CLI or API is exposed yet.
- Global policies may use curated or explicitly approved anonymized evidence only.
- Raw user evidence is never pooled globally by default.

## 16. Observability

Required bounded metrics:

- decisions by domain, mode, action, policy, and fallback reason;
- shadow disagreement and canary assignment;
- propensity/overlap/ESS diagnostics;
- evaluator kind, calibration version, censor rate, and outcome delay;
- raw quality, harm, tokens, latency, and cost;
- outbox depth, oldest age, lease reclaim, retry, and dead letter;
- projector latency and graph errors;
- policy epoch, CAS conflicts, fail-closed reads, promotion, and rollback;
- identity purge and tombstone rejection.

`learning_write` success, projected row count, tool HTTP success, and the old
“9.8/10 plumbing” checklist are not answer-quality metrics.

## 17. Verification matrix

| Gate | Current result |
|---|---|
| Real Runner tool/final persistence points | pass |
| Atomic turn + adaptive event | pass |
| Concurrent idempotency and gap-free order | pass |
| Projector lease/retry/dead-letter behavior | pass |
| Live graph-outage pending/retry/dead-letter path | pass |
| Live Postgres migration/integration | pass |
| Live Neo4j projection | pass |
| Real agent-memory MCP non-leakage | pass |
| Identity cascade, tombstone, private purge | pass |
| Deletion/projector TOCTOU interleaving | pass |
| Two-process rollback and stale CAS rejection | pass |
| Partition fail-closed behavior | pass |
| Point-specific decision IDs | pass |
| Atomic oversized-turn spill and fault cleanup | pass |
| Capability/evidence/audit-bound policy service | pass |
| Production shadow composition and baseline fallback | pass |
| Supported SNIPS/DR OPE | pass |
| Canary false-promotion/harm/poison gate | pass |
| Unseen-family generalization | **fail** |
| Real configured production model quality | **not run** |
| Real production randomized traffic | **not run** |
| Learned-action canary/active serving | **not implemented** |
| Production operator CLI/API | **not implemented** |

## 18. Industrial readiness verdict

There is no universal online “industrial AI score” that converts these checks into a
credible single number. NIST AI RMF, contextual-bandit OPE practice, and production
experimentation all require evidence by risk dimension.

Aura's defensible scorecard is therefore:

| Dimension | Verdict |
|---|---|
| event integrity and replay | live-validated mechanism |
| identity isolation and deletion | live-validated mechanism |
| distributed rollback | live-validated mechanism |
| agent-memory coexistence | live validated |
| production shadow wiring | implementation validated |
| policy promotion control | capability/evidence/audit mechanism validated |
| OPE support | validated on known spike surface |
| sequential canary gate | statistically validated in seeded simulation |
| challenger generalization | failed current gate |
| real-model benefit | unproved |
| production activation | **blocked** |

The result is a strong adaptive platform, not a proven quality win. Aura “wins a lot”
on architecture, durability, privacy, and evaluation rigor. It has not yet won on the
only number that authorizes serving: real-model, real-traffic quality and harm.

## 19. Production activation criteria

Activation remains blocked until all are true:

1. The real configured production model—not Qwen 2B—runs the four initial domains.
2. Static champion and challenger are randomized only over safe eligible decisions.
3. Every assignment records support and propensity.
4. Independent evaluator calibration passes on a held-out human/deterministic set.
5. A preregistered power analysis sets cohort size and look schedule.
6. Quality lower and harm upper bounds pass per domain and overall.
7. Missing/differential outcomes remain within declared limits.
8. The learned-action serving adapter is implemented and independently fault-tested;
   until then `canary` and `active` remain baseline-only.
9. A production cohort loader proves promotion observations came from the immutable
   assignment/outcome ledger rather than caller-constructed input.
10. Community Neo4j remains inaccessible to untrusted general Cypher, or Enterprise
    property privileges/isolation are deployed.
11. Full unit, integration, live-infrastructure, race, privacy, retention, restart,
    and rollback suites pass.
12. An operator explicitly approves the candidate.

## 20. Delivery sequence

1. **Keep off by default.** Typed events, outbox, private graph, deletion fence,
   policy service, promotion gate, and atomic spill handling are implemented.
2. **Run shadow only.** Actual hooks and committers are composed behind the
   authoritative shadow state; learned actions still cannot serve.
3. **Calibrate evaluators.** Build versioned deterministic/human sets for each domain
   and load closed cohorts from the immutable ledger.
4. **Run real-model shadow.** Use Aura's real configured model—not Qwen 2B—and collect
   supported propensities without changing served actions.
5. **Run real canary.** Use the preregistered closed cohort and sequential gate.
6. **Activate narrowly.** Promote one proven domain/action catalog at a time.
7. **Extend surfaces.** Add memory, provider, context, document, multimodal, and
   recovery adapters only after each gets its own risk/evaluation contract.

## 21. Primary references

- Vowpal Wabbit contextual bandits:
  <https://vowpalwabbit.org/docs/vowpal_wabbit/python/latest/tutorials/python_Contextual_bandits_and_Vowpal_Wabbit.html>
- Vowpal Wabbit off-policy evaluation:
  <https://vowpalwabbit.org/docs/vowpal_wabbit/python/latest/tutorials/off_policy_evaluation.html>
- Wang, Agarwal, Dudík, *Optimal and Adaptive Off-policy Evaluation in Contextual
  Bandits*:
  <https://www.microsoft.com/en-us/research/publication/optimal-adaptive-off-policy-evaluation-contextual-bandits/>
- Anytime-valid off-policy inference:
  <https://arxiv.org/abs/2210.10768>
- Confidence sequences for bounded random variables:
  <https://arxiv.org/abs/2210.08639>
- Debezium transactional outbox:
  <https://debezium.io/documentation/reference/transformations/outbox-event-router.html>
- Neo4j operations and editions:
  <https://neo4j.com/docs/operations-manual/current/introduction/>
- Neo4j property-based access control:
  <https://neo4j.com/docs/operations-manual/current/authentication-authorization/property-based-access-control/>
- OpenTelemetry GenAI conventions:
  <https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/>
- NIST AI Risk Management Framework:
  <https://airc.nist.gov/airmf-resources/airmf/5-sec-core/>

## 22. Repository evidence

- `.planning/spikes/097-qwen-multidomain-reward-surface`
- `.planning/spikes/098a-static-centroid-baseline`
- `.planning/spikes/098b-graph-knn-policy`
- `.planning/spikes/098c-linucb-policy`
- `.planning/spikes/098d-graph-prior-linucb`
- `.planning/spikes/099-neo4j-outcome-policy-graph`
- `.planning/spikes/100-adaptive-policy-governance-stress`
- `.planning/spikes/101-aura-adaptive-shadow-e2e`
- `.planning/spikes/102-adaptive-generalization-ope-gate`
- `.planning/spikes/103-adaptive-runner-outbox-proof`
- `.planning/spikes/104-adaptive-identity-distributed-safety`
- `.planning/spikes/105-adaptive-quality-canary-gate`

The JSON artifacts and executable tests in those directories are the evidence. This
specification does not promote claims beyond their recorded scope.
