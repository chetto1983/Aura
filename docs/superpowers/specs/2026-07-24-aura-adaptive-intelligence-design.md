# Aura Adaptive Intelligence — Production Design Specification

**Date:** 2026-07-24
**Status:** Shadow plumbing validated; adaptive production serving blocked
**Evidence:** Spikes 095–105
**Initial domains:** reasoning, tool routing, skill routing, knowledge retrieval,
memory recall
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

The current production champion remains Aura's static behavior. Graph kNN is the
designated first shadow candidate, but no immutable snapshot builder or production
scorer currently loads it. It cannot serve unless a randomized canary on the real
configured model and real traffic passes the calibrated promotion gate.

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
| Canary statistics, spike 105 | validated seeded simulation only | false-promotion, power, harm, poison, censor behavior of that function | production inference or real-model win |

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
- Querying Neo4j for adaptive scoring, snapshot loading, or private projection on the
  request hot path. Existing owner-scoped memory recall is the explicit exception.
- Writing adaptive evidence through LLM-facing `memory_add_*` tools.
- Claiming Qwen 2B results transfer to a larger or different production model.
- Creating or mutating executable skills, code, configuration, prompts, policies,
  or model weights through a runtime learned action.
- Treating model-asserted approval or successful execution without error as authority
  or proof that an improvement is safe and useful.
- Activating every adaptive surface at once.

## 5. Adaptive surface catalog

All surfaces use the same decision, outcome, policy, rollout, and deletion contracts.
Each surface has its own action catalog and promotion evidence; success in one domain
does not automatically promote another.

| Surface | Safe strategy choices | Authoritative outcome examples | State |
|---|---|---|---|
| Reasoning | direct, bounded, deep | deterministic correctness, calibrated answer rubric, tokens, latency | generic observation only; adapter required before each model request is built |
| Tool discovery | semantic top-1, model top-3, full eligible catalog | correct discovery set/order, later tool use, independent task result | adapt ordered discovery results before deferred-tool exposure; model tool call is observation |
| Skill routing | semantic top-1, model top-3, full eligible catalog | correct skill, artifact rubric, explicit correction | adapt ordered read-only skill candidates; pinned/use/write actions are not arms |
| Executable skill reuse | lexical, structural, graph, vector, hybrid, none | gateway-observed use linked to later calibrated outcome | future producer-consumer episode contract; no mutation authority |
| Knowledge/document retrieval | none, vector, graph expansion, rerank depth | citation support, deterministic answer, retrieval relevance | adapt internal retrieval strategy before execution; model query/scope is observation |
| Memory recall | off, owner-scoped long-term top-4, long-term top-8 | explicit correction, recall rubric, final quality | adapt existing `ArchivalRecaller` before the deployed MCP call |
| Memory write | do not write, candidate, supersede | human confirmation, later contradiction, temporal validity | future adapter |
| Model/provider routing | local/cloud/model capability class | calibrated quality, cost, latency, availability | future adapter |
| Context management | retain, compact, recall, tool-result eviction depth | answer rubric, cache/tokens, lost-fact checks | future adapter |
| Multimodal route | local/cloud/fallback strategy | modality-specific deterministic or human rubric | future adapter |
| Retry/recovery | retry, alternate provider, ask user, stop | terminal correctness, duplicate-side-effect checks | future adapter |
| Scheduler/concurrency | serial, bounded parallel, defer | completion, deadline, cost, conflict rate | research only |

“Future adapter” means the control-plane contract is reusable; it is not permission to
add an action before its risk analysis, evaluator, and canary are specified.

SAGE's AppWorld result supports testing skill reuse, not copying its weight-training
stack or text/name heuristic. Provenance proves exposure, not causal contribution.
Skill reuse stays shadow-only until randomized skill/no-skill exposure defines
many-to-many attribution, interference, horizon, censoring, and contamination.

Retrieval families are peers: lexical/structural, graph, vector, hybrid/reranked, and
none are frozen versioned actions. Aura benchmarks them on its own model and calibrated
data; each snapshot binds corpus/ACL epoch, parser, embedding, index, reranker, top-K,
and tie-breaking. Neo4j availability does not privilege graph or vector retrieval.

Improvement mutation is a separate proposal workflow. It requires exact artifact/source
binding, server-side current-message approval, owner capability, idempotency, isolated
review, normal Aura verification gates, and immutable audit. Adaptive evidence alone
cannot write or activate executable production assets.

Memory recall reuses the vendored agent-memory sidecar, MCP bridge, POLE+O graph,
owner scope, dedup, forget, and untrusted-data fence. The first actions vary only
off/top-K; memory mutation is never an arm and PostgreSQL owns adaptive facts. Move
the exact fenced result out of stable `messages[1]` into one synthetic non-persisted
dynamic-tail item. The context budgeter treats it as indivisible, protects it from
L1/L2.5 changes, and positions it immediately before the current model-visible user
message, or at the tail when none exists. Delivery commits only after the final
transformed request fits and contains the exact item; otherwise record
`none/context_budget` and omit it—never truncate it. The whole multi-query MCP
operation is bracketed by equal epochs advanced by every result-affecting writer;
Neo4j read-committed and a bookmark alone are insufficient. Result-neutral
`User`/adaptive projector writes require proof; bad epochs or untracked affecting
writes are diagnostic-only. Raw query/content and content fingerprints remain forbidden.

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
17. A pre-execution assignment never claims delivery; exposure follows its durable fact.
18. Missing/fallback delivery remains in ITT and never enters served-action OPE without
    a valid marginal exposure probability.

## 7. Architecture

```text
request
  │
  ├─ explicit override + provider capabilities + gateway eligibility
  ├─ static Aura decision (always available)
  ├─ authoritative PostgreSQL policy epoch gate
  │     └─ unavailable/off/rollback => static decision
  ├─ immutable local challenger snapshot
  ├─ durable shadow or randomized safe canary assignment
  ├─ domain adapter executes an eligible strategy
  ├─ durable delivery/exposure fact
  └─ expose result to downstream model/consumer
          │
          ▼
answer / correction / deterministic evaluator / human rubric
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

The adaptive scorer is local and immutable after context features exist. Remote
teachers, snapshot building, private projection, and adaptive Neo4j analysis remain
off the hot path. The existing owner-scoped Neo4j-backed memory-recall call is the
only initial exception; the adaptive policy chooses its bounded top-K before that call.

The distributed safety epoch is different: before an adaptive action can affect
execution, the process reads `aura.adaptive_policy_state`. This gives a hard rollback
bound of one authoritative read before the next action. A read error returns the
baseline. An optimized cache may be introduced only if it preserves an explicit,
measured stale-serving bound and still fails closed during a partition.

### 7.3 Current observation path

The current Runner integration records terminal tool results, terminal assistant
answers, request-level reasoning configuration, top-level model-emitted tool calls,
tokens, cost, latency, and bounded operational fields. Tool and assistant completion
events explicitly carry `quality_observed:false`.

This is schema-`1.0` operational observation, not production-eligible adaptive
evidence. It does not distinguish repeated model rounds in one request, record the
candidate action catalog or true behavior propensity, identify a frozen snapshot, or
represent a multiplexed skill/knowledge choice hidden inside tool arguments.
Independent or human evidence is not yet composed into the production lifecycle.

### 7.4 Schema-2.0 assignment, delivery, outcome, and correction contract

Amendment #93.4 defines the first promotion-eligible ledger contract. Each typed
assignment uses a unique owner-scoped assignment ID derived from request, canonical
decision point, and point ordinal. It freezes policy/snapshot/model/cohort identity,
ordered eligible actions, champion/recommended/selected intended action, optional
experiment and arm identity/propensity, marginal intended-action probabilities,
bounded features, and selection/override reason. It cannot claim post-execution truth.
The canary gate uses arm assignment. An explicit static/no-op action is representable.

After execution, one deterministic immutable delivery fact records actual exposure,
its known marginal probability or explicit unknown, effective bounded parameters,
registered result IDs/order and revisions, or fallback. It commits before downstream
exposure. Missing delivery remains ITT missing/censored; treatment-as-served OPE
requires a recorded positive marginal exposure probability.
Outcomes/corrections append separately; operational completion remains ineligible.

The draw and durable assignment commit happen before the selected strategy executes.
If assignment, execution, or delivery persistence fails, the learned result is
discarded and static behavior serves. Payloads contain only typed safe projections—
registered IDs, bounded limits, booleans, and lengths—never raw prompts, queries,
arguments, documents, memories, credentials, reasoning, or content fingerprints.

Schema-`2.0` event IDs are deterministic from assignment/kind/stable source identity.
One delivery exists per assignment and one terminal outcome per evaluator/source key.
Corrections form one same-owner/scope chain; missing targets, forks, cycles, and
cross-kind links are invalid. Schema-`1.0` history is never eligible.
The existing `decision` event kind carries assignments; schema `2.0` adds `delivery`
and retains the existing `outcome` and `correction` kinds.

## 8. Implemented PostgreSQL plumbing contracts

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
The implemented schema-`1.0` envelope has:

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
hash match. Reusing an UUID with changed content is a hard conflict. The current
database check requires only an object payload with a string `schema_version`; it does
not yet enforce Amendment #93.4's typed assignment, delivery, outcome, and correction
fields or assignment/delivery uniqueness. Schema-`2.0` enforcement must be additive
so immutable historical rows are retained but excluded from eligible cohorts.

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

The shadow-only release exposes no Go, CLI, or HTTP policy-mutation API. Direct
runtime table mutation is revoked. PostgreSQL retains the narrow capability-bound
transition function plus append-only evidence/audit schema for migration
compatibility, but no dormant application caller is shipped. A future mutation
surface must land with an authorized production composition root and live
capability/audit evidence in the same amendment.

Migration `0058`'s single evidence-report rule for both `canary` and `active` is not
the final transition contract. It must be replaced by distinct sealed artifact kinds:
an admission plan for `shadow -> canary`, and a loader-reconstructed closed outcome
report for `canary -> active`. The transition revalidates artifact kind, exact scope,
hashes, eligibility, and target state in the same transaction.

Canary assignment is designed to hash a point-specific assignment ID and policy
version into 10,000 stable buckets. The current reasoning hook instead derives one
`DecisionIDForPoint(requestID, "reasoning")` for every model round in a request, so
multi-round observations collide semantically. Amendment #93.4 requires the point
ordinal in every typed assignment ID before canary evidence can be eligible.

### 8.5 Production composition boundary

Aura's chat boot path now composes the decision hook and atomic turn committer only
when the authoritative boot policy is `shadow`. The hook records generic observations
while the static action remains served; no learned snapshot scorer is composed.
Before every observation or adaptive commit, a fresh policy read gates the path;
`off`, `rollback`, or a policy-store error falls back to the existing non-adaptive
persistence path.

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
6. Projector user creation matches agent-memory's non-null `id`, `created_at`, and
   `attributes_json` schema, or uses `MATCH` after its supported user upsert.
7. Live proof projects first, then passes owner-scoped agent-memory write/read.
8. The vendored Apache-2.0 license/notices ship in source and image; provenance names
   both upstream/fork base and the actual later Aura tree revision.

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
> outcomes frozen at the training cutoff, with a versioned local snapshot and an
> explicit static prior when action-local support is absent.

Spike 102 invalidated promotion of that challenger on unseen families. It may be
implemented as a frozen shadow snapshot while Aura collects stronger real-model
evidence. LinUCB and graph-prior LinUCB remain research challengers, not production
fallbacks.

The first implementation has no mutable recent overlay. Every refresh creates a new
fully hashed snapshot and policy version.

No algorithm may compensate for missing overlap. An action with no supported evidence
uses the static prior.

## 11. Off-policy evaluation

Production logs distinguish randomized experiment-arm propensity from each action's
marginal behavior probability. The intention-to-treat canary gate uses the assigned
arm even when champion and challenger serve the same action. OPE uses marginal action
propensity and requires non-zero support for every candidate action being evaluated.

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

OPE is an admission diagnostic only when its logging policy has real support. Shadow
traffic served at champion propensity `1.0` is not OPE-eligible. OPE cannot claim
canary outcomes or activate a policy.

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

The implemented statistical function validates the following fields on an in-memory
batch:

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

Five looks divide total alpha `0.05`. The current ESS-weighted independent-arm
Wilson/Newcombe bounds are simulation evidence only, not a production authorizer.

Spike 105 used a 2-point harm margin and 2,500 assignments per arm:

- equal quality/equal harm: `0/1000` false promotions;
- quality `65% -> 78%`, equal harm: `1000/1000` promotions;
- the same quality gain with harm `1% -> 8%`: `0/1000` promotions.

These seeded trials validate gate behavior, not model quality. Before a real canary,
Aura must run a power analysis using the real baseline rate, expected minimum effect,
harm rate, randomization ratio, and planned looks. Low traffic extends the experiment;
it does not lower the evidence requirement.

Production uses exactly one randomized focal request per evaluation conversation;
later requests stay champion diagnostics. Preregistration freezes binary quality/harm
endpoints, snapshot/policy, focal predicate, owner/session/time blocks, carryover and
interference assumptions, power simulation, and five alpha allocations totaling
`0.05`. Each look applies exact randomization inference under the frozen assignment
mechanism and inverts it for quality/harm effect bounds. Unmodelled carryover or
interference invalidates the cohort.

The ledger-only loader reconstructs assignments, deliveries, outcomes, corrections,
and membership. ITT uses arm propensity. Treatment-as-served OPE requires known
positive marginal exposure probability and rejects missing/fallback mismatch. The
loader seals exact scope plus ledger/report hashes for transactional revalidation.

Shadow-to-canary admission uses a distinct immutable artifact containing the verified
snapshot, evaluator calibration, power/support/randomization plan, preregistered focal
cohort, safety limits, rollback plan, and operator approval. It claims no canary
outcomes. Only a closed loader-sealed real-canary outcome artifact may authorize
canary-to-active.

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

`shadow` records diagnostics but serves the static choice at behavior probability
`1.0`. A sealed admission artifact is required before `canary`, which uses stable
randomized assignment only for one preregistered focal safe domain per request.
`active` requires a closed loader-sealed real-canary outcome artifact. `rollback`
disables observing and adapting until an audited operator transition.

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
- Historical schema-`1.0` observations used tool-argument byte count plus SHA-256;
  those rows are ineligible and schema `2.0` forbids every content-derived fingerprint
  of prompts, queries, arguments, memories, and documents.
- Plain hashes protect only non-secret immutable artifact integrity. Schema `2.0`
  performs no content-derived deduplication.
- Logs and spans carry bounded IDs/enums, not arguments or retrieved text.
- RLS/FK owner scope is enforced in PostgreSQL.
- Adaptive Neo4j labels are private to the internal projector.
- Destructive/risky actions receive zero exploration.
- Direct runtime policy DML is revoked, and the shadow release exposes no Go, CLI,
  or API mutation surface.
- Global policies may use curated or explicitly approved anonymized evidence only.
- Raw user evidence is never pooled globally by default.

## 16. Observability

Required bounded metrics:

- assignments by domain, mode, intended action, policy, and selection reason;
- deliveries by actual action, exposure status, and bounded fallback reason;
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

## 17. Validation, delivery, and evidence appendix

The current verification matrix, readiness verdict, activation criteria, ordered
delivery sequence, primary research links, and repository evidence index live in
`2026-07-24-aura-adaptive-intelligence-delivery-and-evidence.md`.
