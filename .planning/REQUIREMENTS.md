# Requirements: Aura — v2.1.0 HERMES-CLAUDE_PARITY

**Defined:** 2026-08-05
**Core Value:** When Aura says she did something, she did it — and she can find what she knew.

> **Where these come from.** Two live sessions of 2026-08-04 were exported from
> `aura.conversations` and audited ([`docs/audit/live-conversations-2026-08-04/`](../docs/audit/live-conversations-2026-08-04/)),
> then cross-checked against `D:/tmp/hermes-agent` and 30+ vendor agent prompts by four
> research agents (`.planning/research/`). Every requirement below traces to an observed
> defect or a cited reference, not to a preference.
>
> **The organising insight.** Aura's long-term memory holds nine `learned_lesson` facts.
> Eight are workarounds for a defective surface, and six restate rules the system prompt
> already contains. She wrote her own bug report. This milestone's job is to make those
> lessons unnecessary — see **ACC-03**.

## v1 Requirements

### Harness correctness

- [ ] **HARN-01**: A mutating tool call re-issued in the same turn with identical arguments executes again and returns a fresh result, never a recorded one
- [ ] **HARN-02**: A genuinely retried dispatch (CLI or scheduler restart, approval resume) still executes at most once
- [ ] **HARN-03**: When a replay is correct, the returned result is labelled as replayed so the model can tell it apart from a fresh execution
- [ ] **HARN-04**: A memory correction closes exactly the fact it names and leaves sibling facts sharing the same subject and predicate untouched
- [ ] **HARN-05**: Several memory operations apply as one atomic call, validated on the final state, so a correction cannot destroy what it was meant to replace
- [ ] **HARN-06**: A turn does not end on a stated-but-unexecuted intention — either the action runs, or the turn says plainly it did not and why
- [ ] **HARN-07**: The reply is in the operator's language, and internal deliberation never reaches it as user-facing text

### Tool surface

- [ ] **TOOL-01**: The tools needed on most turns are callable without spending a `tool_search` round trip first
- [ ] **TOOL-02**: The model is never asked to supply a parameter the host overwrites and discards
- [ ] **TOOL-03**: A withheld destructive action is raised and resolved by the host; the model never relays a resume payload
- [ ] **TOOL-04**: Scheduling a task takes one natural-language `when` instead of five mutually exclusive time fields
- [ ] **TOOL-05**: Recalling memory takes one question; the host chooses graph traversal or hybrid search and reports which it used
- [ ] **TOOL-06**: Applying a skill is one call — reading a skill without applying it is no longer a separate action
- [ ] **TOOL-07**: Skill *use* is separated from skill *lifecycle* (create, update, delete, install, snippets), so the frequent path stays small
- [ ] **TOOL-08**: Indexing a produced file accepts its description in the same call
- [ ] **TOOL-09**: Web search drops the parameters inferable from the query itself
- [ ] **TOOL-10**: A fetch that returns a bot-block, consent wall, or empty extraction is reported as a failed read, never handed back as if it were the page

### Automation

<!-- Directive: "simplify AND automatize more". A step the host can take is not the model's to remember. -->

- [ ] **AUTO-01**: A file produced for the operator reaches them without the model having to remember to send it
- [ ] **AUTO-02**: A file the operator will need later becomes findable without a separate deliberate indexing step
- [ ] **AUTO-03**: A durable fact revealed during a turn is captured as part of doing the work — and never harvested from reasoning traces (see CTX-05)
- [ ] **AUTO-04**: Every parameter the host already knows is filled host-side across the whole surface, native and MCP alike, not just where it was noticed

### Compatibility

<!-- The tool-shape changes in TOOL-* and MCP-* land on a system with live persisted state. -->

- [ ] **COMPAT-01**: Conversation history rehydrated from turns recorded against an older tool schema still builds a valid request — an old `tool_calls` payload never produces a broken wire message or a crash
- [ ] **COMPAT-02**: A pause created against a previous `ask_user` shape either resumes correctly or fails loudly with an actionable message — never silently
- [ ] **COMPAT-03**: A scheduled `agent_job` created before the change still fires, resolving tools at fire time against the current surface

### MCP trust and facade

- [ ] **MCP-01**: MCP tool descriptions reach the model as ordinary text, without the untrusted-data wrapper
- [ ] **MCP-02**: Per-call result fencing and fail-closed risk classification for unknown tools remain in force and are proven by test
- [ ] **MCP-03**: Trust is unconditional across every mounted MCP server — those Aura ships, those added later, and those minted by her own self-extension alike. **Operator decision, 2026-08-05**, taken against the research recommendation to scope removal to code-reviewed recipes: the residual risk is carried by MCP-02's per-call result fencing and fail-closed risk classification, and by the operator's control over what gets mounted at all
- [ ] **MCP-04**: Calendar and WhatsApp are reachable through a curated surface instead of 28 raw third-party tools
- [ ] **MCP-05**: `accountId` is resolved host-side from the operator's configuration, like `user_identifier`

### Context management

- [ ] **CTX-01**: The context budget decision uses the provider's real reported token count, not a foreign-tokenizer estimate
- [ ] **CTX-02**: A `tool_search` result whose schemas were later re-loaded becomes evictable, so repeated lookups stop accumulating permanently
- [ ] **CTX-03**: A pruned skill body leaves a marker, so the model can tell a skill's instructions are gone rather than believing it still holds them
- [ ] **CTX-04**: The operator can see what is consuming the context window by category, not only how full it is
- [ ] **CTX-05**: Reasoning traces never reach a summarizer or fact extraction, so scratch-work conclusions cannot be preserved as facts
- [ ] **CTX-06**: A spike measures, on real exported conversations with known-correct answers, whether retrieval over indexed history recovers what the ladder drops — against summarization, and against both — and its result decides which ships
- [ ] **CTX-07**: When the context is over threshold and cannot be reduced, the reason is stated rather than the session simply failing or silently degrading

### Memory tiers

- [ ] **MEM-01**: Past conversation is semantically searchable, with Postgres remaining the system of record for turns and ArcadeDB holding a derived per-identity projection
- [ ] **MEM-02**: One retrieval call spans short-term conversation and long-term facts
- [ ] **MEM-03**: Reasoning traces are persisted to the graph with edges to the entities they touched, and enter context only when explicitly retrieved
- [ ] **MEM-04**: One person is one entity — the operator's profile and preferences do not split across a name and an identity UUID
- [ ] **MEM-05**: Recording a multi-valued fact does not create a junk entity node per distinct value
- [ ] **MEM-06**: The PRD amendment extending #91 (reasoning persisted to the graph, retrieved only on demand, never summarized or harvested) is committed **before** any reasoning-tier code

### Surface legibility

- [ ] **SURF-01**: The system prompt names only tools that are actually loaded, and is regenerated as part of the un-defer rather than after it
- [ ] **SURF-02**: Per-tool operational rules live in that tool's description, arriving with its schema at the moment of use
- [ ] **SURF-03**: Files produced but not yet delivered or indexed are surfaced next to the turn, not left for the model to remember
- [ ] **SURF-04**: The model can tell that memory was already injected, and that a tool dropped from the manifest is still callable
- [ ] **SURF-05**: The obsolete `learned_lesson` facts and the `always-deliver-files` skill are retired once the defects they compensate for are fixed
- [ ] **SURF-06**: A skill's bundled scripts are reachable from what the skill tool returns, without discovering the mount point by hand
- [ ] **SURF-07**: Each preinstalled skill carries the operational detail its family needs — no skill is a stub beside siblings that are full playbooks

### Acceptance

- [ ] **ACC-01**: Every phase is validated by a real scenario run against the live stack and scored on the response and the artifact produced — a green test suite is not evidence of completion
- [ ] **ACC-02**: Phase evidence is read from OpenTelemetry traces, `aura.tool_invocations`, `aura.conversation_turns` and `aura.context_rot_events` — no new eval harness is built
- [ ] **ACC-03**: **Milestone exit gate.** With the nine `learned_lesson` facts and the `always-deliver-files` skill deleted, replaying the audited scenarios produces correct tool choice, automatic delivery and successful self-retrieval — and Aura does not re-learn any retired lesson

## v2 Requirements

Deferred. Tracked, not in this roadmap.

### Context

- **CTX-V2-01**: LLM summarization rung with anti-thrash, cooldown and deterministic fallback — **promoted to v1 only if CTX-06's spike selects it**; STACK.md confirms it needs no new dependencies whichever way the spike lands
- **CTX-V2-02**: Durable cross-restart anti-thrash state (needs a migration; in-memory is the simpler default)

### Tool surface

- **TOOL-V2-01**: Merge `fs_glob` and `fs_grep` — **blocked on telemetry**, see Out of Scope
- **TOOL-V2-02**: Provider reasoning-block replay for models that require it for multi-turn tool use (not needed by DeepSeek via OpenRouter today)

## Out of Scope

| Feature | Reason |
|---------|--------|
| Merging `fs_glob` + `fs_grep` now | Five of six vendor harnesses keep name-search and content-search separate; only hermes merges. The tool name is a zero-cost disambiguator. Aura has one local reason to want it (the 10-slot promotion cap) — measure that in real sessions first |
| Restoring `internal/eval/` | It was broken, which is why it was deleted. OTel, `tool_invocations` and `conversation_turns` already carry the evidence (ACC-02) |
| `make_document` routing tool | The friction it was meant to fix was the F-1 replay bug; fixing the harness removes the need |
| `remind_me` / `remember` as new wrapper tools | Delivered by flattening `task` and `memory_recall` in place (TOOL-04, TOOL-05), so there stays one obvious way to do it |
| Moving conversation persistence off Postgres | RLS fail-closed, branch replay, token/USD aggregation and rot events all live there. ArcadeDB gets a derived projection (MEM-01), never the source of truth |
| Reasoning in the default context window | Amendment #91's budget rationale holds — hermes measured 27% of a 214-turn payload sitting in reasoning blobs. MEM-03 keeps retrieval explicit |
| Un-deferring every tool | Anthropic's guidance and Aura's own measured 56-definition / ~20k-token incident both cap the useful manifest. The long tail stays deferred |
| Cockpit surfaces for the new signals | CTX-04 and the memory tiers are agent-facing first. A UI pass is a separate milestone unless the operator asks otherwise |

## Fix-on-touch

Not requirements — corrections to make in whatever phase touches the file (CLAUDE.md: *fix on touch, never skip*).

| Item | Detail |
|------|--------|
| `internal/toolinvocations/redact.go:23` | Documents `AURA_CONTEXT_PREVIEW_CAP_BYTES=2048`; the real default in `config_knobs.go:98` is `30000` — 14× off, in a comment guiding redaction logic |

## Traceability

Populated during roadmap creation (`.planning/ROADMAP.md`, Phases 45-52).

| Requirement | Phase | Status |
|-------------|-------|--------|
| HARN-01 | Phase 45 | Pending |
| HARN-02 | Phase 45 | Pending |
| HARN-03 | Phase 45 | Pending |
| HARN-04 | Phase 45 | Pending |
| HARN-05 | Phase 49 | Pending |
| HARN-06 | Phase 45 | Pending |
| HARN-07 | Phase 45 | Pending |
| TOOL-01 | Phase 48 | Pending |
| TOOL-02 | Phase 47 | Pending |
| TOOL-03 | Phase 47 | Pending |
| TOOL-04 | Phase 48 | Pending |
| TOOL-05 | Phase 49 | Pending |
| TOOL-06 | Phase 48 | Pending |
| TOOL-07 | Phase 48 | Pending |
| TOOL-08 | Phase 47 | Pending |
| TOOL-09 | Phase 47 | Pending |
| TOOL-10 | Phase 47 | Pending |
| AUTO-01 | Phase 47 | Pending |
| AUTO-02 | Phase 47 | Pending |
| AUTO-03 | Phase 49 | Pending |
| AUTO-04 | Phase 48 | Pending |
| COMPAT-01 | Phase 48 | Pending |
| COMPAT-02 | Phase 47 | Pending |
| COMPAT-03 | Phase 48 | Pending |
| MCP-01 | Phase 46 | Pending |
| MCP-02 | Phase 46 | Pending |
| MCP-03 | Phase 46 | Pending |
| MCP-04 | Phase 46 | Pending |
| MCP-05 | Phase 46 | Pending |
| CTX-01 | Phase 50 | Pending |
| CTX-02 | Phase 50 | Pending |
| CTX-03 | Phase 50 | Pending |
| CTX-04 | Phase 50 | Pending |
| CTX-05 | Phase 49 | Pending |
| CTX-06 | Phase 51 | Pending |
| CTX-07 | Phase 50 | Pending |
| MEM-01 | Phase 49 | Pending |
| MEM-02 | Phase 49 | Pending |
| MEM-03 | Phase 49 | Pending |
| MEM-04 | Phase 45 | Pending |
| MEM-05 | Phase 45 | Pending |
| MEM-06 | Phase 49 | Pending |
| SURF-01 | Phase 48 | Pending |
| SURF-02 | Phase 47 | Pending |
| SURF-03 | Phase 47 | Pending |
| SURF-04 | Phase 50 | Pending |
| SURF-05 | Phase 52 | Pending |
| SURF-06 | Phase 48 | Pending |
| SURF-07 | Phase 48 | Pending |
| ACC-01 | Phase 45 | Pending |
| ACC-02 | Phase 45 | Pending |
| ACC-03 | Phase 52 | Pending |

**Coverage:**
- v1 requirements: 52 total (corrected from the 51 stated at requirements-definition time — a
  direct count of `- [ ] **<ID>**` lines in this file returns 52 unique requirement IDs across
  the 9 categories; the count below was a miscount at definition time, not a scope change)
- Mapped to phases: 52
- Unmapped: 0 ✓

---
*Requirements defined: 2026-08-05*
*Last updated: 2026-08-05 — roadmap created (Phases 45-52); traceability populated, 52/52
mapped; corrected the v1 requirement count from 51 to 52*
