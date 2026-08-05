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
>
> **How anything here gets marked done — read this before checking a box.** Every
> requirement below is verified by talking to the running Aura and reading what she did.
> Not a unit test. Not an integration test. Not a smoke test. A green suite is not
> evidence and never closes a box (**ACC-01**). The reason is in the audit that produced
> this milestone: the replay defect that made her act on a stale result, report the wrong
> conclusion, and write that conclusion into her own memory as fact was invisible to the
> entire test suite. It surfaced because a human watched a real conversation go wrong.

## v1 Requirements

### Harness correctness

- [ ] **HARN-01**: A mutating tool call re-issued in the same turn with identical arguments executes again and returns a fresh result, never a recorded one
- [ ] **HARN-02**: A genuinely retried dispatch (CLI or scheduler restart, approval resume) still executes at most once
- [ ] **HARN-03**: When a replay is correct, the returned result is labelled as replayed so the model can tell it apart from a fresh execution
- [ ] **HARN-04**: A memory correction closes exactly the fact it names and leaves sibling facts sharing the same subject and predicate untouched
- [ ] **HARN-05**: Several memory operations apply as one atomic call, validated on the final state, so a correction cannot destroy what it was meant to replace
- [ ] **HARN-06**: A turn does not end on a stated-but-unexecuted intention — either the action runs, or the turn says plainly it did not and why
- [ ] **HARN-07**: The reply is in the operator's language, and internal deliberation never reaches it as user-facing text
- [ ] **HARN-08**: Two tool calls arriving in one assistant message with the same id are repaired **deterministically** before the request is sent (`<id>_d<n>`, never a random id, which would break prompt-cache prefix stability). Aura has no such repair today and DeepSeek — her default model — is named by hermes as a provider that rejects duplicate ids outright; a degraded long-context turn emitting two calls under one id fails the request
- [ ] **HARN-09**: Identical `(name, arguments)` calls are distinguished by **where they arrive**, not by their provider-supplied id: two in the *same* assistant message are a model error and only the first runs; two in *different rounds* are a deliberate re-issue and both run. This is the discrimination F-1 lacks — `[058]` and `[062]` in the audit were separate rounds with an `fs_write` between them, and the second was served from the registry

### Tool surface

- [ ] **TOOL-01**: Exactly **14 tools** are loaded in the model's manifest. A hard cap, not a target:

  | # | Tool | Covers |
  |---|------|--------|
  | 1 | `text_response` | the loop's only terminal |
  | 2 | `ask_user` | pause, clarify, approve |
  | 3 | `shell_exec` | terminal, background via param |
  | 4 | `fs_read` | |
  | 5 | `fs_write` | write + exact-string edit |
  | 6 | `fs_search` | find by name + search contents |
  | 7 | `document` | search + open |
  | 8 | `memory` | recall + atomic write batch |
  | 9 | `web` | search + fetch |
  | 10 | `skill` | list + view + linked files |
  | 11 | `skill_manage` | create, patch, delete, install |
  | 12 | `task` | schedule, list, cancel |
  | 13 | `delegate` | swarm |
  | 14 | `comms` | calendar, mail, contacts, WhatsApp |
  | + | `tool_search` | **infrastructure — mandatory.** Without it the deferred tail is unreachable; hermes states the rule as *"Core tools are never deferred. Always-load means always-load. No exceptions."* |

  So **15 loaded**, not 14: fourteen domain tools plus the one that reaches everything else.
  `read_tool_output` is loaded too until TOOL-13 deletes it, briefly making 16 — the same count
  Claude Code ships, whose 16 likewise include plumbing (`Task`, `BashOutput`, `KillBash`).

  Everything else is deferred behind `tool_search`. Four tools leave the model's surface
  entirely because the host takes the step over: `send_file` (AUTO-01), `document_index`
  and `document_describe` (AUTO-02), and `current_time` (already injected in the volatile
  block). Reference point: Claude Code ships 16 loaded tools and no deferred pattern at
  all — but those 16 cover coding alone, while these 14 must also carry documents, memory,
  comms, scheduling and delegation. The budget is stricter than the comparison suggests
  and has no slack
- [ ] **TOOL-02**: The model is never asked to supply a parameter the host overwrites and discards
- [ ] **TOOL-03**: A withheld destructive action is raised and resolved by the host; the model never relays a resume payload. **Carve-out:** the swarm relay fields (`proxied_from_child_id`, `proxied_tool_call_id`) stay — a headless worker has already returned its report by the time the parent relays its question, so which worker asked is knowledge only the parent holds. Removing them would leave a worker's question unattributed or undeliverable
- [ ] **TOOL-04**: Scheduling a task takes one natural-language `when` instead of five mutually exclusive time fields
- [ ] **TOOL-05**: Recalling memory takes one question; the host chooses graph traversal or hybrid search and reports which it used
- [ ] **TOOL-06**: Loading a skill's content **is** using it — the read-without-applying action does not exist. Hermes exposes a single `skill_view`; Aura's `info`/`use` pair differs only by a wrapper sentence, and that cosmetic split cost ~4k duplicated tokens in the audited session when she read `docx` and `xlsx` through one verb and then re-read both through the other
- [ ] **TOOL-07**: Skill *use* is separated from skill *lifecycle* (create, update, delete, install, snippets), so the frequent path stays small
- [ ] **TOOL-08**: Indexing a produced file accepts its description in the same call
- [ ] **TOOL-09**: Web search drops the parameters inferable from the query itself
- [ ] **TOOL-10**: A fetch that returns a bot-block, consent wall, or empty extraction is reported as a failed read, never handed back as if it were the page
- [ ] **TOOL-12**: A deferred tool whose exact name the model can already see is loaded without spending a search first — the listing makes the name visible, so discovery and loading are not forced into two round trips
- [ ] **TOOL-13**: `read_tool_output` stops occupying a loaded slot. Paging a large output should use the file tool that already exists — **neither reference harness has a bespoke paging tool**: hermes writes the spill to a file (*"the model can `read_file` to access the full output"*) and Claude Code's 16 tools have none either (`Read` takes `offset`/`limit`; `BashOutput` is for live background shells, not completed results). Two independent harnesses, no third way.

  **Deleting it outright is the goal, and it is not trivial — three sub-problems, to be sized before committing to the full form:**
  1. **Filesystem boundary.** Sidecars live host-side in `AURA_RUN_DIR` (absolute, swept on a timer); `fs_read` reads inside the sandbox container under `/workspace`. The spill must land somewhere the sandboxed reader can reach.
  2. **Description debt.** `fs_read`'s own text says *"A large result pages to a sidecar you read with read_tool_output"* — every description naming the tool has to be rewritten with it (SURF-02 territory).
  3. **GC contract.** `AURA_RUN_DIR` is swept and `reserve.go`'s `resultExpiredMarker` depends on that. A spill in the persistent `/workspace` either accumulates in the operator's own space or needs its own sweep.

  **Fallback if (1) proves expensive: keep the tool but make it deferred rather than loaded.** That recovers the manifest slot — the actual scarce resource under TOOL-01 — at near-zero cost, and paging a result is rare enough that one search round trip is acceptable. The L1 eviction pointer keeps working unchanged
- [ ] **TOOL-14**: The PRD amendment ratifying the tool-surface tier change is committed **before** any of its code. It must (a) supersede amendment **A4** (2026-05-30, `prd.md:751,783`) by name, which declares `read_tool_output` a *"Builtin non-deferred"* with a byte-paging contract that TOOL-13 changes; (b) extend amendment **#44** (`prd.md:1371`), which already ratified `sandbox_exec` as *"non-deferred di proposito"* because a live E2E showed the model cramming a whole command line into one argument when it could not see the schema — *"il modello DEVE vedere lo schema"*; and (c) change the tiering axis stated at `prd.md:154` from **size** (*"Tool grandi → Deferred: true"*) to **frequency plus a hard count budget**. Covers TOOL-01, TOOL-13 and MCP-04. This is an extension of ratified reasoning, not a reversal of it
- [ ] **TOOL-11**: Finding files by name and searching their contents are one tool (`target: files | content`). **This reverses an earlier decision in this document.** The research established that five of six vendor harnesses — Claude Code included — keep them separate, and on that evidence the merge was parked pending telemetry. TOOL-01's hard cap overrules it: those vendors spend 16 slots on coding alone and can afford two, while Aura must fit documents, memory, comms, scheduling and delegation into the same budget. The cap forces the merge; the vendor evidence still says it is the weaker shape, and that tension is recorded rather than resolved

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
- [ ] **MCP-04**: Calendar, mail, contacts and WhatsApp are reachable through **one** curated `comms` surface — a single loaded slot replacing 28 raw third-party tools, **always loaded, never deferred**. Skills take two of the fourteen slots and connected accounts take one, deliberately: hermes calls skills *"your procedural memory — reusable approaches for recurring task types"*, and Aura extends herself many times a day where she consults a calendar occasionally. Collapsing 28 to a handful is what makes keeping them in front of the model affordable; the two are one move in two steps, not alternatives. Undeferring the 28 raw tools instead would put ~56 definitions in every turn's manifest, which is the exact configuration Aura already measured at ~20k tokens/turn and recorded as the point where tool-choice accuracy collapses (`llm_agent_promote.go:88-93`). Reference point: Claude Code ships **16 tools, all loaded, no deferred pattern** — every one a curated surface rather than an endpoint wrapper
- [ ] **MCP-05**: `accountId` is resolved host-side from the operator's configuration, like `user_identifier`

### Context management

- [ ] **CTX-01**: The context budget decision uses the provider's real reported token count, not a foreign-tokenizer estimate
- [ ] **CTX-02**: A `tool_search` result whose schemas were later re-loaded becomes evictable, so repeated lookups stop accumulating permanently
- [ ] **CTX-03**: A pruned skill body leaves a marker, so the model can tell a skill's instructions are gone rather than believing it still holds them
- [ ] **CTX-04**: The operator can see what is consuming the context window by category, not only how full it is
- [ ] **CTX-05**: Reasoning traces never reach a summarizer or fact extraction, so scratch-work conclusions cannot be preserved as facts
- [ ] **CTX-06**: A spike measures, on real exported conversations with known-correct answers, whether retrieval over indexed history recovers what the ladder drops — against summarization, and against both — and its result decides which ships
- [ ] **CTX-07**: When the context is over threshold and cannot be reduced, the reason is stated rather than the session simply failing or silently degrading
- [ ] **CTX-08**: Tool output is bounded **per turn**, not only per result. After a round's results are collected, if their total exceeds the turn budget the largest are spilled to disk until it is under. Aura caps each result (`AURA_CONTEXT_PREVIEW_CAP_BYTES`) and has no aggregate: ten medium results in one parallel batch each clear the per-result cap and still overflow the turn — the exact shape of a swarm fan-out or a wide multi-tool round. Hermes calls this the third of three defenses and sets it at 200K chars

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
- [ ] **SURF-06**: A skill's references, templates and scripts are fetched **through the skill tool itself** — the first call returns the skill body plus a `linked_files` index, and a second call naming one of those paths returns it. The model never learns where skills are mounted because it never needs to (hermes `skill_view` pattern). This is what F-6 actually wanted: `ls /skills/` was a workaround for a tool that would not hand back its own attachments
- [ ] **SURF-07**: Each preinstalled skill carries the operational detail its family needs — no skill is a stub beside siblings that are full playbooks
- [ ] **SURF-08**: The deferred roster lists every deferred capability **by name**, and carries three statements hermes found necessary: that tools already loaded need no search; that a name appearing in the list must not be reported as unavailable; and that a generic tool — the terminal above all — must not be substituted for a specific one without searching first. The third is the exact failure of audit turn `[054]`, where she reached for `shell_exec` because the specific tool was not in front of her. The roster degrades deterministically when it will not fit — full listing, then names only, then one line per server — sorted so a single oversized server cannot cost a small one its listing

### Delegation

<!-- Hermes parity. Aura's swarm today: one goal string per worker, parent blocks, workers
     cannot delegate further (their registry is Without(reg, "swarm_spawn")). -->

- [ ] **SWARM-01**: A worker brief separates *what to accomplish* from *the context it needs* — file paths, error messages, constraints — instead of forcing both into one string
- [ ] **SWARM-02**: The model sees the operator's actual concurrency and depth limits in the tool schema, rather than discovering them by failing
- [ ] **SWARM-03**: A top-level delegation returns the turn immediately; its results re-enter the conversation when the work finishes, and the model cannot opt out of this
- [ ] **SWARM-04**: A delegation issued *by a worker* runs synchronously — an orchestrating worker needs its own workers' results inside its own turn
- [ ] **SWARM-05**: A worker can itself orchestrate, bounded by the configured depth — opening the nesting the PRD designed and the current registry-minus-`swarm_spawn` implementation forecloses
- [ ] **SWARM-06**: A worker that needs the operator reaches them, attributed to the worker that asked — the relay survives the TOOL-03 approval rework
- [ ] **SWARM-07**: Concurrent workers writing durable facts (AUTO-03) and reasoning traces (MEM-03) into one identity's graph neither corrupt nor duplicate, and each write names the worker that made it
- [ ] **SWARM-08**: Workers reason over the same flattened tool surface the parent does, verified after the un-defer rather than assumed from registry inheritance
- [ ] **SWARM-09**: Delegated work is durable — a task survives a process restart, is claimable from Postgres, and is never silently lost nor silently retried. Implements the approved-but-unbuilt [durable swarm messaging design](../docs/superpowers/specs/2026-06-29-durable-swarm-messaging-design.md); SWARM-03's background delegation is this substrate's first consumer, not a parallel mechanism
- [ ] **SWARM-10**: The operator (and the parent) can watch a worker work — a tail-able live transcript per child, rather than waiting blind for the consolidated report
- [ ] **SWARM-11**: The PRD amendment ratifying the durable swarm substrate is committed **before** any of its code

### Steering

<!-- Implements the design study at docs/superpowers/specs/2026-07-23-mid-turn-steering-design.md
     (study only, no code, no amendment yet). Its own finding: the loop already injects
     user-role messages mid-run on three paths, so a steer is a fourth instance of an
     existing pattern. -->

- [ ] **STEER-01**: The operator can type into a running turn; the message is injected at the next round boundary as ordinary user input, never interrupting a tool mid-execution
- [ ] **STEER-02**: A steer does not extend the step or wallclock budget — steering redirects the work, it does not buy more of it
- [ ] **STEER-03**: A steer is echoed on the wire and persisted where it belongs in sequence, so a reload, a resume, or a later replay shows it at the point it actually landed
- [ ] **STEER-04**: A steer that arrives after its run has ended is returned to the operator to re-send as a normal turn, never silently swallowed
- [ ] **STEER-05**: Steering works from the operator's channels, not only the cockpit
- [ ] **STEER-06**: The PRD amendment ratifying mid-turn steering is committed **before** any of its code

### Acceptance

- [ ] **ACC-01**: **Mandatory, every requirement, no exceptions.** A requirement is verified only by a real conversation with the running Aura, scored on the answer she actually gave and the artifact or state she actually produced. A passing test — unit, integration, race, or smoke — is **not** evidence that a requirement is met. Tests keep the code honest; they say nothing about whether the agent behaves. Any requirement whose only evidence is a green suite is **not done**
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
- **TOOL-V2-03**: **The three-tool bridge** — `tool_search` + `tool_describe` + `tool_call`, where a deferred tool is invoked THROUGH the bridge and never enters the manifest at all. Hermes' model; it makes the manifest constant regardless of catalog size (they carry ~3,300 Cloudflare tools whose names alone are ~32K tokens) and dissolves the promotion machinery outright — `activated`, `everLoaded`, `maxPromotedDeferredTools`, `promoteFromMeta`, `deriveActivated` all become dead code, and with them F-3's manifest-versus-callability confusion and the catalog-drift class hermes warns about (*"a session-keyed catalog that drifts out of sync with the live tool registry produces silent tool dropouts"* — which is what `deriveActivated` is).
  **Deferred deliberately, not overlooked.** Two reasons: Aura's gateway classifies risk by tool NAME and fails closed, and the idempotency key is built from `tools.Spec` + args — behind a bridge both must unwrap, which inserts a step into precisely the two code paths Phase 45 is fixing bugs in. And the benefit scales with catalog size: after the `comms` facade Aura's deferred tail is ~10-15 tools, not thousands.
  **Note it would NOT fix F-4** — `tool_describe` results accumulate in the transcript exactly as `tool_search` results do today; that is CTX-02's job either way.
  **Trigger to reopen:** the deferred tail passes ~30 tools, or a self-extension-minted MCP server mounts a surface of Cloudflare's order.

## Out of Scope

| Feature | Reason |
|---------|--------|
| ~~Merging `fs_glob` + `fs_grep`~~ | **Reversed 2026-08-05** — promoted to TOOL-11. The vendor evidence against it stands; TOOL-01's 14-slot cap overrules it anyway |
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
| ACC-01 | Phase 45 | Pending |
| ACC-02 | Phase 45 | Pending |
| ACC-03 | Phase 54 | Pending |
| AUTO-01 | Phase 47 | Pending |
| AUTO-02 | Phase 47 | Pending |
| AUTO-03 | Phase 49 | Pending |
| AUTO-04 | Phase 48 | Pending |
| COMPAT-01 | Phase 48 | Pending |
| COMPAT-02 | Phase 47 | Pending |
| COMPAT-03 | Phase 48 | Pending |
| CTX-01 | Phase 50 | Pending |
| CTX-02 | Phase 50 | Pending |
| CTX-03 | Phase 50 | Pending |
| CTX-04 | Phase 50 | Pending |
| CTX-05 | Phase 49 | Pending |
| CTX-06 | Phase 53 | Pending |
| CTX-07 | Phase 50 | Pending |
| CTX-08 | Phase 50 | Pending |
| HARN-01 | Phase 45 | Pending |
| HARN-02 | Phase 45 | Pending |
| HARN-03 | Phase 45 | Pending |
| HARN-04 | Phase 45 | Pending |
| HARN-05 | Phase 49 | Pending |
| HARN-06 | Phase 45 | Pending |
| HARN-07 | Phase 45 | Pending |
| HARN-08 | Phase 45 | Pending |
| HARN-09 | Phase 45 | Pending |
| MCP-01 | Phase 46 | Pending |
| MCP-02 | Phase 46 | Pending |
| MCP-03 | Phase 46 | Pending |
| MCP-04 | Phase 46 | Pending |
| MCP-05 | Phase 46 | Pending |
| MEM-01 | Phase 49 | Pending |
| MEM-02 | Phase 49 | Pending |
| MEM-03 | Phase 49 | Pending |
| MEM-04 | Phase 45 | Pending |
| MEM-05 | Phase 45 | Pending |
| MEM-06 | Phase 49 | Pending |
| STEER-01 | Phase 52 | Pending |
| STEER-02 | Phase 52 | Pending |
| STEER-03 | Phase 52 | Pending |
| STEER-04 | Phase 52 | Pending |
| STEER-05 | Phase 52 | Pending |
| STEER-06 | Phase 52 | Pending |
| SURF-01 | Phase 48 | Pending |
| SURF-02 | Phase 47 | Pending |
| SURF-03 | Phase 47 | Pending |
| SURF-04 | Phase 50 | Pending |
| SURF-05 | Phase 54 | Pending |
| SURF-06 | Phase 48 | Pending |
| SURF-07 | Phase 48 | Pending |
| SURF-08 | Phase 48 | Pending |
| SWARM-01 | Phase 51 | Pending |
| SWARM-02 | Phase 51 | Pending |
| SWARM-03 | Phase 51 | Pending |
| SWARM-04 | Phase 51 | Pending |
| SWARM-05 | Phase 51 | Pending |
| SWARM-06 | Phase 51 | Pending |
| SWARM-07 | Phase 51 | Pending |
| SWARM-08 | Phase 51 | Pending |
| SWARM-09 | Phase 51 | Pending |
| SWARM-10 | Phase 51 | Pending |
| SWARM-11 | Phase 51 | Pending |
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
| TOOL-11 | Phase 48 | Pending |
| TOOL-12 | Phase 48 | Pending |
| TOOL-13 | Phase 50 | Pending |
| TOOL-14 | Phase 46 | Pending |

**Coverage:**
- v1 requirements: 77 total
- Mapped to phases: 77
- Unmapped: 0 ✓

---
*Requirements defined: 2026-08-05*
*Last updated: 2026-08-05 — roadmap created (Phases 45-52); traceability populated, 52/52
mapped; corrected the v1 requirement count from 51 to 52*
