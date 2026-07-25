# Aura Adaptive Intelligence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Aura's schema-1 adaptive observation spike with a production-shaped, privacy-safe assignment/delivery/outcome ledger, five real controllable adapters, coherent Neo4j Agent Memory reuse, and reproducible statistical evidence without treating the Qwen 2B portability spike as production quality proof.

**Architecture:** PostgreSQL is the immutable source of assignment, delivery, outcome, correction, cohort, snapshot, and evidence truth. Each adapter follows `durable assignment -> execute -> durable delivery -> expose`, falling back to its static champion on any learned-path failure. Neo4j stores private projections and supplies the already-deployed Agent Memory context; an equal before/after corpus epoch makes multi-query recall eligible. Promotion evidence is reconstructed from sealed ledger cohorts and evaluated with exact blocked randomization inference. Canary and active serving remain unavailable until their separate artifacts and operator-authorized control surface land.

**Tech Stack:** Go 1.26, PostgreSQL/sqlc, Neo4j Cypher, Python 3.13/pytest, Neo4j Labs Agent Memory MCP, llama.cpp/OpenAI-compatible API, Docker Compose, OpenTelemetry.

**Design sources:** [production specification](../specs/2026-07-24-aura-adaptive-intelligence-design.md), [delivery and evidence appendix](../specs/2026-07-24-aura-adaptive-intelligence-delivery-and-evidence.md), and PRD Amendment #93.4 in [prd.md](../../../prd.md).

## Progress checkpoint — 2026-07-24

**Current approved head:** `fee72ff13965cabd6e4a797057c6895492b63a3f`

- The schema-2 Assignment/Delivery Go contract is complete through
  `03643cc9a`. Its spec and code-quality reviews both returned `APPROVE`.
- PostgreSQL/SQLC enforcement and the shared chat/serve migration gate are
  complete through `fee72ff13`. Migrations `0060`–`0063` are hash-locked
  immutable history; validation-only `0064` closes the concurrent audit window.
  The final spec and code-quality reviews both returned `APPROVE`.
- The persistence commits after the Go contract are `b0216f494`,
  `79a5f719d`, `f941a0e0f`, `001d6f974`, `ca587adb8`, `9ed7f625b`, and
  `fee72ff13`.
- Verified evidence at the approved head includes clean head-64 install,
  down/up, versioned upgrade and dirty fail-closed paths, deterministic
  Go/PostgreSQL identity parity, dirty-62 loader exclusion, live
  INSERT/UPDATE/DELETE lock contention, schema-2 integration under race,
  adaptive/database unit race, SQLC regeneration, `go vet ./...`,
  `go build ./...`, diff-scoped golangci-lint with zero findings, diff checks,
  and repository file-size checks.
- Task 1 is intentionally not marked complete yet. The next slice must add
  typed Store APIs that transactionally load the persisted Assignment before
  recording Delivery, preserve exact idempotence/conflict behavior, and prevent
  schema-2 callers from bypassing typed constructors through generic `Record`.
- Outcome/correction contracts, snapshots, cohorts, and sealed evidence remain
  Task 2 work. No production runtime adapter, Agent Memory change, benchmark,
  final quality gate, or push has been completed.
- Do not replay unchanged remote CI. Resume with the Store slice, use TDD, and
  repeat spec review before code-quality review.

## Global execution contract

- [ ] Read `D:\Aura\CLAUDE.md` completely before every new agent session and before each delegated task. More-specific `AGENTS.md` files may add constraints.
- [ ] Use WSL for Go, Python, SQL, Git, Docker, and benchmark commands:

```bash
export PATH=/usr/local/go/bin:/home/davide/.local/bin:/home/davide/go/bin:/usr/bin:/bin
cd /mnt/d/Aura
```

- [ ] Start every production slice with a failing focused test. After every Go edit run `go vet ./...`, `go build ./...`, the affected unit package, and its race test. Do not commit until those commands pass.
- [ ] Keep every owned source file at or below 600 lines. Extract a concern before adding behavior to a near-limit file.
- [ ] Stage only the paths owned by the slice. Never use `git add -A`; never bypass hooks. Every commit is atomic and includes `Co-Authored-By: Codex <noreply@openai.com>`.
- [ ] Preserve schema-1 rows as immutable, readable, ineligible history. Delete each generic schema-1 runtime producer in the same commit that replaces its seam.
- [ ] Never persist raw prompts, queries, tool arguments, documents, memories, credentials, chain-of-thought, embeddings, scores, or content-derived fingerprints in adaptive payloads or metrics.
- [ ] Do not rerun unchanged CI merely to observe it. Run focused verification after each change and the repository's full closing gates once, after the final implementation state.

## Target file structure

**Adaptive contracts and evidence (`internal/adaptive/`)**

- `contract.go` — schema-2 assignment, delivery, outcome, and correction types plus validation.
- `contract_ids.go` — deterministic assignment and event IDs.
- `outcome_recorder.go` — trusted typed outcome/correction ingestion bound to registered evaluator provenance.
- `cohort.go` / `cohort_store.go` — immutable focal cohort specification, claim, and ledger reconstruction.
- `snapshot.go` / `snapshot_store.go` — immutable owner/model policy snapshots and local graph-kNN scoring.
- `evidence.go` / `evidence_store.go` — exact randomization inference and sealed artifacts.
- `coordinator.go` — shared assignment/delivery orchestration and static fallback semantics.
- `projector_worker.go` — bounded Start/Stop lifecycle around `ProjectOne`.
- Delete `hook.go` and `promotion.go` when their typed replacements are composed.

**Runtime adapters**

- `internal/agent/adaptive_reasoning.go` — one reasoning assignment per primary-model round.
- `internal/agent/tools/adaptive_search.go` — free-text deferred-tool ordering.
- `internal/agent/tools/adaptive_skill.go` — queried read-only skill-list ordering.
- `internal/documents/retrieval_plan.go` — versioned retrieval plan shared by document search and GraphRAG.
- `internal/runner/dynamic_recall.go` — typed off/top-4/top-8 recall and final exposure guard.
- `internal/conversations/context_dynamic.go` — indivisible non-persisted dynamic-tail budgeting.

**Agent Memory fork**

- `docker/agent-memory/src/neo4j_agent_memory/integration_context.py` — extracted context/search operations and safe metadata.
- `docker/agent-memory/src/neo4j_agent_memory/graph/client.py` — same-transaction corpus epoch increments and epoch reads.
- `internal/knowledge/migrations/0005_memory_corpus_revision.cypher` — singleton epoch constraint/state.
- `docker/agent-memory/LICENSE` and `NOTICE` — upstream Apache-2.0 distribution material.

## Task 1: Land the schema-2 typed ledger

**Files:**

- Create: `internal/adaptive/contract.go`
- Create: `internal/adaptive/contract_ids.go`
- Create: `internal/adaptive/contract_test.go`
- Modify: `internal/adaptive/event.go`
- Modify: `internal/adaptive/event_test.go`
- Create: `internal/db/migrations/0060_adaptive_typed_ledger.up.sql`
- Create: `internal/db/migrations/0060_adaptive_typed_ledger.down.sql`
- Modify: `internal/db/queries/adaptive_outbox.sql`
- Regenerate: `internal/db/sqlc/`
- Modify: adaptive store integration tests

- [ ] **Step 1: Verify the migration heads** — `find internal/db/migrations -maxdepth 1 -type f -printf '%f\n' | sort | tail`; expected PostgreSQL head before this task: `0059_decommission_legacy_learning.up.sql`.
- [ ] **Step 2: Write failing contract tests** for canonical point+ordinal assignment IDs, deterministic kind/source event IDs, explicit static action, frozen ordered catalogs, policy epoch/version/mode, immutable snapshot ID/hash, environment/cohort and provider/model scope, eligibility/catalog hashes, separate arm/action/exposure propensities, explicit override truth, delivery intended-vs-actual action, success/fallback status, independent result count, known-vs-unknown exposure probability, terminal evaluator/source uniqueness, correction-chain validity, and privacy-key rejection.
- [ ] **Step 3: Run the focused red test** — `go test ./internal/adaptive/ -run 'TestAssignment|TestDelivery|TestOutcome|TestCorrection|TestPrivatePayload'`; expect undefined schema-2 types or failed validation.
- [ ] **Step 4: Implement the typed public surface**:

```go
type DecisionPoint string

const (
	PointReasoning     DecisionPoint = "reasoning"
	PointToolDiscovery DecisionPoint = "tool_discovery"
	PointSkillRouting  DecisionPoint = "skill_routing"
	PointKnowledge     DecisionPoint = "knowledge_retrieval"
	PointMemoryRecall  DecisionPoint = "memory_recall"
)

type ActionProbability struct {
	ActionID    string  `json:"action_id"`
	Probability float64 `json:"probability"`
}

type Assignment struct {
	SchemaVersion       string
	AssignmentID        uuid.UUID
	OwnerID             uuid.UUID
	RequestID           uuid.UUID
	Point               DecisionPoint
	PointOrdinal        uint32
	PolicyEpoch         uint64
	PolicyVersion       string
	PolicyMode          PolicyMode
	SnapshotID          uuid.UUID
	SnapshotSHA256      string
	Environment         EvaluationEnvironment
	ProviderID          string
	ModelID             string
	CohortID            *uuid.UUID
	EligibleActions     []string
	EligibilitySHA256   string
	CatalogSHA256       string
	ChampionActionID    string
	RecommendedActionID string
	IntendedActionID    string
	ExperimentID        string
	ArmID               string
	ArmProbability      *float64
	ActionProbabilities []ActionProbability
	SelectionReason     string
	Override            bool
	Features            map[string]float64
}

type Delivery struct {
	SchemaVersion       string
	AssignmentID        uuid.UUID
	IntendedActionID    string
	ActualActionID      string
	Status              DeliveryStatus
	ExposureKnown       bool
	ExposureProbability *float64
	FallbackReason      string
	ResultCount         int
	ResultIDs           []ResultID
	Revisions           map[string]string
	EffectiveLimits     map[string]int
}
```

`AssignmentID` must hash owner, request, canonical point, and ordinal. `EventID` must hash assignment, event kind, and a stable source identity. Constructors return canonical `Event` values and reject unregistered fields before JSON serialization.

- [ ] **Step 5: Add `EventDelivery = "delivery"`** and keep generic `NewEvent` only for schema-1 compatibility and internal promotion/rollback facts. Schema-2 callers must use typed constructors.
- [ ] **Step 6: Add additive database enforcement**: delivery event kind; partial unique assignment index on schema `2.0`; one delivery per assignment; one terminal outcome per assignment/evaluator/source; object/shape/range checks; schema-1 rows unaffected.
- [ ] **Step 7: Add SQLC loader queries** that return schema-2 facts only and never infer eligibility from schema-1. Regenerate with the repository command; never hand-edit generated Go.
- [ ] **Step 8: Verify**:

```bash
go test ./internal/adaptive/
go test -tags=db_integration ./internal/adaptive/ -run 'Typed|Schema2|Outbox'
go test -race ./internal/adaptive/
go vet ./...
go build ./...
```

- [ ] **Step 9: Commit** — `feat(adaptive): add typed schema-2 evidence ledger`.

## Task 2: Build immutable snapshots, focal cohorts, and sealed evidence

**Files:**

- Create: `internal/adaptive/snapshot.go`
- Create: `internal/adaptive/snapshot_store.go`
- Create: `internal/adaptive/snapshot_test.go`
- Create: `internal/adaptive/cohort.go`
- Create: `internal/adaptive/cohort_store.go`
- Create: `internal/adaptive/cohort_test.go`
- Create: `internal/adaptive/evidence.go`
- Create: `internal/adaptive/evidence_store.go`
- Create: `internal/adaptive/evidence_test.go`
- Create: `internal/adaptive/outcome_recorder.go`
- Create: `internal/adaptive/outcome_recorder_test.go`
- Delete: `internal/adaptive/promotion.go`
- Delete: `internal/adaptive/promotion_test.go`
- Create: next free PostgreSQL migration, expected `0065_adaptive_sealed_evidence.*.sql`
- Modify: `internal/db/queries/adaptive_policy.sql`
- Add: focused DB integration tests

- [ ] **Step 1: Recheck the migration head immediately before allocation.**
- [ ] **Step 2: Write red tests** for immutable owner/provider/model snapshots, top-5 nonnegative-cosine mean scoring, zero-neighbor static fallback, preregistered point/ordinal predicate, unique request membership, at most one randomized request per evaluation conversation, deterministic correction folding, missing/fork/cycle rejection, and artifact hash stability.
- [ ] **Step 3: Write red statistical tests** using enumerated tiny blocked assignments whose exact p-values are hand-checkable. Include quality/harm margins, alpha-spent looks, censoring, cluster/conversation blocks, ITT primary analysis, served-action diagnostic analysis, and arm-vs-action propensity mismatch rejection.
- [ ] **Step 4: Implement the scorer** as a pure local function over the sealed snapshot. Similarity is `max(0, cosine)`; use at most five neighbors; predicted value is the arithmetic mean of neighbor outcomes weighted by nonnegative similarity; empty/invalid support returns the static champion.
- [ ] **Step 5: Implement durable focal claims** with both constraints: unique `(cohort_id, request_id)` is the request-level focal membership key, and unique `(cohort_id, evaluation_conversation_id)` enforces at most one randomized request in an evaluation conversation. The claim stores the exact point/ordinal predicate match and happens atomically before the arm draw. A cohort freezes policy/provider/model/snapshot/action catalogs, arm probabilities, evaluator versions, power/support plan, safety limits, planned looks, and cutoff.
- [ ] **Step 6: Implement the ledger-only loader**: reconstruct assignments, deliveries, eligible outcomes, and folded corrections at the cohort cutoff; include missing delivery/outcome as censored ITT rows; reject unsealed catalogs, unknown exposure probabilities, cross-owner facts, and facts beyond cutoff.
- [ ] **Step 7: Implement a production typed outcome/correction recorder.** `OutcomeRecorder.RecordOutcome` accepts only a typed `OutcomeObservation`, loads the referenced schema-2 assignment, binds domain/owner/provider/model, validates the registered evaluator kind/ID/version/rubric/calibration/provenance hash, and appends the deterministic terminal event. `RecordCorrection` transactionally loads the current effective leaf and rejects missing, cross-owner/domain/evaluator, cross-kind, fork, or cycle links. Operational tool/HTTP/persistence/model-self-report completion cannot call this eligible recorder. Compose the deterministic and calibrated-judge benchmark evaluators through this same service; no benchmark may insert ledger rows directly.
- [ ] **Step 8: Implement separate sealed artifact kinds**:

```go
type EvidenceKind string

const (
	EvidenceCanaryAdmission EvidenceKind = "canary_admission"
	EvidenceCanaryOutcome   EvidenceKind = "canary_outcome"
)
```

The transition function must revalidate artifact kind, target state, scope, cutoff, snapshot/cohort hashes, ledger hash, and operator capability transactionally.

- [ ] **Step 9: Delete caller-built `CanaryBatch` and ESS/IPW-Wilson authorization.** If simulation still needs Wilson/ESS, move it under `.planning/spikes/` or a `_test.go` helper with no production export.
- [ ] **Step 10: Verify** focused unit, DB integration, privileges, migrations, race, vet, and build.
- [ ] **Step 11: Commit outcome ingestion separately** — `feat(adaptive): record typed evaluator outcomes`.
- [ ] **Step 12: Commit sealed evidence** — `feat(adaptive): seal cohort-built promotion evidence`.

## Task 3: Add the coordinator and correct request/round identity

**Files:**

- Create: `internal/adaptive/coordinator.go`
- Create: `internal/adaptive/coordinator_test.go`
- Modify: `internal/runner/runner.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/llm_agent.go`
- Create: `internal/agent/model_round.go`
- Modify: relevant runner/agent tests
- Delete after replacement: `internal/adaptive/hook.go`, `internal/adaptive/hook_test.go`
- Delete after replacement: `internal/runner/adaptive_persist.go`
- Modify: `internal/runner/interfaces.go`, `runner_persist.go`, `cmd/aura/chat_adaptive.go`

- [ ] **Step 1: Write red coordinator tests** proving assignment persists before executor entry, delivery persists before exposure callback, learned-result discard on assignment/execution/delivery failure, static fallback, exact retry reuse, and no duplicate delivery.
- [ ] **Step 2: Implement one shared primitive**:

```go
type Exposure[T any] struct {
	Value    T
	Delivery Delivery
}

func DecideAndDeliver[T any](
	ctx context.Context,
	assignment Assignment,
	assign func(context.Context, Event) error,
	learned func(context.Context, Assignment) (Exposure[T], error),
	deliver func(context.Context, Event) error,
	static func(context.Context) (T, error),
) (T, error)
```

The learned value is returned only after its typed delivery commits. Any learned-path failure invokes `static`; static failure remains a normal request error.

- [ ] **Step 3: Mint request UUIDv7 before context assembly** in `turnLocked`; pass it into `buildAgent` and every adapter rather than minting a second ID inside `buildAgent`.
- [ ] **Step 4: Add `modelRoundOrdinal`** local to the primary-model loop. Increment only for a newly built primary request. Transport retries reuse the ordinal. Router/critic/finalizer/title calls and model-emitted tool calls never consume it.
- [ ] **Step 5: Remove the generic hook and generic terminal-outcome bridge** after typed seams compile. Retain `TurnCommitter` only as a lower-level atomic event+turn primitive where the typed runtime uses it.
- [ ] **Step 6: Verify** runner/agent/adaptive unit and race tests, then vet/build.
- [ ] **Step 7: Commit** — `refactor(adaptive): replace generic hooks with typed coordination`.

## Task 4: Implement per-round reasoning control

**Files:**

- Create: `internal/agent/adaptive_reasoning.go`
- Create: `internal/agent/adaptive_reasoning_test.go`
- Modify: `internal/agent/llm_agent.go`
- Modify: `internal/agent/llm_agent_reasoning.go`
- Modify: reasoning override/build-request tests

- [ ] **Step 1: Write red tests** for distinct assignment IDs across rounds, stable IDs across exact transport retry, static champion action, explicit override as exogenous diagnostic, assignment before request construction, delivery before stream, and static fallback at all three persistence boundaries.
- [ ] **Step 2: Define a frozen action catalog** from already-supported reasoning tiers plus explicit static/no-op. Do not create unsupported provider parameters.
- [ ] **Step 3: Insert the adapter** after static tier computation and before selected request construction. Run remaining request transforms, validate final caps, commit exact delivered tier/model revision, then stream.
- [ ] **Step 4: Ensure schema-1 `BeforeModel` no longer exists** and no top-level tool-call observation is relabeled as reasoning evidence.
- [ ] **Step 5: Verify** agent/adaptive focused tests, race, vet, build.
- [ ] **Step 6: Commit** — `feat(adaptive): control reasoning per model round`.

## Task 5: Implement deferred-tool and read-only skill ordering

**Files:**

- Create: `internal/agent/tools/adaptive_search.go`
- Create: `internal/agent/tools/adaptive_search_test.go`
- Modify: `internal/agent/tools/search.go`
- Modify: tool-search corpus/mount/promotion tests
- Create: `internal/agent/tools/adaptive_skill.go`
- Create: `internal/agent/tools/adaptive_skill_test.go`
- Modify: `internal/agent/tools/skill_read.go`
- Modify: `cmd/aura/serve_adapters.go`
- Modify: skill tests

- [ ] **Step 1: Write red tool tests** proving only free-text discovery draws an assignment; exact `select:` is exogenous; candidates are frozen before ranking; only registered nonsecret tool IDs and catalog revision are delivered; delivery precedes returned/promoted results.
- [ ] **Step 2: Compose ordered strategies** over the existing semantic/BM25 implementations. The static champion remains unchanged and serves on every adaptive failure.
- [ ] **Step 3: Write red skill tests** proving only `action=list` with a nonblank query is adaptive. Blank list, info, use, pinned skills, and every write action remain exogenous.
- [ ] **Step 4: Compose the queried list adapter** in `newSkillTool`; preserve owner scope and the stable skills `AlwaysBlock` byte-for-byte.
- [ ] **Step 5: Verify** tool/skill packages, agent promotion tests, races, vet, build.
- [ ] **Step 6: Commit tool discovery** — `feat(adaptive): order deferred tool discovery`.
- [ ] **Step 7: Commit skill routing separately** — `feat(adaptive): order read-only skill discovery`.

## Task 6: Converge document retrieval on versioned plans

**Files:**

- Create: `internal/documents/retrieval_plan.go`
- Create: `internal/documents/retrieval_plan_test.go`
- Modify: `internal/documents/retrieve.go`
- Modify: `internal/documents/graphrag.go`
- Modify: `internal/agent/tools/document_search.go`
- Modify: `cmd/aura/docs.go`, `cmd/aura/main.go`
- Modify: retrieval/GraphRAG/live fail-closed tests

- [ ] **Step 1: Write red tests** proving plan assignment occurs before embedding/graph/reranker I/O; every plan preserves identical owner, ACL, and document scope; delivery records exact ordered chunk IDs and registered revisions; persistence failure returns the static result.
- [ ] **Step 2: Define only supported plans**, including explicit static/no-op, sparse, vector seed, vector+rerank, and vector+rerank+expand when their dependencies are healthy.
- [ ] **Step 3: Move route/depth/rerank execution behind one plan executor** and make GraphRAG consume it. Delete duplicate strategy branching in the same commit.
- [ ] **Step 4: Commit delivery before `DocumentSearch.Execute` returns**; never log query text/hash, content, score, or embedding.
- [ ] **Step 5: Verify** document/tool unit, live integration, race, vet, build.
- [ ] **Step 6: Commit** — `feat(adaptive): execute versioned retrieval plans`.

## Task 7: Make Agent Memory context coherent and distributable

**Files:**

- Create: `internal/knowledge/migrations/0005_memory_corpus_revision.cypher`
- Create: `docker/agent-memory/src/neo4j_agent_memory/integration_context.py`
- Modify: `docker/agent-memory/src/neo4j_agent_memory/integration.py`
- Modify: `docker/agent-memory/src/neo4j_agent_memory/graph/client.py`
- Add: Agent Memory epoch/context/writer-coverage tests
- Add: `docker/agent-memory/LICENSE`, `docker/agent-memory/NOTICE`
- Modify: `docker/agent-memory/Dockerfile`, `compose.yaml`
- Add: distribution provenance test

- [ ] **Step 1: Verify the Neo4j head** is still `0004_decommission_legacy_learning.cypher`.
- [ ] **Step 2: Write red transaction tests** proving a fork-mediated mutation and epoch increment share one managed transaction, mutation failure does not increment, missing singleton rolls back, and reads do not increment.
- [ ] **Step 3: Add the singleton constraint/state** and extend `Neo4jClient.execute_write` so all result-affecting fork writes advance the epoch transactionally.
- [ ] **Step 4: Split before behavior change**: move the `MemoryIntegration.get_context/search` facade behavior from `integration.py` into `integration_context.py`, leaving both below 600 lines. Keep `_tools.py`, `__init__.py`, `memory/long_term.py`, and other over-limit modules untouched.
- [ ] **Step 5: Write red metadata tests** for exact ordered `{kind,id,order}`, requested/effective per-type K, counts, registered retriever/reranker/embedding/index revisions, equal before/after epochs, and absence of raw/content-derived data.
- [ ] **Step 6: Replace only Aura's long-term-only facade path with one metadata-aware orchestration.** The mixin reads the epoch, calls the existing `LongTermMemory.search_preferences` and `search_entities` retrieval primitives exactly once each, preserves their returned order, assembles the byte-compatible preference/entity context, captures those same objects' stable IDs/counts, and reads the epoch again. It must not call `MemoryClient.get_context` and then repeat searches. Calls that request short-term or reasoning continue to delegate to the unchanged `MemoryClient.get_context` path and are diagnostic-only.
- [ ] **Step 7: Bracket that entire delivered multi-query operation** with the same service-owned monotonic epoch. Equal epochs are coherent; unequal/missing epochs remain usable static recall but are ineligible adaptive evidence. A bookmark or plain read-committed transaction is not accepted as a snapshot substitute. A characterization test compares long-term-only text before and after extraction and asserts one call per existing retrieval primitive.
- [ ] **Step 8: Add live writer coverage and a source guard** rejecting mutating direct-driver Cypher outside the graph client. Prove message/entity/preference/fact/relationship/forget/buffered/consolidation writes advance the epoch.
- [ ] **Step 9: Add upstream Apache-2.0 license/notices and truthful provenance**. OCI labels identify upstream base `c1c2d65`, the actual vendored Aura revision supplied as `AURA_AGENT_MEMORY_VENDOR_REV`, and Apache-2.0. Copy license files into the image.
- [ ] **Step 10: Verify** Python unit/live tests, migration/idempotency, image provenance smoke, file-size gate.
- [ ] **Step 11: Commit epoch/context** — `feat(memory): expose coherent typed recall metadata`.
- [ ] **Step 12: Commit distribution metadata separately** — `build(memory): ship Agent Memory license and provenance`.

## Task 8: Add protected dynamic-tail memory recall

**Files:**

- Modify: `internal/runner/interfaces.go`
- Create: `internal/runner/dynamic_recall.go`
- Create: `internal/runner/dynamic_recall_test.go`
- Modify: `internal/runner/runner_context.go`, `runner.go`
- Create: `internal/conversations/context_dynamic.go`
- Create: `internal/conversations/context_dynamic_test.go`
- Modify: `internal/conversations/context.go`
- Modify: `cmd/aura/serve_recall.go`
- Modify/Add: recall MCP and runner wiring tests

- [ ] **Step 1: Write red context tests** for fresh and resumed placement, ordinary-history eviction before recall, byte-identical preservation, whole-item omission, hard-cap final check, and unchanged stable `messages[1]`.
- [ ] **Step 2: Replace string recall with typed recall** carrying fenced text, ordered IDs, effective limits, revisions, and coherent epoch. Candidate catalog is `off`, `top4`, `top8`, capped by configured maximum; short-term and reasoning flags stay false.
- [ ] **Step 3: Budget one indivisible non-persisted `DynamicTail`**. Reserve its exact token cost during L1/L2.5, then place it immediately before the current visible user or at tail. Never truncate it.
- [ ] **Step 4: Write red runner tests** proving exact inclusion across all primary-model rounds, delivery before fake-client invocation, no learned tail on delivery failure, and `none/context_budget` delivery when the item cannot fit.
- [ ] **Step 5: Add the final exposure guard** after all hooks/transforms and immediately before streaming. It rechecks hard cap, exact single inclusion, placement, IDs/revisions, and epoch coherence, then commits delivery. Failure removes the learned item and serves the static/off path.
- [ ] **Step 6: Verify** conversation/runner/cmd tests, memory integration, races, vet, build.
- [ ] **Step 7: Commit** — `feat(adaptive): deliver bounded dynamic memory recall`.

## Task 9: Compose the projector lifecycle and compatible graph identity

**Files:**

- Modify: `internal/adaptive/graph.go`, `graph_test.go`
- Create: `internal/adaptive/projector_worker.go`
- Create: `internal/adaptive/projector_worker_test.go`
- Modify: projector live/race tests
- Modify: `cmd/aura/chat_boot.go`, `chat_adaptive.go`, provisioning/shutdown wiring

- [ ] **Step 1: Write red graph tests** for compatible create/repair/no-overwrite behavior:

```cypher
MERGE (u:User {identifier:$owner_id})
ON CREATE SET
  u.id=$owner_id,
  u.created_at=datetime($created_at),
  u.attributes_json='{}'
ON MATCH SET
  u.id=coalesce(u.id,$owner_id),
  u.created_at=coalesce(u.created_at,datetime($created_at)),
  u.attributes_json=coalesce(u.attributes_json,'{}')
```

- [ ] **Step 2: Write red worker tests** for bounded polling, lease retry/backoff, dead letter, tombstone races, clean cancellation, restart, and drain.
- [ ] **Step 3: Implement `Start(ctx)`/`Stop(ctx)`** around `ProjectOne`; compose exactly one worker in production startup and stop it during ordered shutdown.
- [ ] **Step 4: Extend the live compatibility proof**: project first; verify non-null User fields; perform owner-scoped Agent Memory write/read; project again; verify attributes, memory, and private adaptive subgraphs survive.
- [ ] **Step 5: Prove projector result neutrality**: User repair/private adaptive edges do not change corpus epoch or recalled IDs/order. Only then may they remain exempt from corpus-epoch increments.
- [ ] **Step 6: Verify** adaptive unit/live/race, startup/shutdown, retention/tombstone, vet, build.
- [ ] **Step 7: Commit** — `feat(adaptive): run the private graph projector`.

## Task 10: Replace composition and prove behavior scientifically

**Files:**

- Modify: `cmd/aura/chat_adaptive.go`, `chat_boot.go`, adapter constructors
- Create: `internal/eval/adaptive_benchmark.go`
- Create: `internal/eval/adaptive_benchmark_test.go`
- Create: `.planning/spikes/106-adaptive-aura-portability/` after rechecking that `105-adaptive-quality-canary-gate` is still the spike head
- Add: sealed benchmark artifact schemas and reports under the spike directory
- Modify: operational runbook only after measured results exist

- [ ] **Step 1: Add a cross-domain composition test** proving the production binary wires all five typed adapters, no schema-1 producer remains, shadow always serves champion with marginal action probability `1.0`, and canary/active still fail safely until authorized.
- [ ] **Step 2: Delete remaining dark predecessors**: request-level `DecisionIDForPoint`, `PolicyGate.Adapt/canaryAssigned`, generic hook, generic terminal adaptive outcome, caller-built promotion API, recall-in-AlwaysBlock, and duplicate retrieval routing. Run `deadcode -test ./...` and fail on any newly unreachable owned symbol.
- [ ] **Step 3: Implement a benchmark harness through Aura**, not a direct model-only shortcut. It must:
  - generate preregistered seeded scenarios for all five domains;
  - freeze datasets, model IDs, prompts/evaluator versions, action catalogs, and seeds;
  - record champion, shadow recommendation, operational cost/latency, assignment/delivery linkage, and independent deterministic/human/calibrated outcomes;
  - run negative controls, static equivalence, restart, rollback, privacy, retention, concurrency, and failure injection;
  - emit machine-readable artifacts with environment and Git revision.
- [ ] **Step 4: Prove llama.cpp portability only** with `unsloth/Qwen3.5-2B-GGUF`, file `Qwen3.5-2B-Q4_K_M.gguf`. Capture only allow-listed `aura.settings` rows, override them transactionally, run the real Aura binary/adaptive path against llama.cpp, then restore captured rows exactly in a guaranteed cleanup. Label every Qwen result `portability_only=true`; it cannot satisfy quality/promotion gates.
- [ ] **Step 5: Run real-model shadow** using Aura's configured production model. Shadow serves the static champion at probability `1.0`; challenger output is diagnostic. Prepare evaluator calibration, snapshot, focal cohort/power/support plan, safety/rollback limits, and rollback plan; obtain explicit operator admission approval; bind that approval into the immutable canary-admission artifact; only then seal it.
- [ ] **Step 6: Obtain a separate explicit operator authorization before canary mutation.** In one later atomic slice, add the capability-gated CLI/API, audited admission-artifact transition, one-focal-assignment serving, supported randomization, and rollback. Do not infer mutation authorization from successful shadow results or the earlier admission-artifact approval.
- [ ] **Step 7: Run the closed real canary only after authorization**, seal the ledger-reconstructed outcome artifact, apply exact blocked randomization inference, obtain activation approval, and promote one domain/catalog at a time.
- [ ] **Step 8: Verify the full repository once at final source state**:

```bash
git diff --check
scripts/check-file-size.sh
AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" scripts/quality_snapshot_gate.sh
go vet ./...
go build ./...
go test ./...
go test -race ./...
go test -tags=db_integration ./...
go test -tags=neo4j_integration ./...
bash scripts/coverage_docker.sh
golangci-lint run ./...
make quality-full
```

Update `docs/aura-quality-snapshot.md` from measured final-revision results before running `scripts/quality_snapshot_gate.sh`. Also run Python Agent Memory tests, Docker/image provenance smoke, `dupl`, `deadcode -test`, vulnerability scan, the repository coverage matrix (at least 85%), mutation gate (at least 70%), and the real E2E score required by `CLAUDE.md` (>9.8).

- [ ] **Step 9: Send the final source and evidence artifacts to a fresh adversarial agent.** It must read `CLAUDE.md`, attempt to falsify identity/order, privacy, delivery-before-exposure, epoch coherence, statistical validity, Qwen labeling, real-model provenance, rollback, and legacy removal. Fix every reproducible blocker and repeat until it returns `APPROVE`.
- [ ] **Step 10: Commit each proof/runbook correction atomically, push only the fully verified branch, and inspect remote CI without replaying unchanged local gates.**

## Completion definition

This plan is complete only when the five production seams use typed schema-2 assignments and deliveries; the deployed Agent Memory fork supplies coherent typed recall without a parallel retriever; the private projector has a real lifecycle; schema-1 and caller-built promotion paths are absent from production; Qwen 2B proves only llama.cpp/Aura portability; the configured real model supplies the quality evidence; sealed cohort reconstruction and exact inference back every transition; all repository gates pass at the final source revision; an adversarial reviewer approves; and that exact revision is pushed with green remote CI.
