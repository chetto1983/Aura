# Feature Research: Tool Surface & Ceremony (v2.1.0 HERMES-CLAUDE_PARITY)

**Domain:** Agent-harness tool-surface design (granularity, ceremony parameters, approval
flows, batch shapes)
**Researched:** 2026-08-05
**Confidence:** HIGH for tool-granularity consensus and approval-flow patterns (cross-verified
across 6 independent vendor tool sets + Aura's own code); MEDIUM for the exact param-count
targets in `TOOL-SIMPLIFICATION.md` (one engineer's proposal, now checked against the reference
collection rather than re-derived from scratch).

## Method

Primary sources, read directly, cited by file:line or vendor+file:

- Aura: `docs/audit/live-conversations-2026-08-04/{TOOL-SIMPLIFICATION.md,FINDINGS.md}`,
  `internal/agent/mcptools/bridge_memory.go`, `internal/agent/tools/task.go`,
  `internal/agent/llm_agent_promote.go` (the `maxPromotedDeferredTools = 10` cap).
- Hermes (`D:/tmp/hermes-agent/tools/`): `file_tools.py` (`search_files`), `memory_tool.py`
  (batch `operations[]`, write-gate/stage pattern), `clarify_tool.py` (ask-user equivalent),
  `cronjob_tools.py` (scheduler).
- Vendor tool schemas (`D:/tmp/system-prompts-and-models-of-ai-tools/`): Anthropic Claude Code
  (`Anthropic/Claude Code/Tools.json`), Amp (`Amp/claude-4-sonnet.yaml`), Cursor
  (`Cursor Prompts/Agent Tools v1.0.json`), Windsurf (`Windsurf/Tools Wave 11.txt`), Warp.dev
  (`Warp.dev/Prompt.txt`), Devin AI (`Devin AI/Prompt.txt`).

No web search was needed — the question is answerable entirely from on-disk primary sources,
which outrank speculation per the research brief.

---

## Q1 — Tool granularity: how many tools, and which merges are safe

### The headline finding: file-search granularity is a 5-0 vendor consensus, and it disagrees
### with `TOOL-SIMPLIFICATION.md` §B1

| Vendor | Name-pattern search | Content search | Merged? |
|---|---|---|---|
| Claude Code | `Glob` (2 params: pattern, path) | `Grep` (9 params: pattern, path, glob, output_mode, -A/-B/-C, -n, -i, type, head_limit, multiline) | **No** — plus a third tool `LS` for directory listing |
| Amp | `glob` (3 params) | `Grep` (4 params: pattern, path, glob, caseSensitive) | **No** — plus `list_directory` |
| Cursor | `file_search` (fuzzy filename) | `grep_search` (5 params) | **No** — plus `list_dir`, `codebase_search` (semantic) |
| Windsurf | `find_by_name` (7 params: Pattern, Excludes, Extensions, FullPath, MaxDepth, SearchDirectory, Type) | `grep_search` (6 params) | **No** — plus `list_dir` |
| Warp.dev | `file_glob` | `grep` | **No** |
| **Hermes** | — | — | **Yes** — `search_files{pattern, target: content\|files, path, file_glob, limit, offset, output_mode, context}` (8 params) |

Every reference vendor except Hermes keeps filename search and content search as two separate,
narrow tools. `TOOL-SIMPLIFICATION.md` §B1 proposes merging Aura's `fs_glob`+`fs_grep` into one
`fs_search{pattern, target, path, glob, max_results}`, citing Hermes as precedent. **This is the
one place the audit should be challenged, not adopted verbatim.**

**Why the split survives at scale, evidenced, not assumed:** in every non-Hermes vendor, the
*tool name itself* is the disambiguator (`grep` for content, `glob`/`find_by_name` for names) —
a zero-cost decision because the model's training data already binds `grep`→content and
`glob`/`find`→filenames as Unix muscle memory. Hermes' `target: content|files` enum moves that
same decision into a *parameter value* the model must still get right, but now it competes with
seven sibling parameters in one schema, and picking the wrong `target` for a given `pattern`
produces a confusing empty result rather than a wrong-tool error the model can immediately
diagnose. Windsurf's `grep_search` even explicitly forbids callers from expressing this as one
knob (`Includes` is glob-shaped but is *scoped to* `SearchPath`, never a mode switch).

**Why Aura is not simply "wrong to consider it," though:** Aura has one real, vendor-absent
constraint the split-tool vendors don't carry — `maxPromotedDeferredTools = 10`
(`internal/agent/llm_agent_promote.go:33`). Only the 10 most-recently-used deferred tools keep
a live schema in the manifest; the rest are still callable (F-3) but cost a `tool_search` round
trip to re-render. Two frequently-co-used tools (`fs_glob`, `fs_grep`) burn two of those ten
slots where Claude Code/Amp/Cursor have no such cap to protect. That is a legitimate,
Aura-specific reason to *want* the merge that none of the reference vendors need — but it should
be made explicit and weighed against the F-1-adjacent risk of a parameter-selection error,
rather than imported as "Hermes does it, so it's safe."

**Recommendation:** keep `fs_glob`/`fs_grep` as two separate tools (already minimal per audit
§D: `fs_glob` ~2 params, `fs_grep` ~4). If the 10-slot promotion cap is later shown to bind in
practice (both tools evicting something else useful within one turn), revisit the merge then,
with the `target` enum as the *first* declared property (mirroring how Windsurf/Cursor front-load
the highest-signal field) and a description no longer than Hermes' (~450 chars) — not a
Claude-Code-style multi-paragraph guide crammed into one schema, which would erase the token
savings the merge exists to capture. **Complexity: LOW to reverse if wrong** (a schema-only
change plus one dispatch branch), **HIGH cost of being wrong** (reintroduces an F-1-shaped
class of silent-mismatch failure into the single highest-call-volume tool pair in the harness).

### The general principle the audit doesn't state, extracted from the evidence

Tool-count vs. param-count is not a domain property, it's a **call-frequency** property:

- **High-frequency primitives** (file read/write/search, `ask_user`) — every vendor keeps these
  as *many narrow tools with few params*. The schema-selection cost is paid every single call,
  so it must be near-zero; splitting by name achieves that.
- **Low-frequency / administrative operations** (Hermes' `cronjob`: 14 top-level properties;
  Hermes' `memory`: fewer live params but an optional batch array) — vendors are comfortable with
  *one broad tool, many optional params*. The schema cost is paid once per manifest render, not
  once per call, so extra optional fields are cheap and a second tool would just be more manifest
  weight for a rarely-exercised surface.

This reframes `TOOL-SIMPLIFICATION.md` §C2 (`skill`, 10 actions/8 params) correctly, but for a
sharper reason than raw counting: the defect isn't "too many actions," it's that `skill` mixes a
**high-frequency action** (`use`) with a **low-frequency cluster** (`create`/`update`/`install`/
`archive`/`save_snippet`/`restore`) inside one schema — no reference vendor mixes "invoke a
capability" and "author a capability" in the same tool at all (none of Claude Code, Amp, Cursor,
Windsurf even expose skill/capability *authoring* as a model-facing tool). §C2b's split
(`skill{action: use|list}` frequent + `skill_manage{...}` rare-and-verbose) is the correct fix
under this principle, not just a token-count trim. **Complexity: MEDIUM** (two registry
registrations, existing handler logic mostly moves without changing).

The same principle validates dropping `info` (§C2a): reading a skill body and using it put
identical tokens in context, so having two actions for one effect is pure ceremony regardless of
frequency tier.

### `task`'s 11 params is a different defect class than raw param count

Hermes' `cronjob` has *more* top-level properties (14) than Aura's `task` (11,
`internal/agent/tools/task.go:110-122`: `action, schedule_kind, cron, at, every_minutes, tz,
kind, payload, step_budget, notify, task_id`) and is not flagged as overloaded in its own
codebase — because Hermes' extra params are independently optional, not mutually exclusive.
Aura's defect is specifically that `schedule_kind` forces a branching choice among four other
fields (`cron`/`at`/`every_minutes`/`tz`) that are meaningful only in combination — a schema
shape that doesn't match the actual decision space. Hermes' fix, already shipped and directly
applicable: one string field, `schedule: "30m" | "every 2h" | "0 9 * * *" | ISO-8601`, parsed
server-side (`cronjob_tools.py:1063`). `TOOL-SIMPLIFICATION.md` §C1's `when` field is the same
move and is well supported by this precedent — not "fewer params for its own sake," but
"collapse a branching/mutually-exclusive parameter cluster into one natural-language-parseable
field." **Complexity: MEDIUM** — needs a small natural-language date/recurrence parser in Go
(`internal/agent/tools/task.go` + a new parser package); the cron/RFC-3339 fallback paths are
mechanical.

### `document_index` + `document_describe` merge (§B2) — agrees with evidence

This is the one merge in the audit that *is* well supported: the two calls are always
sequential, always target the same just-created object, and no split-tool vendor's Read/Glob
split applies here because there is no alternate mental model competing for the "target" —
`document_describe` is definitionally metadata-for-the-thing-you-just-indexed, closer to Claude
Code's own `Read` tool folding pagination and structured-extraction into one call than to the
Grep/Glob split. **Complexity: LOW.**

---

## Q2 — Ceremony parameters: model-supplied vs. host-injected

**Table stakes, strongly cross-verified:** identity/tenancy/billing-sensitive parameters that
the model could type but the host must not trust are host-injected, never model-facing, in every
system studied:

- Aura's own `withMemoryUserIdentifier` (`internal/agent/mcptools/bridge_memory.go:45-58`)
  already does this correctly at the bridge layer — it unconditionally overwrites
  `args["user_identifier"]` with the real identity from `identityctx.IdentityID(ctx)`. The bug
  is purely that the schema still *declares* the field `required`, so the model spends 36
  characters typing a value that is thrown away on every one of the four memory tools
  (`memory_recall`, `memory_upsert_fact`, `memory_forget`, `memory_merge_entities`).
- Hermes' `cronjob` schema handler explicitly documents the same pattern in the opposite
  direction, as a *security* control rather than a token-economy one: `model` / `provider` /
  `base_url` are declared in the Python function signature but the registry handler
  (`cronjob_tools.py:1176-1180`) deliberately does **not** read them from `args` — "per-job
  inference pins are user-owned … the agent must not be able to point unattended spend at a
  different model." This is the same host-injection discipline Aura needs on
  `user_identifier`, just applied to a different sensitive field, which raises the bar from
  "nice token savings" to "established pattern for keeping spend/identity-routing parameters out
  of model hands."
- Hermes' `_validate_cron_base_url` goes further: even where a *user-facing* CLI can set
  `base_url`, the model-facing path is validated so a stored credential can never be routed to an
  attacker-controlled host — directly analogous to why `user_identifier` must never be
  model-supplied even as a default: a compromised or confused model turn could otherwise target
  another tenant's memory.

**Fix for Aura, per the audit, confirmed correct:** drop `user_identifier` from the four
model-facing schemas; keep the unconditional bridge injection exactly as-is.
**Complexity: LOW** (schema-only edit in 4 places; `bridge_memory.go` logic is untouched —
`acceptsUserIdentifier` already fails safe by returning `true` when the property is absent from
an unparseable schema, so removing the property is backward-compatible with the injection code).

**`ask_user`'s `resume_context`/`proxied_from_child_id` ceremony (§A2/A3):** no reference vendor
makes the model re-type or relay a value the harness itself produced in the same turn. The
closest parallel is Cursor/Windsurf's `run_terminal_cmd`: the model supplies only the command; it
never sees or forwards an approval token (see Q3). Aura's `args_sha256`/`conversation_id`/
`request_id`/`tool_call_id`/`tier` blob is unique to the reference collection in requiring the
model to carry state across a round trip — every other design either resolves the state
host-side synchronously (Cursor) or stages it host-side for out-of-band resolution (Hermes
memory gate, see Q3). **This is unambiguously ceremony, not a genuine model decision, and should
be removed exactly as §A2 proposes.** **Complexity: MEDIUM** — requires the gateway to retain
the pending approval keyed by something the harness already has (`tool_call_id`/reservation key)
and resume the *same* tool call after approval, rather than routing the resume through a second
`ask_user` invocation. This is the harder half of the fix (it touches the approval/resume state
machine in `internal/gateway`), not the schema trim.

**`clarify_tool` corroborates the target shape for `ask_user`:** Hermes' equivalent is
`clarify{question, choices?, multi_select?}` — 3 params, `MAX_CHOICES = 4`, an "Other" option
auto-appended by the UI layer, and an explicit prompt instruction to never restate options inside
`question` (`clarify_tool.py:188-249`). This is a near-exact match for §A2's target
`ask_user{question, kind, options?}` (3 params) and should be read as corroboration, not just
inspiration — a second, independent harness converged on the same 3-parameter shape for the same
primitive.

---

## Q3 — Approval / confirmation flows: who mediates?

**Consensus, HIGH confidence: the host mediates, never the model — but the shape of "host
mediates" differs by whether the channel is synchronous or asynchronous, and Aura needs the
asynchronous shape.**

Two distinct patterns were found across the reference set, plus one non-pattern:

1. **Inline UI-intercept (Cursor, Windsurf).** The model calls `run_terminal_cmd`/`run_command`
   with only the command; before execution, the host shows an approve/deny/modify UI
   synchronously in the *same* client process, and the tool call simply doesn't return until a
   decision is made (Cursor: "The actual command will NOT execute until the user approves it …
   Do NOT assume the command has started running"; Windsurf adds a model-set `SafeToAutoRun`
   self-assessment flag, but the gate is still host-side). The model never sees a resume token —
   it just eventually gets a tool result (success output, or a rejection message).
2. **Stage-and-resume out-of-band (Hermes `memory` tool).** `_apply_write_gate` /
   `_apply_batch_write_gate` (`memory_tool.py:911-1013`) evaluate a gate; on `allow`, the write
   proceeds normally; on `stage`, the call returns `{"staged": true, "pending_id": ...}` and the
   *actual* mutation is applied later by `apply_memory_pending`, called directly by a `/memory
   approve` handler — **outside the model's tool-call loop entirely.** The model is told a
   pending id exists; it does not carry, copy, or resubmit any part of the write.
3. **No per-action gate at all (Devin AI).** Devin's safety boundary is environment isolation (a
   dedicated cloud VM), not per-call confirmation; its only interaction primitive in this space is
   `<message_user request_auth="True">` for *credentials*, a different concern (secrets, not
   destructive-action consent). Not applicable to Aura, which explicitly treats
   `shell_exec` against the operator's own host as the primary surface (per project history) and
   therefore cannot substitute isolation for consent.

**Aura's existing withhold→`ask_user`→resume (validated positively in F-8) is neither pattern 1
nor pattern 2 as currently built — it makes the *model* the resume vehicle**, which is the
ceremony §A2 rightly targets. Pattern 1 (Cursor/Windsurf) doesn't fit Aura's architecture: Aura's
channels are multi-device and asynchronous (a Telegram approval can arrive minutes later from a
different session than the one that issued the call), so the tool call cannot simply block
in-process waiting on a synchronous UI decision the way Cursor's single-client model can.
**Pattern 2 (Hermes' stage/resume) is the correct target shape for Aura specifically because of
that architecture, not merely because it is "less ceremony"**: the gateway already holds a
reservation/approval record server-side (`resume_context` proves the data exists); the fix is to
make the *host* re-drive the withheld tool call from that record once approval lands, instead of
asking the model to relay the blob through a fresh `ask_user` call. **Complexity: MEDIUM-HIGH**
— this is a gateway/state-machine change (`internal/gateway`), not a schema trim; the schema trim
(3-param `ask_user`) is the easy 20% of §A2, the resume-without-model-mediation plumbing is the
other 80%.

**Dependency:** this fix depends on and should land after (or alongside) the F-1 replay-key fix,
since both touch the same reservation/idempotency machinery (`internal/agent/
idempotency_operation.go`, `internal/gateway/reserve.go`) — doing them as unrelated phases risks
two uncoordinated changes to the same state machine.

---

## Q4 — Batch/atomic tool shapes

**Hermes' `memory.operations[]` is the clearest precedent, and it maps directly onto Aura's F-2
data-loss bug, not just onto token efficiency.** `apply_batch`
(`memory_tool.py:562-669`) validates every operation against a *working copy*, commits only if
the **final** state fits the char budget, and is explicitly framed by its own docstring as
replacing "the multi-turn consolidate-then-retry dance." Applied to Aura: a single
`memory_write{operations:[{action:"forget", …}, {action:"upsert", …}]}` call that closes exactly
the one fact it names and adds the replacement in the same atomic operation would have made
the F-2 failure mode (`supersedes:true` closing 8 facts because the model had no way to name
"exactly this one") structurally impossible rather than merely rare. **This is the strongest
evidence in the entire research pass that a batch shape prevents a *class* of error, not just a
round trip** — §C4 undersells it by filing it under "context/efficiency" when it is really a
correctness/data-loss fix. **Complexity: MEDIUM-HIGH** — needs an atomic multi-op path through
`internal/documents` or the ArcadeDB memory bridge with all-or-nothing commit semantics
equivalent to Hermes' "work on a copy, commit only if the whole batch validates," which is a real
transactional-semantics change, not just a wider schema.

**A second, independent precedent for the pattern:** Claude Code's `MultiEdit`
(`Anthropic/Claude Code/Tools.json:253-298`) — "All edits are applied in sequence … atomic —
either all succeed or none are applied." This is sequential-with-rollback rather than
Hermes' final-state-only check, a materially different atomicity model worth naming explicitly:
Aura's `memory_write` should follow Hermes' **final-state-only** validation (because the
correctness property that matters is "is the end state under budget / internally consistent,"
not "did each intermediate step individually succeed"), not Claude Code's sequential-rollback
model, which is suited to text-diff application where each edit is independently well-formed but
memory consolidation is not.

**Where else a batch shape would help in Aura, beyond what the audit names:** `document_index`
today is one-file-at-a-time; a batch ingest of N files from one `fs_glob` result is a plausible
future differentiator but is explicitly **not** in scope for this milestone (no evidence of it
causing a defect in the audited sessions) — noted here only as a dependency flag for a future
phase, not a v2.1.0 recommendation.

---

## Feature Landscape

### Table Stakes (every mature harness in the reference set agrees)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Host-injected identity/tenancy params, never model-typed | Aura (bridge_memory.go), Hermes (cronjob model/provider/base_url) both already enforce this at the handler layer; the model-facing schema must match | LOW | Depends on: `bridge_memory.go` (unchanged), 4 memory tool schemas (edited) |
| Host-mediated approval, model never carries a resume token | Cursor, Windsurf (inline); Hermes memory (stage/resume) — zero vendors make the model relay approval state | MEDIUM-HIGH | Depends on: `internal/gateway/reserve.go`, F-1 replay-key fix (shared state machine) |
| Narrow, single-purpose tools for high-frequency primitives (read/write/search/ask) | 5/5 vendors keep glob and grep separate; all keep read/write/edit separate | LOW (mostly "leave alone") | Aura's `fs_read`/`fs_write`/`fs_edit`/`fs_glob`/`fs_grep` already match this shape per audit §D |
| Collapse mutually-exclusive parameter clusters into one parsed field | Hermes `cronjob.schedule` collapses 4 fields into 1 | MEDIUM | Depends on: `internal/agent/tools/task.go`, a new NL/cron/RFC-3339 parser |
| Separate "use" from "manage" for any capability with an authoring surface | No vendor mixes capability-invocation with capability-authoring in one tool | MEDIUM | Depends on: `internal/agent/tools/skill.go` split into `skill` + `skill_manage` |

### Differentiators (Aura-specific, beyond vendor consensus)

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Atomic batch memory write (`memory_write{operations[]}`) | Not just efficiency — makes the F-2 multi-fact-closure data-loss bug structurally impossible, which no single-op vendor memory tool (Windsurf's CRUD `create_memory`) achieves | MEDIUM-HIGH | Depends on: ArcadeDB memory bridge, ties to F-2 fix directly |
| Deferred-tool promotion cap (`maxPromotedDeferredTools=10`) as a *design constraint* feeding merge decisions | Gives Aura a principled, evidence-based reason to consider merges (e.g., fs_glob+fs_grep) that reference vendors never need to weigh — but only when the cap is shown to bind, not preemptively | LOW to reconsider | Depends on: `internal/agent/llm_agent_promote.go` |
| Curated third-party facade (`calendar{action:...}`, `whatsapp{action:...}`) replacing 28 raw MCP tools | No reference vendor mounts a third-party MCP server's full schema surface verbatim; this is a genuinely Aura-specific integration-shape problem the reference collection doesn't directly solve, but the "one action-oriented tool per rare/administrative domain" principle (Q1) applies directly | MEDIUM-HIGH | Depends on: `aura-pim-mcp`, WhatsApp bridge; biggest context win in the whole milestone per audit §E (~4.5k tokens at turn [130] alone) |

### Anti-Features (tempting, evidenced against)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|------------------|-------------|
| Merge `fs_glob`+`fs_grep` into one `fs_search{target:...}` purely because Hermes does it | Fewer manifest entries, matches the milestone's "flatten" instinct | 5/5 other reference vendors keep them separate; folding a name-vs-content decision into a parameter (rather than a tool name) trades a near-zero-cost decision for a parameter-selection error mode, for a gain (1 of 10 promotion slots) that isn't proven to bind yet | Keep separate; revisit only if the 10-slot cap is shown to evict one of this pair in practice |
| Model-mediated approval blob (`resume_context` round-tripped through `ask_user`) | Feels natural since the gateway already produces the blob and something has to carry it forward | Zero reference vendors make the model responsible for approval state; F-1-adjacent risk of the model mangling/omitting fields the harness itself generated | Host-side stage/resume (Hermes memory pattern): tool call returns a pending id, host resumes the *original* call from its own record once approved |
| "Reduce action count" as the goal for every multiplexer (`skill`, `task`) | Simplicity narrative from the audit's framing | Undercounts what actually causes confusion — Hermes' `cronjob` has *more* top-level params than Aura's `task` and is not a problem, because none of its fields are mutually exclusive and it's a low-frequency tool | Split by call-frequency tier (frequent vs. administrative), not by raw action/param count |
| A `make_document` router tool | Aura's own transcript shows the model reaching for shell because "I don't know which tool" | Root cause was F-1 (stale replay), not a missing router; no reference vendor has a generic "pick the right specialized tool for me" meta-tool — Claude Code's `Task`/general-purpose agent is the closest analog and is reserved for genuinely open-ended multi-step work, not single-call routing | Fix F-1; flatten `skill` per C2 so `use` doesn't require a preceding `info` |

## Feature Dependencies

```
Ceremony strip (user_identifier, 4 memory tools)
    └──independent──> can land first, lowest risk, no shared state

ask_user schema trim (8→3 params)
    └──requires──> gateway resume-without-model-mediation (stage/resume state machine)
                       └──shares state with──> F-1 replay-key fix (idempotency_operation.go, reserve.go)

task.when collapse (11→5 params)
    └──requires──> new NL/cron/RFC-3339 parser (net-new, no existing Aura dependency)

skill / skill_manage split
    └──requires──> skill.go refactor (existing handler logic redistributes, no new state)
    └──enhances──> removal of make_document as a considered feature (root-causes the same symptom)

memory_write batch (operations[])
    └──requires──> ArcadeDB memory bridge atomic-commit path (net-new transactional semantics)
    └──fixes──> F-2 (supersedes over-closure) structurally, not just via a guardrail check

calendar/whatsapp facade (28→~4 tools)
    └──requires──> aura-pim-mcp, whatsapp bridge (existing sidecars; facade is a new routing layer)
    └──independent of──> all native-tool changes above
```

### Dependency Notes

- **`ask_user` trim requires the resume state machine, which shares state with F-1:** both touch
  the same reservation/idempotency keying (`internal/agent/idempotency_operation.go`,
  `internal/gateway/reserve.go`). Sequencing these as unrelated phases risks two changes racing
  on the same code; the roadmap should either land them in the same phase or make the ordering
  explicit (F-1 first, since the resume fix can then reuse a corrected key shape).
- **`memory_write` batch enhances rather than merely parallels the F-2 fix:** F-2's proposed
  guardrail ("refuse or confirm when `supersedes` would close >1 fact") is a check; the batch
  shape is a structural fix that makes the dangerous state (single-op `supersedes:true` on a
  multi-valued predicate) something the model never needs to reach for in the first place. If
  only the guardrail ships without the batch shape, the multi-fact-correction workflow the model
  actually wants ("close this one lesson and add the corrected one") still has no single-call
  expression.
- **The third-party facade is independent of every native-tool change** and can be scheduled in
  parallel — it touches `aura-pim-mcp`/whatsapp bridge routing, not the native tool registry.

## MVP Definition (mapped to this milestone, not a general product MVP)

### Launch With (Phase 45+, this milestone)

- [ ] Drop `user_identifier` from the 4 memory tool schemas — lowest risk, zero shared state,
      immediate ceremony win
- [ ] `ask_user` schema trim to 3 params, paired with the gateway resume-without-model-mediation
      fix and the F-1 key fix (shared state machine — do together)
- [ ] `task.when` collapse (11→5 params) with a natural-language/cron/RFC-3339 parser
- [ ] `skill` / `skill_manage` split, dropping the `info` action
- [ ] `memory_write{operations[]}` atomic batch — structurally closes F-2, not just guards it
- [ ] `document_index`+`document_describe` merge (low-risk, well-evidenced)
- [ ] `web_search` param trim (8→3), inferring `category`/`time_range` from recency markers in
      the query

### Add After Validation (later in v2.1.0, once the above are measured)

- [ ] Curated `calendar{action:...}` / `whatsapp{action:...}` facade (biggest single context win
      per audit §E, but touches two external sidecars and is independent — sequence it once the
      native-tool changes have proven the pattern)
- [ ] Re-evaluate `fs_glob`+`fs_grep` merge **only if** the 10-slot promotion cap is shown to
      evict one of this pair during real sessions (instrument first, merge only on evidence)

### Future Consideration (explicitly out of scope for v2.1.0)

- [ ] Batch `document_index` for N files from one `fs_glob` result — no observed defect motivates
      it yet; flag for a future phase if it recurs
- [ ] A generic tool router/dispatcher (`make_document`-style) — anti-feature per the analysis
      above; do not resurrect unless F-1 is fixed and the symptom still recurs

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| `user_identifier` ceremony strip | MEDIUM (token/UX) | LOW | P1 |
| `ask_user` trim + resume state machine | HIGH (removes a whole failure class) | MEDIUM-HIGH | P1 |
| `task.when` collapse | MEDIUM | MEDIUM | P1 |
| `skill`/`skill_manage` split | MEDIUM-HIGH (root-causes a reported "tool doesn't work" perception) | MEDIUM | P1 |
| `memory_write` atomic batch | HIGH (data-loss prevention, not just UX) | MEDIUM-HIGH | P1 |
| `document_index`+`describe` merge | LOW-MEDIUM | LOW | P2 |
| `web_search` trim | LOW-MEDIUM | LOW | P2 |
| calendar/whatsapp facade | HIGH (largest context reduction) | MEDIUM-HIGH | P2 (sequence after natives land) |
| `fs_glob`+`fs_grep` merge | LOW, contested by evidence | LOW to try, MEDIUM-HIGH risk if wrong | P3 — instrument before deciding |

**Priority key:** P1 = land this milestone; P2 = land this milestone if P1 lands cleanly, else
next; P3 = do not schedule without new evidence (promotion-cap telemetry).

## Competitor (Reference Harness) Feature Analysis

| Feature | Claude Code / Amp / Cursor / Windsurf / Warp | Hermes | Aura's plan |
|---------|---|---|---|
| Name vs. content search | 2 separate narrow tools (5/5 vendors) | 1 merged tool, `target` enum | Keep 2 separate (disagrees with Hermes precedent, agrees with majority) |
| Scheduler ceremony | N/A (no vendor here has a cron tool) | 1 tool, `schedule` string collapses temporal fields | Adopt the collapse (`task.when`) |
| Ask-user shape | Cursor/Windsurf: host-mediated inline, no model-carried token; Windsurf's `suggested_responses` is UI-side only | `clarify{question, choices?, multi_select?}` — 3 params | Match Hermes' 3-param shape; approval flow uses stage/resume (Hermes memory pattern), not Cursor's inline block (doesn't fit Aura's async multi-channel model) |
| Destructive-action approval | Inline UI intercept (Cursor `run_terminal_cmd`, Windsurf `run_command` + `SafeToAutoRun`) | Stage + out-of-band `/memory approve`, `apply_memory_pending` bypasses the model | Adopt Hermes' stage/resume — Aura's channels are async, Cursor's synchronous block doesn't transfer |
| Memory/capability writes | Windsurf `create_memory{Action: create\|update\|delete}` — single-op, Id-addressed | `memory{operations:[...]}` — atomic batch, final-state-only validation | Adopt Hermes' batch shape (closes F-2 structurally) |
| Capability authoring vs. use | No vendor exposes authoring as a model-facing tool at all | N/A (no skill-authoring analog in this collection) | Split `skill`/`skill_manage` (Aura-specific, no direct vendor precedent, but the "no vendor mixes use+author" absence is itself the evidence) |
| Third-party integration surface | N/A (no vendor here mounts a full third-party MCP tool set) | N/A | Curated facade — Aura-specific problem, no reference solves it directly |

## Sources

- `D:/Aura/docs/audit/live-conversations-2026-08-04/TOOL-SIMPLIFICATION.md` — primary proposal,
  validated in parts (A1, A2/A3, B2, C1, C2, C3, C5, C4), challenged in one part (B1)
- `D:/Aura/docs/audit/live-conversations-2026-08-04/FINDINGS.md` — F-1 through F-10, ground truth
  for why each fix matters
- `D:/Aura/internal/agent/mcptools/bridge_memory.go` — verified `user_identifier` injection
  mechanism (lines 45-58) and hidden-tool policy (lines 28-43)
- `D:/Aura/internal/agent/tools/task.go` — verified 11-parameter schema (lines 110-122)
- `D:/Aura/internal/agent/llm_agent_promote.go` — verified `maxPromotedDeferredTools = 10`
  (line 33)
- `D:/tmp/hermes-agent/tools/file_tools.py` — `SEARCH_FILES_SCHEMA` (lines 2242-2259)
- `D:/tmp/hermes-agent/tools/memory_tool.py` — `apply_batch` (lines 562-669), write-gate/stage
  (lines 911-1013), `MEMORY_SCHEMA` (lines 1152-1217)
- `D:/tmp/hermes-agent/tools/clarify_tool.py` — `CLARIFY_SCHEMA` (lines 188-249)
- `D:/tmp/hermes-agent/tools/cronjob_tools.py` — `CRONJOB_SCHEMA` (lines 1027-1133),
  `_validate_cron_base_url` (lines 436-513), model/provider/base_url deliberately excluded from
  the model-facing handler (lines 1176-1180)
- `D:/tmp/system-prompts-and-models-of-ai-tools/Anthropic/Claude Code/Tools.json` — `Glob`,
  `Grep`, `LS`, `Read`, `Edit`, `MultiEdit` schemas
- `D:/tmp/system-prompts-and-models-of-ai-tools/Amp/claude-4-sonnet.yaml` — `glob`, `Grep`,
  `list_directory`, `codebase_search_agent` schemas
- `D:/tmp/system-prompts-and-models-of-ai-tools/Cursor Prompts/Agent Tools v1.0.json` —
  `file_search`, `grep_search`, `list_dir`, `codebase_search`, `run_terminal_cmd`
- `D:/tmp/system-prompts-and-models-of-ai-tools/Windsurf/Tools Wave 11.txt` — `find_by_name`,
  `grep_search`, `list_dir`, `create_memory`, `run_command` (+ `SafeToAutoRun`)
- `D:/tmp/system-prompts-and-models-of-ai-tools/Warp.dev/Prompt.txt` — `read_files`, `grep`,
  `file_glob` kept separate
- `D:/tmp/system-prompts-and-models-of-ai-tools/Devin AI/Prompt.txt` — no per-action approval
  gate; `message_user request_auth` for credentials only

---
*Feature research for: Aura v2.1.0 HERMES-CLAUDE_PARITY tool-surface reshape*
*Researched: 2026-08-05*
