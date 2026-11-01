# Persisted board progress — 2026-11-01

## Tree and method

Source: `1344ba204` plus the scoped progress changes, after board UI commit `3656f6fe1`.
WSL Go 1.27.3 and native Linux Node 24.18.0. Migration 0149 was the next available slot.
PostgreSQL ran in the task-owned container `aura-postgres-board-progress-20261101`,
database `aura_board_progress_20261101`, loopback port 5449. Existing production data was
not used by the persistence tests.

Progress is a finite ratio in [0,1]; the UI displays round(ratio*100). The installed
SVAR slider is reused with step 0.01. Missing/null create means zero; missing/null update
preserves the stored value. Explicit zero resets it. Movement does not infer progress.

## Browser and real persistence

Playwright drove the production frontend bundle on the authenticated lab origin. Static
assets were page-routed to the local build; board API requests were forwarded to the new
Go AG-UI handlers connected to the disposable database. The remaining API was the lab API.
The composition harness used the existing loopback identity fallback, so this checks the
new HTTP/store path, not new authentication behavior.

- Created progress 0.55 through the real handler; native slider exposed min 0, max 1,
  step 0.01, value 0.55. One ArrowRight saved 0.56; GET and reload displayed 56%.
- Home saved 0, GET and reload displayed 0%; End saved 1, GET and reload displayed 100%.
- Chrome's accessibility tree exposed the named progressbar, value 1, ignored=false.
- Screenshot: ignored `.playwright-mcp/page-2026-11-01T15-00-40-418Z.png`.
- Desktop/mobile layout and live-API save/cancel/move/delete are documented in
  `2026-11-01-board-ui-live-api.md`. No deployment of this frontend/backend to the VM
  is claimed by these browser routes.

## Real agent tool path

Method: `.claude/skills/aura-model-in-the-loop/SKILL.md`. The freshly built Aura CLI ran
the real runner with profile `single_user_hardened`, tool loading and PostgreSQL ledger.
The local hand endpoint supplied controlled model replies; these are not model-quality
results. Each following reply was chosen after inspecting the actual preceding tool result.

| Tool call ID | Action and actual tool result | Ledger end row |
| --- | --- | --- |
| progress_list_retry | list existing board | seq 2, status ok |
| progress_agent_add | add at 0.55; snapshot 55% complete | seq 3, status ok |
| progress_agent_update | update same ID to 0.73; snapshot 73% complete | seq 4, status ok |
| progress_agent_read | list same ID at 73% complete | seq 5, status ok |
| progress_agent_reset | update same ID to 0; snapshot 0% complete | seq 6, status ok |
| progress_agent_verify | list same ID at 0% complete | seq 7, status ok |

Final direct PostgreSQL read: card `2796c5e1-d37f-4d15-8024-bfc38082c20f`, progress 0,
source chat, updated_by agent. Gateway verdict/reason fields in these ledger end rows
were absent; status is the observed success evidence, not an invented verdict string.

An initial attempt timed out while the hand endpoint awaited a reply; no add ran. The
subsequent automated hand loop completed all calls and the CLI printed its final answer
and printed `bye`. The driver's 15-second process-wait expired during telemetry shutdown;
the CLI subsequently stopped, but its exit code was not captured. An earlier setup attempt reached local ArcadeDB using
fixture credentials and was refused; it was stopped before any board mutation. The final
run pointed ArcadeDB to unused loopback port 2499. Memory was unavailable and is not
proven. No external LLM, messaging, embedding or provider delivery is claimed.

## Checks and limits

- RED observed for missing HTTP progress output, missing frontend percentage and the
  absent store input fields; GREEN after implementation.
- Full race tests for internal/board, internal/agui and internal/agent/tools passed;
  internal/db race tests passed after advancing its explicit migration-head contract.
  Vet and the current CLI build passed. SQLC regeneration passed.
- Disposable database board race integration: 91.0% statement coverage. Tests include
  no-op timestamps/actor, omitted update, progress-only update, move/duplicate, explicit
  zero, legacy default and identity isolation.
- Migration 0149 down/up passed, preserving existing card content; database constraints
  rejected negative, >1, NaN, both infinities and NULL. Real-handler HTTP integration
  passed including invalid input and omitted/null preservation.
- Mutation spot-check of validateProgress: 10/11 killed (90.91%). This measures the
  changed validation function, not whole-repository mutation coverage.
- Full frontend suite: 403 test files passed, none failed. V8 coverage: statements
  92.78%, functions 91.72%, branches 87.50%; configured line floor also passed.
  Existing unrelated React act warnings were present.
- Full lint: zero warnings/errors; typecheck, Knip and Prettier passed. Production build
  passed; the existing Shiki C++ chunk warning remains (797 kB, threshold 500 kB).
- Independent source review reported no actionable findings. Persistent Playwright
  regressions cover layout and API progress save/reload/reset/complete; their CI execution
  is separate from the browser-tool evidence above.

Only additive progress persistence and the requested board presentation are covered.
Authentication, memory and deployment retain the explicit boundaries above.

Source commit: `119f77078`. All pre-commit checks passed (format, file cap, frontend
duplication, vet, lint). Task-owned bridge/model processes and the disposable PostgreSQL
container were removed after collecting the evidence.

## Publication and CI follow-up

Source and embedded frontend (`231134593`) were pushed to GitHub and GitLab with all
pre-push gates green, including all 27 integration-tier builds. On that SHA, frontend
unit/coverage, lint, database integration, race tests and publication workflows passed.
The first full Playwright run passed 223 cases and failed the new mobile progress case:
55 rapid ArrowRight calls produced an actual native input value of 0.54 on retry.

A minimal harness using the installed Kanban/Editor/Slider reproduced this under Chromium
mobile emulation with CPU throttling: at rate 4, 2/6 trials saved 0.54; at rate 6, 1/5 did.
Captured native input and Editor change events both ended at 0.54, so the database did not
drop the final increment. The dependency's controlled input updates through React effects.
This does not establish every timing mechanism inside Chromium/React.

The regression now polls the observed slider value while sending keyboard increments,
then blurs and asserts 0.55 before Save; API, reload, zero and one assertions remain.
Three throttled mobile trials of that sequence saved 0.55. The multi-lifecycle test has
a 60-second budget because its first CI attempt exceeded 30 seconds. This intentionally
corrects an unreliable input-generation assumption; it does not change the expected ratio
or claim that the dependency accepts arbitrary bursts of keyboard events without loss.

The first CI run ultimately completed with 28 successful jobs and only that Playwright
job failing. The correction passed focused typecheck, type-aware lint and Prettier;
independent review found no actionable issues. It changes test input generation only,
so the already measured backend, migration and embedded bundle remain unchanged.

## Final result

Commit `ab8a4f1f1` is on GitHub and GitLab. [CI run 41324662123](https://github.com/chetto1983/Aura/actions/runs/41324662123)
completed successfully with all 29 jobs green. CodeQL, Skills, edge image publication and
the installer verification/publication workflow also passed on that SHA. The earlier
source-triggered ArcadeDB MCP publication passed on `231134593`.

Playwright's final successful job reported 222 first-attempt passes, two passes on retry
(the desktop and mobile progress cases), and 112 conditionally skipped cases. Both initial
progress attempts exceeded the 60-second test budget; cleanup then reported a closed page,
so those logs do not identify the original pending step. This is a remaining reliability
limitation of the new E2E scenario, not evidence of a database value mismatch. It is not
claimed to be fixed by the earlier rapid-key correction.

An additional first-attempt browser run used the production bundle, Chromium mobile
emulation with CPU throttling rate 4, the new Go handlers and a recreated task-owned
PostgreSQL database. It saved 0.55, reloaded, then saved 0 and 1; each actual API response
returned the requested ratio. This shares the static-asset/API-routing and authentication
boundaries described above. Only the task-owned card was deleted; the temporary API
processes and database were removed again after the run.

A read-only check of the lab API still did not expose progress consistently in its returned
cards. This session did not redeploy the VM; repository/image publication and VM installation
are distinct outcomes. The available-update dialog was closed locally during the diagnostic
run; no update or postponement action was submitted.
