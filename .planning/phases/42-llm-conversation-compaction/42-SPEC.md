# Phase 42: Industrial Conversation Compaction — Replacement Specification

**Status:** Approved architecture; replacement planning required
**Supersedes:** legacy COMPACT-01..11 and plans 42-01..07
**Authority:** `docs/superpowers/specs/2026-07-13-industrial-conversation-compaction-design.md` §17
**Traceability:** `42-TRACEABILITY.md`

## Goal

Deliver a provider-portable, production-grade context lifecycle that compacts before destructive loss, preserves canonical evidence and recent continuity, supports recursive operation and recovery, separates working summaries from durable memory, handles typed artifacts, and can be promoted only through measurable safety and quality gates.

## Requirements

### IC-01 — Provider capabilities and exact budget

Every provider declares context/output limits, tokenizer or conservative estimator, schema support, internal-role mapping, modalities, tool-cycle rules, usage/cost, native-compaction metadata, and retention behavior. Implement the exact §17.1 quantities and defaults. Non-positive or fixed-content-exhausted capacity returns `insufficient_input_capacity` without ratio math; unrecoverable overflow returns `context_unavailable` without persistence or L2.5 retry.

**Acceptance:** table-driven provider/model tests cover valid, unknown, invalid, zero/negative, fixed-exhausted, fallback-estimated, and calibrated cases; no quantity is double-counted.

### IC-02 — Semantic units, recent tail, and L1

Build the invocation/tool/approval/stream keyed semantic-unit state machine. Select pairwise-disjoint summarized, tail, protected, and excluded manifests. Keep a token-based recent tail, expanding backward only to a complete unit and at most 20% of capacity. L1 may clear/offload only reconstructable content and must leave typed authorized references.

**Acceptance:** fixtures cover parallel calls/results, approvals, ask-user, retries, cancellation, missing/duplicate results, streamed partials, malformed legacy rows, tail expansion, and no-op reasons.

### IC-03 — Proactive L2.4 before L2.5

`LoadManagedHistory` returns a pure `BudgetDecision` before mutation. At projected utilization ≥0.80 or inadequate forecast headroom, attempt semantic compaction toward ≤0.55. Require ≥max(4096,10% capacity) tokens saved and ≥20% reduction. L2.5 is forbidden until L2.4 succeeds or records an allowed waiver reason.

**Acceptance:** over-soft/over-hard eligible histories call semantic compaction before any rot event; hysteresis prevents repeat compaction at target; disabled/unsupported/rejected/timeout paths record exact waiver codes.

### IC-04 — Durable distributed claims

Use a short durable claim transaction, inference outside locks, then a short serializable compare-and-swap finalize. Stable operation IDs survive transport/process/DB retries. Claims carry leases and state. Stale results never activate. Restore and finalize contend on the same active pointer.

**Acceptance:** independent-process DB tests cover duplicates, uniqueness conflicts, stale completion, new governance events, lease expiry/reclaim, process death, restore races, and manual/auto priority.

### IC-05 — Versioned manifest checkpoints

Store branch-aware generations, parent links, manifests/digests, structured summary JSON, budget/provider/quality metadata, and one active pointer. Use RFC 8785 + SHA-256 v1. Reconstruction is pure and logical: `[L0, always-block, internal summary, tail order, post-watermark]` with every captured turn classified exactly once.

**Acceptance:** schema constraints, atomic activation, digest mismatch, branch isolation, migration golden fixtures, and byte-stable repeated reconstruction pass.

### IC-06 — Safe structured summarization

Keep L0/governance outside compaction. Maintain a typed unresolved-user-instruction ledger with authority and revocation. Generate versioned structured summaries with no tools. Render through a dedicated internal-context envelope or escaped non-authoritative data block. Reject malformed, authority-confusing, poisoned, policy-dropping, oversized, empty, or insufficiently reductive output.

**Acceptance:** schema tests plus adversarial role spoofing, delimiter/encoded injection, fake summaries, poisoned tool output, revocation, malicious quotation, L0 hash, ledger, safety predicate, and manifest checks.

### IC-07 — Unified coordinator and triggers

One coordinator owns manual, proactive, task-boundary/idle, model-requested-safe-point, and overflow triggers. Proactive work prefers boundaries/idle. Overflow performs one bounded compaction and one reconstruction, never a loop. Failures leave the prior projection active.

**Acceptance:** all trigger surfaces produce identical checkpoint semantics; overflow timeout/failure returns original/recoverable state with no partial write; streaming never activates mid-response.

### IC-08 — Typed content parts and artifacts

Introduce typed content parts with storage ID, MIME, digest, size, tenant/owner, encryption, retention, provider requirements, and fallback. Links to turns are immutable and reload authorization is mandatory. Referenced artifacts have reachability retention/GC, backup/migration, digest validation, and explicit missing/unauthorized states.

**Acceptance:** provider-request tests prove real supported-media projection and explicit reference-only fallback; cross-identity, missing, corrupt, migrated, and GC-reachability cases pass.

### IC-09 — Recursive compaction and rebase

Support at most four incremental generations. Rebase when depth/coverage/entailment/similarity gates fail. Hierarchically summarize canonical semantic-unit chunks ≤60% of summarizer capacity, preserving manifests and ledgers. Rebase failure keeps last-known-good active and disables automatic generations.

**Acceptance:** four-generation chain, fifth-generation rebase, oversized canonical source, scorer versioning, drift threshold, failed rebase, and no duplicate-tail facts pass.

### IC-10 — Durable memory lifecycle

Compaction emits typed memory candidates separately from the continuation summary. Candidates include tenancy, purpose/consent, provenance, minimized evidence, confidence, authority, sensitivity, region, encryption, retention, and revocation. Automatic promotion defaults off. Retrieval is tenant/identity/capability/purpose/region/sensitivity gated; deletion and consent withdrawal propagate.

**Acceptance:** idempotent candidate creation, cross-identity denial, secret rejection, expiry, supersession, deletion/forget-me, consent withdrawal, region isolation, and restore/rebuild dedupe pass.

### IC-11 — Recovery and restore

Recovery order is active → last-known-good compatible → bounded canonical rebuild → `context_unavailable`. Never inject the full oversized transcript. Restore requires budget/version preview and transactional pointer change. Corrupt checkpoints/artifacts are quarantined and visible.

**Acceptance:** incompatible schema, corrupt digest, missing artifact, oversized originals, failed restore, last-known-good, quarantine, and disaster-recovery drills pass.

### IC-12 — Operations surfaces

Expose the shared coordinator through CLI, REPL, Telegram, AG-UI, and web. Provide checkpoint history, trigger/reason, token delta, model, quality, source manifests, preview, diff, restore, compacting state, and degradation distinctions. Owner/capability gates and accessibility apply everywhere.

**Acceptance:** behavioral tests prove command interception, authorization/IDOR protection, bounded bodies, sanitization, localization, keyboard/screen-reader operation, marker distinctions, preview/diff/restore, and consistent outcomes.

### IC-13 — Observability, evaluation, rollout

Ship bounded-cardinality redacted metrics and access-controlled traces before activation. Use the §17.13 corpus and thresholds. Roll out disabled → shadow → deterministic tenant/conversation canaries at 1/5/20/50%, with ≥24h and ≥1000 attempts each. Enforce automatic rollback thresholds. Shadow never runs live counterfactual continuations.

**Acceptance:** 500+ golden and 200+ adversarial versioned cases meet every safety, retention, continuation, reduction, latency, failure, and cost threshold; rollback drills prove each trigger.

### IC-14 — Compatibility and terminal acceptance

All slices deploy with activation disabled, are backwards-readable, and are rollback-compatible. Retire legacy plans, update PRD/ROADMAP/REQUIREMENTS/config/docs, provide additive migrations for anything shipped, and prove full CI, race, leak, mutation, integration, E2E, privacy, security, and rollout gates.

**Acceptance:** traceability has no unmapped legacy item; old plans are absent/archived; structural plan checks pass; terminal evidence records exact commands, versions, results, and manual-only checks.

## Delivery order

1. Capabilities, budgets, semantic units, telemetry, schema/claims/recovery in shadow-only mode.
2. Structured summarizer, authority ledger, validation corpus, preview/restore; activation disabled.
3. Typed content parts, artifact durability, L1, provider projection.
4. Atomic L2.4 decision seam + L2.5 gate + overflow recovery.
5. Recursive generation, hierarchical rebase, multi-process and corruption recovery, canary controls.
6. Durable memory/privacy lifecycle and separate security review.
7. All surfaces, complete evaluation, staged rollout, docs, terminal acceptance.

## Global prohibitions

- Do not execute legacy `42-01`..`42-07` plans.
- Do not delete or rewrite canonical turns.
- Do not use process-local locks for correctness.
- Do not hold a DB transaction during inference.
- Do not store the summary as an ordinary user turn.
- Do not let L2.5 run before L2.4 attempt/waiver.
- Do not split a semantic unit or active stream/tool cycle.
- Do not treat host-local sidecars as durable.
- Do not auto-promote memory without class policy.
- Do not enable production activation before recovery, evaluation, and rollback gates pass.
