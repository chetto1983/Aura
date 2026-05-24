---
name: aura-plan-builder
description: Use when the user invokes Aura plan work, asks what deep-refactor slice is next, asks where the Aura plan lives, wants source/plan/benchmark/progress files prepared or repaired, says planning docs are stale or not operational, asks to plan sub-phases, or asks to make future Aura agents implementation-ready.
---

# Aura Plan Builder

Use this skill to create phase folders that are executable by future agents
without relying on chat memory. The skill is expected to be proactive: when the
user gives a vague prompt such as "use aura-plan-builder", "prepare the next
phase", or "this plan skill is not smart enough", reconstruct the target slice
from Aura's durable files and proceed with the safest bounded action.

The core job is not just file formatting. It is to make the next agent
operational: target known, source of truth known, affected files named,
verification known, missing evidence explicit, and open decisions isolated.

## Benchmark Doctrine

Every Aura phase and sub-phase must have a clear `benchmark.md` before it can be
called ready. Smoke tests are prohibited as completion evidence.

- A benchmark row must name the exact command or live probe, fixture/data
  source, expected ground truth, pass/fail threshold, PRD gate, and current
  result.
- Ground truth means durable facts: SQLite rows, API responses, filesystem
  artifacts, durable events, rendered UI state, retrieved source bytes, or
  provider/tool response fields.
- "It starts", "200 OK", "no panic", "nonzero body", and "tool_calls > 0" are
  only prechecks. They can never prove a phase gate by themselves.
- If credentials, fixtures, source ids, live services, or model access are
  missing, the benchmark result is `blocked` or `not run` with the missing
  input named. Do not replace it with a weaker smoke test.
- A verifier must reject any phase that labels smoke-only checks as benchmark
  pass evidence.

## Language Policy

Pick language lanes before creating or repairing any planning artifact:

- **Conversation lane**: answer the user in the user's current language. If the
  user writes Italian, chat updates and final answers must be Italian, except
  for commands, paths, file names, code identifiers, and exact quoted text.
- **Planning-file lane**: preserve each file's dominant language. Aura
  deep-refactor planning artifacts default to English technical records unless
  a folder or file explicitly establishes another language.
- **No accidental bilingual mush**: do not mix languages inside one paragraph,
  table row, status line, or checklist item. If a bilingual artifact is needed,
  separate it with explicit headings, for example `English Technical Record`
  and `Sintesi operativa`.
- **Stable technical vocabulary**: keep PRD item names, capability strings,
  commands, package paths, JSON keys, and test names literal even when the prose
  language is Italian.
- **Parent/sub-phase summaries**: use the file language for durable status. If
  the user needs an Italian explanation, put it in a separate concise section
  and do not translate the canonical technical rows ad hoc.
- **Verifier/research agents**: include the conversation language and durable
  file language in prompts. Ask them not to translate file paths, commands,
  symbols, or quoted evidence.
- **Quality gate**: before claiming readiness, check touched files for
  accidental mixed-language prose. Mixed-language output is acceptable only when
  it is intentional, section-separated, and labeled.

## Plan Location Map

Point future agents to the right durable files:

- `D:/Aura/PRD.md` is the canonical deep-refactor route and phase source of
  truth. The tracked path may appear as `prd.md` on case-insensitive Windows;
  treat those names as the same file.
- `D:/Aura/.planning/post-drift-2026-05-21/INDEX.md` is the current post-drift
  execution pointer: closed phases, active/next slice, and planning repair
  status.
- `D:/Aura/.planning/deep-refactor/INDEX.md` is the legacy phase-folder map and
  provenance pointer for existing deep-refactor phase files. Use it to locate
  old phase folders, not as the default "next slice" source when the post-drift
  index is newer.
- `D:/Aura/.planning/aura-deep-refactor-decisions.json` is the durable ADR
  register for architectural decisions.
- `D:/Aura/scripts/ralph/progress.txt` is local resume memory and recent shipped
  learning. Read it for context; do not make it the canonical phase plan.
- `D:/Aura/scripts/ralph/prd.json` is active only when the current user turn
  explicitly selects Ralph queue execution.
- Retired resume files such as `D:/Aura/CONTINUE-HERE.md`,
  `D:/Aura/.planning/HANDOFF.json`, and
  `D:/Aura/.planning/deep-refactor/.continue-here.md` are historical if found.
  Do not recreate or update them.
- Archived planning files under `D:/Aura/docs/_archive/` and old wave folders
  are evidence only. They never override `PRD.md`, the ADR JSON, or the current
  post-drift index.
- For phase families with lettered sub-phases,
  `D:/Aura/.planning/deep-refactor/PhaseNN/plan.md` is the true parent/master
  plan. The descriptive unlettered PRD slice, such as
  `Phase01_Stabilize_Map`, is a child inside `PhaseNN/subphases/`; do not use
  it as the parent container.
- `D:/Aura/.planning/deep-refactor/PhaseNN/subphases/PhaseNNX_Name/plan.md`
  is the bounded implementation plan for a lettered sub-phase.
- Standalone phases with no lettered children may keep the existing
  `D:/Aura/.planning/deep-refactor/PhaseNN_Name/` layout until they become a
  phase family.
- Every planning document must have one visible role: canonical, resume,
  orientation, phase-plan, reference, evidence, or archive. If a file cannot be
  assigned one role, repair, archive, or ignore it before using it to steer
  implementation.

## GSD / Ralph Boundary

GSD and Ralph are pattern sources for Aura, not canonical planning systems. Do
not create GSD `PROJECT.md`, `ROADMAP.md`, `STATE.md`, `.planning/phases`, or
GSD verification reports as Aura source of truth.

Ralph is a slice-sizing and queue-shaping pattern source only. Treat
`D:/Aura/scripts/ralph/prd.json` as active only when the current user turn
explicitly selects Ralph queue work. Otherwise, Ralph ideas must be translated
into Aura phase files before implementation.

Allowed GSD imports:

- goal-backward "must be true" planning,
- vertical slice decomposition,
- dependency and file-ownership analysis,
- plan-checker style gap reports.

Rejected GSD imports:

- `/gsd:*` commands as authoritative Aura workflows,
- automatic commits or broad phase execution,
- structural grep checks as completion evidence,
- any artifact that bypasses Aura's `source.md`, `plan.md`, `benchmark.md`,
  and `progress.md` contract.

Allowed Ralph imports:

- one bounded story per fresh context,
- append-only Aura phase `progress.md` records, not
  `scripts/ralph/progress.txt` unless the current turn explicitly selects
  Ralph queue work,
- archive-before-manual-queue-edit discipline,
- story sizing that fits one implementation context window.

Rejected Ralph imports:

- `D:/Aura/scripts/ralph/prd.json` as Aura's default active queue,
- running `scripts/ralph/ralph.sh` unless the current turn explicitly asks,
- automatic commits or tool-spawn loops,
- updating queue state instead of Aura phase `progress.md`.

GSD/Ralph-derived ideas must land in the active Aura phase `source.md`,
`plan.md`, `benchmark.md`, `progress.md`, or the decision log before they can
guide implementation. Reference maps, parent summaries, and skills may
cross-link or explain the idea, but they are not enough by themselves to make an
idea implementation-driving.

## Source Merge Rule

The source merge rule prevents research from becoming a shadow workflow.
Source findings from `D:/tmp`, online documentation, papers, GSD, Ralph,
installed skills, or subagents are evidence only until translated into an Aura
artifact. Implementation-driving findings must be represented in the active
phase `source.md`, `plan.md`, `benchmark.md`, `progress.md`, or the decision
log. Supporting cross-links may also be placed in:

- a parent phase summary under `D:/Aura/.planning/deep-refactor/PhaseNN/`;
- a project reference map under `D:/Aura/docs/`;
- a parent skill under `D:/Aura/.codex/skills/`.

Every adopted source needs an explicit rejected or deferred counterpart. No
source can be cited as decoration; each row must name the decision it supports,
the pattern Aura adopts, the pattern Aura rejects or defers, and the destination
file that will own the decision.

## Examples And 2026 Practice Gate

Every Aura plan must include concrete prior art before it can be
implementation-ready:

- Search `D:/tmp` for local example projects, prototypes, reference repos, or
  extracted experiments that match the slice. Use `rg --files D:/tmp` plus
  targeted `rg` queries. Record the exact example paths inspected.
- If no useful `D:/tmp` example exists, record the search queries and result as
  `missing-example`, then make the absence visible in `source.md` and
  `plan.md`.
- Search current external sources for 2026 best practice when the slice depends
  on architecture, provider APIs, protocol behavior, UI/frontend, security,
  storage, scheduling, RAG, agent loops, observability, or tool execution.
  Prefer official documentation, primary sources, maintained example repos,
  standards, release notes, and current production-grade libraries.
- Record what Aura adopts and what it rejects. "Best practice" is not evidence
  unless it is tied to a source, date/context, and a concrete Aura decision.
- A plan that lacks both a `D:/tmp` example sweep and a 2026 best-practice sweep
  is `draft`, not `ready-for-implementation`.
- If network access, credentials, or local examples are unavailable, mark the
  source row `blocked` or `not found`; do not silently skip the gate.

## Direct Child Skill Map

`aura-plan-builder` is the parent skill for planning. It may directly use or
request these skills when their trigger is present, but their outputs must be
translated back into Aura phase files:

| Trigger | Direct Skill | Required Output |
| --- | --- | --- |
| Critical architecture phase before lock, especially Phase 4 runtime, Phase 5 tools, or Phase 7 RAG | `improve-codebase-architecture` | 3-4 design alternatives, chosen deep-module shape, rejected alternatives, RFC notes, source files, verification, residual risk |
| The phase map is hard to see because implementation details or stale docs are dominating | `zoom-out` | Current source-of-truth map, module/caller map, active slice, deferred work |
| A ready `plan.md` needs atomic executable slices or a derived queue | `to-issues` | Aura phase slices with file ownership, dependencies, benchmark rows, and no external tracker publish unless explicitly requested |
| Plan quality is uncertain or multiple reviewers disagree | `gsd-plan-checker` as read-only pattern source | BLOCKER/MAJOR/MINOR findings translated into `progress.md` and repaired in `plan.md` / `benchmark.md` |
| The phase claims completion or readiness | `gsd-verifier` or an equivalent fresh verifier agent | Goal-backward verification against PRD gates, not task-count completion |
| The user asks to push, deploy, publish, open a PR, or close CI for planned Aura work | `aura-implementation-loop` with implicit `github:github`, `github:yeet`, `github:gh-fix-ci`, or `finishing-a-development-branch` imports | plan-to-ship handoff only; planning skill does not push or open PRs |
| The user asks to edit or create skills | `writing-skills` / `skill-creator` | Minimal skill patch, trigger-safe description, validation notes |

Child-skill outputs are evidence, not authority. Record adopted and rejected
ideas in `source.md`, `plan.md`, `benchmark.md`, or `progress.md`. Never let a
child skill create a competing `ROADMAP.md`, `STATE.md`, queue, or phase tree.

## Plan To Implementation Contract

Every phase plan must hand the implementation skill a complete execution
contract:

- selected slice and explicit non-goals,
- canonical store and transaction boundary,
- `D:/tmp` examples inspected, with adopted/rejected patterns or
  `missing-example` evidence,
- current 2026 best-practice sources inspected, with adopted/rejected patterns,
- affected source files and closest tests,
- baseline commands and post-edit commands,
- real debug probes for likely failures,
- debug-readiness fields,
- likely failure modes,
- reproduction command for the highest-risk failure,
- ground-truth probe that proves the behavior instead of only process health,
- dedicated slice-QA scope: reviewer mode, files/diff to inspect, ground-truth
  fact to assert, negative/adversarial check when relevant, and PASS/HOLD
  threshold,
- rollback or stop signal for unsafe drift,
- residue expectation for files, queues, servers, traces, and generated output,
- E2E metrics and pass/fail thresholds,
- atomic local commit boundary and suggested commit message,

Required `benchmark.md` rows for implementation-ready phases:

| Row Type | Must Include |
| --- | --- |
| Baseline | exact command, package/fixture, current expected result |
| Unit/integration | command, expected durable code or DB/API/artifact fact |
| Real E2E | live command/probe, fixture/data source, full assistant/user-visible output, durable ground truth, latency/token/tool/artifact metrics |
| Regression negative | what must not happen, such as impersonation, stale vector suppression, phantom tool claim, raw secret/log leak |
| Dedicated slice QA | bounded QA command/reviewer prompt, exact diff/files to inspect, ground-truth assertion, negative/adversarial check, token/scope cap, PASS/HOLD verdict |
| Residue gate | expected post-run state: no untracked generated junk, no stale queue promotion, no orphan dev server, no unrecorded handoff drift |

Smoke readiness checks may appear only as prechecks and must never close a PRD
gate.

Slice QA is mandatory but bounded. Do not plan a full QA sweep for every slice.
The full `aura-qa-pipeline` sweep is reserved for explicit validation sweeps,
phase promotion, broad regression hunts, or user request. Ordinary slices need a
dedicated micro-QA packet tied to their diff and benchmark row.

When the user asks "where is the main plan of Aura?", answer:
`D:/Aura/PRD.md` for the canonical route, with
`D:/Aura/.planning/post-drift-2026-05-21/INDEX.md` for the current execution
pointer. If they specifically ask where the old deep-refactor phase-folder map
lives, answer `D:/Aura/.planning/deep-refactor/INDEX.md` and label it as the
legacy/provenance map.

## Output Contract

A numbered parent phase that owns lettered sub-phases lives under:

```text
D:/Aura/.planning/deep-refactor/PhaseNN/
  plan.md
  source.md
  benchmark.md
  progress.md
  subphases/
    PhaseNN_Description/
      plan.md
      source.md
      benchmark.md
      progress.md
    PhaseNNA_Subphase_Description/
      plan.md
      source.md
      benchmark.md
      progress.md
```

Standalone phases without lettered children may live directly under
`D:/Aura/.planning/deep-refactor/PhaseNN_Description/`.

Optional supporting artifacts are allowed when useful, for example
`package-matrix.md`, `api-map.md`, `risk-register.md`, or `eval-set.md`.

Every create/repair output must be usable without this chat. A good phase folder
lets a future agent answer four questions from files alone:

- What exactly does the PRD require?
- Which sources support or reject each implementation decision?
- Which benchmark proves each PRD gate?
- What did the fresh verifier find, and how was it handled?

When the user targets a parent phase, the output is the whole phase family under
the true parent/master folder, not only the first sub-phase. For example,
"continue Phase 1" means inspect, plan, repair, and verify
`Phase01/subphases/Phase01_...`, `Phase01/subphases/Phase01A_...`,
`Phase01/subphases/Phase01B_...`, and `Phase01/subphases/Phase01C_...` unless
the user explicitly narrows the task to one folder.

For PRD sub-phases, preserve the letter in the folder name:

```text
D:/Aura/.planning/deep-refactor/Phase01/subphases/
  Phase01_Stabilize_Map/
  Phase01A_Run_Event_Foundation/
  Phase01B_Identity_Capability_Grants/
  Phase01C_Question_Gate/
```

Legacy sibling sub-phase folders such as
`D:/Aura/.planning/deep-refactor/Phase01A_Run_Event_Foundation/` are not the
canonical layout. If they already exist, treat them as migration/evidence
sources: copy their contents into the true parent/master phase
`PhaseNN/subphases/` tree when repairing the parent phase, then update
references in the parent summary. Do not delete legacy folders unless the user
explicitly asks for cleanup.

## Workflow

1. Run `git status --short` and note unrelated dirty files. Do not revert them.
2. Read the minimum Aura startup set:
   `D:/Aura/AGENTS.md`, `D:/Aura/CLAUDE.md`,
   `D:/Aura/PRD.md`, `D:/Aura/.planning/aura-deep-refactor-decisions.json`,
   `D:/Aura/.planning/post-drift-2026-05-21/INDEX.md`, and
   `D:/Aura/scripts/ralph/progress.txt`.
   Read `D:/Aura/.planning/deep-refactor/INDEX.md` only when a phase-folder map
   or old phase provenance is needed.
   Read archived docs only as historical evidence when a phase decision needs
   predecessor context. Read active phase `progress.md` only when the active
   slice needs the append-only phase log.
   Read `D:/Aura/scripts/ralph/prd.json` only when the task is explicitly
   queue/Ralph work.
3. Infer the requested target before asking the user:
   - explicit user phase or folder wins,
   - if the user names a parent phase such as "Phase 1" and does not name a
     lettered sub-phase, enter Sub-Phase Sweep Mode for every matching parent
     and lettered phase in `PRD.md` plus the relevant index, placing lettered
     sub-phase files under the true parent/master `PhaseNN/subphases/`
     directory when deep-refactor phase folders are still the selected
     artifact,
   - otherwise use the current/next slice named by
     `.planning/post-drift-2026-05-21/INDEX.md`,
   - use `.planning/deep-refactor/INDEX.md` only to map old PRD phase names to
     existing legacy phase folders,
   - otherwise inspect `PRD.md`, the post-drift index, and existing phase
     folders for the next draft or incomplete phase after the last completed
     phase,
   - otherwise use the lowest-numbered PRD phase or post-drift milestone that
     has no ready planning artifact,
   - ask only when two durable sources conflict or the inferred action would
     edit an unexpected target.
4. Infer mode before asking the user:
   - `audit` for review/check/inspect/no-edit language,
   - `create` when the target phase folder or required files are missing,
   - `repair` when the folder exists but required files are missing, stale, or
     below the file contract,
   - `resume` when startup files name a next phase but the user gave no mode;
     in resume mode, inspect the folder and then choose `create`, `repair`, or
     `audit` based on file state.
5. State a compact operational-readiness note before editing:
   target phase, source of truth, mode, files that may change, verification,
   unrelated dirty files, language lanes, and why no question is needed. If a
   question is needed, ask one concrete question with the conflicting durable
   evidence.
   In Sub-Phase Sweep Mode, list every canonical sub-phase folder under the
   parent `subphases/` directory and every verifier pass that will be required.
6. Locate the phase section in `PRD.md` and extract every phase-relevant goal,
   step, gate, dependency, explicit non-goal, and "must/never" constraint into
   a working PRD coverage list.
7. Inspect existing phase folders and old `.planning/` waves only as evidence.
   Do not execute old waves as-is.
8. In `create` or `repair`, create or update the phase folder with the required
   files. In `audit`, do not write.
9. Search local sources first with `rg`: PRD, ADRs, docs, `.planning`, code,
   tests, examples under `D:/tmp`, and existing reference maps. The `D:/tmp`
   example sweep is mandatory for every plan.
10. Search online sources for current 2026 best practice. Prefer primary
   sources: official docs, standards, release notes, maintained example repos,
   papers, and production-grade library docs. The 2026 practice sweep is
   mandatory for every implementation-ready plan; mark it `blocked` or
   `not found` only when the source cannot be accessed.
11. If required sources are missing or uncertain, spawn focused research agents.
   Give each agent one narrow source question and ask for citations plus
   adopt/reject recommendations. Do not ask them to edit files.
12. Write `source.md` as an audit table, not a link dump.
13. Write `plan.md` only after source audit is good enough to support
    decisions. Include or reference the PRD coverage matrix.
14. Write `benchmark.md` with planned checks and live results. Every row must
    include command/probe, fixture or data source, expected ground truth,
    pass/fail threshold, PRD gate, and result. If a benchmark has not run, mark
    it `not run` or `blocked` with the missing input and treat the phase as
    draft. Do not substitute smoke checks for benchmark evidence.
15. Run the Planning Quality Gate before any final answer in create/repair mode.
16. Spawn a fresh verification agent before calling the folder verified,
    ready-for-discussion, or ready-for-implementation. Give it only the phase
    folder and the relevant PRD section. Ask for BLOCKER/MAJOR/MINOR findings
    with file/line references.
17. In `repair`, apply fixes from the verifier, then rerun validation. Repeat
    until there are no BLOCKER or MAJOR findings, or stop and report why the
    remaining finding needs a user decision. In `audit`, report findings only.
18. In `create` or `repair`, append the verifier result and repair outcome to
    `progress.md`; do not rely on chat memory for validation state. In `audit`
    mode, report findings only and do not write progress unless the user
    explicitly changes the task to repair.
19. Do not create branches, PRs, or pushes unless the user explicitly asks for
    that git action in the current turn. Planning-only readiness can be
    "ready for discussion" without a commit. Any implementation or QA write
    slice emitted by this plan must still close with one atomic local commit
    before the next slice starts.

## Sub-Phase Sweep Mode

Use this mode when the user targets a parent phase ("Phase 1", "plan the whole
phase", "all sub-phases", "all sub phase", "the full phase family") or when
the PRD/INDEX shows lettered children for the target phase and the user has not
explicitly narrowed the request.

Sub-Phase Sweep Mode is planning-only. It may repair documentation for many
phase folders, but it must not implement code. Implementation remains one
bounded slice at a time.

Sweep workflow:

1. Build a phase-family list from `D:/Aura/PRD.md`, the post-drift index, and
   `D:/Aura/.planning/deep-refactor/INDEX.md` only when old phase folders are
   still relevant.
2. Include the parent/master phase folder, the descriptive unlettered PRD
   slice, and every lettered child with the same numeric prefix. Canonical
   child paths live under:
   `D:/Aura/.planning/deep-refactor/PhaseNN/subphases/PhaseNNX_Description/`.
3. If lettered child folders exist as siblings beside the parent folder, treat
   them as legacy evidence. In repair mode, copy their required files into the
   canonical parent/master `PhaseNN/subphases/` tree before verification. In audit mode,
   report the layout mismatch and do not write.
4. For every sub-phase, run the normal create/repair workflow:
   source audit, plan, benchmark, progress, PRD coverage matrix, Planning
   Quality Gate.
5. Spawn a separate fresh verifier for each sub-phase folder. Do not use one
   broad verifier for the whole family.
6. Repair each sub-phase until it has no BLOCKER or MAJOR findings, or record
   the exact unresolved decision that prevents repair.
7. Append verifier results to each sub-phase `progress.md`.
8. Write or update a parent summary artifact inside the parent/master phase
   folder, such as
   `D:/Aura/.planning/deep-refactor/PhaseNN/subphase-summary.md`,
   with:
   sub-phase status, verifier result, open decisions, implementation order, and
   first bounded implementation slice.

Parent readiness rule:

- A parent phase is not `ready-for-discussion` until every required sub-phase is
  at least `verified` or explicitly marked deferred with a PRD-backed reason.
- A parent phase is not `ready-for-implementation` until the first
  implementation sub-slice is selected, bounded, and linked from the summary.
- If any child has a BLOCKER or MAJOR finding, the parent phase is not ready.
- When a parent or child phase becomes ready for implementation, name
  `$aura-implementation-loop` as the execution skill and include the first
  bounded slice, baseline tests, affected files, and non-goals in the parent
  summary or child `progress.md`.

## Planning Quality Gate

Before a create/repair response is finished, check the phase folder against this
gate. Repair misses before asking for verification:

- Every PRD step and gate appears in a coverage matrix with:
  PRD item, plan location, benchmark/test location, source/evidence, status, and
  owner or reason if deferred.
- Every implementation gate has at least one benchmark row or an explicit
  blocked/deferred explanation.
- Every executable slice has a dedicated slice-QA row that is bounded to the
  slice diff and names a ground-truth assertion. Full QA sweeps are not allowed
  as the default per-slice plan.
- Every benchmark row asserts ground truth and has a measurable pass/fail
  threshold. Smoke-only rows are labeled precheck and are not counted toward
  phase readiness.
- Every source row says which decision it supports and what Aura rejects or
  avoids.
- `source.md` contains a `D:/tmp` example sweep with exact paths or a
  `missing-example` result, and a 2026 best-practice sweep with dated/current
  source evidence or an explicit blocker.
- Every open decision is isolated in `plan.md` with the consequence of each
  option.
- The document language is coherent: no accidental mixed-language prose inside
  a paragraph, table row, status line, or checklist item.
- `benchmark.md` distinguishes static checks, code tests, live probes, metrics,
  thresholds, and actual result or `not run`.
- `progress.md` uses exact columns: date, actor, change, verification, blockers,
  deviations from plan.
- The phase status does not overclaim. If live benchmarks are `not run`, the
  phase is not complete. If no fresh verifier ran, the phase is not verified.

Recommended coverage matrix format:

```markdown
| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| <step/gate text> | plan.md#... | benchmark.md#... | source.md#... | covered/deferred |
```

If the matrix becomes too large for `plan.md`, put it in a supporting artifact
such as `coverage-matrix.md` and link it from `plan.md`, `source.md`, and
`benchmark.md`.

## Smart Defaults

When the user invokes this skill without a precise phase, do useful work from
durable state instead of stopping. For the current Aura route, use
`D:/Aura/PRD.md`, `.planning/post-drift-2026-05-21/INDEX.md`, the ADR JSON, and
recent shipped learnings from `scripts/ralph/progress.txt`. If an old phase
folder is still selected, resolve lettered children to the canonical
parent-owned path such as `Phase01/subphases/Phase01A_Run_Event_Foundation`
when that parent/subphase relationship exists, then inspect it.

Default first action for a likely next phase:

1. Decide whether the target is a single sub-phase or a parent-phase sweep.
2. Read the phase folder file list and required files if present.
3. Compare the folder against the File Requirements in this skill.
4. Compare the folder against the exact PRD phase section.
5. If files are missing or empty, repair them.
6. If files exist, audit them first, then repair only if the user asked for
   readiness or the invocation clearly implies "make it ready".

Do not confuse "smart" with broad. A smart run narrows quickly, names evidence,
and moves one bounded phase toward readiness.

## First Response Pattern

For vague or resume-style invocations, avoid a bare clarification question. Use
this shape:

```text
I inferred:
- target: PhaseNN or PhaseNN/subphases/PhaseNNX_Name from <durable file/path>
- mode: resume -> repair/audit because <file state>
- source of truth: PRD.md section + decision log IDs + post-drift index; old folder map from .planning/deep-refactor/INDEX.md only if relevant
- files I may edit: <phase folder files>
- verification: <commands or verifier pass>
- language: chat=<user language>, files=<dominant file language>
- sub-phases: <only in Sub-Phase Sweep Mode; list canonical parent/subphases folders and verifier passes>
- dirty worktree: <unrelated files>

Proceeding unless I find a durable conflict.
```

If there is a durable conflict, replace "Proceeding" with one concise question
that quotes the conflicting paths and asks which source should win.

## File Requirements

`plan.md` must include:

- status,
- phase goal,
- scope and non-goals,
- roadmap,
- dependencies,
- decisions required before implementation,
- PRD coverage matrix or a link to a supporting coverage artifact,
- implementation gates,
- rollback/deviation rule.

`source.md` must include:

- local source files,
- external sources,
- example repositories or paths,
- `D:/tmp` example paths inspected and search queries used,
- current 2026 best-practice sources inspected,
- what Aura adopts,
- what Aura rejects,
- audit status,
- missing-source questions.

Every source row should answer: "What decision does this source support?"

`benchmark.md` must include:

- static validation commands,
- code tests,
- live-agent probes,
- metrics,
- fixture or data source,
- expected ground truth,
- target thresholds,
- PRD gate covered,
- actual result or `not run`,
- date and actor for each live result.
- dedicated slice-QA packet definition for every executable slice.

Do not call a phase complete if live benchmarks are missing.
Do not call a phase complete if the only passing checks are smoke tests.
Do not call an implementation slice complete if its dedicated slice QA is
missing or replaced by a broad full-sweep plan.

`progress.md` must be append-only and include:

- date,
- actor,
- change,
- verification,
- blockers,
- deviations from plan.

## Subagent Policy

Use subagents when the user explicitly asks for them or when the current runtime
permits independent source discovery or verification for a phase plan. A
create/repair phase plan is not `verified` until a fresh verifier has run. If
subagents are unavailable or not permitted, label the result `self-audited`, not
`verified`, and say exactly what could not be independently checked.

Research agent prompt shape:

```text
Research one missing source question for Aura PhaseNN. Do not edit files.
Authority: read-only research. Budget: bounded to the named question. Output
schema: sources with URLs/paths, local evidence, what Aura should adopt, what
Aura should reject, confidence, and remaining gaps. Return citations. Do not
edit files, spawn more agents, or make durable decisions.
```

Verification agent prompt shape:

```text
Audit D:/Aura/.planning/deep-refactor/PhaseNN/subphases/PhaseNNX_Description
or D:/Aura/.planning/deep-refactor/PhaseNN for a parent summary against the
relevant PRD phase and the required file contract. Do not edit files. Return
BLOCKER/MAJOR/MINOR findings with file/line references and readiness verdict.
Authority: read-only verification. Budget: bounded to the phase folder and
named PRD section. Output schema: findings, evidence, readiness verdict, and
recommended next action. Do not edit files, spawn more agents, or make durable
decisions.
```

Keep research agents narrow. One broad "find everything" agent produces mush.

Verifier result handling:

- BLOCKER: repair before any readiness claim, or stop and report the conflict.
- MAJOR: repair and rerun verifier before calling the plan ready.
- MINOR: in `create` or `repair`, repair when cheap; otherwise record it in
  `progress.md` with why it is acceptable. In `audit`, report it only.
- No findings: in `create` or `repair`, append the verifier verdict to
  `progress.md` and state exactly what is verified. In `audit`, report the
  verdict only.

## Readiness Labels

- `draft`: folder exists, but sources or benchmarks are incomplete.
- `source-audited`: `source.md` has local and external evidence with
  adopt/reject notes.
- `benchmark-ready`: `benchmark.md` has concrete commands, metrics, and live
  probe definitions.
- `self-audited`: Planning Quality Gate passed locally, but no independent
  verifier ran.
- `verified`: fresh verifier found no BLOCKER or MAJOR findings, and any MINOR
  findings are repaired or recorded with rationale.
- `ready-for-discussion`: verified, source-audited, benchmark-ready, and open
  decisions are clearly isolated.
- `ready-for-implementation`: ready-for-discussion plus the user agrees with
  open decisions and the first implementation slice is bounded.
- `parent-ready-for-discussion`: every required sub-phase is verified or
  explicitly deferred with a PRD-backed reason, and the parent summary names the
  first implementation slice.

## Guardrails

- Do not implement code while creating the phase plan unless the user asks.
- Do not turn old `.planning/wave*` files into active queues.
- Do not mark online sources as audited unless you actually opened or searched
  them in this run.
- Do not use sources as decoration; every source must support or reject a
  concrete decision.
- Do not say a phase is verified unless a fresh verifier actually ran.
- Do not say a parent phase is planned if only one child sub-phase was planned.
- Do not create new lettered sub-phase folders beside the parent/master
  `PhaseNN` folder; canonical sub-phase files live under
  `PhaseNN/subphases/`.
- In `create` or `repair`, do not leave verifier findings only in chat; append
  the result and repair status to `progress.md`. In `audit`, do not write
  progress unless the user explicitly changes the task to repair.
- Do not create branches, PRs, or pushes automatically. Ask or wait for explicit
  user instruction in the current turn for remote actions. Planning-only runs do
  not commit by themselves. For implementation and QA execution, require one
  atomic local commit per verified slice before the next slice starts.
