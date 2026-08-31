# Phase 49: Memory tiers - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-31
**Phase:** 49-memory-tiers
**Areas discussed:** Conversation memory projection, Unified recall, Reasoning graph, Automatic capture and atomic operations

---

## Conversation Memory Projection

### Projection timing

| Option | Description | Selected |
|--------|-------------|----------|
| After every turn, asynchronous and reconcilable | Persist PostgreSQL first; project without blocking the response; retry idempotently and reconcile gaps. | ✓ |
| Synchronous before completing the turn | Immediate projection, but ArcadeDB or embedding failures affect response latency and reliability. | |
| At compaction or conversation end | Simpler, but turns are not searchable until a later boundary. | |

**User's choice:** After every turn, asynchronous and reconcilable.

### Recall eligibility

| Option | Description | Selected |
|--------|-------------|----------|
| Only content no longer in active context | Project immediately but suppress material the model already holds; include prior and compacted history. | ✓ |
| Any indexed turn | Maximum coverage but duplicates active prompt content. | |
| Only completed conversations | Avoids duplication but hides compacted turns from the current conversation. | |

**User's choice:** Only content no longer in active context.

### Projection lifecycle

| Option | Description | Selected |
|--------|-------------|----------|
| Faithful and rebuildable projection | Propagate edits/deletes and let PostgreSQL remain the sole authority. | ✓ |
| Immutable ArcadeDB archive | Preserve deleted/edited versions and create a second authority. | |
| Independent projection expiry | Apply a separate TTL and accept incomplete historic search. | |

**User's choice:** Faithful and rebuildable projection.

### Searchable content

| Option | Description | Selected |
|--------|-------------|----------|
| User messages and final assistant answers | Keep source provenance; exclude reasoning and raw tool traffic. | ✓ |
| Full transcript including tools | Broader recall with substantial noise and sensitive-payload risk. | |
| User messages only | Safer but loses assistant conclusions. | |

**User's choice:** User messages and final assistant answers.

**Notes:** The user required Hermes and LibreChat to be consulted continuously. Hermes' active-lineage suppression and role-default search informed these choices.

---

## Unified Recall

### Cross-tier ranking

| Option | Description | Selected |
|--------|-------------|----------|
| Separate candidates with RRF | Query facts and conversations separately; fuse rank positions without comparing incompatible raw scores. | ✓ |
| Fixed 50/50 quota | Always return both tiers even when one is irrelevant. | |
| Normalize and average raw scores | Tunable but sensitive to model/index distribution changes. | |

**User's choice:** Separate candidates with RRF.

### Conversation result context

| Option | Description | Selected |
|--------|-------------|----------|
| Bounded anchored window | Return the hit with nearby turns and navigation metadata. | ✓ |
| Matching message only | Lower token cost but weak referent resolution. | |
| Entire conversation | Maximum context with high cost and noise. | |

**User's choice:** Bounded anchored window, with the added requirement that conversations remain freely explorable.

### Exploration surface

| Option | Description | Selected |
|--------|-------------|----------|
| Progressive disclosure in `memory_recall` | Search, browse, open, and scroll using one tool, IDs, and cursors. | ✓ |
| Scroll only from search results | No browsing or direct conversation open. | |
| Separate conversation tool | Clearer split but breaks the single recall surface. | |

**User's choice:** Progressive disclosure in `memory_recall`.

### Recall output

| Option | Description | Selected |
|--------|-------------|----------|
| Structured evidence without synthesis | Typed facts/conversation excerpts with provenance and explicit abstention. | ✓ |
| Memory-generated synthesized answer | Shorter but less auditable and adds inference. | |
| Evidence plus synthesis | More convenient with extra cost, latency, and error surface. | |

**User's choice:** Structured evidence without synthesis.

**Notes:** ArcadeDB's server-side `vector.fuse` RRF and Hermes' anchored progressive navigation were the concrete references.

---

## Reasoning Graph

### Eligible reasoning content

| Option | Description | Selected |
|--------|-------------|----------|
| Provider-exposed and authorized reasoning only | Use content Aura may already show/persist; no hidden-CoT reconstruction or new summarizer. | ✓ |
| Any available thinking | Persist non-visible thinking and cross provider-policy boundaries. | |
| New post-task LLM synthesis | Uniform but no longer the original trace and risks fact contamination. | |

**User's choice:** Provider-exposed and authorized reasoning only.

### Graph granularity

| Option | Description | Selected |
|--------|-------------|----------|
| Trace per answer with ordered steps | Bounded step nodes, entity edges, and PostgreSQL source linkage. | ✓ |
| Single text node per answer | Simple but weak step-level traversal. | |
| Node per stream delta | Faithful timing but excessive graph noise. | |

**User's choice:** Trace per answer with ordered steps.

### Retrieval boundary

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit opt-in in `memory_recall` | Default recall excludes reasoning; explicit mode searches/traverses traces. | ✓ |
| Automatic inclusion when relevant | Convenient but violates `MEM-03/CTX-05`. | |
| Separate reasoning tool | Strong split but expands model-facing surface. | |

**User's choice:** Explicit opt-in in `memory_recall`.

### Tool-call payload

| Option | Description | Selected |
|--------|-------------|----------|
| Bounded and redacted payload | Keep status, duration, allowed fields, bounded observation, provenance, and entity audit edges. | ✓ |
| Complete arguments and results | Maximum fidelity with secret, blob, and storage risk. | |
| Tool statistics only | Safe but insufficient to understand a successful strategy. | |

**User's choice:** Bounded and redacted payload.

**Notes:** Before confirming the retrieval boundary, the user requested direct review of Neo4j Agent Memory's documentation and repository. The chosen schema ports its `ReasoningTrace -> ReasoningStep -> ToolCall` and `TOUCHED` patterns to ArcadeDB, but deliberately does not copy Neo4j `get_context()`'s reasoning-on default.

---

## Automatic Capture and Atomic Operations

### Capture eligibility

| Option | Description | Selected |
|--------|-------------|----------|
| Durable attributable evidence only | Explicit user statements and reliable allowed-tool observations with provenance/confidence; no reasoning or hypotheses. | ✓ |
| Explicit “remember” only | Safe but fails natural mid-task capture required by `AUTO-03`. | |
| Any potentially useful information | High recall with high false-memory and contamination risk. | |

**User's choice:** Durable attributable evidence only.

### Durability timing

| Option | Description | Selected |
|--------|-------------|----------|
| Async queue with final barrier | Serialize capture off the hot path and prove durability before task completion. | ✓ |
| Synchronous on every discovery | Minimal loss window but stalls each task step. | |
| Single extraction at task end | Simple but fragile on interruption and requires broad rereading. | |

**User's choice:** Async queue with final barrier.

### Duplicates and contradictions

| Option | Description | Selected |
|--------|-------------|----------|
| Merge evidence with controlled supersede | Enrich duplicate provenance; retain temporal conflicts; only the principal host can supersede. | ✓ |
| Latest wins | A single bad observation can erase valid knowledge. | |
| Reject contradictions | Blocks representation of real changes over time. | |

**User's choice:** Merge evidence with controlled supersede.

### Atomic batch contract

| Option | Description | Selected |
|--------|-------------|----------|
| Final-state all-or-nothing | Validate a working final state and commit once; rollback and report unchanged live state on any error. | ✓ |
| Atomic per operation | A mid-batch failure leaves partial state. | |
| Best effort | Applies valid items and reports failures, contrary to `HARN-05`. | |

**User's choice:** Final-state all-or-nothing.

**Notes:** Hermes' `apply_batch` is the behavioral reference. ArcadeDB's existing explicit transaction client is the implementation seam.

---

## the agent's Discretion

- Projector/outbox integration mechanism, provided it reuses an existing Aura pattern.
- Retrieval tuning, embedding configuration, result/window limits, and cursor representation.
- Deterministic reasoning-step segmentation, entity extraction, and identity resolution.
- Reasoning retention defaults, redaction caps/allowlists, transaction isolation, retry counts, and flush timeout.

## Deferred Ideas

None. The approval-resume todo was reviewed, confirmed already closed, and not folded into Phase 49.
