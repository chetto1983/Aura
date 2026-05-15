---
name: aura-implementation-loop
description: Body-dependent Aura implementation process. Use when implementing or resuming any bounded Aura deep-refactor phase or sub-phase, maintaining handoff/progress state, preventing context rot, or choosing plugins for an Aura implementation slice. Always read this SKILL.md before acting.
---

# Aura Implementation Loop

Execute one bounded Aura implementation slice from durable files, not chat
memory. This skill works for every Aura deep-refactor phase, not only the
current Phase 1 slice. The catalog description is not enough: load this body
before acting.

## Use Modes

- **Implementation/resume**: read durable state, run the operational gate, run
  baseline tests, patch, verify, and record.
- **Read-only/forward-test**: produce readiness notes and commands only. Do not
  run baseline tests, edit code, or update handoff/progress files. For
  verification or review requests, inspect only the requested files and return
  findings, scores, blockers, or patch suggestions in the requested format.
- **Planning/readiness only**: inspect phase files and report missing readiness
  items. Do not edit code.
- **Plugin-choice only**: use Plugin Policy and active tool inventory. Do not
  run the full implementation startup unless a code slice is also being
  executed.

## Language Policy

Choose the language lane before writing any user-facing or durable text:

- **Conversation lane**: write chat updates and final answers in the user's
  current language. If the user writes Italian, answer in Italian only, except
  for paths, commands, code identifiers, exact file names, and quoted source
  text.
- **Repository lane**: preserve the dominant language of each existing file.
  Aura deep-refactor planning files currently default to English technical
  records unless a file already declares a different language.
- **No mixed prose**: do not mix languages inside the same paragraph, table
  cell, status line, or checklist item. If bilingual output is needed, use
  separate labeled sections such as `English Technical Record` and `Sintesi
  operativa`.
- **Technical tokens stay literal**: keep commands, package names, capabilities,
  test names, paths, JSON keys, and code symbols exactly as they are.
- **Handoff/progress files**: write durable records in the file's language. Add
  a separate user-language summary only when explicitly requested or when the
  file already has a user-facing summary section.
- **Subagents**: tell delegated agents the conversation language and the durable
  file language. Their returned summaries may use the conversation language;
  citations, paths, commands, and code identifiers stay literal.
- **Self-check**: before the final answer, scan touched docs or summaries for
  accidental mixed-language prose. If the user flags language quality, repair
  the language policy or affected text before continuing implementation work.

## Source Order

Start every non-trivial Aura implementation by reading durable context in this
order. Read only files that exist, and stop as soon as the active slice, source
of truth, affected area, canonical store, verification path, and non-goals are
known. Do not bulk-read phase folders or old wave plans.

1. `D:/Aura/CONTINUE-HERE.md`
2. `D:/Aura/.planning/HANDOFF.json`
3. `D:/Aura/.planning/deep-refactor/.continue-here.md`
4. `D:/Aura/AGENTS.md`
5. `D:/Aura/CLAUDE.md`
6. `D:/Aura/prd.md`
7. `D:/Aura/.planning/aura-deep-refactor-decisions.json`
8. `D:/Aura/.planning/deep-refactor/INDEX.md`
9. the parent phase `progress.md` and `subphase-summary.md` when a parent
    phase owns lettered children
10. the active phase folder's `plan.md`, `source.md`, `benchmark.md`, and
    `progress.md`

Read `D:/Aura/docs/aura-master-plan.md` only as historical evidence when a
phase decision needs predecessor context. Read `.planning/progress.txt` only
when the selected slice needs the append-only progress log. Read
`D:/Aura/scripts/ralph/prd.json` only for Ralph queue work. The list above
preserves the repo startup order; after that startup, `prd.md` plus
`.planning/aura-deep-refactor-decisions.json` is the active deep-refactor route.
If `aura-master-plan.md` conflicts with `prd.md`, ADR-036, or the current phase
folders, treat the master plan as strategic evidence and stop only when the
conflict changes the selected slice or implementation boundary.

## GSD / Ralph Boundary

Aura-specific planning remains canonical. GSD and Ralph are helpers only after
the active PRD/ADR/phase files agree.

- Use `D:/Aura/prd.md`, `.planning/aura-deep-refactor-decisions.json`,
  `.planning/deep-refactor/INDEX.md`, current handoff files, and the selected
  phase `source.md`, `plan.md`, `benchmark.md`, and `progress.md` as source of
  truth.
- Consult GSD only for read-only patterns such as goal-backward planning,
  vertical slice sizing, dependency analysis, and gap reports.
- Use `scripts/ralph/prd.json` only when the current turn explicitly selects
  Ralph queue work.
- Translate any GSD/Ralph-derived idea into Aura's phase files before
  implementation. Do not let GSD `STATE.md`/`ROADMAP.md` or a Ralph queue become
  the source of truth.

## Direct Child Skill Map

`aura-implementation-loop` is the parent skill for execution, repair, and real
debug/E2E closure. It may directly use these child skills when the trigger is
present:

| Trigger | Direct Skill | Required Output |
| --- | --- | --- |
| Selected phase is not implementation-ready | `aura-plan-builder` | repaired `source.md`, `plan.md`, `benchmark.md`, and `progress.md` before code edits |
| Critical architecture shape blocks implementation | `improve-codebase-architecture` | options/RFC translated into the active phase files before patching |
| The local map is lost or docs conflict | `zoom-out` | current canonical route, active slice, files to touch, deferred work |
| A failure is real but root cause is unknown | `gsd-debug` or `systematic-debugging` as read-only/debug pattern | reproduction, hypotheses, eliminated causes, root cause, patch target, verification |
| Auth, grants, sandbox, payload privacy, or tool authority is touched | `codex-security:threat-model`, `codex-security:security-scan`, or `codex-security:validation` | concrete risks and mitigations mapped to code/tests |
| Production/container E2E is required | `docker-compose-orchestration` | compose state, health checks, logs inspected, cleanup/residue state |
| Frontend or local web UI changes are involved | Browser/plugin capability when available plus repo web tests | screenshot/probe evidence, build result, no overlapping/blank UI |
| Code review or phase closure is requested | `gsd-code-review`, `gsd-verify-work`, or `gsd-validate-phase` as read-only reviewers | severity-ranked findings or goal-backward PASS/FAIL with file/line evidence |
| Skill behavior itself is being edited | `writing-skills` / `skill-creator` | minimal skill patch and validation notes |

Child-skill output is advisory evidence. The implementation loop remains
responsible for patching, verifying, updating `benchmark.md`/`progress.md`, and
leaving the workspace clean.

## Plan -> Implement -> Debug -> E2E Chain

Every non-trivial slice follows this chain:

1. **Plan gate**: phase files are current, source-audited, and benchmark rows
   name real ground truth. If not, stop and run `aura-plan-builder`.
2. **Implementation gate**: run baseline commands, patch only named files, then
   run targeted package tests before broader gates.
3. **Real debug gate**: when a test, probe, or user-visible behavior fails,
   reproduce it against ground truth before patching. Use SQLite rows, API
   responses, filesystem artifacts, durable events, logs with redaction,
   rendered UI state, or provider/tool response fields. Do not debug from model
   claims alone.
4. **E2E metrics gate**: live probes must capture the full user-visible reply
   or artifact, durable backing facts, latency, token/cost counters when
   available, tool-call count and tool names, artifact structure/content, and
   pass/fail mismatches. Do not expose or persist raw CoT.
5. **Residue gate**: before final response, check `git status --short`, close
   spawned agents, stop temporary servers started by the slice, leave Docker in
   the requested state, avoid generated junk, and update handoff/progress only
   with verified facts.

No slice is complete if it only passes smoke checks, leaves a stale derived
queue, leaves unrecorded benchmark drift, or relies on a child skill report
without translating the accepted result into Aura's phase files.

If the active phase cannot be identified from durable files, ask one concrete
question before editing. If the phase plan is missing, stale, lacks source
audit, or has no verifier pass, use `$aura-plan-builder` first. Do not
implement from old `.planning/wave*` files directly.

## Phase Resolution

Resolve the target phase from user text and durable files before reading code:

- If the user names a specific phase or folder, use that target.
- If the user says "next", "continue", or gives no phase, use
  `CONTINUE-HERE.md`, `HANDOFF.json.next_action`, and
  `.planning/deep-refactor/.continue-here.md`.
- Use `D:/Aura/.planning/deep-refactor/INDEX.md` plus `D:/Aura/prd.md` to map
  the phase number/name to the canonical folder.
- For parent phase families, read the parent `D:/Aura/.planning/deep-refactor/PhaseNN/`
  files and then the selected child under `PhaseNN/subphases/`.
- For standalone phases, read the standalone `PhaseNN_Description/` folder.
- Never assume Phase01A unless the durable pointers currently select it.

If the selected phase has no ready `plan.md`, `source.md`, `benchmark.md`, and
`progress.md`, or if its verifier status is missing/failed, stop and run
`$aura-plan-builder` for that phase before implementation.

This is the correct behavior for future phases whose folders do not exist yet.
Return a `planning-needed` readiness result with the resolved target folder and
the exact `$aura-plan-builder` handoff. Do not treat missing phase artifacts as
permission to improvise implementation from `prd.md` alone.

## Phase State Outcomes

After phase resolution, choose one outcome:

- `ready-for-implementation`: required phase files exist, source audit and
  verifier pass are current, benchmark rows are concrete, baseline commands are
  known, and no stop condition applies.
- `planning-needed`: the phase folder/files are missing, stale, unverified, or
  not mapped into the canonical parent/subphase layout. Invoke
  `$aura-plan-builder` for that exact phase, then resume this skill.
- `blocked`: durable files conflict, open decisions affect implementation, or
  baseline tests fail before edits.
- `read-only-report`: the user asked for review, forward-test, scoring, or
  plugin choice without implementation.

## Reconciliation Gate

Before code edits, reconcile the phase docs against each other:

- Compare `source.md` missing-source or open-question sections with `plan.md`,
  `progress.md`, parent `subphase-summary.md`, and handoff files.
- If the plan/handoff has an accepted default, treat the stale question as
  resolved and update the phase docs before implementation if it would mislead
  a future agent.
- If the answer is still open and affects schema, package boundary,
  idempotency, payload policy, outbox scope, or canonical store, stop and ask.

## Operational Gate

Before editing code, state a readiness note:

```text
Target:
Mode:
Slice:
Source of truth:
Canonical store / transaction boundary:
External references / examples:
Affected files found with rg/direct reads:
Baseline verification:
Post-edit verification:
Benchmark ground truth:
Dirty state:
Preserved unrelated changes:
Non-goals:
Language:
```

Dirty-state checks must include both global and targeted views:
`git rev-parse --short HEAD`, `git log -1 --oneline`, `git status --short`,
and `git status --short -- <affected paths>`. If durable handoff
`last_known_git` disagrees with the real HEAD, record the mismatch in the
readiness note and update handoff files before implementation when the mismatch
would otherwise mislead a future agent.

Choose `rg` probes from the selected phase plan and source map. Examples:
run/event phases may probe `NewHub`, `ReceiveMessage`, `makeEmit`,
`OutboundEvent`, `RunID`, `ParentRunID`, `run_events`, `migrations`, and
`idempotency`; Telegram/channel phases should probe the named adapter,
renderer, streaming, and test symbols in their own phase docs.

If any readiness item is unknown, inspect more. If it cannot be discovered
safely, ask one concrete question.

## Context-Rot Discipline

Keep the active context small and reconstructible:

- Prefer a 10-line working map over copying long docs into chat.
- Load source files only when they define the behavior being changed.
- Do not rely on verifier/subagent chat as durable state; record only accepted
  results in phase `progress.md` or handoff files.
- Before a pause, compaction risk, or handoff, update `CONTINUE-HERE.md`,
  `.planning/HANDOFF.json`, and `.planning/deep-refactor/.continue-here.md`.
- After a shipped implementation slice, update child `progress.md`, live rows
  in `benchmark.md`, and `.planning/progress.txt`.
- Do not create extra context-pack files unless existing handoff/progress files
  cannot hold recovery state.

Checkpoint text:

```text
Current slice:
Changed files:
Tests run and results:
Blockers:
Deviations:
Next exact command or file:
```

## Implementation Loop

1. **Inspect**: read the narrow files and tests that define current behavior.
2. **Hypothesis**: state what must change and what must not change.
3. **Plan**: name exact files to edit and verification to run.
4. **Baseline**: run the phase benchmark's current-tree tests before edits,
   unless in read-only/forward-test mode. Baseline benchmark rows must assert
   ground truth, not merely smoke readiness.
5. **Patch**: use `apply_patch` for manual edits. Keep changes coherent and
   small. Never revert user changes.
6. **Verify**: run targeted tests first, then `go vet ./...`, `go build ./...`,
   and `go test ./...` when shared runtime/storage/channel behavior changed.
7. **Record**: update progress and benchmark results only after the slice is
   verified or blocked with evidence.

For Go edits, never finish without at least the narrow package tests and the
appropriate build/vet gate from `AGENTS.md`. For web edits, run
`npm --prefix web run build`.

Smoke tests are allowed only as prechecks. They do not count as phase
benchmarks, do not close PRD gates, and must not be recorded as completion
evidence. A valid benchmark names the command or live probe, fixture or data
source, expected ground truth, pass/fail threshold, PRD gate, and result.

## Agent-Loop Policy

Default to the main Codex loop. Spawn or delegate only when the user explicitly
asks for subagents, parallel agent work, or verification agents.

When delegation is authorized:

- delegate only bounded sidecar work that can run in parallel,
- assign disjoint write ownership for code-changing workers,
- tell workers they are not alone in the codebase and must not revert others,
- name the conversation language and durable file language in the delegation
  prompt,
- require structured results: findings, citations, confidence, changed files,
  tests run, and blockers,
- keep child transcripts out of durable state; persist only summaries, artifact
  handles, and decisions,
- review worker patches before integrating,
- close agents after collecting useful results.

For Aura's own future agent loop, preserve the PRD invariant: single flat agent
loop by default; swarm/team mode is opt-in, policy-driven, durable, and
traceable.

## Plugin Policy

Use plugins only when they add trustworthy external context or action. Repo
files, tests, PRD, ADRs, and phase folders remain higher authority.

Before recommending a plugin-backed capability, check the active tool/plugin
inventory for this session and use only capabilities that are actually
available. Prefer already-installed, read-only use. Do not request plugin or
connector installation unless the user explicitly asks to use that specific
plugin/connector in the current turn, the capability is known installable, no
already-available tool can satisfy the request, and `tool_search` has already
been tried when available. A durable slice may justify using an
already-installed connector as a source of truth, but it does not by itself
authorize installation.

External writes require explicit current-turn authorization. This includes
remote-mutating GitHub actions, issue/PR comments, pushes, publishing,
connector writes, plugin/connector installation, and writes to any named
external system. Prefer read-only inspection and report the proposed write when
authorization is missing.

| Need | Use | Rule |
| --- | --- | --- |
| Local UI/manual browser verification | Browser | Use after frontend or local web changes; capture screenshots/probes when relevant. |
| PRs, review comments, issues, CI, publishing | GitHub | Use only for GitHub work or when the user asks for PR/CI/review flow. Master-direct Aura work does not imply PR creation. |
| Auth, grants, audit, tool execution, sandbox, payload privacy | Codex Security | Use for threat-model or security-audit slices before shipping risky changes. |
| OpenAI/Codex/API/Agents SDK behavior | Local OpenAI docs/skills when available; otherwise official OpenAI domains; installed `openai-developers` only when already available or explicitly requested | Prefer local repo/docs first, then official OpenAI docs with citations; do not rely on memory for current OpenAI product details. |
| Design-to-code or code-to-design | Figma | Use only for UI/design handoff work. |
| Docs, spreadsheets, slide artifacts | Documents, Spreadsheets, Presentations | Use when the output or source artifact is that file type. |
| External team state | Available installed connector for the named system of record | Use only when that system is the source of truth, the user has authorized access, and repo/planning files are insufficient. Prefer read-only access; confirm writes. |

Do not install, enable, or use a plugin-backed capability just because it
exists. Name the plugin-backed capability and why it is needed before using it.

## Stop Conditions

Stop and report instead of editing when:

- durable startup files disagree about the active phase,
- the phase folder is not implementation-ready,
- baseline tests fail before any code edit,
- `benchmark.md` lacks concrete ground-truth checks, pass/fail thresholds, or
  PRD-gate coverage,
- the only available verification is smoke-level readiness such as startup,
  `200 OK`, nonzero output, no panic, or tool-call count,
- the slice has no canonical store or transaction boundary,
- phase docs contain unresolved decisions that affect schema, package boundary,
  idempotency, payload policy, outbox scope, or canonical store,
- the requested change would mutate user data, wiki, Qdrant, SQLite runtime
  state, logs, or secrets without explicit permission,
- the implementation would change Telegram rendering, web `/api/chat` response
  shape, cron/missed-run behavior, or swarm/team topology outside its owning
  PRD phase.

## Phase-Agnostic Slice Pattern

Every implementation slice, for any phase, must be:

- selected from that phase's `plan.md` and PRD coverage matrix,
- supported by that phase's `source.md` and decision log entries,
- small enough to name affected files before editing,
- guarded by baseline tests from that phase's `benchmark.md`,
- bounded by explicit non-goals from the phase docs,
- recorded back into that phase's `progress.md`, live benchmark rows, and
  handoff files after verification.

Use the selected phase's canonical store and transaction boundary. Do not reuse
Phase01A's run/event schema unless the active phase actually owns run/event
foundation work.

## Current Example

Use this example only when durable startup files currently point to Phase01A.
When durable state points to Phase01A, the first implementation slice is the
vertical proof: schema + `internal/storage/runs` + minimal `internal/chat.Hub`
consumer wiring. It includes `runs`, `run_events`, `run_outbox`,
`run_idempotency_keys` or equivalent, and separate `audit_events`. Still read
the Phase01A files before acting; this section is only an example of the
bounded-slice shape.

## External Notes

Official Codex guidance: plugins connect Codex to external tools or data,
skills encode process, and plugins can bundle skills, app integrations, and MCP
servers. Treat plugin examples in release notes as ecosystem evidence, not an
availability guarantee or default recommendation.
