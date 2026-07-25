# Onboarding redesign — digest-in-prefix, graph-on-demand

**Date:** 2026-07-25
**Status:** research + design proposal. No code changed.
**Verdict on the operator's complaint ("troppo lento e serve a poco"):** confirmed, and the two
halves share one root cause — the design spends its entire latency budget writing artifacts that
the read path structurally cannot return.

---

## 1. Recommendation in one paragraph

Split the operator profile into two tiers, Letta-style. Tier 1 is a **single bounded, stable
"operator digest"** (≤400 tokens, rendered once by the *existing* `RenderAgentMD`) stored as **one
row in Postgres**, injected at the **end of the system prompt** (messages[0]) where it is
byte-identical on every turn and therefore sits inside the cached prefix. Tier 2 is the **existing
Neo4j graph** — entities, the full fact set, preference history, the raw interview — which stays
exactly where it is and becomes **pull-on-demand** via the memory tools the agent already has,
discoverable because the digest's last line is an explicit hook telling the model those details
exist. The onboarding write collapses to **one Postgres INSERT inside the request the operator
watches** plus a **fire-and-forget background graph fill over a single reused MCP connection**; the
wizard closes in milliseconds. The per-turn query-keyed `memory_get_context` injection into
messages[1] is **removed from the prompt** and demoted to a tool, which stops it from invalidating
the prompt cache every single turn.

---

## 2. Ground truth — what the code actually does today

### 2.1 The write path: ~20 serial connect-handshake-call-close cycles

`StoreConfirmed` iterates four collections plus a sentinel, one MCP call each
(`cmd/aura/memory_onboarding.go:34-77`):

| Group | Source | Count for a filled profile |
|---|---|---|
| `memory_add_entity` | `internal/onboarding/memory_store.go:84-97` | up to 3 (person, org, location) |
| `memory_add_fact` | `internal/onboarding/memory_store.go:104-115` | 9 fixed + `len(People)` |
| `memory_add_preference` | `internal/onboarding/memory_store.go:122-142` | up to 6 + `len(Vetoes)` |
| `memory_store_message` (raw draft) | `cmd/aura/memory_onboarding.go:67-75` | 1 |
| sentinel fact | `cmd/aura/memory_onboarding.go:88-92` | 1 |

Every one of those goes through `m.write` → `callMemoryToolText`
(`cmd/aura/memory_onboarding.go:95-101`), and `callMemoryToolText` **opens and closes a fresh
streamable-HTTP MCP connection per call** (`cmd/aura/memory.go:188-204`):

```go
cli, err := mcp.OpenServer(callCtx, memoryServerName, server)   // memory.go:198
defer func() { _ = cli.Close() }()                              // memory.go:202
return cli.CallTool(callCtx, tool, args)                        // memory.go:203
```

At the measured 336-395 ms warm / 1327 ms cold per round trip that is ~7-8 s of pure connection
setup inside a single foreground request. **There is no timing instrumentation anywhere on this
path** — `callMemoryToolText` emits no `slog` duration, so the regression was invisible until a
human noticed a spinner.

The same request also mints a Telegram recovery link *before* the profile write
(`internal/agui/onboarding_provision.go:404-407`), adding more serial latency to the operator's
wait.

### 2.2 The read path: facts are structurally unreachable

Per-turn recall calls `memory_get_context` with long-term only, `max_items` default 8
(`cmd/aura/serve_recall.go:30-46`). The sidecar's long-term `get_context` assembles **preferences
and entities only** (`docker/agent-memory/src/neo4j_agent_memory/memory/long_term.py:1413-1468`):

```python
if include_preferences: ... parts.append("### User Preferences")   # long_term.py:1434-1446
if include_entities:    ... parts.append("\n### Relevant Entities") # long_term.py:1449-1466
return "\n".join(parts)                                             # long_term.py:1468
```

There is no facts branch. Both searches are semantic with `threshold: float = 0.7`
(`long_term.py:1185`, `long_term.py:1286`). So the 9+N facts written at
`internal/onboarding/memory_store.go:104-115` — role, works_for, located_in, timezone, expertise,
stack, projects, goals, interests, knows×N — **can never appear in context**, and the entities and
preferences that *can* appear only do so when the current turn's query happens to clear 0.7 cosine
against them. "Che ore sono da me?" does not clear 0.7 against `timezone` as a fact, and `timezone`
is not even a candidate.

### 2.3 The block is injected in the worst possible place for the cache

The recall block lands in the runner's `AlwaysBlock` → messages[1]
(`internal/runner/runner_context.go:48-75`, `internal/runner/runner.go:101-104`,
`internal/runner/interfaces.go:62-68`), and it is keyed on the **current user message**:

```go
// recallQuery (the current user message) keys top-K relevance; "" => identity-only.
block, err := r.archivalRecaller(ctx, owner.ID, recallQuery)   // runner_context.go:62
```

Anthropic's cache is a strict prefix hash over tools → system → messages; one changed byte in the
prefix is a full miss for everything after it. A block at messages[1] whose content is a function of
messages[N] means **every turn re-prefills the entire conversation history**. The current design
therefore pays maximum prefill for minimum recall — the worst quadrant.

### 2.4 The status probe costs a full MCP handshake to read one boolean

`AppShell` fires `fetchOnboardingStatus()` unconditionally on mount, on every page load
(`web/src/AppShell.tsx:149-162`). That endpoint reaches `memoryProfileStore.Status`
(`cmd/aura/memory_onboarding.go:105-137`), which does one `memory_get_facts` call — i.e. one
connect+handshake+close — then filters predicates **client-side** because the tool matches only on
subject (`cmd/aura/memory_onboarding.go:104-105`). ~350 ms of cold-start tax on every app load, to
learn a bit that, once true, never flips back.

### 2.5 The wizard: 8 chrome steps around a 5-question interview, every one a round trip

- Interview state machine is 5 questions + a draft step: `StepIdentity`, `StepWork`, `StepProjects`,
  `StepSocial`, `StepStyle`, `StepDraft` (`internal/onboarding/session.go:32-43`, transitions at
  `:196-210`).
- The wizard shows **8** progress entries (`web/src/onboarding/ProfileOnboardingWizard.tsx:37-46`),
  padding the perceived length with `runtime` and `telegram`.
- Mount blocks on `startProfileOnboarding()` behind a full-screen spinner
  (`ProfileOnboardingWizard.tsx:143-165` and `:226-228`) — **even though the prompt text the server
  returns is thrown away**: `localizedProfileStep` overwrites `step.content` with the i18n bundle
  (`ProfileOnboardingWizard.tsx:60-66`). The operator waits on a round trip for a string that is
  already in the JS chunk.
- Each answer round-trips with a `stepBusy` lock (`ProfileOnboardingWizard.tsx:134`, `:196-202`).
- Finish shows a full-screen blocking "saving" state (`ProfileOnboardingWizard.tsx:172-186`,
  `:245-247`) for the duration of §2.1's 7-8 s.

### 2.6 The artifact we need already exists and is already thrown away

`RenderAgentMD` (`internal/onboarding/draft_render.go:34-39`) renders exactly the bounded,
stable-section-order digest this proposal wants, size-capped by `MaxAgentMDBytes`
(`draft_render.go:5`). Today its output is stored **once, as an opaque `memory_store_message`
blob** (`cmd/aura/memory_onboarding.go:67-75`) that no read path ever fetches. Agent.md was retired
as an on-disk file by Amendment #87 (`internal/channels/telegram/bot.go:86`), but the renderer
survived — we are re-promoting its *output*, not the file.

---

## 3. Where I disagree with the framing

**(a) This is at least as much a storage-choice problem as a memory-architecture problem.** A
boolean flag and a ~400-token blob are being kept in a graph database behind an MCP handshake.
Postgres is already in every request path, already holds identity, and answers in ~1 ms. Batching
the MCP writes (the brief's option B) makes a *bad read path* faster; it does not make the read path
correct. Move the always-on tier to Postgres and the write-batching question mostly evaporates.

**(b) "Facts are written and never read" is true, but "read facts too" is the wrong fix.** Feeding
9-13 more triples per turn through the same 0.7-threshold semantic search would make the messages[1]
block *bigger and still query-varying* — strictly worse for the cache, which is the constraint the
operator named. The right move is to promote the ~4 genuinely-always-relevant facts (role,
works_for, located_in, timezone) into the stable digest and leave the long tail behind a tool the
model is explicitly told about. Discoverability, not injection.

**(c) The 5 questions are not the slow part.** Cutting questions is the obvious move and the wrong
one — it trades the only real signal for a latency win that belongs to the transport. Cut the *round
trips per question* to one for the whole interview instead.

---

## 4. The always-on digest

### 4.1 Budget: 400 tokens hard cap

Enforced at **write time**, not read time — the stored bytes ARE the injected bytes, so nothing can
drift per turn.

| Section | Budget | Contents | Cut priority |
|---|---:|---|---|
| Identity line | 40 tok | name, role, employer, location, timezone | **never cut** |
| Communication contract | 130 tok | language, tone, response length, custom instructions, vetoes | **never cut** |
| Working context | 130 tok | stack (top 8), active projects (top 3), goals (top 2) | cut 4th |
| Social | 60 tok | interests (top 3), key collaborators (top 3) | **cut 1st** |
| Retrieval hook | 40 tok | one line naming what else exists + which tool fetches it | **never cut** |

Overflow order (deterministic, applied in this sequence until under budget): interests → collaborators
→ goals beyond 2 → projects beyond 3 → stack beyond 8 → truncate custom instructions to 200 chars.
Name, language, tone, vetoes and the hook line are never dropped: they are the operator's *contract*
with the agent, and violating them is the failure the operator notices immediately.

### 4.2 Shape

```
<operator_profile>
{Name} — {role} at {company}, {location} ({timezone}).
Language: {lang}. Tone: {tone}. Response length: {length}.
Never: {veto1}; {veto2}.
Custom: {custom_instructions ≤200 chars}
Stack: {stack top 8}.  Active: {projects top 3}.  Goals: {goals top 2}.
Also known: interests, collaborators, full fact history and past sessions for this
operator live in long-term memory — call memory_search when a turn depends on them.
</operator_profile>
```

A realistic filled profile renders at ~130-160 tokens; 400 is headroom for verbose operators.

### 4.3 What stays retrievable, and by what mechanism

Everything else: the verbatim interview draft, `knows×N`, every fact triple, entity descriptions,
preference history, session transcripts. Mechanism is the **tooling that already exists** — the
`memory_search` / `memory_get_facts` / `memory_get_entity` wire tools enumerated at
`cmd/aura/memory.go:57-100`, surfaced to the agent through the memory bridge. No new retrieval
machinery.

The only thing that changes is that the model is *told they exist*. This is the load-bearing idea
borrowed from the Claude Code memory design: the always-on part is a bounded **index of hooks**, not
content; the content is one read away. Today the graph is a corpus with no index entry, which is
functionally identical to it not existing — hence "serve a poco".

### 4.4 Where the facts land, explicitly

| Fact predicate (`memory_store.go:104-115`) | New home |
|---|---|
| `role`, `works_for`, `located_in`, `timezone` | **digest** identity line (~25 tok total) |
| `stack`, `projects`, `goals` | **digest** working-context line, top-N truncated |
| `expertise` | digest if it fits after `stack`; else graph |
| `interests`, `knows×N` | **graph only**, named by the hook line |
| everything else / future | graph only |

All of them are **still written to the graph** — the digest is a projection, not a replacement. The
graph remains the authoritative store; the digest is the cache-stable read-through of its hot subset.

---

## 5. The write path

### 5.1 Recommendation: (A) one reused connection **+** (C) digest-first, graph-async. Not (B), not yet.

| Option | Operator-perceived latency | Build cost | Verdict |
|---|---|---|---|
| **A. Reuse one MCP connection for all of `StoreConfirmed`** | ~1.3 s cold + 20 × call-RTT ≈ **2.5 s** | ~40 LOC in `cmd/aura/memory_onboarding.go` + a `callMemorySession` sibling to `callMemoryToolText` (`cmd/aura/memory.go:188`). Half a day incl. tests. | **Take it.** Pure Go, no fork, no image rebuild, and it improves every future multi-write path. |
| **B. Batched `memory_save_profile` tool in the vendored fork** | ~1.4 s | New tool in `docker/agent-memory/src/neo4j_agent_memory/mcp/_tools.py` + Python tests + image rebuild + version bump + permanent fork divergence to re-apply upstream. 2-3 days. | **Defer.** Buys ~1 s over (A) and only for this one call site, at the highest maintenance cost of the three. Revisit if profile writes ever become hot. |
| **C. Digest-first, graph-async** | **~5 ms** | Migration + one INSERT + one `context.WithoutCancel` goroutine (the pattern is already in the file, `internal/agui/onboarding_provision.go:412`). ~1 day incl. the reconciler. | **Take it.** This is where the operator-visible win actually comes from. |

(A)+(C) together: ~1.5 days, no fork change, no Python, no rebuilt sidecar image.

### 5.2 Concrete flow

`CompleteProfile` (`internal/agui/onboarding_provision.go:390-430`) becomes:

1. Render the digest from `entry.session.Answers` via `RenderAgentMD` +
   the §4.1 truncator. Pure function, microseconds.
2. **One** Postgres INSERT into a new `aura.identity_profile` row:
   `identity_id`, `digest`, `raw_draft`, `onboarding_state` (`completed` | `skipped`),
   `graph_synced bool`, `updated_at`. Transactional with nothing else.
3. Return to the client. **Done — the wizard closes here.**
4. In a `context.WithoutCancel` goroutine: open **one** MCP connection, replay entities → facts →
   preferences → raw message → sentinel over it, then set `graph_synced = true`.

Move the Telegram link mint (`onboarding_provision.go:404-407`) off this path — the completion screen
requests it itself, since it only needs it once the screen renders.

Migration number: **do not hardcode**. `ls internal/db/migrations/ | tail -1` at landing time
determines the next free slot (CLAUDE.md §Persistence, imperative rule).

### 5.3 Crash safety

If the process dies between step 2 and step 4, the graph is empty while status reads "completed".
That is acceptable **because the thing that is actually read is already durable** — the digest is in
Postgres. `raw_draft` in the same row makes step 4 fully replayable: a boot-time reconciler (or
`aura memory backfill-profile <identity>`) picks up every row with `graph_synced = false` and retries.
This is a strict improvement over today, where a failure halfway through the 20 calls leaves a
half-populated graph with no record of what was intended.

### 5.4 Instrument first

Before any of this, add `slog` duration around `mcp.OpenServer` and `cli.CallTool` in
`callMemoryToolText` (`cmd/aura/memory.go:196-203`). The 336-395 ms / 1327 ms figures come from an
external measurement, not from anything the system records. Without instrumentation the improvement
is unfalsifiable and the next regression is equally invisible.

---

## 6. The frontend

| Symptom | File:line | Fix |
|---|---|---|
| Status probe on every app load, one MCP handshake for one boolean | `web/src/AppShell.tsx:149-162` → `cmd/aura/memory_onboarding.go:105-137` | Fold `{required, completed, skipped}` into the session/bootstrap payload the shell already fetches (`useCapabilities`, `AppShell.tsx:19`) → **zero extra requests**. Fallback if that payload can't be extended: serve `/api/onboarding/status` from the new Postgres column (~1 ms) **and** short-circuit in `localStorage` keyed by identity id — `completed` is monotonic, so a cached `true` never needs revalidating. |
| Mount blocks on a round trip for a string already in the bundle | `ProfileOnboardingWizard.tsx:143-165`, `:226-228`, `:60-66` | Render step 1 immediately from i18n; fire `startProfileOnboarding()` in parallel and only `await` the session token when the first answer is submitted. Removes the entire opening spinner. |
| One round trip per interview answer, UI locked by `stepBusy` | `ProfileOnboardingWizard.tsx:134`, `:196-202`; `onboardingStepDispatch.ts` | The transitions are a deterministic client-knowable state machine (`internal/onboarding/session.go:196-210`). Either (i) advance optimistically on submit and reconcile with the response, or (ii) better — collect all 5 answers client-side and POST **once**, blocking only on the draft step, which genuinely needs the server-rendered draft. (ii) removes 4 round trips and makes back-navigation instant. |
| Full-screen blocking "saving" for 7-8 s | `ProfileOnboardingWizard.tsx:172-186`, `:245-247` | With §5.2 this is ~5 ms — delete the blocking state, go straight to the completion screen, and show a small inline "syncing memory" chip driven by `graph_synced` if you want the affordance at all. |
| 8 progress dots for a 5-question interview | `ProfileOnboardingWizard.tsx:37-46` | Show 5. `runtime` and `telegram` are post-interview setup, not interview steps; putting them in the same stepper makes the interview *look* 60% longer than it is. Perceived length is the complaint. |
| Telegram mint on the critical path | `internal/agui/onboarding_provision.go:404-407` | Move to the completion screen's own request. |

Net: the wizard goes from ~6 blocking round trips + a 7-8 s finish to **1 blocking round trip
(the draft) + an instant finish**.

---

## 7. Prompt-cache impact

**Today.** Stable: tools + system prompt. Varies every turn: the messages[1] `AlwaysBlock`, because
its content is a semantic-search result keyed on the current user message
(`runner_context.go:62`, `serve_recall.go:34-46`). Consequence: the cache breaks at messages[1], so
the *entire* conversation history re-prefills every turn — and frequently for a block that recalled
nothing, because of the 0.7 threshold (`long_term.py:1185`).

**Proposed.** Stable prefix: tools → system prompt → `<operator_profile>` digest, with the cache
breakpoint placed **after** the digest. Varies per turn: conversation history + the live user
message, and nothing else. The digest bytes change only when the operator edits their profile —
i.e. approximately never — so a profile edit costs exactly one cache re-write.

Arithmetic: we ADD ≤400 tokens (typically ~150) of prefix and REMOVE a 120-200-token per-turn block.
The added tokens bill at cached-read rates after turn 1; the removed block was invalidating the whole
history at full prefill rates. On a 20-turn conversation carrying ~10k tokens of history this is
roughly an order of magnitude less billed prefill, *and* the recall is deterministic instead of
threshold-dependent. **The digest is cheaper than what it replaces**, which is the constraint the
operator set.

The query-keyed `memory_get_context` recall does not need to die — it needs to stop being a prompt
injection. Demote it to a tool the model calls when a turn actually needs archival context. That is
precisely Letta's split: memory blocks pinned in the context window vs archival memory queried
on-demand via tools ([Letta: memory blocks](https://docs.letta.com/guides/core-concepts/memory/memory-blocks),
[Letta: archival memory](https://docs.letta.com/guides/core-concepts/memory/archival-memory)).

---

## 8. Migration for already-onboarded operators

Live data exists in the graph. Nothing is deleted from Neo4j.

1. **Backfill command** `aura memory rebuild-digest [--all | <identity>]`. For each identity with an
   onboarding sentinel (`cmd/aura/memory_onboarding.go:130`), over **one** MCP connection:
   - Prefer the stored raw draft (`memory_store_message`, written at
     `cmd/aura/memory_onboarding.go:70-73`) — it is already the exact artifact, just truncate to the
     §4.1 budget.
   - Otherwise reconstruct from `memory_get_facts` + entity/preference reads and re-render.
   - INSERT the `aura.identity_profile` row, `graph_synced = true` (the graph is already the source).
2. **Where reconstruction fails**, leave `digest` NULL. The runner treats NULL as "no digest" and
   falls back to today's `archivalRecallProvider` path — nobody regresses, and the failure is visible
   in a count rather than as silent degradation.
3. **Status flags** are seeded in the same pass. Keep `/api/onboarding/status` dual-reading
   (Postgres first, MCP sentinel fallback) for one release, then delete the MCP path — that alone
   removes a handshake from every page load for every user.
4. **No re-interview.** Any design that asks existing operators to answer 5 questions again to get
   the new format has failed the premise.

---

## 9. Risks / what could go wrong

- **Digest staleness is the biggest failure mode.** It is written once and never updated, so it will
  slowly lie (job change, moved city, new project). Mitigation: a "regenerate profile digest" action
  in the settings profile editor, and re-render on any profile edit. Letta solves this by letting the
  agent rewrite its own memory blocks; I recommend **starting manual** — see the next risk.
- **Injection surface moves upward in trust.** Today the recall block is explicitly fenced as
  UNTRUSTED reference data (`cmd/aura/serve_recall.go:75-78`). The digest is operator-authored, so it
  is legitimately higher-trust — but if a future `profile_update` tool lets the *agent* write it, an
  injected instruction lands in the **stable system prefix**, which is strictly worse than
  messages[1] and persists across every future turn. Keep the digest write path operator-only, and if
  agent-authored updates ever land, they must go through an explicit approval gate.
- **400 tokens is a judgement call, not a measurement.** Validate with the live CoT eval harness
  (`internal/eval`, build tag `cot_eval`) A/B on digest-on vs digest-off, scoring whether replies
  actually use the profile. Budget the eval spend first — it is a paid run.
- **Async graph fill hides errors.** A permanently-failing background fill would be silent. The
  `graph_synced` column plus a startup log line for the outstanding count is the minimum; a
  `/api/health` counter is better.
- **Two stores, one truth.** Postgres digest + Neo4j graph can diverge. Accepted deliberately: the
  digest is defined as a *derived projection*, always rebuildable from the graph by §8.1, and never
  the write target for anything but onboarding/profile-edit.

### Could not determine

- **Real warm/cold handshake split.** Nothing in `callMemoryToolText`
  (`cmd/aura/memory.go:188-204`) records timings; the 336-395 ms / 1327 ms figures are external. The
  first commit should be instrumentation, so the win is measurable rather than asserted.
- **Whether the shell already has a bootstrap payload that can carry the onboarding flags.** I saw
  `useCapabilities` imported at `web/src/AppShell.tsx:19` but did not open it. If it returns a
  per-session object, that is the free home for the flags; if not, the localStorage + Postgres
  fallback in §6 still removes the MCP handshake.
- **Whether `memory_get_facts` can filter by predicate server-side.** The comment at
  `cmd/aura/memory_onboarding.go:104-105` says it matches only on subject, which is why Status
  filters client-side. A fact-retrieval tool for tier 2 would need client-side filtering or a fork
  change — worth confirming against `docker/agent-memory/src/neo4j_agent_memory/mcp/_tools.py`
  before committing to a tool shape.
- **Actual per-identity digest size distribution** — only one live operator profile exists to sample.

---

## 10. Suggested landing order

1. Instrument `callMemoryToolText` (½ day, no behaviour change, makes everything else measurable).
2. Postgres `aura.identity_profile` migration + digest render + `Status` read from Postgres, dual-read
   fallback (1 day). **Kills the per-page-load handshake immediately.**
3. Single-connection `StoreConfirmed` + async graph fill + reconciler (1 day). **Kills the 7-8 s.**
4. Digest into the system prefix; demote `memory_get_context` to a tool (1 day). **Fixes "serve a
   poco" and the cache.**
5. Frontend: bootstrap-payload flags, non-blocking start, single-POST interview, 5-dot stepper
   (1-2 days).
6. Backfill command + one-release dual-read window (½ day).

Steps 1-3 are worth shipping alone even if 4 is contested — they are pure latency with no semantic
risk.

---

## Sources

External patterns:
- [Letta Docs — Memory blocks (core memory)](https://docs.letta.com/guides/core-concepts/memory/memory-blocks) — blocks are pinned into the context window, always visible, no retrieval.
- [Letta Docs — Archival memory](https://docs.letta.com/guides/core-concepts/memory/archival-memory) — cannot be pinned; must be queried on demand via tools. This is the tier split adopted here.
- [Anthropic — Prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching) — cache prefixes are built tools → system → messages; any byte change in the prefix is a full miss. Basis for §7.
- Claude Code memory design (`MEMORY.md` index of one-line hooks + on-demand file reads) — basis for the §4.3 "bounded index of hooks, not content" rule.
- Curated local mirrors consulted: `D:/tmp/memory-research/{mem0,neo4j-agent-memory,supermemory}`, `D:/tmp/mem0`.

Aura code (all claims above cite file:line): `cmd/aura/memory_onboarding.go`, `cmd/aura/memory.go`,
`cmd/aura/serve_recall.go`, `internal/onboarding/{memory_store,session,draft_render}.go`,
`internal/runner/{runner,runner_context,interfaces}.go`, `internal/agui/onboarding_provision.go`,
`web/src/AppShell.tsx`, `web/src/onboarding/{ProfileOnboardingWizard.tsx,onboardingApi.ts}`,
`docker/agent-memory/src/neo4j_agent_memory/memory/long_term.py`.
