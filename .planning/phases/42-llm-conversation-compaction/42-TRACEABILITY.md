# Phase 42 Legacy-to-Industrial Traceability

**Authority:** `docs/superpowers/specs/2026-07-13-industrial-conversation-compaction-design.md` §17  
**Rule:** Legacy plans `42-01` through `42-07` are retired and MUST NOT execute. This matrix must remain synchronized with the replacement SPEC and plans.

## Legacy requirements

| Legacy item | Disposition | Replacement | Rationale |
|---|---|---|---|
| COMPACT-01 single LLM summary call | Superseded | IC-06, IC-10 | Summary is schema-validated, capability-gated, chunkable, and recursively rebasable. |
| COMPACT-02 9-section prompt | Retained + hardened | IC-06 | Keep structured continuation content; add authority ledger, typed envelope, injection rejection, schema versions. |
| COMPACT-03 summary turn + watermark | Superseded | IC-04, IC-05 | Replace scalar watermark with branch-aware manifests, digests, generations, claims, and CAS active pointer. |
| COMPACT-04 checkpoint reconstruction | Superseded | IC-05, IC-11 | Disjoint summarized/tail/protected/post-watermark sets; last-known-good recovery. |
| COMPACT-05 CLI command | Retained | IC-12 | All triggers call one coordinator and expose reasoned outcomes. |
| COMPACT-06 REPL command | Retained | IC-12 | Same coordinator; never enters normal LLM turn path. |
| COMPACT-07 Telegram command | Retained | IC-12 | Same coordinator, ownership/capability gates, localized result. |
| COMPACT-08 overflow fallback | Retained + reordered | IC-03, IC-07 | Proactive L2.4 must be attempted/waived before L2.5; overflow remains one bounded attempt. |
| COMPACT-09 configuration knobs | Superseded | IC-01, IC-13 | Exact provider/model budgets, hysteresis, rollout modes, validated defaults. |
| COMPACT-10 cost attribution/list projection | Retained + expanded | IC-09, IC-13 | Full metrics, checkpoint history, bounded labels, numerical gates. |
| COMPACT-11 bounded/atomic lifecycle | Superseded | IC-04, IC-11 | Durable claim → inference → serializable CAS finalize; recovery and corruption semantics. |

## Legacy decisions and prohibitions

| Legacy decision | Disposition | Replacement |
|---|---|---|
| Summary stored as role=user with ToolCallID marker | Removed | Structured JSON canonical payload rendered through dedicated internal-context envelope. |
| Latest checkpoint by created_at/watermark | Removed | Compare-and-swap active pointer scoped by conversation + branch. |
| Entire captured prefix summarized | Removed | Explicit summarized/tail/protected manifests with disjointness proof. |
| No recent verbatim tail | Removed | Token-based tail with semantic-unit floor and bounded backward expansion. |
| Auto only at L2.5 dead-end | Removed | L2.4 at 80% projected utilization, target 55%, plus overflow fallback. |
| One compaction generation only | Removed | Generation chain capped at four, then hierarchical canonical rebase. |
| Text-only phase | Removed | Typed content-part prerequisite with verified provider projection or explicit reference-only fallback. |
| Long-term memory out of scope | Removed | Separate typed candidate/promotion/retrieval lifecycle with privacy and consent gates. |
| Rich recovery UI out of scope | Removed | History, preview, diff, restore, corruption status, and accessibility are required. |
| Original turns retained | Retained | Canonical transcript remains immutable, searchable, and authoritative. |
| Atomic persistence | Retained + expanded | Short claim transaction and short serializable finalize transaction; LLM call occurs outside DB locks. |
| No unbounded retry loop | Retained | One overflow attempt/reconstruction; proactive failures are reasoned and non-destructive. |
| System prompt byte stability | Retained | L0 remains outside summary and its hash/bytes are verified. |

## Legacy plans

| Plan | Disposition | Replacement program slice |
|---|---|---|
| 42-01 DB watermark + reconstruction | Retired | Provider/budget foundations; manifest schema; claims/CAS; recovery. |
| 42-02 summarizer core | Retired | Structured summary, authority ledger, validator, adversarial corpus. |
| 42-03 configuration | Retired | Exact model-aware budget and rollout registry. |
| 42-04 Runner + auto fallback | Retired | Unified coordinator, L2.4-before-L2.5 decision seam, overflow policy. |
| 42-05 CLI/REPL | Retired | Unified manual surfaces after coordinator stability. |
| 42-06 Telegram | Retired | Unified channel/API surfaces with ownership and capability gates. |
| 42-07 web + terminal gate | Retired | Full operations UX, evaluation, canary, and terminal acceptance. |

## Replacement requirement index

## Executable legacy task ledger (exhaustive)

Every `<task>` in the retired baseline is mapped below; the baseline files are retained only as Git history and none is executable.

| Legacy task | Disposition | Destination / rationale |
|---|---|---|
| 42-01 T1 migration 0036 + sqlc | Superseded | 42-02 checkpoint schema/queries plus 42-08 rollout schema; scalar watermark schema cannot satisfy branch/CAS control. |
| 42-01 T2 marker/truncation/store | Superseded | 42-01 semantic manifests + 42-02 store/finalize; ordinary-user marker removed. |
| 42-01 T3 reconstruction | Superseded | 42-02 canonical branch reconstruction/LKG. |
| 42-02 T1 one-call summarizer/prompt | Superseded | 42-03 schema/validator/chunking/ledger. |
| 42-02 T2 prompt docs | Retained+hardened | 42-10 runbook and `prd.md`; authoritative lifecycle replaces activation note. |
| 42-03 T1 four knobs/clamp | Superseded | 42-01 capability/budget and 42-09 persisted effective state; static config disabled bootstrap only. |
| 42-04 T1 Runner.Compact | Superseded | 42-03 coordinator + 42-05 decision ladder. |
| 42-04 T2 overflow hook | Superseded | 42-05 L2.4-before-L2.5 and bounded overflow. |
| 42-05 T1 CLI/composition | Retained+hardened | 42-07 CLI plus 42-09 live composition root. |
| 42-05 T2 REPL command | Retained | 42-07 unified coordinator surface. |
| 42-05 T3 Telegram command | Retained+hardened | 42-07 ownership/capability/localization. |
| 42-06 T1 GET API | Retained+hardened | 42-07 versioned history/preview API, no legacy wire-shape promise. |
| 42-06 T2 POST API | Retained+hardened | 42-07 coordinator mutation with authz/body/error gates. |
| 42-07 T1 web hooks | Retained+hardened | 42-07 versioned typed API hooks. |
| 42-07 T2 composer command | Retained+hardened | 42-07 common manual surface. |
| 42-07 T3 gauge/metrics | Superseded | 42-07 distinct semantic/L1/L2.5 UX and 42-10 acceptance. |

## Legacy prohibition ledger (exhaustive by baseline plan)

| Plan | All prohibitions disposition |
|---|---|
| 42-01 | Immutable originals retained (42-02); marker-on-wire removed (42-03 envelope); capture sequence becomes manifest/governance fences (42-02); L0 byte stability retained (42-02/03); file-size rule retained globally. |
| 42-02 | Const-only nine-section prompt superseded by versioned schema; persistence separation, usage preservation, English continuation, and bounded documentation changes are retained/hardened in 42-03/10 and `prd.md`. |
| 42-03 | No fifth floor knob, no cheap model, no fabricated env helper, no import cycle/logging remain compatibility constraints; static knobs are disabled bootstrap and persisted state is authoritative (42-09). |
| 42-04 | No goroutine, retry loop, worse fallback, in-finalize recapture, ladder regression, or behavior-changing relocation: all retained/hardened by 42-02 claims and 42-05 coordinator ladder. |
| 42-05 | No duplicated implementation, command-to-LLM forwarding, below-floor error, or bogus slash turn: retained by 42-07 common coordinator surfaces. |
| 42-06 | Legacy no-DTO/NewServer-shape restrictions are superseded where versioning requires it; owner gate, Runner mutation, bounded body, and sanitized errors are retained by 42-07. |
| 42-07 | Distinct semantics/glyph, correct versioned wire casing, real new tests, and loading/notice UX are retained/hardened by 42-07. |

## Unshipped legacy persistence inventory

| Artifact | Disposition | Destination |
|---|---|---|
| `0036_conversation_compactions.up/down.sql` legacy scalar schema | Removed as executable proposal | 42-02 owns additive `0036_compaction_checkpoints`; 42-08 owns additive `0039_compaction_rollout`. |
| legacy `conversation_compactions` sqlc queries/generated methods | Removed | 42-02 `compaction.sql` branch/manifest/claim/CAS queries. |
| four `AURA_COMPACT_*` schema/config fields | Superseded | 42-01 capability registry + 42-09 durable effective snapshot; compatibility reader may accept old env but cannot activate. |
| ordinary-user summary marker and latest-by-time query | Removed | 42-02 active pointer + 42-03 internal envelope. |

Closure proof: 16/16 legacy tasks, 7/7 prohibition groups (covering every individual baseline bullet), and every proposed migration/query/config/wire artifact have a disposition and destination. Therefore no executable legacy item survives unmapped.

| ID | Capability |
|---|---|
| IC-01 | Provider capabilities and exact fail-closed budget model |
| IC-02 | Semantic-unit selection, recent tail, and L1 typed editing |
| IC-03 | L2.4-before-L2.5 budget-decision seam |
| IC-04 | Distributed claim, idempotency, and CAS finalization |
| IC-05 | Branch-aware manifest checkpoints and deterministic reconstruction |
| IC-06 | Structured safe summarization and authority ledger |
| IC-07 | Manual, lifecycle, proactive, and overflow coordinator |
| IC-08 | Typed content parts and artifact durability |
| IC-09 | Recursive generations and hierarchical rebase |
| IC-10 | Durable memory projection with privacy lifecycle |
| IC-11 | Last-known-good recovery, preview, restore, and quarantine |
| IC-12 | CLI, REPL, Telegram, AG-UI, and web operations UX |
| IC-13 | Redacted observability, numerical evaluation, and staged rollout |
| IC-14 | Migration compatibility, documentation, and terminal acceptance |
