# Phase07D Source Audit - User/Operational Memory Typed Tiers

Status: source-audited and self-audited on 2026-05-17. Current code already
contains most Phase07D behavior via Phase-N, Phase-O, and Phase-Q evidence, but
this folder is not independently verified.

## Source Audit

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` lines 646-724 | Memory is typed product semantics, not one storage bucket | Keep user profile memory, operational memory, proposal queue, archive, wiki, source corpus, projections, and cache as separate layers | Promoting raw archive rows, tool failures, or source text into user/wiki memory accidentally | read |
| `D:/Aura/prd.md` lines 806-862 | RAG uses task-level recall over schema-aware layers | Use `recall_user_memory` and `recall_operational` as task-level tools for their tiers | A single `memory(mode=...)` surface for every memory kind | read |
| `D:/Aura/prd.md` lines 1482-1522 | Phase 7 gates for typed layers, handles, freshness, and golden evals | Preserve layer labels, citation handles, and degraded-read annotations for user/operational recall | Calling Phase07D closed from tool existence alone | read |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-026 | Typed memory governance and promotion | Use proposal/review or question-backed movement into user and operational memory | Direct casual chat-to-memory writes without review, question, or explicit intent | read |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-029 | Retrieval projections expose freshness state | Reuse the Phase07C compact-memory freshness annotation for current compact-backed tiers | Treating shared compact projection freshness as the final per-tier projection story | read |
| `D:/Aura/docs/phase07b-current-types-audit-2026-05-16.md` lines 317-323 | Original G.2.2 deferral | Recheck the deferral against current code after Phase-N/O/Q landed | Continuing to describe user/operational memory as absent after they are wired | read |
| `D:/Aura/.planning/deep-refactor/Phase06_Tool_Experience_Loop/subphases/Phase06_lesson_promotion/{plan.md,benchmark.md,progress.md}` | Operational memory writer and recall path | Treat `WriteApprovedLesson -> KindOperational -> recall_operational` as implementation evidence to verify | Treating raw `tool_attempts` as operational memory without validation/promotion | read |
| `D:/Aura/.planning/deep-refactor/Phase09_Memory_Source_Discipline/subphases/Phase09_user_memory_promotion/{plan.md,benchmark.md,progress.md}` | User memory writer and recall path | Treat `proposed_updates kind=user_memory -> WriteApprovedUserFact -> recall_user_memory` as implementation evidence to verify | Writing user facts to wiki by default | read |
| `D:/Aura/.planning/deep-refactor/Phase09_Memory_Source_Discipline/subphases/Phase09_user_memory_write_guards/{benchmark.md,progress.md}` | Write guard evidence | Keep `memory.user.write` and ambiguity gate as Phase 9 write-governance evidence adjacent to Phase07D | Expanding Phase07D into a broad write-policy rewrite | read |
| `D:/Aura/internal/storage/memoryindex/collections.go` | Collection registry state | `CollectionUserMemory` and `CollectionOperational` are first-class registry entries with storage backends | Leaving these tiers as scaffolded enum values | read |
| `D:/Aura/internal/storage/memoryindex/store.go` | Compact store and FTS filtering | Use `KindUserMemory` and `KindOperational` filters over `compact_memory_documents` plus FTS | Adding a second store before a benchmark proves the shared compact store is limiting | read |
| `D:/Aura/internal/learning/writer.go` | Operational memory writer | Approved `operational_memory` proposals become `KindOperational` rows with stable handles and content hashes | Indexing every raw tool attempt or run event as a lesson | read |
| `D:/Aura/internal/learning/user_memory_writer.go` | User memory writer | Approved `user_memory` proposals become `KindUserMemory` rows with category tags and stable handles | Letting pending proposals appear in recall | read |
| `D:/Aura/internal/api/summaries.go` lines 231-264 | Approval hook | Approved user/operational proposals are routed away from wiki auto-apply into typed memory writers | Wiki auto-apply for typed memory proposals | read |
| `D:/Aura/internal/agent/tools/registry/recall_user_memory.go` | User recall surface | Return approved active user facts only, with category filtering and `[user_memory]` handles | Mixing pending proposals or wiki facts into user recall | read |
| `D:/Aura/internal/agent/tools/registry/recall_operational.go` | Operational recall surface | Return approved active operational lessons only, with tool/error filters and `[operational]` handles | Surfacing unvalidated raw failures as lessons | read |
| `D:/Aura/internal/agent/tools/registry/memory_search.go` lines 137-227 | Federated `search_memory` boundary | Keep user/operational memory out of the legacy `search_memory` scope enum; use task-level tools instead | Adding user/operational tiers to broad `scope=all` without a separate policy decision | read |
| `D:/Aura/cmd/aura/app_wire.go` lines 184-199 | Runtime tool registration | Register `search_memory`, `recall_operational`, and `recall_user_memory` after compact memory indexing, all with freshness store injection | Registering recall tools before their backing index exists | read; locally covered by `TestRegisterMemoryRecallToolsWiresTypedTiersAndFreshness` |
| `D:/Aura/internal/agent/tools/registry/tool_definitions.go` | Tool manifest metadata | Mark recall tools read-only, idempotent, active-turn tools with examples | Treating recall as a write-capable memory operation | read |
| `D:/Aura/internal/{storage/memoryindex,learning,agent/tools/registry}/*_test.go` | Existing narrow proof | Keep writer, registry, recall, pending-exclusion, filter, freshness, and example-schema tests as baseline | Using only a process-start smoke check as Phase07D proof | read |
| Mem0 official docs, Add Memory | Memory write model | Use structured extracted memories, metadata/category filters, and scoped identifiers as a pattern | Importing Mem0 as a backend or storing raw transcripts by default | audited online 2026-05-17 |
| Mem0 official docs, Control Memory Ingestion | Ingestion guardrails | Keep filter/verification/confidence/update controls in the source map | Letting speculative facts become durable memory | audited online 2026-05-17 |
| Letta official docs, Archival memory | Searchable out-of-context memory | Use explicit tool-mediated long-term recall and tags as a pattern | Letting long-term memory be always in prompt context | audited online 2026-05-17 |
| Letta official docs, Context hierarchy | Tier choice | Keep frequently needed core state separate from queried archival/external RAG tiers | One universal memory abstraction for all facts and documents | audited online 2026-05-17 |

## External Sources Opened

- `https://docs.mem0.ai/core-concepts/memory-operations/add`
- `https://docs.mem0.ai/cookbooks/essentials/controlling-memory-ingestion`
- `https://docs.letta.com/guides/core-concepts/memory/archival-memory/`
- `https://docs.letta.com/guides/core-concepts/memory/context-hierarchy/`

## Adopted Decisions

- User memory and operational memory are task-level recall tiers, not new broad
  `search_memory` scopes.
- Current code evidence supports closure: both tiers have registry entries,
  approved writers, active-only recall tools, app registration, targeted tests,
  a disposable direct mixed-tier no-leak golden, and a live `cmd/probe_chat`
  mixed-tier recall probe.
- Phase07D may use `compact_memory_documents` for now because `kind`, `handle`,
  `tags`, `status`, `content_hash`, and freshness fields already carry the
  tier contract.
- Operational memory only stores validated lessons after promotion or approval.
  Raw tool attempts remain experience-store evidence.
- User memory writes remain governed by Phase9 policy. Phase07D records the
  retrieval tier contract and defers write-policy expansion.

## Rejected Or Deferred Decisions

- No new user-memory or operational-memory tables in Phase07D unless a verifier
  proves shared compact storage breaks layer semantics.
- No broad `search_memory(scope=user_memory\|operational\|all)` change in this
  slice; task-level recall tools remain the explicit interface.
- No direct indexing of `tool_attempts`, `run_events`, or `audit_events` into
  operational memory.
- No Mem0/Letta backend dependency.
- Per-tier projection rows such as `user_memory_fts` and
  `operational_memory_fts` are deferred until a projection slice needs them.

## Missing Source Questions

- Resolved 2026-05-17: Phase07D closure includes a live `cmd/probe_chat`
  fixture with seeded user and operational memory rows. Broader wiki/source
  RAG evals remain future Phase 7 work.
- Should the API approval path keep using system-actor back-compat for
  `WriteApprovedUserFact`, or should Phase 9 hardening require actor-aware
  `WriteApprovedUserFactAs` from dashboard review context?
- Should `projection_state` eventually split compact projection freshness by
  `kind`, or is one compact projection row enough while all tiers share FTS and
  Qdrant mirrors?
