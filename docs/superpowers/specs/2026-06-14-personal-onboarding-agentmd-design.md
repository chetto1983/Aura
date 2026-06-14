# Design — Richer personal onboarding + standard `Agent.md`

- **Date:** 2026-06-14
- **Status:** Draft (awaiting user review → writing-plans)
- **Author:** Claude (brainstorming session with Davide)
- **Relates to:** Phase 14 (Onboarding) — open live sign-off; PRD §Slice 10 (`Agent.md` profile) + §Slice 11 (Memory, Phase 15)

## 1. Context & problem

A live Telegram re-test (2026-06-14) confirmed the bot works end-to-end after re-linking
the account (the prior "can not use" was a **data wipe** by `make coverage`'s Postgres reset
on 2026-06-13 — not a code regression). But the re-test surfaced the real gap: **Phase 14
onboarding is a skeleton.**

The interview asks only 2 questions — name, then one lumped "language/timezone/tone/length"
prompt — and parses answers with crude substring matching
([profile_onboarding.go:329 `answersFromText`](../../../internal/channels/telegram/profile_onboarding.go)).
The resulting `Agent.md` for the primary user is effectively empty:

```
## Facts
- Name: Davide
## Preferences
- Prefer Italian responses
## Context            ← empty
## Custom Instructions ← empty
```

It never captures **job/role, company, projects, goals, expertise, tech stack, interests,
or key relationships** — exactly the info that makes a personal assistant personal.

## 2. Research basis (curated D:/tmp sources + AGENTS.md standard)

Four parallel surveys + the AGENTS.md standard converged on a consistent "senior-dev" shape:

- **mem0** (`D:/tmp/mem0/mem0/configs/prompts.py:15-25`) — 7 durable-fact buckets: preferences,
  personal details (incl. **relationships**), plans/**goals**, activities, health,
  **professional details (job/work habits/career goals)**, misc. Pure conversation
  extraction + ADD/UPDATE/DELETE/NONE reconciliation (`prompts.py:176-325`).
- **neo4j-labs agent-memory** (`schema/models.py`, `memory/long_term.py`) — POLE+O graph
  (PERSON/ORG/LOCATION/EVENT + typed `KNOWS`/`EMPLOYED_BY`/…), typed `Preference`/`Fact`
  with `confidence`, **valid-time** (`valid_from`/`valid_until`, supersede-not-delete),
  embedding dedup at cosine ≥0.95. The Phase-15 backbone.
- **Leaked product prompts** (`D:/tmp/system_prompts_leaks/…`) — universal **two-axis** model:
  *who you are* (ChatGPT "Preferred name / Role / Other info"; Gemini Job Title/Location) +
  *how to respond* (ChatGPT "How would you like… to respond" + verbosity; Claude
  `<userPreferences>` Behavioral vs Contextual). Perplexity `memory_update`: capture
  "name, role, company, team, colleagues, preferences, tools, projects, goals" — **never
  ephemeral instructions**. Behavioral rules: relevance-gate silently, domain isolation,
  **sensitive-data blocklist** (Gemini), honor in-chat language override.
- **openhuman** (`memory_store/unified/profile.rs`, `learning/profile_md_renderer.rs`) —
  markdown user-profile with class taxonomy (style/identity/tooling/veto/goal/connected),
  per-facet provenance (`confidence`, `evidence_count`, `state: provisional→active`,
  `user_state: auto|pinned|forgotten`). Separates *assistant* persona (SOUL.md) from *user*
  profile (PROFILE.md).
- **agents.md standard** (https://agents.md) — a **project**-instruction file, NOT a user
  persona format. Contributes structure only (freeform markdown headings, nested precedence,
  32 KB convention). Aura's `Agent.md` reuses the name but is a **user** profile.
- **peterskoett/self-improving-agent** — capture→review→**promote@recurrence≥3**→inject cycle.
  Maps directly onto Aura's existing `AURA_PROFILE_CERTAINTY_N` (default 3,
  [config.go:158](../../../internal/config/config.go)) "observation threshold for auto-add"
  + `profile.AddFact` + `activelearn`/`semindex`.

## 3. Goals / non-goals

**Goals (Phase A, this work):**
1. A richer Telegram onboarding interview (~5 prompts) that captures job + the missing
   personal categories.
2. **LLM-based** parsing of free-text answers into structured fields (replace substring matching).
3. A standardized multi-section `Agent.md` (8 sections).
4. Closes the open Phase 14 live sign-off with a profile that is actually useful.

**Non-goals (deferred to Phase B):**
- Passive conversation-mining of durable facts (continuous enrichment).
- Neo4j memory-graph writes for profile facts, valid-time supersede, dedup.
- HTML-comment managed blocks (only if hand-authored prose protection is needed).

## 4. Decisions (locked in this session)

| Decision | Choice |
|---|---|
| Capture strategy | **Hybrid, staged** — richer interview now; passive extraction deferred to Phase B |
| `Agent.md` layout | **Rich, 8 sections** (mirrors openhuman/product taxonomy) |
| Answer parsing | **LLM extraction** (one bounded call per answer) — kills substring matching |
| Interview length | **~5 prompts** before the draft |

## 5. Standard `Agent.md` structure (Phase A)

Eight sections, stable order. Example (target for Davide):

```
# Agent.md

## Identity
- Preferred name: Davide
- Role: <role> @ <company/team>
- Location / timezone: Europe/Rome
- Languages: Italian (replies), English

## Expertise & Tools
- Domains: <…>
- Stack / tools: <…>

## Projects & Goals
- <current project>
- Goal: <…>

## Interests
- <…>

## People
- Andrea — business partner (sales / NVIDIA bundle)

## Style
- Tone / verbosity / format
- Reply language: Italian

## Vetoes (never do)
- <hard rules>

## Custom Instructions
- <free text>
```

Section updates remain **heading-targeted**, reusing the existing dedup-safe
[`profile.AddFact`](../../../internal/profile/render.go) primitive (inserts a bullet under
`## Heading`). This *is* the "managed block" for Phase A — no HTML markers needed yet.

## 6. Data-model changes

- **`internal/profile/render.go`** — extend `AgentContent` from 4 → 8 string-slice fields
  (Identity, ExpertiseTools, ProjectsGoals, Interests, People, Style, Vetoes,
  CustomInstructions); update `RenderAgentMD` section order. Keep `MaxAgentMDBytes=32768`.
  Keep `AddFact` (Phase B reuses it; may generalize to `AddToSection(section, item)`).
- **`internal/profile/store.go`** — `Preferences` gains `Location string`. `Metadata`
  unchanged. (preferences.json stays the structured style companion.)
- **`internal/onboarding/session.go`** — expand `Answers` with: `Role, Company, Location,
  Expertise, Stack, Projects, Goals, Interests, People, Vetoes`. Add interview `Step`s
  (see §7). Keep the confirm/edit/skip state machine + terminal events.
- **`internal/onboarding/extractor.go`** — `ExtractDraft` renders the 8 sections from the
  expanded `Answers`. Bounded per-field (existing `maxAnswerFieldBytes`).
- File-size discipline: if `session.go` or `profile_onboarding.go` approaches 600 LOC,
  split by concern (`session_steps.go`, `profile_onboarding_extract.go`).

## 7. The interview (≈5 prompts, Italian, LLM-extracted)

State-machine steps (in order), then the existing draft → **Conferma / Modifica / Salta**:

1. **Identity** — *"Come ti chiami, di cosa ti occupi (ruolo + azienda/team) e dove sei (fuso orario)?"*
2. **Work** — *"Quali sono le tue competenze principali e lo stack/strumenti che usi di solito?"*
3. **Projects & goals** — *"A cosa stai lavorando in questo periodo e quali obiettivi hai?"*
4. **Interests & people** — *"Interessi/hobby + persone ricorrenti con cui collabori (nome + ruolo)?"*
5. **Style** — inline buttons: tono (diretto/amichevole/formale), lunghezza (breve/normale/dettagliata),
   lingua, voce on/off.

Each step's free-text answer is parsed by an **LLM extractor seam** into the structured
`Answers` fields. The Style step stays button-driven (deterministic). This replaces
`answersFromText`/`answersForStep`.

### LLM extractor design

- New seam in `internal/onboarding` (e.g. `AnswerExtractor` interface) so the session is
  unit-testable with a fake; production impl uses the already-wired `llm.Client`.
- One bounded call per free-text step: input = step + raw answer; output = a strict JSON
  object of the fields that step targets (structured-output / tool-forced, like
  agent-memory's `ExtractionPayload`). Few-shot examples per step.
- Fail-soft: on extraction error/timeout, fall back to storing the raw answer in the most
  likely section (never block onboarding); log a WARN. Honors "no regex on NLP".
- Cost: ≤4 small extraction calls per onboarding (Style is button-driven). Negligible.

## 8. Using the profile (system-prompt behavior — Phase A wiring)

`Agent.md` is already injected into `messages[1]` via `RenderContextBlock`
(`<profile:Agent.md>` markers). Add the distilled behavioral rules to the system prompt
(English, per project rule):
- Apply profile facts **silently and only when relevant** (ChatGPT 99%-rule; Claude
  Behavioral-vs-Contextual; Gemini domain-isolation).
- **Privacy blocklist** — never infer/surface health, religion, ethnicity, orientation,
  political affiliation, financial/legal unless the user raises them.
- Keep "respond in Italian" but honor an explicit in-chat language override.

## 9. Phase B — passive enrichment (deferred; design only)

End-of-turn reflection extracts durable facts (role/company/projects/goals/people/prefs;
**never** ephemeral "make it shorter"). Each candidate accrues `evidence_count` in the
Neo4j memory graph; when `evidence_count ≥ AURA_PROFILE_CERTAINTY_N` it is **promoted** to
the matching `Agent.md` section via `AddFact`. Contradictions **supersede** via valid-time
(PRD amendment #24), not delete. Dedup by category + cosine on the existing granite-97m
embed + Neo4j HNSW. **Reuses `activelearn`/`semindex` + the certainty-N config that already
exist** — no new infra. Built as a Phase-15-adjacent slice, not here.

## 10. Testing (Phase A)

- **`internal/onboarding`** — table-driven session-machine tests for the new steps/transitions;
  extractor tests with a **fake `AnswerExtractor`** (deterministic, no live LLM); edge tests
  (skip/cancel/restart mid-interview). goleak + race.
- **`internal/profile`** — render tests for all 8 sections (order, empty-section handling,
  dedup via `AddFact`, 32 KB cap).
- **`internal/channels/telegram`** — offline dispatch tests: `/onboard` → 5 answers → draft →
  confirm writes the rich `Agent.md` + preferences.json; the LLM extractor is faked.
- **Coverage ≥85%** across the touched packages (project floor).
- **Live re-test** via the running daemon + Telegram harness: `/onboard`, answer the 5
  prompts naturally, confirm, then assert the rich `Agent.md` on disk
  (`~/.aura/agents/<id>/Agent.md`) — ground truth, not the reply text.

## 11. Risks / open items

- **LLM extraction latency/cost** on a slow turn — mitigated by fail-soft fallback + the
  Style step staying deterministic.
- **Migration of existing skeleton profiles** — `/onboard` restart overwrites cleanly
  (`writeCompleted`); no migration needed (profiles are filesystem, per-identity).
- **Spec location** — placed under `docs/superpowers/specs/` per the brainstorming skill;
  may instead belong in `.planning/phases/14-*/` if treated as a Phase-14 re-spec (user call).
- This redefines Phase 14 scope — flip the ROADMAP/quality-snapshot rows when Phase A ships
  + the live sign-off passes.

## 12. File-change summary (Phase A)

| File | Change |
|---|---|
| `internal/profile/render.go` | `AgentContent` 4→8 fields; `RenderAgentMD` order; (opt) generalize `AddFact` |
| `internal/profile/store.go` | `Preferences.Location` |
| `internal/onboarding/session.go` | expand `Answers`; new `Step`s + transitions |
| `internal/onboarding/extractor.go` | render 8 sections; `AnswerExtractor` seam |
| `internal/onboarding/extractor_llm.go` (new) | production LLM extractor (structured output) |
| `internal/channels/telegram/profile_onboarding.go` | wire extractor; drop `answersFromText`; 5-step prompts |
| system prompt | profile-usage behavioral rules (relevance-gate, privacy, language) |
| tests across the above | table-driven + fake extractor + live re-test |
