# Industrial Conversation Compaction Design

**Date:** 2026-07-13  
**Target:** Phase 42 — LLM conversation compaction  
**Status:** Draft architecture; blocked until adversarial review approves the normative invariants below

## 1. Outcome

Phase 42 becomes Aura's complete production context-management program rather than only a manual compaction command plus overflow fallback. It preserves the existing deterministic ladder while adding proactive semantic compaction before lossy removal, durable recovery, recursive operation, typed memory extraction, multimodal references, operator controls, and measurable quality gates.

The canonical stored transcript remains immutable. Compaction changes the model's working projection, not the historical record.

## 2. Industry Findings

Production implementations converge on the following controls:

- Trigger semantic compaction before the model's hard context limit, using token utilization rather than turn count alone.
- Reserve capacity for system instructions, tools, reasoning, the current request, and maximum output.
- Preserve a recent verbatim tail so local conversational continuity does not depend entirely on a lossy summary.
- Keep system/developer instructions and selected tool interactions outside destructive reduction.
- Clear or offload bulky tool results before paying for semantic summarization.
- Require a meaningful reduction per edit to amortize prompt-cache invalidation and compaction latency.
- Retain a durable source transcript or source range so summaries are auditable and recoverable.
- Support both automatic and explicitly forced compaction through the same orchestration contract.
- Permit repeated compaction without recursively degrading summaries into untraceable prose.
- Expose applied edits, tokens saved, latency, cost, trigger reason, and quality signals.
- Treat conversational working memory separately from durable cross-session memory.
- Evaluate factual retention, instruction retention, tool continuity, safety constraints, and continuation success—not token reduction alone.

Representative primary implementations include OpenAI Responses compaction sessions and server-side thresholds, Anthropic server-side compaction/context editing, LangChain summarization middleware and Deep Agents, Open WebUI's global/per-model thresholds, and Semantic Kernel history reducers.

## 3. Context Ladder

The ladder is ordered and deterministic:

1. **L0 — invariant instructions:** system prompt, governance frame, identity/capability constraints. Never summarized or dropped.
2. **L1 — selective context editing:** mask or offload stale tool results, thinking/reasoning artifacts, and reconstructable payloads. Leave typed placeholders and durable references.
3. **L2.4 — proactive semantic compaction:** summarize the eligible old prefix when projected utilization crosses the configured soft threshold. Preserve the recent tail verbatim.
4. **L2.5 — emergency deterministic reduction:** remove only eligible oldest pairs when semantic compaction is unavailable, fails, or cannot free enough room. This is a last resort and emits a degradation event.
5. **L3 — explicit/lifecycle compaction:** manual command, model-requested safe point, task boundary, idle-time operation, and overflow recovery. Every trigger uses the same coordinator and checkpoint format.
6. **L4 — durable memory projection:** extract typed, provenance-bearing facts, preferences, decisions, commitments, and unresolved work for cross-session retrieval. L4 is not injected wholesale; retrieval remains relevance- and authority-gated.

## 4. Budget and Trigger Model

Aura computes an effective input budget for every model call:

`effective_input = model_context - reserved_output - system_and_tools - safety_margin`

The coordinator estimates the rendered context using the provider tokenizer when available and a conservative fallback otherwise. It records the estimator and error margin used.

L2.4 triggers when either condition is true:

- projected input after the next turn exceeds the per-model soft utilization threshold; or
- the remaining headroom is below the configured reserve for one normal turn plus maximum output.

Defaults are model-aware. A global ceiling applies, while per-model overrides may compact earlier. Thresholds cannot exceed a safe fraction of the provider window.

Compaction is skipped unless it can remove at least the configured minimum tokens or reduction ratio. Hysteresis separates the trigger threshold from the post-compaction target to prevent compaction on every turn.

The coordinator supports `disabled`, `shadow`, `canary`, and `enabled` rollout modes. Shadow mode calculates decisions and evaluation fixtures without changing the active projection.

## 5. Eligibility and Recent Tail

Each attempt selects a closed source range ending at `checkpoint_seq`. It compacts only complete semantic units:

- user/assistant pairs remain paired;
- tool calls remain paired with their results;
- an in-flight tool call, approval, form, or streaming response is never split;
- L0, always-block, pinned turns, protected tools, and active governance constraints are ineligible;
- the most recent configurable token budget and minimum turn count remain verbatim;
- large media and tool payloads become typed references before summarization.

Tail selection is token-based with a turn-count floor. It expands to the nearest safe semantic boundary. If no eligible prefix remains, the attempt is a non-error no-op with a reason code.

## 6. Checkpoint Contract

Every successful compaction writes one immutable checkpoint containing:

- checkpoint ID, conversation ID, generation, parent checkpoint ID;
- source start/end sequence and source content digest;
- summary turn sequence and schema/prompt version;
- trigger kind and reason;
- model/provider and tokenizer/estimator identity;
- effective budget, threshold, target, recent-tail size;
- tokens before/after/saved, latency, usage, and cost;
- integrity/evaluation status and failure diagnostics;
- rollout mode and idempotency key;
- restore/supersession metadata.

The summary turn and checkpoint row are committed atomically. Original turns, attachments, tool payloads, and prior checkpoints remain intact. Reconstruction selects the active checkpoint generation, validates its source digest, then produces:

`[L0, always-block, active summary, preserved recent tail, post-checkpoint turns]`

No turn may appear both inside the summary source range and as verbatim tail unless explicitly listed as tail in the checkpoint metadata.

## 7. Summary Schema and Safety

The generated summary uses a versioned structured envelope rather than unconstrained prose. Required sections cover:

- user intent and success criteria;
- authority, security, safety, privacy, and compliance constraints;
- all unresolved user instructions and commitments;
- decisions and rationale;
- current state and completed work;
- artifacts, files, entities, and durable references;
- tool outcomes and pending/in-flight operations;
- errors, failed approaches, and fixes;
- open questions, blockers, and next actions;
- user preferences and relevant verbatim quotations;
- memory candidates with provenance;
- modality references that must be reloaded rather than guessed.

The compaction call has no tools, produces only the schema, and treats transcript content as untrusted data. Instructions embedded in messages or tool output cannot alter the compaction policy. Security- and governance-relevant clauses are deterministically extracted and compared before accepting the checkpoint.

Output is parsed and validated. Malformed, oversized, empty, policy-dropping, or insufficiently reductive summaries are rejected without persistence.

## 8. Recursive Compaction

Repeated compaction creates a generation chain. The next summary input contains:

- the prior structured summary;
- newly eligible original turns;
- provenance and integrity metadata;
- the preserved recent tail.

It does not summarize an opaque rendered summary without its schema and provenance. Generations are bounded by configuration and periodically rebased from canonical originals when drift or chain depth exceeds policy. Only one checkpoint is active; prior generations remain auditable and restorable.

Idempotency and a per-conversation serialization lock prevent duplicate or overlapping checkpoints. Concurrent turns after the captured source watermark remain outside the checkpoint and are preserved verbatim.

## 9. Tool, Artifact, and Multimodal Handling

L1 stores reconstructable bulky content outside the prompt and substitutes typed placeholders containing content kind, durable ID, digest, preview, and reload instructions. Protected tools may be excluded from clearing.

Compaction never invents descriptions for unavailable media. Image, audio, video, document, code, and binary content are represented by typed artifact references plus verified metadata or an existing trusted caption/transcript. Retrieval can reload the original when relevant.

Pending tool calls and results are atomic units. If a provider returns compaction during a tool cycle, Aura completes or safely cancels the cycle before activating the checkpoint.

## 10. Durable Memory

Compaction and long-term memory share a source event but have different products:

- the checkpoint summary supports continuation of one conversation;
- memory candidates support selective reuse across conversations.

Candidates use typed schemas for fact, preference, decision, commitment, entity, and unresolved task. Each carries source conversation/range, evidence excerpt or digest, confidence, authority, sensitivity, expiry, and supersession state. Policy rejects secrets and transient details by default. Promotion may be automatic only for low-risk high-confidence classes; otherwise it requires an explicit policy or user action.

Retrieval is relevance-, recency-, identity-, capability-, and sensitivity-gated. Memories are never injected merely because compaction created them.

## 11. Recovery and User Experience

All manual surfaces and the web cockpit use the shared coordinator. Operators can inspect checkpoint history, trigger reason, token delta, model, quality status, and source range. Authorized users can preview the generated summary, compare generations, restore a previous generation, or rebuild from canonical originals.

Undo changes the active projection; it never deletes history. A restore is itself an audited event. The UI clearly distinguishes semantic summarization, L1 offloading, and L2.5 lossy degradation.

Automatic compaction never interrupts a partial response. Inline operation is allowed only when required to recover from overflow; proactive compaction prefers turn boundaries or idle execution. Streaming surfaces expose a compacting state if completion is delayed.

## 12. Failure Semantics

- Token-estimation uncertainty uses the conservative bound.
- Summary or validation failure leaves the active checkpoint unchanged.
- Persistence is transactional and idempotent.
- A proactive failure continues with the prior projection if sufficient headroom remains.
- Overflow recovery attempts one bounded semantic compaction, then one reconstruction. It does not loop.
- L2.5 may run only when policy allows degraded continuation; otherwise Aura returns the original context-limit error.
- Digest mismatch or corrupt checkpoint fails closed to reconstruction from canonical originals.
- Provider-native compaction may be used behind the coordinator only if it returns enough metadata to satisfy Aura's audit, recovery, and evaluation contracts; otherwise Aura uses local compaction.

## 13. Observability and Evaluation

Metrics and structured events cover trigger counts, no-op/failure reasons, source and output tokens, reduction ratio, headroom after compaction, latency, cost, cache invalidation/reuse, generation depth, L2.5 fallback, restore rate, and memory promotion/retrieval.

The acceptance suite includes:

- deterministic budget and boundary selection tests;
- pair/tool-call atomicity and recent-tail tests;
- concurrency, idempotency, rollback, and digest validation;
- recursive compaction and rebase tests;
- multimodal/reference round trips;
- prompt-injection and governance-retention adversarial cases;
- factual, decision, instruction, entity, artifact, and pending-task retention datasets;
- continuation tasks comparing uncompacted, compacted, and L2.5 baselines;
- token reduction, latency, cost, and cache-impact budgets;
- restore/undo and UI accessibility tests;
- shadow/canary rollout gates with rollback thresholds.

No deployment is promoted solely because tests pass structurally. Quality thresholds must show that compaction preserves continuation success and safety while meeting reduction targets.

## 14. Delivery Decomposition

Phase 42 should be replanned into dependency-ordered slices:

1. Budget model, eligibility engine, schema, migrations, checkpoint chain, and reconstruction.
2. Structured summarizer, validation, security controls, idempotency, and manual coordinator.
3. L1 typed offloading improvements and proactive L2.4 with shadow mode.
4. Overflow recovery, L2.5 policy integration, recursive compaction, and rebase.
5. Multimodal/artifact references and durable memory projection/retrieval.
6. CLI, REPL, Telegram, AG-UI, web history/preview/diff/restore, and accessibility.
7. Observability, evaluation corpus, load/concurrency tests, canary rollout, documentation, and terminal acceptance.

Each slice must specify migrations, runtime-state transitions, rollback behavior, daemon-free tests, integration tests, mutation targets, and measurable exit criteria. Existing Phase 42 artifacts are rewritten consistently; stale declarations that L2.4, multimodal handling, memory, repeated compaction, or recovery UI are out of scope are removed.

## 15. Non-goals

- Deleting or rewriting the canonical transcript.
- Treating summaries as authoritative over original evidence.
- Provider-specific behavior that bypasses Aura's policy and audit contracts.
- Injecting all long-term memories into every prompt.
- Repeated automatic retries without a strict bound.
- Summarizing active tool transactions or unsupported media into invented prose.

## 16. Design Acceptance

The replanned phase is complete only when every requirement above maps to an implementation task, automated verification, rollout control, and recovery path. “Industry complete” means the system supports proactive reduction, safe continuation, auditability, recovery, recursive operation, typed modalities, durable memory separation, and measured quality as one coherent lifecycle.

## 17. Normative Invariants After Adversarial Review

This section replaces any conflicting or less-specific language in Sections 3–16. Replanning must use these requirements verbatim as architecture constraints.

### 17.1 Exact budget model

All quantities are integer tokens:

- `window_tokens`: provider-declared context window.
- `reserved_output_tokens`: configured maximum output.
- `safety_margin_tokens = max(1024, ceil(window_tokens × 0.02), estimator_error_tokens)`.
- `rendered_fixed_tokens`: system/developer instructions and rendered tool schemas.
- `rendered_history_tokens`: candidate working projection, excluding fixed content and pending input.
- `pending_input_tokens`: current user/event input.
- `forecast_turn_tokens`: configured p95 observed turn size after at least 200 samples; otherwise `min(8192, ceil(window_tokens × 0.05))`.
- `input_capacity = window_tokens - reserved_output_tokens - safety_margin_tokens`.
- `projected_input = rendered_fixed_tokens + rendered_history_tokens + pending_input_tokens + forecast_turn_tokens`.

No component is counted twice. Proactive L2.4 triggers when `projected_input / input_capacity >= trigger_ratio` or remaining headroom is below `forecast_turn_tokens`. Defaults are `trigger_ratio=0.80`, `target_ratio=0.55`, `minimum_saved_tokens=max(4096, ceil(input_capacity × 0.10))`, and `minimum_reduction_ratio=0.20`. Validation requires `0.30 ≤ target_ratio < trigger_ratio ≤ 0.90`. Activation is rejected unless both minimum-savings controls pass and projected post-compaction utilization is at or below target.

Provider tokenization is preferred. A fallback must expose an upper bound `ceil(estimate × 1.15) + 256` and record estimator ID/error. An unknown or invalid model window disables proactive compaction; overflow recovery remains available.

Configuration validation requires `window_tokens > reserved_output_tokens + safety_margin_tokens + minimum_fixed_reserve_tokens`, where `minimum_fixed_reserve_tokens=1024`. If `input_capacity <= 0`, or if `rendered_fixed_tokens + pending_input_tokens >= input_capacity`, proactive compaction is disabled as `insufficient_input_capacity` and no ratio or percentage-derived savings calculation is performed. Overflow recovery may compact eligible history only when fixed plus pending input can fit afterward; otherwise it returns `context_unavailable` without persistence or L2.5 retries.

`estimator_error_tokens` is the provider-declared bound when available; otherwise it is the calibrated p99 absolute undercount over a rolling minimum of 1,000 observed requests, scoped by provider, model, and tokenizer/estimator version. Until that sample exists, the mandated 15%+256 upper bound is used and `estimator_error_tokens=0` because the error allowance is already embedded. Calibration versions and sample windows are recorded.

### 17.2 L2.4 must precede L2.5

`LoadManagedHistory` may not apply L2.5 directly. It first produces a pure `BudgetDecision{Projection, L1Edits, SemanticCandidate, EmergencyCandidate, Reason}`. No L2.5 projection or rot event may be returned or persisted until L2.4 was attempted or explicitly waived as `disabled`, `no_eligible_prefix`, `provider_unsupported`, `quality_rejected`, or `timeout`. This seam and the L2.5 gate ship atomically. Acceptance must prove an over-soft/over-hard eligible history attempts semantic compaction before any hard-drop event.

### 17.3 Disjoint capture manifests and reconstruction

Each attempt records:

- `captured_watermark_seq`;
- ordered `summarized_turn_seqs`;
- ordered `tail_turn_seqs`;
- ordered `protected_turn_seqs`;
- post-watermark turns defined as `seq > captured_watermark_seq`;
- digest per manifest plus a complete-capture digest.

The summarized, tail, and protected sets are pairwise disjoint. Every captured canonical turn belongs to exactly one of those sets or an explicit excluded manifest. Reconstruction is logical, independent of physical append sequence:

`[L0, always-block, active internal summary, tail manifest order, seq > captured watermark]`.

RFC 8785 canonical JSON plus SHA-256 is the initial digest algorithm; its version and included immutable turn fields are stored. Golden fixtures prove each canonical turn appears exactly once across schema/projection migrations.

### 17.4 Semantic-unit state machine

Selection operates on semantic units keyed by parent turn, invocation ID, tool-call ID, approval ID, and response/stream ID. A unit may include user input, assistant preamble, parallel calls, matched results, approvals, ask-user responses, retries, cancellation, and assistant continuation. Open, malformed, duplicated, missing-result, cancelled, retried, or partially persisted units are ineligible unless an explicit legacy normalization rule closes them.

The recent-tail start expands backward only to include its complete containing unit, capped at 20% of input capacity. If atomicity exceeds the cap or eliminates the minimum-saving prefix, compaction is a reasoned no-op. Fixtures cover parallel calls, missing/duplicate results, cancellations, approvals, streamed partials, retries, and malformed legacy rows.

### 17.5 Normative checkpoint schema

Checkpoints include conversation, branch/projection, generation, parent, captured watermark, all manifests/digests, structured summary JSON, schema/prompt/projection versions, trigger/reason, model/provider/estimator, budget/threshold/target/tail values, usage/cost/latency, quality state, rollout mode, idempotency/claim state, and restore/supersession state.

The relational contract requires:

- unique `(conversation_id, branch_id, generation)`;
- unique `(conversation_id, branch_id, idempotency_key)`;
- parent FK with `child.generation = parent.generation + 1`;
- one compare-and-swap active-projection pointer per conversation/branch;
- immutable checkpoint rows and audited restore events referencing old/new active pointers;
- structured summary JSON as canonical storage; rendered text is a versioned projection;
- checkpoint retention no shorter than its canonical conversation unless stricter privacy policy applies.

Before claim creation, the initiating coordinator generates and durably retains one operation ID. The same ID is the idempotency key across transport, process, and database retries. A uniqueness conflict returns the existing claim or completed checkpoint outcome; it never starts duplicate inference.

Reconstruction is a pure versioned function selected only through the active pointer. Restore never chooses “latest by timestamp.”

### 17.6 Distributed claims, concurrency, and idempotency

No process-local lock is a correctness mechanism. Compaction uses a durable three-step protocol:

1. In a short transaction, claim `(conversation, branch, captured_watermark, base_active_generation)` using row locking, a unique idempotency key, lease, and `pending` state.
2. Summarize and validate outside the transaction.
3. In a short serializable transaction, finalize only if the claim remains pending, the active generation equals the base generation, and no new governance-ledger event invalidates the capture. Insert summary/checkpoint, complete the claim, and compare-and-swap the active pointer atomically.

Stale completions become `superseded`. Expired abandoned claims may be reclaimed. Restore and finalization contend on the same pointer row. Manual claims outrank unstarted automatic claims but cannot preempt active inference. Tests use independent connections and separate processes for duplicates, stale completion, lease expiry, restore races, and process death.

### 17.7 Typed content-part prerequisite

Industrial multimodal support requires a prerequisite typed content-part and attachment architecture. Parts carry storage ID, MIME type, digest, byte length, owner/tenant, encryption class, retention class, provider requirements, and text fallback. Attachment-to-turn links are immutable; every reload rechecks authorization. Provider adapters negotiate supported modalities and project either the verified original or an explicit reference-only fallback.

Referenced artifacts live at least as long as every reachable checkpoint/memory and use reachability GC, backup/migration, digest validation, and explicit `missing`/`unauthorized` states. Host-local sidecars are not described as durable. Acceptance inspects actual provider request projections for supported media and reference-only behavior otherwise.

### 17.8 Authority and injection safety

Authoritative L0/developer/governance inputs are never summarized. Aura maintains a typed unresolved-user-instruction ledger with source sequence, authority, state, and quoted-data encoding; revocations are ledger events. Historical claims and tool content remain untrusted data.

The summary is rendered through a dedicated internal-context envelope supported by every adapter. If a provider lacks an internal role, an escaped data block follows a fixed developer-level “non-authoritative historical data” instruction. Verbatim quotations remain encoded fields, never interpolated instructions. Acceptance compares L0 hashes, the unresolved ledger, deterministic safety predicates, and manifests—not circular natural-language extraction. Reject malformed, authority-confusing, policy-dropping, or poisoned summaries. Adversarial fixtures cover role spoofing, delimiter/encoded injection, fake summaries, poisoned tools, revocation, and malicious quoted text.

### 17.9 Recursive drift and hierarchical rebase

`max_generation_depth=4`. Rebase is mandatory at depth four, or when invariant-ledger coverage is below 100%, artifact-reference coverage below 100%, factual entailment below 0.98 on deterministic/curated probes, or similarity to a canonical hierarchical baseline below 0.90. Entailment and similarity use a frozen, versioned probe set and scorer contract; deterministic settings are mandatory where supported, and scorer/model/prompt versions are recorded with every result.

Rebase partitions canonical semantic units into chunks no larger than 60% of summarizer input capacity, summarizes each into the same schema, then reduces them hierarchically while carrying manifests and ledgers. Failure leaves the last-known-good checkpoint active and disables further automatic generations pending retry or operator action.

### 17.10 Bounded recovery

Recovery order is:

1. validate the active checkpoint;
2. select the last-known-good compatible checkpoint;
3. build a bounded projection from canonical originals using the same selector;
4. if none fits, expose `context_unavailable` without changing the pointer.

Recovery never injects the full oversized transcript. Restore is previewed against the current model budget and schema/projection compatibility before transactional activation. Corrupt checkpoints/artifacts are quarantined and visible to operators. Disaster-recovery tests cover incompatible versions, missing artifacts, corrupt digests, oversized originals, and failed restores.

### 17.11 Durable-memory privacy lifecycle

Memory candidates carry tenant/identity owner, purpose/consent basis, source manifest, minimized evidence digest, confidence, authority, sensitivity, region, encryption class, retention/expiry, and supersession/revocation state. Automatic promotion is disabled until a class-specific policy is configured. Creation is idempotent across restore/rebuild.

Retrieval is relevance-, recency-, tenant-, identity-, capability-, purpose-, region-, and sensitivity-gated. Source deletion, consent withdrawal, expiry, and “forget me” propagate to candidates and memories. Tests cover cross-identity denial, deletion, expiry, supersession, secrets, and regional isolation.

### 17.12 Provider capability contract

Every adapter declares context window, maximum output, tokenizer/upper-bound estimator, structured-output support, internal-role mapping, accepted content parts, tool-cycle ordering, usage/cost reporting, native-compaction metadata, and storage/ZDR behavior. Activation requires bounded input estimation, schema-validatable output, safe role/envelope mapping, and usage reporting. Native compaction is accepted only if it yields enough metadata for Aura's manifests, audit, recovery, and evaluation.

The summarizer model is preflighted independently. Oversized sources use the hierarchical algorithm; unsupported adapters reject the attempt without persistence.

### 17.13 Numerical evaluation and rollout gates

The versioned corpus contains at least 500 golden conversations stratified across chat, code, research, tool-heavy, approval, multilingual, multimodal/reference, recursive, and recovery cases, plus at least 200 adversarial conversations. Promotion requires:

- 100% L0 and unresolved-ledger retention;
- zero accepted authority-escalation cases;
- at least 99% tool/pending-state retention;
- at least 98% factual/decision retention;
- continuation success no more than two percentage points below uncompacted baseline at 95% confidence;
- median token reduction at least 40%;
- post-projection at/below target in at least 99% of attempts;
- p95 proactive latency at most 8 seconds and overflow latency at most 15 seconds;
- compaction failure at most 1%;
- compaction cost at most 15% of the following 20-turn median saved-input cost.

The cost gate is an offline and per-canary-stage cohort metric, never a per-attempt activation condition. It uses the price snapshot captured at compaction, only conversations with at least five eligible post-compaction model turns, and at most the first 20 such turns. Conversations ending earlier are right-censored and reported separately, not counted as passing. Restored/superseded checkpoints end their cohort interval; model or price changes start a normalized sub-cohort. Promotion requires the bound on both the aggregate eligible cohort and every provider/model stratum with at least 100 attempts.

Metrics use bounded labels and contain no message/summary text, user IDs, artifact names, or secrets. Canary sampling is deterministic by tenant/conversation at 1%, 5%, 20%, then 50%, with at least 24 hours and 1,000 attempts per stage. Automatic rollback occurs on any safety regression, continuation regression over two points, failure above 2% for 15 minutes, p95 latency breach for 30 minutes, or restore rate above 1%. Shadow mode runs selection and summary generation but no live counterfactual continuation; it stores only redacted structural/quality scores under production consent/retention rules. Enabled mode is prohibited until shadow gates and restore drills pass.

### 17.14 Safe delivery order

1. Provider capabilities, exact budgets, semantic units, redacted telemetry, schema/versioning, distributed claims/CAS, last-known-good recovery, and shadow-only migration.
2. Structured summarizer, authority ledger, validation/adversarial corpus, manual coordinator, preview, and restore; activation stays disabled.
3. Content-part/attachment architecture, artifact durability, L1 editing, and provider-projection tests.
4. One atomic ladder slice: proactive L2.4 seam, L2.5 waiver/fallback, shadow evaluation, and overflow recovery.
5. Recursive generations, hierarchical rebase, corruption recovery, multi-process tests, and canary controls.
6. Durable-memory projection, privacy lifecycle, retrieval/deletion gates, and separate security review.
7. All user surfaces, history/preview/diff/restore, accessibility, full evaluation, staged rollout, documentation, and terminal acceptance.

Every slice is deployable with activation disabled, backwards-readable, and rollback-compatible. Telemetry and recovery precede activation. Content-part and durable-memory work remain separate workstreams inside Phase 42.

### 17.15 Supersession and traceability

The current 11-requirement Phase 42 specification and plans 42-01 through 42-07 are legacy inputs, not executable plans after this design is approved. Replanning begins with a traceability matrix classifying every old requirement, decision, prohibition, migration, and task as `retained`, `superseded`, or `removed`, with destination and rationale. No old plan may execute after replacement SPEC acceptance. Unshipped migrations are replaced; shipped schemas require additive compatibility migrations.
