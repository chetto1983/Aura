# Phase 19 Closure Plan

Date: 2026-05-04

Status: in progress

Scope: close Phase 19 without leaving ambiguous debt around procedural memory, skill-backed scheduled routines, wake gates, and legacy cleanup.

Primary references:

- `docs/implementation-tracker.md`
- `docs/aura-picobot-hermes-inventory-2026-05-03.md`
- `docs/code-inventory-phase-19-2026-05-04.md`
- `docs/llm-wiki.md`

## Goal

Phase 19 should finish with Aura able to learn and run repeatable procedures safely:

- factual memory stays in sources/wiki/archive;
- procedural memory is represented as reviewed skill proposals;
- scheduled routines use skills, toolsets, context anchors, persisted outputs, and wake gates;
- long-running or recurring work has E2E proof, metrics, and no silent mutation path;
- legacy code is classified instead of deleted by instinct.

This phase is not a dashboard phase. UI work is allowed only when it unlocks review, install, or operator decisions.

## Current State

Already shipped:

- code inventory and low-risk cleanup (`19a`);
- review-gated `propose_skill_change` (`19b`);
- live latency gate on the memory scorecard (`19b.1`);
- graph-aware semantic index (`19c`);
- named toolset profiles for jobs and swarm (`19d`);
- skill/context-backed scheduled `agent_job` payloads (`19e`);
- persisted agent-job output, metrics, wake signatures, and deterministic skip gates (`19f`);
- scheduled-routine E2E harness plus log-driven runtime-context fix (`19g`, `19g.1`);
- skill proposal lifecycle decision (`19h`): generic `/summaries` approval is review-only, with install/smoke documented as a manual admin handoff.

Remaining risk:

- real-user prompts need one final drill to ensure this is useful and fast, not just architecturally correct;
- legacy/debt items need final classification so Phase 20 does not inherit vague cleanup.

## Closure Slices

### 19g - Scheduled Routine E2E Harness

Objective: prove that skill-backed agent jobs are cheap, resumable, and not context-hungry.

Implementation:

- Add `cmd/debug_agent_jobs`.
- Use a hermetic temp wiki and temp SQLite scheduler DB.
- Seed a monitored wiki page and a scheduled `agent_job` payload with:
  - `enabled_toolsets`;
  - `skills`;
  - `context_from`;
  - `wake_if_changed`;
  - `notify=false`.
- Run sequence:
  1. first run executes and persists `last_output`, `last_metrics_json`, and `wake_signature`;
  2. second run sees unchanged `wake_if_changed` signal and skips before the LLM call;
  3. mutate the monitored wiki page;
  4. third run executes again and updates the persisted signature.
- Print a compact report:
  - run number;
  - skipped;
  - LLM calls;
  - tool calls;
  - tokens;
  - elapsed milliseconds;
  - wake signature changed yes/no;
  - last output preview.

Verification:

- Unit or integration tests for the harness with a fake LLM.
- Optional `-live-llm` mode for real model latency only when explicitly run.
- `go test ./internal/scheduler ./internal/telegram ./internal/tools ./cmd/debug_agent_jobs`
- `staticcheck` on touched packages.
- Full `verify-go.ps1`.

Done when:

- run -> skip -> mutate -> rerun passes deterministically;
- skipped run makes zero LLM calls;
- persisted output/metrics are visible after each run;
- the tracker records the measured fake and, if run, live metrics.

Debt not allowed:

- no dashboard dependency;
- no broad filesystem/source mutation;
- no direct wiki/skill mutation from the job;
- no recursive scheduling tools inside the job.

### 19h - Skill Proposal Install/Smoke Decision

Objective: make the procedural-memory workflow unambiguous.

Status: done for Phase 19 with Option A. See `docs/plans/2026-05-04-skill-proposal-lifecycle.md`.

Decision first:

- Option A, minimal close: skill proposals stay review-only in Phase 19; approved proposals are marked reviewed, and install/smoke is a documented manual operator step for Phase 20. **Chosen.**
- Option B, stronger close: approval can install the skill through the existing admin-gated skill installer, run the proposal smoke prompt, and record pass/fail.

Preferred implementation if choosing Option B:

- Add a small backend service for skill proposal approval:
  - load pending proposal;
  - validate `SKILL.md` again;
  - install/update/delete only behind admin/review gate;
  - run smoke prompt in a bounded fake or live harness depending on mode;
  - record smoke result in proposal provenance or a dedicated metadata field;
  - if smoke fails, keep the proposal visible and mark it failed/quarantined.
- Keep the generic `/summaries` review behavior safe: approval must not silently mutate wiki pages or skill files unless the explicit skill workflow is invoked.

Verification:

- Tests for create/update/delete skill proposal review paths.
- Tests that normal summary approval still does not mutate skills.
- Tests that failed smoke leaves the skill uninstalled or quarantined.

Done when:

- every skill proposal has a clear lifecycle:
  - draft;
  - review;
  - install/smoke or documented manual handoff;
  - usable or failed/quarantined.

Debt not allowed:

- no model-direct `create_skill` write path;
- no silent install on plain proposal creation;
- no unbounded smoke execution;
- no credential leakage in smoke logs.

### 19i - Real-User Routine Drill

Objective: test usefulness from normal prompts, not only code paths.

Prompts:

1. "Ogni mattina usa questa skill e mandami solo le cose importanti."
2. "Monitora questa pagina e avvisami solo se cambia davvero."
3. "Esegui ora il job e dimmi cosa hai saltato perche invariato."
4. "Crea una skill dal modo in cui facciamo il briefing."

Metrics to record:

- elapsed milliseconds;
- LLM calls;
- tool calls;
- tokens;
- selected tools;
- proposal created yes/no;
- skipped correctly yes/no;
- output usefulness notes.

Acceptance:

- at least 3 prompts pass in the live or realistic harness;
- no job uses recursive/dangerous tools;
- recurring routines stay propose-only by default;
- user-visible output is concise and actionable;
- slow answers are treated as failures or follow-up work, not "technically passed".

Debt not allowed:

- no prompt-specific hacks;
- no ignoring latency;
- no success criteria based only on "the model eventually got there".

### 19j - Legacy And Debt Closure

Objective: finish Phase 19 cleanup without unsafe deletion.

Classify:

- Wiki `.yaml` fallback:
  - keep if any production wiki may still contain `.yaml`;
  - remove only after an explicit migration check.
- `search_wiki`:
  - keep as a narrow wiki-only tool unless a replacement route covers every caller/test.
- Scheduler migrations:
  - keep; existing `aura.db` files may still need them.
- `web/node_modules/flatted/golang/pkg/flatted` in `go list ./...`:
  - mitigate if it pollutes normal Go verification;
  - do not delete `node_modules` blindly.
- Generated dashboard assets and packaging files:
  - leave untouched unless taking a frontend/build/release slice.

Acceptance:

- each item is marked `keep`, `remove`, or `defer` with a reason;
- any removal has targeted tests and full Go verification;
- deferred items are not listed as "unknown debt".

Debt not allowed:

- no broad cleanup commit;
- no deleting compatibility code because it "looks old";
- no staging `.env`, raw wiki data, databases, or generated churn.

### 19-close - Formal Closure

Objective: make the tracker a clean handoff into Phase 20.

Tasks:

- Update `docs/implementation-tracker.md`:
  - mark 19g/19h/19i/19j as done or explicitly deferred;
  - add Phase 19 closure criteria;
  - mark Phase 19 as closed;
  - open Phase 20.
- Add a concise final status note:
  - what shipped;
  - what was intentionally deferred;
  - which commands passed;
  - next phase recommendation.

Closure criteria:

- code inventory exists and verified cleanup is done;
- procedural memory proposals exist and are review-gated;
- skill proposal install/smoke path is implemented or explicitly deferred with owner-facing workflow;
- named toolsets are used by scheduled jobs and swarm;
- skill-backed scheduled routines support `skills`, `enabled_toolsets`, `context_from`, and `wake_if_changed`;
- agent-job outputs, metrics, and wake signatures persist;
- E2E harness proves run -> skip -> mutate -> rerun;
- real-user routine drill passes or records concrete follow-up failures;
- legacy/debt is classified.

## Phase 20 Recommendation

Phase 20 should focus on useful autonomy:

1. hot profile and operator preferences;
2. watchers with thresholds and wake gates;
3. "what do you know that might be old?" memory review;
4. self-improvement proposals after repeated failures, slow runs, or repeated manual workflows.

Avoid starting Phase 20 with another dashboard unless it directly supports review, monitoring, or trust.

## Execution Order

1. 19g - E2E harness.
2. 19h - skill proposal install/smoke decision and workflow.
3. 19i - real-user routine drill.
4. 19j - legacy/debt closure.
5. 19-close - tracker closure and Phase 20 opening.

## Commit Discipline

- One commit per slice.
- Stage explicit files only.
- Do not commit `.env`, `.claude/settings.local.json`, `.codex`, `wiki/raw`, database files, binaries, generated dashboard dist churn, or unrelated local edits.
- For docs-only plan edits, no full verification is required; for code slices, run the project loop verification before commit.
