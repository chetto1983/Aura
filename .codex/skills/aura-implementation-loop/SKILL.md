---
name: aura-implementation-loop
description: Use when implementing or resuming a bounded Aura phase or sub-phase, closing an Aura implementation slice, handling commit/push/CI/deploy requests for Aura work, maintaining phase progress, preventing context rot, or choosing plugins for an Aura implementation slice.
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

1. `D:/Aura/AGENTS.md`
2. `D:/Aura/CLAUDE.md`
3. `D:/Aura/PRD.md`
4. `D:/Aura/.planning/aura-deep-refactor-decisions.json`
5. `D:/Aura/.planning/post-drift-2026-05-21/INDEX.md`
6. `D:/Aura/scripts/ralph/progress.txt`
7. `C:/Users/Davide/.claude/projects/d--Aura/memory/MEMORY.md`
8. `D:/Aura/.planning/deep-refactor/INDEX.md` only when old phase-folder
   provenance is needed
9. the parent phase `progress.md` and `subphase-summary.md` when a parent
    phase owns lettered children
10. the active phase folder's `plan.md`, `source.md`, `benchmark.md`, and
    `progress.md`

Archived planning files and retired handoff files are historical evidence only.
Do not recreate or update `CONTINUE-HERE.md`, `.planning/HANDOFF.json`, or
`.planning/deep-refactor/.continue-here.md`. Read `D:/Aura/scripts/ralph/prd.json`
only when the current turn explicitly selects Ralph queue work. The active route
is `PRD.md` plus the ADR JSON plus the post-drift index; older phase folders are
used only when the selected slice still lives there.

## GSD / Ralph Boundary

Aura-specific planning remains canonical. GSD and Ralph are helpers only after
the active PRD/ADR/phase files agree.

- Use `D:/Aura/PRD.md`, `.planning/aura-deep-refactor-decisions.json`,
  `.planning/post-drift-2026-05-21/INDEX.md`, and the selected phase
  `source.md`, `plan.md`, `benchmark.md`, and `progress.md` as source of truth.
- Use `.planning/deep-refactor/INDEX.md` as a legacy phase-folder map when a
  selected slice still needs old phase-folder provenance.
- Consult GSD only for read-only patterns such as goal-backward planning,
  vertical slice sizing, dependency analysis, and gap reports.
- Use `D:/Aura/scripts/ralph/prd.json` only when the current turn explicitly
  selects Ralph queue work.
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
| Any write slice reaches post-edit verification | `aura-qa-pipeline` in dedicated slice-QA mode | bounded QA packet for this slice only: diff inspected, ground-truth probe, negative/adversarial check when relevant, PASS/HOLD verdict |
| Code review or phase closure is requested | `gsd-code-review`, `gsd-verify-work`, or `gsd-validate-phase` as read-only reviewers | severity-ranked findings or goal-backward PASS/FAIL with file/line evidence |
| Push, deploy, PR, or CI closure is explicitly requested | `github:github`, `github:yeet`, `github:gh-fix-ci`, and `finishing-a-development-branch` as implicit imports | scoped remote action, pushed ref, CI run/log verdict, residual dirty state |
| Skill behavior itself is being edited | `writing-skills` / `skill-creator` | minimal skill patch and validation notes |

Child-skill output is advisory evidence. The implementation loop remains
responsible for patching, verifying, updating `benchmark.md`/`progress.md`, and
leaving the workspace clean.

Do not invent Aura-specific deploy skills when installed GitHub or branch
completion skills already cover the workflow shape. Import their rules
implicitly, then apply Aura's stricter local constraints: master-direct unless
the current user request asks for a branch or PR, no remote write without
current-turn authorization, and CI verification after every push.

## Sister Skill Contract

`aura-implementation-loop` is the coordinator. Its sister skills are imported
for their judgment, then translated back into Aura's route:

- `$aura-plan-builder` owns planning readiness and source/plan/benchmark/progress
  repair.
- `$improve-codebase-architecture` owns critical architecture alternatives and
  RFC shape for risky refactors.
- `$zoom-out` owns read-only module/caller/source-of-truth maps when context is
  getting blurry.
- `$to-issues` owns vertical slice decomposition, not external issue publishing
  unless explicitly requested.
- `$aura-qa-pipeline` owns dedicated slice QA. Use its micro mode for each
  slice; do not run the full five-phase QA sweep unless the user asks for a
  validation sweep, phase promotion, or broad regression hunt.
- GitHub publish/CI skills own remote workflow mechanics only after current-turn
  authorization. Local atomic commits are part of Aura's slice discipline, not
  a remote publish action.

Sibling output is never a new canonical route. Accepted decisions must land in
Aura phase files, the ADR JSON, `PRD.md`, or the post-drift index before code,
atomic commit, push, or CI closure relies on them.

## Plan -> Implement -> Debug -> E2E Chain

Every non-trivial slice follows this chain:

1. **Plan gate**: phase files are current, source-audited, and benchmark rows
   name real ground truth. If not, stop and run `aura-plan-builder`.
2. **Implementation gate**: run baseline commands, patch only named files, then
   run targeted package tests before broader gates.
3. **Real debug gate**: when a test, probe, or user-visible behavior fails,
   reproduce it against ground truth before patching. State one hypothesis,
   inspect the durable fact that can prove or falsify it, patch the smallest
   owned cause, then rerun the failing command before broader gates. Use SQLite
   rows, API responses, filesystem artifacts, durable events, logs with
   redaction, rendered UI state, or provider/tool response fields. Do not debug
   from model claims alone.
4. **E2E metrics gate**: live probes must produce an evidence packet with the
   exact command or live probe, fixture or seed data, full non-sensitive
   user-visible reply or artifact, redacted preview plus artifact handle when
   the payload is sensitive/private, durable backing facts, trace/run id or
   artifact handle, latency/token/cost counters when available, tool-call count
   and tool names, artifact structure/content, and mismatch diff. Do not expose
   or persist raw CoT.
5. **Dedicated slice QA gate**: before any atomic commit, run a bounded QA pass
   for this slice only. The QA pass must inspect the diff, re-derive expected
   behavior from source/benchmark, assert at least one ground-truth fact, and
   include a relevant negative/adversarial check when the slice touches tools,
   auth, memory/wiki, storage, channels, scheduling, prompts, or runtime state.
   Do not launch the full `aura-qa-pipeline` sweep unless the user explicitly
   asked for it or the phase is being promoted.
6. **Residue gate**: before final response, check `git status --short`, close
   spawned agents, stop temporary servers started by the slice, leave Docker in
   the requested state, avoid generated junk, and update handoff/progress only
   with verified facts.

No slice is complete if it only passes smoke-level checks, leaves a stale
derived queue, leaves unrecorded benchmark drift, lacks its atomic local commit,
skips dedicated slice QA,
or relies on a child-skill report without translating the accepted result into
Aura's phase files.

If the active phase cannot be identified from durable files, ask one concrete
question before editing. If the phase plan is missing, stale, lacks source
audit, or has no verifier pass, use `$aura-plan-builder` first. Do not
implement from old `.planning/wave*` files directly.

## Phase Resolution

Resolve the target phase from user text and durable files before reading code:

- If the user names a specific phase or folder, use that target.
- If the user says "next", "continue", or gives no phase, use
  `D:/Aura/.planning/post-drift-2026-05-21/INDEX.md`, then `PRD.md`, then
  recent shipped learnings from `scripts/ralph/progress.txt`.
- Use `D:/Aura/.planning/deep-refactor/INDEX.md` plus `D:/Aura/PRD.md` only
  when an old phase-folder number/name must be mapped to a legacy folder.
- For parent phase families, read the parent `D:/Aura/.planning/deep-refactor/PhaseNN/`
  files and then the selected child under `PhaseNN/subphases/`.
- For standalone phases, read the standalone `PhaseNN_Description/` folder.
- Never assume an old phase unless current durable files still select it.

If the selected phase has no ready `plan.md`, `source.md`, `benchmark.md`, and
`progress.md`, lacks a `D:/tmp` example sweep, lacks current 2026 best-practice
source evidence, or if its verifier status is missing/failed, stop and run
`$aura-plan-builder` for that phase before implementation.

This is the correct behavior for future phases whose folders do not exist yet.
Return a `planning-needed` readiness result with the resolved target folder and
the exact `$aura-plan-builder` handoff. Do not treat missing phase artifacts as
permission to improvise implementation from `PRD.md` alone.

## Phase State Outcomes

After phase resolution, choose one outcome:

- `ready-for-implementation`: required phase files exist, source audit includes
  `D:/tmp` examples plus current 2026 best-practice evidence, verifier pass is
  current, benchmark rows are concrete, baseline commands are known, and no stop
  condition applies.
- `planning-needed`: the phase folder/files are missing, stale, unverified, or
  not mapped into the canonical parent/subphase layout, or the source audit
  lacks mandatory `D:/tmp` example and 2026 best-practice sweeps. Invoke
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
D:/tmp examples:
2026 best-practice sources:
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
  results in phase `progress.md`, `benchmark.md`, the post-drift index, or the
  selected canonical planning file.
- Before a pause, compaction risk, or handoff, leave a compact recovery note in
  the active phase `progress.md` when a phase folder owns the work. If there is
  no active phase file, report the compact recovery note in chat and avoid
  creating new handoff files.
- After a shipped implementation slice, update child `progress.md`, live rows
  in `benchmark.md`, and any current execution pointer that would otherwise
  mislead the next agent. Append to `scripts/ralph/progress.txt` only when the
  current turn explicitly selected Ralph queue work.
- Do not create extra context-pack files unless the active planning artifact
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
7. **Record and commit**: update progress and benchmark results only after the
   slice is verified or blocked with evidence, including the dedicated slice-QA
   verdict. For a verified write slice, create exactly one atomic local commit
   before starting the next slice.

For Go edits, never finish without at least the narrow package tests and the
appropriate build/vet gate from `AGENTS.md`. For web edits, run
`npm --prefix web run build`.

Smoke tests are allowed only as prechecks. They do not count as phase
benchmarks, do not close PRD gates, and must not be recorded as completion
evidence. A valid benchmark names the command or live probe, fixture or data
source, expected ground truth, pass/fail threshold, PRD gate, and result.

## Evidence Packet Contract

Every verified implementation slice must leave a durable evidence packet in the
active phase `benchmark.md` and active phase `progress.md`. Handoff files may
carry pause, blocker, or readiness state, but handoff-only evidence cannot close
a slice. The final answer may summarize the packet, but chat alone is not
durable evidence. The packet must include:

- exact command or live probe;
- fixture, seed data, source id, or user-visible input;
- non-sensitive user-visible output or generated artifact; use a redacted
  preview plus artifact handle when the output is sensitive/private;
- durable ground truth inspected, such as SQLite rows, API state, filesystem
  artifact bytes, durable events, rendered UI state, or provider/tool fields;
- trace id, run id, artifact handle, or explicit note that none exists;
- latency and token/cost counters when available;
- tool-call count and tool names;
- mismatch diff or statement that expected and observed facts matched.
- dedicated slice-QA packet: reviewer mode, files/diff inspected, exact
  commands or probes run, ground-truth facts checked, negative/adversarial check
  result or reason none applies, PASS/HOLD verdict, and unresolved risk.

Do not store raw CoT, full child transcripts, secrets, or private payloads as
normal evidence. Store handles, redacted previews, and exact commands whenever
that is enough to reproduce the check.

## Dedicated Slice QA Gate

Every write slice needs its own Q&A pass, but it must be narrow. This gate is
not the full QA pipeline and must not spend tokens rediscovering the whole
project.

Default scope:

- one completed slice diff;
- the active `plan.md`/`benchmark.md` rows for that slice;
- directly affected files and closest tests;
- at most the relevant caller/adapter boundary needed to prove behavior;
- one ground-truth probe beyond "tests passed";
- one negative/adversarial check when the slice can fail unsafe.

Preferred reviewer:

- Use a fresh read-only QA reviewer/subagent when available.
- If subagents are unavailable, run a named, isolated QA pass in the main loop
  and label it `self-audited-slice-qa`. Do not call it independent
  verification.
- If neither a fresh reviewer nor a bounded local QA pass can run, the slice is
  `HOLD`, not complete.

Token and scope limits:

- Do not run the full five-phase `aura-qa-pipeline` for ordinary slices.
- Do not ask a QA reviewer to map the whole codebase.
- Do not inspect unrelated tools/channels/files because they are nearby.
- Do not accept tool-call counts, `200 OK`, startup success, or no panic as QA
  proof.
- Stop QA after the slice-specific PASS/HOLD verdict and record only the packet.

## Atomic Commit Gate

Every write slice ends in exactly one atomic local commit. A slice is not done,
and the next slice must not start, until dedicated slice QA has passed and that
commit exists or the user has explicitly switched the turn to read-only/no-commit
mode.

Rules:

- One slice = one commit. One bug fix = one commit. One planning repair slice =
  one commit. Never batch multiple slices into one commit.
- Commit after tests, dedicated slice QA, and durable evidence updates, not
  before.
- Stage only files owned by the completed slice. Use explicit paths in a mixed
  worktree.
- Keep unrelated dirty or untracked files out of the commit and name them in the
  final report.
- Inspect `git diff --cached` before committing.
- Use a scoped message that names the slice, for example
  `fix(agent): preserve tool result arguments`.
- Push is not part of this gate. Push, PR creation, and CI closure stay under
  the Ship / Deploy Gate and require current-turn authorization.
- If a slice cannot be committed because unrelated changes are entangled in the
  same files, stop and report the exact conflict instead of bundling.

## Failed Verification Debug Loop

When verification fails after a patch:

1. Reproduce the failure with the failing command, probe, or artifact diff.
2. State one hypothesis about the owned cause.
3. Inspect ground truth that can prove or falsify the hypothesis.
4. Patch the smallest owned cause; do not edit tests merely to pass.
5. Rerun the failing command before broader gates.
6. Update the evidence packet with the mismatch, fix, and rerun result.

## Ship / Deploy Gate

Use this gate only when the current user turn explicitly asks to push, deploy,
publish, open a PR, or close CI. Local atomic commit is handled by the Atomic
Commit Gate for every write slice.

Implicitly import existing ship skills instead of creating Aura-specific
duplicates:

- Use `github:github` as the GitHub umbrella when the task involves repository,
  PR, issue, or CI context.
- Use `github:yeet` only when the user asks for a branch/PR publish flow.
- Use `github:gh-fix-ci` when a pushed commit or PR has failing Actions checks.
- Use `finishing-a-development-branch` only when the user is on a branch-style
  integration flow and wants options.

Aura constraints override imported defaults:

- Master-direct workflow is allowed only because Aura explicitly uses it, but
  push still requires current-turn authorization.
- Do not create branches or PRs unless the current user turn asks for that.
- Run `git status --short --untracked-files=all` before staging.
- Stage only files owned by the completed slice. Never stage unrelated dirty or
  untracked files silently.
- If the worktree is mixed, name excluded files in the final report.
- Run relevant local checks before the atomic commit when they were not already
  run.
- After push, verify the CI run for the pushed SHA. Local tests do not replace
  CI verification.
- If CI fails, inspect the failing job/logs, fix in the same slice when owned,
  or report the exact blocker.

Suggested CI commands when using local `gh`:

```powershell
git rev-parse HEAD
gh run list --repo <owner/repo> --commit <sha> --limit 10
gh run watch <run-id> --exit-status
gh run view <run-id> --log-failed
```

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
- a GSD or Ralph queue conflicts with the active Aura phase files,
- a child-skill report has not been translated into Aura phase files,
- the phase folder is not implementation-ready,
- the phase plan lacks `D:/tmp` example evidence or current 2026 best-practice
  source evidence for the selected slice,
- baseline tests fail before any code edit,
- `benchmark.md` lacks concrete ground-truth checks, pass/fail thresholds, or
  PRD-gate coverage,
- dedicated slice QA is missing, overbroad, full-sweep-by-default, or lacks a
  ground-truth assertion,
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
- grounded in the phase's `D:/tmp` example sweep and current 2026 best-practice
  source sweep, or explicitly blocked/not-found with rationale,
- small enough to name affected files before editing,
- guarded by baseline tests from that phase's `benchmark.md`,
- checked by dedicated slice QA with bounded scope and ground-truth evidence,
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
