# Aura Adaptive Intelligence — Delivery and Evidence Appendix

**Parent specification:**
`2026-07-24-aura-adaptive-intelligence-design.md`

## 1. Verification matrix

| Gate | Current result |
|---|---|
| Real Runner tool/final persistence points | pass |
| Atomic turn + adaptive event | pass |
| Concurrent idempotency and gap-free order | pass |
| Projector lease/retry/dead-letter component behavior | pass |
| Production projector Start/Stop lifecycle | **not implemented** |
| Live graph-outage pending/retry/dead-letter path | pass |
| Live Postgres migration/integration | pass |
| Live Neo4j projection | pass |
| Real agent-memory MCP non-leakage | pass |
| Identity cascade, tombstone, private purge | pass |
| Deletion/projector TOCTOU interleaving | pass |
| Two-process rollback and stale CAS rejection | pass |
| Partition fail-closed behavior | pass |
| Point-specific assignment IDs, including model-round ordinal | **not implemented** |
| Atomic oversized-turn spill and fault cleanup | pass |
| Capability/evidence/audit database mechanism | pass |
| Reachable policy service and authorized CLI/API | intentionally absent under #93.3 |
| Production shadow composition and baseline fallback | pass |
| Typed schema-2.0 assignments/deliveries/outcomes/corrections | **not implemented** |
| Immutable snapshot builder and verified local loader | **not implemented** |
| Durable ledger-reconstructed cohort loader | **not implemented** |
| One-focal-assignment request-level statistical unit | **not implemented** |
| Separate experiment-arm and marginal-action propensities | **not implemented** |
| Sealed canary-admission and closed-outcome artifacts | **not implemented** |
| Pre-choice tool/skill/knowledge/memory domain adapters | **not implemented** |
| Supported SNIPS/DR OPE | pass |
| ESS-Wilson canary simulation gate | pass as simulation; ineligible for production |
| Exact randomization production gate | **not implemented** |
| Agent-memory typed context IDs/order/retrieval revision | **not implemented** |
| Agent-memory-compatible projector user upsert | **not implemented** |
| Vendored agent-memory license/revision provenance | **not implemented** |
| Unseen-family generalization | **fail** |
| Real configured production model quality | **not run** |
| Real production randomized traffic | **not run** |
| Learned-action canary/active serving | **not implemented** |
| Production operator CLI/API | **not implemented** |

## 2. Industrial readiness verdict

There is no universal online “industrial AI score” that converts these checks into a
credible single number. NIST AI RMF, contextual-bandit OPE practice, and production
experimentation all require evidence by risk dimension.

Aura's defensible scorecard is therefore:

| Dimension | Verdict |
|---|---|
| event integrity and replay | live-validated mechanism |
| identity isolation and deletion | live-validated mechanism |
| distributed rollback | live-validated mechanism |
| agent-memory coexistence | MCP non-leakage validated; shared-user compatibility blocked |
| production shadow wiring | implementation validated |
| policy promotion control | database mechanism validated; no application caller |
| OPE support | validated on known spike surface |
| sequential canary gate | statistically validated in seeded simulation |
| challenger generalization | failed current gate |
| real-model benefit | unproved |
| production activation | **blocked** |

The result is a strong adaptive platform, not a proven quality win. Aura “wins a lot”
on architecture, durability, privacy, and evaluation rigor. It has not yet won on the
only number that authorizes serving: real-model, real-traffic quality and harm.

## 3. Production activation criteria

Activation remains blocked until all are true:

1. The real configured production model—not Qwen 2B—runs the five initial domains.
2. The frozen point/ordinal predicate atomically claims one focal assignment per
   request and randomizes only safe eligible actions; other points/domains stay
   champion-only diagnostics.
3. Every assignment records support/propensity before execution; one immutable
   delivery fact records actual exposure or fallback before downstream use.
4. Independent evaluator calibration passes on a held-out human/deterministic set.
5. A preregistered power simulation sets cohort size, one focal request per
   evaluation conversation, blocked assignment, interference assumptions, and looks.
6. Quality lower and harm upper bounds pass per domain and overall.
7. Missing/differential outcomes remain within declared limits.
8. The learned-action serving adapter is implemented and independently fault-tested;
   until then `canary` and `active` remain baseline-only.
9. A production cohort loader seals promotion observations reconstructed from the
   immutable assignment/delivery/outcome ledger, and the policy transition revalidates that
   artifact transactionally rather than accepting caller-constructed input.
10. Community Neo4j remains inaccessible to untrusted general Cypher, or Enterprise
    property privileges/isolation are deployed.
11. Exact randomization inference—not ESS-weighted Wilson bounds—passes the frozen
    binary quality/harm gates at the alpha-spent look.
12. Project-first then agent-memory write/read proves compatible `User` creation;
    the Apache-2.0 license/notices and actual vendored-tree provenance ship.
13. Full unit, integration, live-infrastructure, race, privacy, retention, restart,
    and rollback suites pass.
14. An operator explicitly approves both canary admission and later activation.

## 4. Delivery sequence

1. **Land the typed ledger.** Add schema-`2.0` constructors and additive database
   enforcement for unique point-ordinal assignments, distinct experiment-arm/action
   propensities, typed deliveries/outcomes/corrections, and privacy-safe payloads; retain
   schema-`1.0` rows as ineligible history.
2. **Land immutable evidence reconstruction.** Build and verify owner/model-scoped
   snapshots, durable preregistered focal cohort specs and point/ordinal claim,
   deterministic correction folding, exact blocked randomization inference, and the
   ledger-only loader that seals scoped evaluation artifacts. Remove arbitrary
   production `CanaryBatch` and ESS-Wilson promotion authorization.
3. **Land real controllable adapters.** Add reasoning per model round, pre-choice
   tool discovery, actual skill routing, knowledge/document retrieval, and bounded
   owner-scoped long-term memory recall. Reuse `ArchivalRecaller` plus the deployed
   MCP `memory_get_context` with `off`/top-4/top-8; do not add another retriever.
   Before changing Python behavior, extract context/search logic from the 652-line
   `integration.py` facade into a small `integration_context.py`, leaving both files
   below 600 lines; keep `_tools.py`, `__init__.py`, and `memory/long_term.py`
   untouched. Extend the 269-line graph client so fork-mediated writes advance one
   corpus epoch in the same transaction and typed reads accept metadata only when
   their before/after epochs match; migration `0005_memory_corpus_revision` owns the
   singleton constraint. Every other result-affecting writer must advance the same
   epoch; tests prove projector `User` repair/private edges are result-neutral. If
   another over-limit file must be touched, split it behavior-preservingly below 600.
   Move query-dependent fenced recall out of stable `messages[1]` into one synthetic,
   non-persisted dynamic-tail item. Budget and verify the intact final item before its
   delivery commits; if it cannot fit, record `none/context_budget` and omit it.
   Each adapter removes its generic predecessor.
4. **Compose the projector lifecycle.** Start and stop the private outbox projector
   in production with restart, retry/dead-letter, tombstone, and non-recall proof.
   Make `User` creation agent-memory-compatible and prove memory use after projection.
   Ship the vendored license/notices and correct source/image revision provenance.
5. **Prove llama.cpp portability through Aura.** Temporarily override only allow-listed
   `aura.settings` rows for Qwen3.5 2B, drive the real Aura binary and adaptive path,
   then restore the captured rows exactly. Treat the result as portability-only.
6. **Run real-model shadow and seal admission.** Calibrate evaluators and use Aura's
   configured production model—not Qwen 2B. Shadow serves the champion at propensity
   `1.0`; challenger recommendations are diagnostic. Seal the snapshot, focal cohort,
   power/support plan, safety/rollback limits, and operator admission approval.
7. **Add authorized canary control.** In one atomic slice, amend the transition
   contract for the admission artifact, add the capability-gated policy CLI/API,
   audit proof, one-focal-assignment serving adapter, supported randomization, and
   rollback.
8. **Run real canary and activate narrowly.** Reconstruct and seal the closed real
   canary outcome artifact, apply the sequential gate, obtain activation approval,
   then promote one proven domain/action catalog at a time.
9. **Extend surfaces.** Keep producer-consumer skill reuse shadow-only pending its
   interference design. Add memory writes/richer retrieval, provider, context,
   multimodal, and recovery only with separate risk/evaluation contracts.

## 5. Primary references

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
- Neo4j Labs agent-memory:
  <https://github.com/neo4j-labs/agent-memory>
- Randomization inference under interference:
  <https://arxiv.org/abs/1803.02302>
- OpenTelemetry GenAI conventions:
  <https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/>
- NIST AI Risk Management Framework:
  <https://airc.nist.gov/airmf-resources/airmf/5-sec-core/>

## 6. Repository evidence

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

The JSON artifacts and executable tests in those directories are the evidence. The
parent specification does not promote claims beyond their recorded scope.
