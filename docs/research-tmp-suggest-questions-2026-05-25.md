# Research — cold-start question suggestion patterns from D:/tmp/

Date: 2026-05-25
Scope: feeds Wave G2-S2 (`search(action=suggest_questions)`) and any
follow-on stories.
Time-boxed survey (~45 min). Repos under D:/tmp/ ordered by signal.

Aura context recap (no re-audit): G2-S2 ships 4 buckets driven by graph
signals (AMBIGUOUS edges, bridge nodes, INFERRED god-nodes, isolated
nodes) — see `docs/aura-g2-s2-s3-plan-2026-05-25.md` for the spec. This
research looks for OTHER patterns worth lifting either INTO that story
or as separate follow-ups.

---

## Pattern 1 — "Hotness deltas" since last tick

- Source: `D:/tmp/openhuman/src/openhuman/subconscious/situation_report/hotness.rs:1-100` + `mod.rs:1-100`
- License: GPLv3 — concept-lift only, no code copy
- Pattern shape: at each subconscious tick, diff current entity-hotness
  scores against a snapshot from the previous tick; surface the top-K
  movers (positive AND negative |Δ|) with arrows for direction and a
  `(you)` marker for movers whose IDs match the user. Snapshot is then
  refreshed so the next tick has a fresh baseline. Failure is non-fatal
  ("Hotness deltas unavailable" stub).
- Aura applicability: SEPARATE-STORY (G2-S2 follow-up). G2-S2's signals
  are all structural (degree, confidence tag, isolation) — none of them
  capture _what is heating up right now_. Aura already has wiki page
  Git history (mtime per page) and conversation-archive timestamps.
- Sketch (G2-S2.1 — "hot slugs delta"): add a 5th bucket
  `hot_pages_delta`. Compute `delta(slug) = recent_mentions(slug, 7d) -
  prior_mentions(slug, 7d..30d)` from the `conversations` archive +
  `wiki_documents.updated_at`. Top 3 by |Δ| → question template
  "What changed recently about [[slug]]? (mentioned 5x this week vs 0
  prior)". ~80 LOC, snapshot table in SQLite, idempotent.

## Pattern 2 — Cold-start as "load the onboarding skill"

- Source: `D:/tmp/system_prompts_leaks/Perplexity/perplexity-computer.md:19-29`
- License: leaked production prompt — concept only
- Pattern shape: the system prompt explicitly classifies the first
  message into {non-specific, asking-for-examples, specific-task}. For
  the first two, the assistant MUST respond with both text AND a tool
  call (`load_skill("onboarding")` / `load_skill("about-computer")`).
  The skill — not the system prompt — owns the personalised task
  suggestions, which it draws from a `<user_background>` injected
  block. Specific tasks bypass onboarding entirely.
- Aura applicability: SEPARATE-STORY (Phase-ONB territory, already in
  the backlog per `reference_phase_onb_design_2026-05-20`). Not for
  G2-S2 — this is a system-prompt routing/skill pattern, not a graph
  query. Useful confirmation that the right wedge is "ship a tool that
  the orchestrator can call once it decides the turn is cold-start",
  which is exactly what `search(action=suggest_questions)` becomes.

## Pattern 3 — Two-tier suggestion: preprocessing seeds + post-turn follow-ups

- Source: `D:/tmp/elysia/elysia/preprocessing/prompt_templates.py:176-223`
  (`PromptSuggestorPrompt`) + `D:/tmp/elysia/elysia/tree/prompt_templates.py:147-220`
  (`FollowUpSuggestionsPrompt`) + `D:/tmp/elysia/elysia/tree/util.py:642-677`
  (`get_follow_up_suggestions`)
- License: BSD-3-Clause-style — could vendor but concepts are
  sufficient
- Pattern shape: elysia splits the surface in two. (a) At collection
  ingest time, a `PromptSuggestorPrompt` runs once on
  `collection_information + example_objects` and produces 10
  data-aware seed prompts ("specifically reference the collection or
  be specific enough that someone would recognise the data"). (b) After
  each turn, `FollowUpSuggestionsPrompt` produces 2 follow-ups using
  conversation history + environment + `old_suggestions` (anti-repeat
  context) + a `context` string that pins what good follow-ups look
  like ("not too similar to user's prompt; cross-source connections;
  must be answerable with available tools").
- Aura applicability:
  - The (a) preprocessing variant = ABSORB into bucket 4. Today's
    bucket 4 is "isolated nodes" (purely structural). Add a sub-mode:
    when an isolated node is detected, render the question by
    incorporating 1-2 representative tokens from the page body
    (title + first H2) so the question reads "What is the work on
    [[page-X]] about? It's not linked to anything else" instead of
    just "what connects A,B,C". Much higher signal at 45-page scale
    where structural buckets are sparse. ~+30 LOC.
  - The (b) post-turn variant = SEPARATE-STORY (G2-S2.2 / Phase-ONB
    overlap). Aura currently has no "after each turn, surface 2
    follow-ups" channel. This would live in the Telegram outbound
    formatter, not in the search tool.

### Sketch for G2-S2.2 — "post-turn follow-ups"

Add a soft post-turn hook: after the assistant final-reply streams,
call `search(action=suggest_questions, top_k=2, mode="follow_up",
context=last_turn_user_msg + last_turn_assistant_msg)`. The tool body
runs the existing 4 buckets but FILTERED to slugs that were CITED in
the turn (via `[[slug]]` parser). Output rendered as a single
"You might also ask:" line at the end of the message. Anti-repeat: keep
last 5 emitted suggestions per chat in SQLite, exclude verbatim
matches.

## Pattern 4 — Anti-double-emit context across ticks

- Source: `D:/tmp/openhuman/src/openhuman/subconscious/situation_report/reflections.rs:1-50`
- License: GPLv3 — concept only
- Pattern shape: each subconscious tick re-feeds the last 8 reflections
  into the next prompt with the directive "decide whether each still
  holds, has intensified, or has decayed — emit a fresh reflection
  only on a materially new signal". Caps at 8 and uses one-line
  trimming so the prompt section can't blow up.
- Aura applicability: ABSORB into G2-S2 determinism layer. The plan
  already asserts byte-identical output across runs of the same graph
  (criterion #2), but it has no cross-session anti-repeat. Add: keep
  the last K=10 emitted question texts (hashed) in a per-user SQLite
  table. In `SuggestQuestions(topK)`, post-filter any question whose
  hash matches the recent set. If filtering would drop us below
  `topK`, dip into the next bucket priority. Same shape as elysia's
  `old_suggestions` field, same shape as openhuman's
  `recent_reflections` parameter. ~+25 LOC, one new table.

## Pattern 5 — "Style B" actionable-next-steps closing

- Source: `D:/tmp/system_prompts_leaks/Misc/docker-gordon-ai.md:74-95,206,251,252`
- License: leaked production prompt — concept only
- Pattern shape: Docker Gordon's assistant has two end-of-turn styles.
  Style B ("actionable next steps") ends with 2-3 concrete, specific
  follow-up suggestions tightly bound to what was just done (e.g. after
  containerizing → ".dockerignore", "healthcheck", "CI/CD"). Hard
  rules: each suggestion must be a concrete action verb, NOT a vague
  statement ("Ready for deployment" forbidden), RELEVANT to the
  just-completed work (not generic), and NEVER mixed with a friendly
  closing.
- Aura applicability: ABSORB into G2-S2 question-template hygiene.
  Translate the rules into question-template constraints in
  `internal/wiki/questions.go`: (i) every question must name ≥1
  slug verbatim (already in spec), (ii) question must start with an
  interrogative ("What", "Why", "How", "Are"), (iii) reject any
  template that produces a generic stub ("What about [[X]]?" — too
  vague). Add `TestSuggestQuestions_NoGenericTemplates` as a sentinel.
  Cheap, ~+15 LOC, raises floor on output quality.

## Pattern 6 — Hero-queries fallback for "asking for examples"

- Source: `D:/tmp/system_prompts_leaks/Perplexity/perplexity-computer.md:26`
  (refs `references/hero-queries.md` in the loaded skill)
- License: leaked production prompt — concept only (file content not
  in the dump, only the directive)
- Pattern shape: when the user asks "what can you do?" the assistant
  loads an `about-computer` skill whose `references/hero-queries.md`
  is a curated list of model-tested showcase queries. The skill picks
  3-5 hero queries and presents them as "try one of these". This is a
  HUMAN-CURATED fallback for when graph signals are too thin to
  produce good questions automatically.
- Aura applicability: SEPARATE-STORY (G2-S2.3 — "hero-queries
  fallback"). At 45 pages Aura's graph buckets will sometimes return
  <3 hits. Add a curated `runtime-workspace/hero-questions.md`
  (editable like the prompt overlays), 5-10 hand-written examples
  framed in IT/EN. When `SuggestQuestions(topK)` returns <`topK`/2
  questions, fill the rest from this file (deterministic order). The
  user can edit the file; no recompile needed. ~50 LOC including the
  reader + a determinism-preserving merge. Pairs with Pattern 7.

## Pattern 7 — "Tool that should have surfaced" (capability-discovery suggestions)

- Source: `D:/tmp/system_prompts_leaks/Anthropic/claude-opus-4.7.md:541-590`
  (`<mcp_app_suggestions>`)
- License: leaked production prompt — concept only
- Pattern shape: Claude 4.7's system prompt frames tool suggestions as
  "the way a helpful person would suggest a tool they noticed sitting
  right there. Not like a salesperson." Rules: (i) be specific ("I
  could pull your open issues and sort by priority", NOT "I could
  help more with TaskCo access"), (ii) Don't repeat a suggestion the
  person ignored, (iii) Search → suggest BEFORE invoke for third-party
  tools so the user picks the partner. Crucially, this is suggestion
  framed as DISCOVERABILITY of capabilities the user may not know
  exist — orthogonal to graph signals.
- Aura applicability: SEPARATE-STORY (Phase-MCP-UI adjacency, NOT
  G2-S2). Aura has a growing tool surface (`source`, `task`, `wiki`,
  `web`, `file`, MCP tools) and the same discoverability gap. A
  separate `tool(action=suggest)` would scan recently-emitted tool
  names from the last N conversations and surface the LEAST-used
  tools as "did you know you can ask me to X?". Not in scope for G2.

## Pattern 8 — Post-turn reflection that yields user-preference observations

- Source: `D:/tmp/openhuman/src/openhuman/learning/reflection.rs:1-160`
- License: GPLv3 — concept only
- Pattern shape: after each qualifying turn (complexity threshold:
  ≥N tool calls OR response >500 chars), a `ReflectionHook` calls a
  reasoning LLM with the user message + assistant reply + tool calls
  summary, and parses a 4-field JSON: `observations[]`, `patterns[]`,
  `user_preferences[]`, `user_reflections[]`. The output is stored in
  a memory namespace and surfaced into future prompts. Per-session cap
  + rollback on failure.
- Aura applicability: SKIP for G2-S2 specifically (this is a write
  pipeline, not a read/suggestion pipeline), but flag as a future
  Phase-X candidate: the `user_reflections[]` field — explicit
  statements the user authored about themselves ("going forward I
  want…") — is exactly the kind of structured signal that COULD seed
  a personalised hero-questions list (Pattern 6) without hand
  curation. Note for later: if Aura ever adopts a post-turn
  reflection hook, wire its `user_reflections` output into the
  Pattern 6 fallback so the curated file refreshes itself from real
  conversation evidence.

---

## Patterns explicitly checked and rejected

- **Nanobot `cli/onboard.py`**: a CLI config wizard
  (model/temperature/secrets), not a chat cold-start. No applicability.
- **Codex `resume_picker.rs` + `command_popup.rs`**: TUI session
  picker + slash-command palette. Selection-list UX, not
  question-suggestion. Skip.
- **Picobot**: README "cold start" is process startup latency, not
  conversational cold-start. No applicability.
- **Hermes `textInput.tsx`, `conversation_loop.py`,
  `slash/commands/session.ts`**: text-input widget + slash command
  registry. No question-suggestion surface found despite the
  `agent/skill_utils.py` hits — those are skill management, not chat
  bootstrap. (Hermes voice-mode is already lifted separately per
  `reference_hermes_voice_mode_pattern`.)
- **cli-printing-press `SKILL.md:1479` "Auto-Suggest Novel
  Features"**: design-time codegen subagent that proposes features
  for a generated CLI. Not a chat-time pattern. Skip.
- **Kimi-K2.5 paper "cold-start problem"**: this is RL training
  cold-start for vision tool use, not user-facing UX. Skip — wrong
  axis entirely.
- **Sesame Maya voice prompt's "suggest a game"**: voice-mode
  entertainment fallback, irrelevant to knowledge-graph suggestions.
- **Warp/Zed/t3.chat/Antigravity leaks**: greppable matches were all
  about code suggestions / diff hunks, not conversational question
  surfacing.
- **Openhuman `morning_briefing/prompt.md`**: structured scheduled
  briefing (calendar + tasks + emails + market). Useful pattern but
  Aura already has `task(action=schedule)` for scheduled briefings and
  the design problem is different (briefing = aggregate known data;
  suggest_questions = surface what to ASK).

---

## Top 3 patterns for G2-S2 impact (summary)

The highest-leverage patterns for the G2-S2 story as currently
scoped are: **Pattern 4 (anti-double-emit)** as a straight ABSORB
(~25 LOC, prevents the same 4 questions from re-emitting every cold
start and is a known gap in the spec); **Pattern 3a (body-token
enrichment of structural buckets)** as a soft ABSORB in bucket 4 to
raise signal density at Aura's current 45-page wiki size where pure
structural signals are sparse; and **Pattern 5 (template hygiene
rules)** as a 15-LOC sentinel test that bars generic stubs. The
larger SEPARATE-STORY candidates (Pattern 1 hotness-delta as a 5th
bucket, Pattern 3b post-turn follow-ups, Pattern 6 hero-questions
fallback) are real wins but each is its own ~60-100 LOC story and
should land after G2-S2 ships to keep this wave atomic.
