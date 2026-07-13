# Phase 42: Industrial LLM Conversation Compaction - Pattern Map

**Mapped:** 2026-07-13
**Normative source:** `docs/superpowers/specs/2026-07-13-industrial-conversation-compaction-design.md`, Section 17
**Status of old plans:** `42-01` through `42-07` are legacy inputs only (Section 17.15); their simple watermark-row design is not an executable implementation pattern.

## Load-bearing mapping rules

1. The canonical transcript remains immutable. Compaction changes only a versioned working projection.
2. `LoadManagedHistory` must return a pure `BudgetDecision`; it must not perform L2.5. L2.4 attempt/waiver and the L2.5 gate ship together.
3. Correctness is database-backed: durable operation ID, claim/lease, immutable checkpoint, governance invalidation check, and compare-and-swap active pointer. A process-local mutex is not an analog.
4. Reconstruction uses the active pointer and disjoint manifests, never “latest timestamp” and never physical summary-turn append order.
5. Typed content parts are a prerequisite to industrial multimodal compaction. Host-local sidecars are not durable artifacts.
6. The old `ToolCallID` marker idea is superseded as the checkpoint authority mechanism. A summary is canonical structured JSON rendered through a versioned internal-context envelope.

## File Classification

These are the exact recommended implementation files/surfaces implied by Section 17. Existing files are marked **EDIT**; proposed files are **NEW**. A planner may split files to respect the repository's 600-line ceiling, but should preserve these package boundaries.

| New/Modified file | Role | Data flow | Closest current analog | Quality |
|---|---|---|---|---|
| **NEW** `internal/llm/capabilities.go` + adapter declarations | provider/config | request-response | `internal/llm/model_reasoning_caps.go`, `internal/llm/config.go` | role-match |
| **NEW** `internal/conversations/compaction_budget.go` | utility/model | pure transform | `internal/conversations/context.go`, `internal/agent/budget.go` | partial |
| **NEW** `internal/conversations/semantic_units.go` | selector/state machine | pure transform | `internal/toolinvocations/store.go`, `internal/askuser/store.go` | partial |
| **NEW** `internal/conversations/compaction_manifest.go` | model/utility | canonical transform | `internal/skills/manifest.go`, `internal/canonicaljson/*` | role-match |
| **NEW** `internal/conversations/compaction_schema.go` | model/validator | JSON transform | `internal/skills/manifest.go`, `internal/agent/tools/spec.go` | role-match |
| **NEW** `internal/conversations/compaction_claims.go` | store/service | transactional CRUD + lease | `internal/askuser/store.go`, `internal/cron/store_runs.go` | partial |
| **NEW** `internal/conversations/store_compaction.go` | store/model | transactional CRUD + CAS | `internal/conversations/store_append.go`, `internal/agui/saga_journal.go` | partial |
| **NEW/REPLACE UNSHIPPED** checkpoint/claim/pointer/restore SQL migrations + sqlc queries | migration/query | DDL + CRUD | conversation migrations, `context_rot_events.sql` | role-match |
| **NEW** `internal/conversations/compaction_reconstruct.go` | projection/recovery | pure transform + bounded read | `internal/conversations/context.go`, `internal/agui/recovery_hash.go` | partial |
| **NEW** `internal/conversations/compaction_authority.go` | ledger/validator | append-only events + transform | governance audit stores, `internal/toolinvocations/store.go` | partial |
| **NEW** `internal/conversations/compaction_summarize.go` | LLM service | request-response/stream drain | `internal/conversations/title.go` | role-match |
| **NEW** `internal/conversations/compaction_rebase.go` | batch service | hierarchical transform | no close analog | none |
| **EDIT** `internal/conversations/context.go` | ladder coordinator seam | pure decision + gated transform | self | exact seam, behavior replaced |
| **NEW** `internal/runner/runner_compact.go`; **EDIT** runner interfaces/wiring | coordinator | request-response + bounded recovery | runner lifecycle helpers | role-match |
| **NEW/EDIT** `internal/assets/*` content-part and attachment-link surfaces | model/store/provider projection | CRUD + file/object I/O | `internal/assets/types.go`, `store.go`, `context.go` | role-match |
| **NEW** `internal/memory/compaction_candidates.go` + privacy lifecycle store | model/store/policy | CRUD + event propagation | `internal/profile/store_fact.go`, identity-scoped stores | partial |
| **NEW** `internal/conversations/compaction_metrics.go` | telemetry | event-driven aggregation | `internal/agent/metrics.go`, `internal/agui/metrics.go` | exact role |
| **NEW** `internal/conversations/compaction_eval/*`, `testdata/compaction/*` | evaluation | batch/corpus | no 500+200 rollout corpus analog | none |
| **EDIT** root config/knob registry/composition root | config | layered load/validation | `internal/llm/config.go`, `internal/config/config_knobs.go` | exact |
| **EDIT** `internal/agui/conversations_api.go`, server/store interfaces | controller | owner-scoped request-response | existing conversation read/write handlers | exact |
| **EDIT/NEW** CLI, REPL and Telegram command files | controller | command dispatch | existing chat commands and Telegram `/clear` | exact |
| **EDIT** `web/src/conversations/useConversations.ts`; **NEW** checkpoint hooks/types | hook | request-response | existing conversation and rot-event hooks | exact |
| **NEW** `web/src/conversations/CompactionHistory.tsx`, preview/diff/restore dialogs | component | request-response | `ConversationSidebar.tsx`, confirm dialogs, drawer displays | role-match |
| **EDIT** composer/gauge/footer surfaces | component/utility | event + projection | existing quick-command and rot-event marker flow | exact |

## Pattern Assignments

### Provider capabilities and exact budgeting

**Targets:** `internal/llm/capabilities.go`, each provider adapter, `internal/conversations/compaction_budget.go`, config validation and tests.

**Analogs:**

- `internal/llm/config.go:14-40,84-132,156-223,307-361` is the canonical layered-default/config-validation shape. It already owns context-window and output-reservation values, but those two integers are not a Section 17.12 capability contract.
- `internal/llm/model_reasoning_caps.go:15-33` demonstrates a typed provider-advertised capability that clamps untrusted provider metadata to Aura's vocabulary.
- `internal/conversations/context.go:64-101` is the current budget seam, but its fixed `max(output,20000)+13000` formula is superseded.

Copy the typed-capability and fail-fast config style, then add a single provider contract containing context window, maximum output, tokenizer/upper-bound estimator identity and error, structured output, internal-role mapping, accepted content parts, tool-cycle ordering, usage/cost, native-compaction metadata, and storage/ZDR behavior. Adapter activation must validate that contract; do not infer support from provider name.

`compaction_budget.go` should be a pure integer-token transform returning the exact Section 17.1 fields and reason codes. Tests must pin zero/negative capacity before any division, fallback `ceil(estimate*1.15)+256`, 200/1,000-sample thresholds, both minimum-savings gates, and target/trigger validation.

**No exact analog:** Aura has no provider tokenizer registry, calibrated p99 undercount store, per-model capability preflight, or forecast p95 sampler. These are new contracts, not extensions of `ContextConfig.HardCap()`.

### Pure ladder decision and semantic-unit selection

**Targets:** `internal/conversations/context.go`, `compaction_budget.go`, `semantic_units.go`.

**Analog:** `context.go` already separates token counting, L1 eviction and oldest-pair reduction into helpers and table-driven tests. Preserve that pure-helper style, but replace direct L2.5 application with:

```go
type BudgetDecision struct {
    Projection         []Turn
    L1Edits            []ContextEdit
    SemanticCandidate  *SemanticCandidate
    EmergencyCandidate *EmergencyCandidate
    Reason             DecisionReason
}
```

`semantic_units.go` should normalize turns into units keyed by parent turn, invocation, tool call, approval, and response/stream IDs before selecting manifests. `internal/toolinvocations/store.go` and `internal/askuser/store.go:272-320` are useful lifecycle/atomic-claim references, but neither reconstructs a whole assistant tool cycle.

**No exact analog:** parallel calls, matched results, approvals, ask-user replies, retries, cancellation, streamed partials, duplicate/missing results, and legacy normalization have no unified state machine today. Do not copy the old user/assistant pair selector and rename it “semantic.”

### Versioned manifests, schema, hashing and reconstruction

**Targets:** `compaction_manifest.go`, `compaction_schema.go`, `compaction_reconstruct.go`.

**Analogs:**

- `internal/canonicaljson/*` is the repository-owned canonical serialization seam used before SHA-256 hashing.
- `internal/skills/manifest.go` demonstrates versioned typed manifest parsing/validation and explicit rejection of malformed data.
- `internal/agui/recovery_hash.go` demonstrates digest verification on recovery material.

Copy the explicit version fields, deterministic serialization, parse/validate boundary, and fail-closed digest comparison. Store ordered `summarized`, `tail`, `protected`, and `excluded` manifests independently, each digest plus complete-capture digest, algorithm/version, and included immutable field set. Enforce pairwise disjointness and exactly-one classification as a pure invariant.

Reconstruction must be a version-selected pure function:

```text
[L0, always-block, active internal summary, tail manifest order, seq > captured watermark]
```

It reads only through the active pointer, validates schema/projection compatibility and all reachable artifacts, and never selects by timestamp. The old `truncateAtCheckpoint(seq)` pattern is not sufficient because it cannot prove disjoint capture or exactly-once reconstruction.

**No exact analog:** RFC 8785 is stricter than ordinary `encoding/json`; verify the existing canonicalizer against RFC 8785 golden fixtures or replace/extend it explicitly. Aura has no version-dispatch reconstruction registry today.

### Durable claims, immutable checkpoints, CAS pointer and restores

**Targets:** `compaction_claims.go`, `store_compaction.go`, migrations, sqlc queries, multi-process integration tests.

**Analogs:**

- `internal/conversations/store_append.go` is the primary `db.WithTx` and tx-composable write pattern.
- `internal/askuser/store.go:272-320` maps zero affected rows to an already-claimed outcome and orders multi-row claims to avoid deadlocks.
- `internal/agui/saga_journal.go` is an auditable durable state-transition/journal analog.
- scheduler/cron stores provide lease/expiry recovery shapes.

Use short transactions and sqlc query methods. The claim transaction locks the pointer/claim scope and stores operation ID, idempotency key, base active generation, watermark, lease and `pending`. Inference occurs outside a transaction. Finalization is serializable and atomically checks pending claim, lease/ownership, unchanged base pointer, and governance watermark; inserts immutable summary/checkpoint; completes claim; and CAS-updates the active pointer. Restore contends on the same pointer and appends an audit event referencing old/new pointers.

Required SQL invariants belong in the database where expressible: both unique keys, parent FK, branch/projection identity, state checks, immutability trigger/permissions, restore event FKs, and one pointer row per conversation/branch. Parent generation adjacency and governance invalidation also need transactional assertions.

**No exact analog:** there is no existing three-step distributed inference claim protocol, active-generation CAS, immutable checkpoint chain, restore race contract, or cross-process duplicate test. Process-local runner locks are explicitly non-analogous.

### Structured summarizer, authority and injection safety

**Targets:** `compaction_summarize.go`, `compaction_authority.go`, provider envelope renderers, adversarial fixtures.

**Analog:** `internal/conversations/title.go` provides the small LLM-over-history pattern: explicit model, system/user messages, bounded request, complete stream drain, sanitization, and empty-result rejection. Copy only that lifecycle.

The compaction request must disable tools and require schema-valid structured output. Parse JSON before persistence. Render accepted summaries with a dedicated internal-context envelope; adapters without an internal role use a fixed developer instruction plus escaped data. Never interpolate quoted transcript text into instructions.

Governance/audit stores are append-only/event-oriented analogs, but the unresolved-user-instruction ledger is new. It needs typed authority, source seq, quoted-data encoding, state and revocation events. Acceptance compares L0 hashes, ledger coverage, deterministic safety predicates and manifests—not an LLM's summary of those things.

**No exact analog:** Aura has no unresolved-instruction authority ledger, governance capture watermark invalidation, internal-summary role contract across every adapter, or deterministic authority-confusion validator.

### Recursive generations, rebase and bounded recovery

**Targets:** `compaction_rebase.go`, `compaction_reconstruct.go`, coordinator recovery paths.

**Partial analogs:** existing recovery helpers validate hashes and bounded inputs; ordinary runner retry paths demonstrate one-shot retry semantics. Reuse fail-closed errors, injectable clocks and bounded contexts.

Implement Section 17.9 literally: depth four forces rebase; deterministic/curated probes carry frozen scorer/model/prompt versions; canonical semantic units are chunked to at most 60% summarizer capacity and reduced hierarchically while carrying ledgers/manifests. Failure leaves last-known-good active and disables automatic generations.

Recovery order is active validation, last-known-good compatible checkpoint, bounded canonical projection through the same selector, then `context_unavailable`. Never inject the full oversized transcript. Restore preview runs compatibility and budget checks before pointer CAS. Corrupt rows/artifacts are quarantined, not silently skipped.

**No exact analog:** hierarchical summary rebase, scorer thresholds, last-known-good checkpoint selection, checkpoint quarantine and restore-preview budget validation are greenfield.

### Typed content parts, attachments and artifacts

**Targets:** additions under `internal/assets`, turn/content models and migrations, adapter projections and authorization tests.

**Analogs:**

- `internal/assets/types.go`, `store.go` and `context.go` already model durable asset records, source kinds, ownership-aware lookup and provider-context preparation.
- web attachment upload hooks/types are the client-side upload analog.
- artifact display types demonstrate explicit missing/display states.

Extend the asset domain rather than creating compaction-only blobs. Typed parts must carry storage ID, MIME, digest, bytes, tenant/owner, encryption, retention, provider requirements and text fallback. Attachment-to-turn links are immutable. Every reload repeats owner/tenant authorization. Provider adapters select verified originals only when declared supported; otherwise emit an explicit reference-only projection.

Add reachability edges from checkpoints and memories and a GC that respects every reachable edge, retention, backups/migrations and digest validation. Surface `missing` and `unauthorized` distinctly.

**No exact analog:** current asset handling is not a general immutable content-part graph with checkpoint/memory reachability GC. Host-local artifact paths are not acceptable durability analogs.

### Durable memory and privacy lifecycle

**Targets:** `internal/memory/compaction_candidates.go` (or the established memory package after code-owner review), schema/migrations, deletion and retrieval gates.

**Analog:** `internal/profile/store_fact.go` is the closest typed fact persistence/projection; identity-scoped stores show owner checks and source-linked records. Reuse UUID parsing, typed domain projections, transactional writes and explicit store errors.

Memory candidates remain separate from checkpoint summaries. Persist tenant/identity owner, purpose/consent, source manifest, minimized evidence digest, confidence, authority, sensitivity, region, encryption, expiry/retention and supersession/revocation state. Creation is idempotent across rebuild/restore. Automatic promotion defaults off until class policy exists.

Deletion, consent withdrawal, expiry and “forget me” must propagate to both candidates and promoted memories. Retrieval gates all dimensions in Section 17.11 before injection.

**No exact analog:** the current profile/fact store does not implement purpose limitation, regional isolation, consent propagation, class-specific promotion or source-manifest privacy deletion. Treat this as a separate security-reviewed workstream.

### Telemetry, evaluation and rollout

**Targets:** `compaction_metrics.go`, structured event sink, `compaction_eval/*`, `testdata/compaction/*`, rollout config.

**Analog:** `internal/agent/metrics.go:35-54,54-141,296-335` uses injectable Prometheus registries, explicit counters/histograms and bounded label vocabularies. Follow its duplicate-registration-safe constructors and isolated registry tests. `internal/agui/metrics.go` exposes the HTTP metrics seam.

Use bounded labels only (trigger/reason/provider/model-class/rollout state/quality state); never message text, summary text, IDs, artifact names or secrets. Capture estimator/calibration, token deltas, target result, latency, cost snapshot, generation, L2.5 waiver, restore and corruption signals.

The corpus and rollout harness are new: at least 500 stratified golden plus 200 adversarial conversations, frozen scorer contracts, confidence comparison, cohort/right-censor accounting, provider/model strata, deterministic tenant/conversation sampling at 1/5/20/50%, minimum duration/attempts, and automatic rollback predicates exactly as Section 17.13 states.

**No exact analog:** Aura has no counterfactual continuation corpus, statistically gated canary cohort evaluator, right-censored saved-cost calculation, or automated compaction rollout controller. Structural unit tests are not a substitute.

### APIs, UI and commands

**Targets:** AG-UI conversation routes/interfaces, CLI/REPL/Telegram, React hooks, history/preview/diff/restore UI and gauge.

**Backend analog:** `internal/agui/conversations_api.go` supplies the required route pattern: parse ID, `GetForIdentity` owner gate before read/mutation, body cap, sanitized errors, and JSON helpers. Extend narrow runner/store interfaces rather than reaching into sqlc from handlers. Restore and trigger operations accept the durable operation ID/idempotency key and return claim/outcome state; preview is non-mutating.

**Hook analog:** `web/src/conversations/useConversations.ts` establishes typed query keys, `encodeURIComponent`, same-origin credentials, `retry:false`, mutations and targeted invalidation. Unlike the old pattern map, new checkpoint wire structs should have explicit JSON tags/DTOs and stable snake_case API contracts; do not leak evolving Go domain structs as accidental wire schemas.

**UI analog:** `ConversationSidebar.tsx` provides loading/error/empty states, accessible buttons/menus, focus/escape handling and confirmation flow. Existing drawer/display components are the preview/diff analog. Build history with trigger, source range, generation, quality, model, token delta and status; preview before restore; show incompatible/corrupt/missing states; distinguish L1 offload, semantic L2.4 and degraded L2.5. Use existing Button, Dialog/AlertDialog, Badge, Alert, Empty, Skeleton, Tabs and accessible titles rather than custom substitutes.

**Command analogs:** CLI chat subcommands and Telegram `dispatchRich`/`/clear` show interception before an LLM turn and consumer-side backend interfaces. All commands invoke the same runner coordinator with the same durable operation ID. They do not write checkpoint rows directly.

**No exact analog:** checkpoint generation diff, restore preview/compatibility, corruption quarantine UI, compacting streaming state and full accessibility tests are new surfaces.

## Cross-Cutting Shared Patterns

### Transaction and idempotency boundary

Use `db.WithTx`/sqlc and compose package-local transactional helpers. Persist the initiating operation ID before inference and reuse it across HTTP/CLI/process/DB retries. Unique conflict returns the existing claim/outcome. Never keep a DB transaction open during model inference.

### Ownership and privacy

Every AG-UI route and artifact reload owner-gates before unscoped access. Memory additionally gates tenant, identity, capability, purpose, region and sensitivity. Error responses go through existing sanitizers; metrics contain no high-cardinality or sensitive values.

### Immutable canonical data, mutable pointer

Canonical turns, attachment links and checkpoints are immutable. Activation/restoration changes one CAS pointer and appends an audit event. Deletion/privacy lifecycle is explicit policy work, not an UPDATE shortcut.

### Version everything that affects interpretation

Manifest/digest algorithm, included turn fields, summary schema, prompt, projection renderer, tokenizer/estimator/calibration, scorer/model/probe and provider capability versions must be stored. Readers dispatch by stored version and fail closed when incompatible.

### Deterministic, injectable tests

Pure selectors/budgets/manifests use table, property, fuzz and golden tests. Lease/expiry uses injectable clocks. Claims/CAS/restores require independent DB connections and separate-process tests. Provider projections assert the actual outbound request. Recovery tests never skip as green when the database is unavailable.

### Rollout remains disabled through early slices

All migrations are backwards-readable and deployable with activation disabled. Telemetry and recovery precede activation; manual preview/restore precedes proactive activation; content parts precede multimodal claims; memory is independently security-reviewed.

## Explicit No-Analog Areas

| Area | Why no close Aura analog exists |
|---|---|
| Exact provider capability contract and estimator calibration | Current config has window/output integers and reasoning capability discovery only. |
| Unified semantic-unit state machine | Current history reduction is pair-based; tool, approval, retry and stream lifecycle are stored in separate domains. |
| Distributed compaction claim + lease + inference-outside-tx + CAS finalize | Existing atomic writes/claims cover local DB transitions, not long external inference with stale completion. |
| Active versioned checkpoint pointer and immutable restore chain | No conversation projection pointer or checkpoint restore ledger exists. |
| Disjoint ordered manifests with exactly-once reconstruction | Current sequence truncation cannot express protected/tail/excluded membership. |
| Unresolved-user-instruction authority ledger | Governance/audit logs do not model instruction authority and revocation semantics. |
| Hierarchical rebase and drift scorers | No canonical hierarchical summary baseline or frozen entailment/similarity probes exist. |
| Content-part reachability GC across checkpoints and memories | Assets are not yet a general immutable typed-part graph. |
| Durable-memory consent/region/deletion propagation | Profile facts do not satisfy the Section 17.11 lifecycle. |
| 500+200 evaluation corpus and statistical rollout controller | Existing tests/metrics do not implement continuation confidence, cohort cost or staged rollback gates. |
| Checkpoint preview/diff/restore/quarantine cockpit | Existing conversation UI has list/edit/delete flows, not projection recovery operations. |

## Metadata

**Primary analog scope:** `internal/{llm,conversations,canonicaljson,skills,askuser,cron,assets,profile,agent,agui}`, `internal/db`, `internal/runner`, `internal/channels/telegram`, `cmd/aura`, `web/src/{conversations,chat,components}`.
**Pattern conclusion:** strong house patterns exist for bounded provider metadata, layered config, canonical hashing, transactional writes, owner-gated APIs, durable assets, metrics and React request/UI composition. Section 17's core correctness protocols are deliberately greenfield and must not be disguised as exact analogs.
**Extraction date:** 2026-07-13.
