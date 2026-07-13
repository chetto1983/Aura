# Industrial Conversation Compaction Design

**Date:** 2026-07-13  
**Target:** Phase 42 — LLM conversation compaction  
**Status:** Approved architecture; design contract for replanning

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
