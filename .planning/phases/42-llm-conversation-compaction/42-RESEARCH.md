# Phase 42: LLM Conversation Compaction - Research

**Researched:** 2026-07-13
**Domain:** production conversation context management, semantic compaction, durable recovery, multimodal projection, and privacy-governed memory
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

The approved architecture is `docs/superpowers/specs/2026-07-13-industrial-conversation-compaction-design.md`, with Section 17 normative and superseding conflicting older material. Canonical transcripts are immutable; compaction changes only a versioned working projection. Phase 42 includes proactive L2.4 before emergency L2.5, durable checkpoints and recovery, recursive rebase, typed multimodal references, privacy-governed memory, all operator surfaces, observability, and numerical rollout gates.

### the agent's Discretion

Implementation naming and file decomposition may follow existing Aura conventions, provided responsibility, invariants, migration compatibility, rollout order, and numerical gates remain exact.

### Deferred Ideas (OUT OF SCOPE)

None from the legacy context remains deferred where Section 17 now makes it part of Phase 42. Provider-native behavior that cannot satisfy Aura manifests, audit, recovery, security, and evaluation contracts remains out of scope.
</user_constraints>

> **Supersession notice:** The legacy `42-CONTEXT.md`, the 11-requirement `42-SPEC.md`, and plans `42-01` through `42-07` are inputs for a retained/superseded/removed traceability pass, not executable plans. [VERIFIED: design Section 17.15]

## Summary

Aura should implement compaction as a projection-control subsystem, not a summarization helper. The subsystem owns exact token budgeting, semantic-unit selection, distributed claims, structured inference, deterministic validation, immutable checkpoints, compare-and-swap activation, reconstruction, recovery, evaluation, and staged rollout. The canonical `conversation_turns` history remains evidence; summaries are derived, non-authoritative internal context. [VERIFIED: design Sections 17.1-17.15]

Current industry implementations corroborate the semantic-first direction: Anthropic documents server-side compaction as the primary long-running-context strategy; LangChain summarizes older history while preserving recent messages; Semantic Kernel supplies summarization and truncation reducers and preserves system messages; OpenAI exposes Responses compaction and token-counting surfaces. These validate the product category but do **not** replace Aura's stronger durability, authority, privacy, and recovery contract. [CITED: https://docs.anthropic.com/en/docs/build-with-claude/context-windows] [CITED: https://docs.langchain.com/oss/python/langchain/middleware/built-in] [CITED: https://learn.microsoft.com/en-us/semantic-kernel/concepts/ai-services/chat-completion/chat-history] [CITED: https://platform.openai.com/docs/api-reference/responses]

**Primary recommendation:** replan Phase 42 in the seven dependency-ordered slices in Section 17.14, keeping activation disabled until telemetry, recovery, authority validation, corpus gates, and restore drills are proven.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|---|---|---|---|
| Provider capability and budget contracts | LLM adapter | conversations | Adapters know windows, tokenizers, roles, modalities, usage, storage/ZDR; coordinator applies policy. |
| Budget decision and semantic-unit selection | conversations domain | runner | Pure deterministic domain logic; runner supplies pending input and executes the decision. |
| L1/L2.4/L2.5 ladder | conversations coordinator | provider adapters | One coordinator enforces semantic-first order; adapters only perform bounded inference/projection. |
| Claims, checkpoints, active pointer, restore | PostgreSQL/store | conversations domain | Cross-process correctness requires durable uniqueness, leases, row locks, serializable finalization, and CAS. |
| Typed content parts and artifacts | assets/storage | provider adapters | Storage owns durable bytes/authz/retention; adapters negotiate request projection. |
| Authority and unresolved-instruction ledger | conversations/security | adapters | Typed authoritative state is deterministic and never entrusted to summary prose. |
| Durable-memory lifecycle | memory domain | identity/privacy | Separate product from checkpoint summary; promotion/retrieval/deletion are policy gated. |
| Manual/operator surfaces | CLI/REPL/Telegram/AG-UI/web | coordinator | Thin surfaces share operation IDs, preview, trigger, history, diff, restore, and status APIs. |
| Evaluation and rollout | eval/telemetry | coordinator/config | Corpus scoring and bounded metrics determine shadow/canary/enabled promotion and rollback. |

## Phase Requirements and Traceability

The replacement SPEC must assign new requirement IDs to every invariant below. Before planning, create a matrix covering every legacy requirement, decision, prohibition, migration, and task with `retained`, `superseded`, or `removed`, destination, and rationale. No legacy plan executes after replacement SPEC acceptance. [VERIFIED: design Section 17.15]

| Normative group | Required planning destination |
|---|---|
| 17.1 | exact budget arithmetic, estimator calibration, invalid-capacity behavior |
| 17.2-17.4 | pure `BudgetDecision`, semantic-first L2.5 gate, manifest partitioning, semantic-unit state machine |
| 17.5-17.6 | checkpoint/claim schema, active pointer, durable operation ID, distributed lease/CAS protocol |
| 17.7-17.8 | typed content parts, artifact authorization/durability, authority ledger, injection-safe internal envelope |
| 17.9-17.10 | generation depth, hierarchical rebase, last-known-good bounded recovery |
| 17.11 | memory consent, ownership, retention, revocation, deletion, regional isolation |
| 17.12 | provider capability contract and summarizer preflight |
| 17.13 | 700+ corpus, numerical promotion/cost/latency gates, deterministic canary and rollback |
| 17.14-17.15 | safe delivery order, backwards readability, rollback, legacy supersession |

## Standard Stack

### Core

| Component | Current project version | Purpose | Prescription |
|---|---:|---|---|
| Go | 1.26.5 | domain coordinator, adapters, APIs, tests | Keep compaction logic in typed Go services and pure functions. [VERIFIED: `go.mod`] |
| PostgreSQL + pgx | pgx/v5 5.10.0 | durable claims, immutable checkpoints, CAS pointer, RLS | Use short transactions and independent connections for race tests. [VERIFIED: `go.mod`] |
| sqlc-generated query layer | repository-native | compile-time query surfaces | Add migrations/queries, regenerate; do not bypass store/RLS conventions. [VERIFIED: source tree] |
| SHA-256 | Go stdlib | manifest and canonical-capture digests | Pair with RFC 8785 canonical JSON and a stored digest/projection version. [VERIFIED: design 17.3] |
| Existing LLM client/adapters | repository-native | summary inference and provider projection | Extend capability declarations; no provider-specific bypass. [VERIFIED: `internal/llm`] |
| Existing tokenizer + conservative fallback | `tiktoken-go` 0.1.8 plus embedded cl100k | provider estimate where valid | Record estimator identity/error; fallback is `ceil(estimate*1.15)+256`. [VERIFIED: `go.mod`, `internal/conversations/tiktoken.go`, design 17.1] |
| Prometheus + OpenTelemetry | 1.23.2 / 1.44.0 | bounded telemetry and traces | No content, IDs, names, secrets, or unbounded labels. [VERIFIED: `go.mod`, design 17.13] |
| React Query/Vitest | repository-pinned | web mutations/history/restore and frontend verification | Reuse existing hooks, same-origin credentials, and accessible cockpit patterns. [VERIFIED: web source] |

No new external package is required for the architecture. RFC 8785 canonicalization should be implemented as a narrow internal encoder over the checkpoint digest schema or added only after an authoritative package review; ordinary `encoding/json` byte output must not be called RFC 8785 canonical JSON. [VERIFIED: design 17.3] The package-legitimacy gate is therefore not triggered by this research.

## Exact Runtime Contract

All budget quantities are integer tokens. Implement the formulas exactly:

```text
safety_margin_tokens = max(1024, ceil(window_tokens * 0.02), estimator_error_tokens)
input_capacity = window_tokens - reserved_output_tokens - safety_margin_tokens
projected_input = rendered_fixed_tokens + rendered_history_tokens + pending_input_tokens + forecast_turn_tokens
forecast_turn_tokens = observed_p95 after >=200 samples; else min(8192, ceil(window_tokens*0.05))
defaults: trigger=0.80, target=0.55
minimum_saved_tokens=max(4096, ceil(input_capacity*0.10)); minimum_reduction_ratio=0.20
validation: 0.30 <= target < trigger <= 0.90
```

Do not compute ratios when capacity is non-positive or fixed plus pending content already consumes capacity. Unknown/invalid model windows disable proactive compaction. Activation requires both savings controls and post-compaction utilization at/below target. [VERIFIED: design 17.1]

`LoadManagedHistory` must become a pure-decision seam returning `BudgetDecision{Projection,L1Edits,SemanticCandidate,EmergencyCandidate,Reason}`. It cannot itself persist or apply L2.5. The executor attempts L2.4 or records one of the five explicit waivers before L2.5 is legal. Ship this seam and gate atomically. [VERIFIED: design 17.2]

## Architecture Patterns

### System Flow

```text
pending input + canonical branch + provider capabilities
  -> render/count fixed/history/input/forecast
  -> pure budget decision
       -> below threshold: unchanged projection
       -> L1 eligible: typed offload edits -> recount
       -> semantic candidate: durable claim -> infer outside tx -> deterministic validation
            -> serializable finalize + immutable checkpoint + CAS active pointer
            -> reconstruct [L0, always-block, summary, tail, post-watermark]
       -> rejected/waived: policy-gated L2.5 emergency projection + degradation event
  -> model call

checkpoint source event -> typed memory candidates -> policy promotion -> gated retrieval
```

### Pattern 1: Immutable Source, Versioned Projection

Keep canonical turns and immutable attachment links. Store structured summary JSON as canonical derived content; rendered text is versioned. Reconstruction selects only the active pointer, never latest timestamp. Restore writes an audit event and CASes the pointer without deleting history. [VERIFIED: design 17.3, 17.5]

### Pattern 2: Disjoint Capture Manifest

Persist ordered summarized, tail, protected, excluded, and post-watermark definitions plus per-manifest and complete-capture digests. Captured turns occur exactly once. Golden fixtures must remain valid through schema/projection migrations. [VERIFIED: design 17.3]

### Pattern 3: Semantic-Unit State Machine

Group by parent turn, invocation/tool-call/approval/response identifiers. Open, malformed, duplicate, missing-result, cancelled, retried, or partial units are ineligible unless a versioned legacy normalization closes them. Tail expansion is backward-only and capped at 20% of input capacity. [VERIFIED: design 17.4]

### Pattern 4: Durable Claim, Inference Outside Transaction, CAS Finalization

Use a coordinator-generated operation ID as the idempotency key across transport/process/database retries. Claim under row lock with lease and base generation; infer outside transactions; finalize serializably only if claim, pointer generation, and governance ledger remain valid. Stale completion becomes superseded. No process-local mutex is correctness. [VERIFIED: design 17.5-17.6]

### Pattern 5: Typed Internal Historical Context

L0/developer/governance content never enters the summary source. Maintain an unresolved-instruction ledger. Render summaries in a dedicated internal role; otherwise use a fixed developer instruction plus escaped non-authoritative data. Never interpolate quoted transcript text as instructions. [VERIFIED: design 17.8]

### Pattern 6: Recursive Rebase from Canonical Units

At generation four or any quality-coverage threshold breach, hierarchically summarize canonical semantic-unit chunks no larger than 60% of summarizer capacity, preserving ledgers/manifests. Failure retains last-known-good and disables automatic generations. [VERIFIED: design 17.9]

### Pattern 7: Separate Working Summary from Durable Memory

A checkpoint may emit candidates, but promotion is disabled until a class-specific policy exists. Candidate/memory ownership, consent purpose, sensitivity, region, expiry, supersession, and deletion propagation are first-class. Retrieval is separately authorized and relevance gated. [VERIFIED: design 17.11]

## Provider Capability Contract

Every adapter must declare: context window; max output; tokenizer or bounded estimator; structured-output support; internal-role mapping; accepted typed content parts; tool-cycle ordering; usage/cost reporting; native-compaction metadata; storage and ZDR behavior. Summarizer preflight is independent of the target model. Activation requires bounded estimation, schema validation, safe authority mapping, and usage. Native compaction is merely an implementation backend and is rejected if Aura cannot reconstruct its manifests/audit/recovery/evaluation evidence. [VERIFIED: design 17.12]

Provider differences are material. Anthropic officially presents server-side compaction for long-running workflows and documents prompt caching/ZDR characteristics separately; OpenAI exposes count/compact operations in Responses; Semantic Kernel's reducers are useful conceptual analogues but its Python reducer contract is marked experimental. Therefore capability declarations must be runtime data, not model-name conditionals scattered through the coordinator. [CITED: https://docs.anthropic.com/en/docs/build-with-claude/context-windows] [CITED: https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching] [CITED: https://platform.openai.com/docs/api-reference/responses] [CITED: https://learn.microsoft.com/en-us/python/api/semantic-kernel/semantic_kernel.contents.history_reducer.chat_history_reducer.chathistoryreducer]

## Typed Content-Part Prerequisite

Do not claim multimodal compaction until turns use typed parts and immutable attachment links. Each part requires storage ID, MIME, digest, byte length, owner/tenant, encryption/retention classes, provider requirements, and text fallback. Reload rechecks authorization. Adapters either project a verified supported original or an explicit reference-only state; summaries never invent unavailable media descriptions. Reachability GC must retain artifacts for every reachable checkpoint/memory and expose `missing`/`unauthorized`. Host-local sidecars are not durable storage. [VERIFIED: design 17.7]

## Durable Memory Lifecycle

Candidates include owner, tenant, purpose/consent basis, source manifest, minimized evidence digest, confidence, authority, sensitivity, region, encryption, retention/expiry, and supersession/revocation. Creation is idempotent across restore/rebuild. Source deletion, consent withdrawal, expiry, and forget-me propagate. Tests must prove cross-identity denial, secrets rejection, deletion, expiry, supersession, and regional isolation. This workstream receives a separate security review and must not block earlier checkpoint-summary slices. [VERIFIED: design 17.11, 17.14]

## Runtime State Inventory

This is a migration/refactor of live context behavior; file grep is insufficient.

| Category | Current/likely state | Required transition |
|---|---|---|
| Stored data | PostgreSQL canonical conversations/turns, branches, rot events, identities/RLS, assets; no shipped `0036` compaction schema is visible. | Replace unshipped legacy migration. Add claims, immutable checkpoints, manifests/digests, active pointers, restore audit, authority ledger, capability/calibration and candidate lifecycle tables additively. Never rewrite canonical turns. |
| Live service config | Runtime env/config registry and per-provider model metadata; web/Telegram/CLI processes may run different releases. | Version capability/config snapshots, default rollout disabled/shadow, preserve backwards reads, reject invalid windows, and expose compatibility. |
| OS-registered state | No compaction-specific scheduled task/service registration found. | If idle compaction later uses scheduler, persist task kind/operation ID and tolerate rolling versions; do not rely on host-local locks. |
| Secrets/env vars | Legacy planned `AURA_COMPACT_*` names may exist outside git even if schema is unshipped. Provider keys and ZDR/storage policy are external. | Inventory deployment env before rename; accept/deprecate aliases during rollout; never emit secrets in metrics, summaries, evidence, or memory. |
| Build artifacts/packages | Generated sqlc code, compiled Aura binaries, web bundles, migration state, multiple process replicas. | Regenerate sqlc, rebuild all surfaces, verify mixed-version reads, and deploy migration before writers. No new package required. |

**Canonical post-edit question:** after repository edits, old binaries, deployment env, database migration state, active pointers/claims, scheduler rows, generated sqlc, and provider capability snapshots may still embody old semantics; rollout and rollback tasks must address each explicitly.

## Security and Privacy Domain

| ASVS category | Applies | Control |
|---|---|---|
| V2 Authentication | yes | Existing identity authentication on every trigger/read/restore/artifact reload. |
| V3 Session Management | yes | Same-origin credentials and server-side session identity; operation IDs are not auth tokens. |
| V4 Access Control | yes | RLS/store identity scoping; owner/tenant checks on conversation, checkpoint, artifact, memory. |
| V5 Validation | yes | Strict config, structured summary, manifests, digests, role envelope, IDs, bounded body/input sizes. |
| V6 Cryptography | yes | Standard SHA-256 and platform encryption; no custom crypto. |
| V8 Data Protection | yes | Minimized evidence, retention/region/encryption classes, ZDR/storage declarations, deletion propagation. |
| V9 Communications | yes | Existing TLS/provider transport; do not send unsupported content classes. |
| V10 Malicious Code/Input | yes | Transcript/tool/artifact text is untrusted data; deterministic authority checks and adversarial corpus. |
| V11 Business Logic | yes | Semantic-first gate, idempotency, claims, CAS, bounded recovery/retry. |
| V13 API | yes | Authz, rate/work bounds, non-retrying mutation clients, stable error/reason contracts. |

Threats: prompt injection/authority escalation; cross-tenant checkpoint/artifact/memory access; duplicate inference/cost; stale completion overwriting restore; digest/projection confusion; poisoned summaries; tool-pair splitting; sensitive telemetry; deletion failures; and recovery expanding an oversized transcript. Required mitigations are Sections 17.3-17.13, with fail-closed persistence and unchanged active pointer on every validation failure. [VERIFIED: normative design]

## Evaluation and Rollout Architecture

The corpus is at least 500 golden plus 200 adversarial conversations, stratified across chat, code, research, tool-heavy, approval, multilingual, multimodal/reference, recursive, and recovery. Promotion requires exactly: 100% L0 and unresolved-ledger retention; zero accepted authority escalations; >=99% tool/pending retention; >=98% factual/decision retention; continuation no worse than two percentage points below uncompacted at 95% confidence; median reduction >=40%; target achieved >=99%; p95 proactive <=8s and overflow <=15s; failure <=1%; cost <=15% of following eligible 20-turn saved-input median. [VERIFIED: design 17.13]

Cost is an offline/per-stage cohort gate, never per-attempt activation. Require >=5 eligible post-compaction calls, right-censor earlier endings, end intervals on restore/supersession, normalize model/price changes, and require aggregate plus every provider/model stratum with >=100 attempts. Shadow stores only redacted structural/quality scores. Canary stages are deterministic 1/5/20/50%, each >=24h and 1,000 attempts; rollback conditions are normative. [VERIFIED: design 17.13]

## Validation Architecture

| Test tier | Required proof | Command family |
|---|---|---|
| Pure unit/property | budget arithmetic, invalid capacity, boundary selection, manifest partition, semantic units, reconstruction, config validation | `go test ./internal/conversations/...` plus `rapid` properties |
| Store integration | migrations, RLS, constraints, leases, CAS, restore/finalize race, deletion lifecycle | existing PostgreSQL integration harness with independent connections |
| Multi-process | duplicate operation, lease expiry, process death, stale completion, restore race | subprocess integration tests; process-local lock deliberately absent |
| Adapter contract | capabilities, estimators, internal role, structured schema, modality projection, usage/storage metadata | adapter table tests and captured provider requests |
| Adversarial/eval | 700+ versioned corpus, frozen scorers, baseline continuation, recursive drift | dedicated deterministic eval command with recorded model/prompt/scorer versions |
| Frontend | preview/history/diff/restore, trigger states, markers, accessibility | `cd web; npm test` and project typecheck/lint |
| Full gate | race/leak, coverage, migration, daemon-free default | project quality scripts plus `go test -race`; live tiers never silently count as green |

Wave 0 must create replacement SPEC traceability, schema golden fixtures, provider capability fixtures, semantic-unit malformed fixtures, corpus formats, frozen scorer contract, and independent-connection concurrency harness before activation logic.

## Common Pitfalls

1. **Executing legacy plans:** they prohibit requirements now normative. Replace SPEC/plans first.
2. **Summarize after L2.5:** irreversibly loses evidence. Semantic attempt/waiver must precede any rot event.
3. **Double-counting budget components:** rendered fixed/history/pending/forecast are disjoint.
4. **Using average tokenizer error:** normative calibration is p99 absolute undercount; cold start uses 15%+256.
5. **Message-pair-only atomicity:** tools, approvals, retries, cancellations, and streams require a semantic-unit machine.
6. **Local mutex correctness:** replicas and process death require durable claims, leases, uniqueness, and CAS.
7. **Holding DB transactions during inference:** causes contention and cannot safely recover; use the three-step protocol.
8. **Latest-timestamp restore:** activation is only through the explicit pointer.
9. **Plain prose summaries:** lose schema, provenance, authority separation, and deterministic validation.
10. **Circular LLM safety checks:** compare L0 hashes, typed ledger, manifests, and deterministic predicates.
11. **Opaque native compaction:** reject unless it yields Aura-complete evidence.
12. **Pretending attachment metadata is multimodal support:** provider request projection and reload authz must be tested.
13. **Promoting memories because compaction emitted them:** promotion defaults off; retrieval is separately gated.
14. **Full-transcript recovery:** may recreate overflow and injection exposure; recovery is bounded and last-known-good first.
15. **Unbounded telemetry labels/content:** leaks data and destabilizes metrics.
16. **Per-attempt cost gating:** cost is cohort-level and right-censored under the normative method.
17. **Structural tests only:** continuation/safety quality and numerical rollout gates decide promotion.

## Don't Hand-Roll

| Problem | Don't build | Use instead |
|---|---|---|
| Cross-process serialization | in-memory mutex | PostgreSQL row locks, uniqueness, leases, serializable finalization, CAS |
| Hashing/encryption | custom primitives | Go SHA-256 and platform encryption/KMS |
| Provider token count | one universal tokenizer | adapter-declared tokenizer or mandatory conservative bound |
| Authorization | client checks | server identity/store/RLS checks on every read/reload/mutation |
| Long-term memory retrieval | inject-all summary facts | existing memory boundary plus typed policy lifecycle |
| Rollout assignment | random request sampling | stable tenant/conversation cohort hashing |
| Summary evaluation | a second unconstrained prompt only | frozen deterministic probes/scorer contract plus continuation baselines |

## Safe Delivery Order

1. Capabilities, exact budgets, semantic units, redacted telemetry, versioning, distributed claims/CAS, last-known-good recovery, shadow-only migration.
2. Structured summarizer, authority ledger, validators/adversarial corpus, manual coordinator, preview/restore; activation disabled.
3. Typed content parts, durable artifacts, L1, provider projection tests.
4. Atomic L2.4 seam + L2.5 waiver/fallback + shadow evaluation + overflow recovery.
5. Recursive generations, hierarchical rebase, corruption recovery, multi-process tests, canary controls.
6. Durable-memory privacy lifecycle, retrieval/deletion gates, separate security review.
7. All surfaces, history/preview/diff/restore, accessibility, full evaluation, staged rollout, docs, terminal acceptance.

Every slice is backwards-readable and rollback-compatible with activation disabled. Migrations precede writers; old readers must tolerate additive records or deployment must enforce a compatible ordering. [VERIFIED: design 17.14]

## Resolved Questions

- **Is proactive L2.4 in scope?** Yes, and it must precede L2.5.
- **May canonical turns be deleted or rewritten?** No.
- **Can provider-native compaction be authoritative?** No; it is usable only behind the complete Aura contract.
- **Is a process-local lock sufficient?** No.
- **Can summaries carry system authority?** No; they are internal non-authoritative historical data.
- **Is multimodal summarization possible before typed parts?** No; typed parts/artifact durability are prerequisites.
- **Is durable memory the checkpoint summary?** No; it is a separate, privacy-governed projection.
- **How deep may recursive summary chains go?** Four generations; then mandatory rebase, sooner on quality breach.
- **May recovery inject the full original transcript?** No; recovery is bounded.
- **When can enabled rollout begin?** Only after shadow gates and restore drills pass.

## Environment Availability

| Dependency | Required by | Available | Evidence/Fallback |
|---|---|---|---|
| Go toolchain | backend/tests | yes | `go.mod` targets 1.26.5; verify executable during planning |
| PostgreSQL | durable protocol/RLS | project dependency | daemon-free unit fakes exist; integration tier requires configured DB |
| Node/npm | web tests | project dependency | existing web project; verify versions in execution environment |
| Provider credentials/models | live preflight/eval | environment-dependent | contract tests use fakes; live tier must fail in CI or explicitly report unavailable, never silent-green |
| Object storage | durable typed artifacts | existing S3/local abstraction | host-local-only storage cannot satisfy industrial durability; configure durable backend before activation |

## Assumptions Log

None. Recommendations are grounded in the normative approved design, repository inspection, or cited official documentation. Deployment-specific availability must be probed during planning/execution and is not asserted here.

## Sources

### Primary (HIGH confidence)

- `docs/superpowers/specs/2026-07-13-industrial-conversation-compaction-design.md`, especially normative Section 17.
- Repository source: `internal/conversations`, `internal/llm`, `internal/db`, `internal/assets`, `cmd/aura`, `web/src/chat`, `go.mod`, and planning artifacts.
- Anthropic context windows: https://docs.anthropic.com/en/docs/build-with-claude/context-windows
- Anthropic prompt caching: https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching
- OpenAI Responses API reference: https://platform.openai.com/docs/api-reference/responses
- LangChain built-in summarization middleware: https://docs.langchain.com/oss/python/langchain/middleware/built-in
- Semantic Kernel chat history reducers: https://learn.microsoft.com/en-us/semantic-kernel/concepts/ai-services/chat-completion/chat-history

### Secondary (MEDIUM confidence)

- Semantic Kernel Python reducer experimental status: https://learn.microsoft.com/en-us/python/api/semantic-kernel/semantic_kernel.contents.history_reducer.chat_history_reducer.chathistoryreducer
- LangChain context engineering overview: https://docs.langchain.com/oss/python/langchain/context-engineering

## Metadata

**Confidence breakdown:** stack HIGH (repository verified); architecture HIGH (normative design); provider landscape HIGH/MEDIUM (official docs, capability details still require adapter-specific preflight); pitfalls HIGH (derived directly from invariants and current seams); deployment environment MEDIUM until probed.

**Research date:** 2026-07-13
**Valid until:** 2026-08-12 for architecture; recheck provider capabilities, pricing, retention/ZDR, and model windows immediately before implementation and every rollout stage.
