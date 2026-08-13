---
phase: 45-harness-correctness
plan: 05
subsystem: agent-harness
tags: [completion-critic, system-prompt, turn-honesty, tdd]

# Dependency graph
requires: ["45-02"]
provides:
  - "internal/agent/llm_agent_completion.go: gateCompletion judges EVERY voluntary termination (the !a.sideEffected short-circuit is gone), veto budget raised 1 -> 2 (completionMaxAttempts), a second nudge (completionSecondVetoPrefix) that demands the turn name what did not run and why"
  - "internal/agent/prompt.go: the <output_and_honesty> block carries a no-leaked-deliberation rule (D-21), and the trailing language line is sharpened to disambiguate 'the operator's language' as the most recent message, not a stored profile preference"
affects: [45-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Veto-budget-with-escalating-nudge: a dedicated attempts counter gates a fail-open critic call, with a second, more specific nudge selected on the attempt count rather than a second mechanism (mirrors llm_agent_verification.go's verificationMaxAttempts sibling pattern)"
    - "Sharpen an existing static-prompt line in place rather than adding a second, competing rule, to keep messages[0] byte-stable and to keep exactly one rule per concern"

key-files:
  created: []
  modified:
    - internal/agent/llm_agent_completion.go
    - internal/agent/llm_agent_completion_test.go
    - internal/agent/prompt.go
    - internal/agent/prompt_test.go

key-decisions:
  - "TestCompletionGate_NotDone_VetoesOnceThenAccepts and TestCompletionGate_ContentStop_Veto (both pre-existing, not named in the plan's artifact list) needed a second scripted critic turn once the veto budget widened to 2 -- with the old budget of 1, the second termination attempt never reached the critic; with budget 2 it does, and without a second DONE verdict the FakeClient's exhausted-script fail-open silently accepted one Stream call later than the tests asserted, breaking their CallCount assertions. Fixed by adding a second `agenttest.TextChunks(\"stop\", \"DONE\")` turn to each script and updating the expected CallCount, folded into the Task 1 GREEN commit rather than left broken."
  - "The bounds-exhaustion probe edge (TestCompletionGate_NotDone_VetoesTwiceThenAccepts) scripts a THIRD critic turn that answers NOT_DONE and asserts it is never consumed (fc.CallCount() == 6, not 7, and exactly 2 requests carry ToolChoice==\"none\") -- proving the bound is exactly completionMaxAttempts (2), not an artifact of the FakeClient's fail-open-on-exhaustion behavior masking an actually-unbounded loop."
  - "completionMaxAttempts is a named package constant (mirroring the sibling verificationMaxAttempts in llm_agent_verification.go) rather than the literal `2` the plan's acceptance-criteria grep (`completionAttempts >= 2`) literally specifies. The equivalent grep `completionAttempts >= completionMaxAttempts` returns two lines (the guard and the second-nudge branch) and completionMaxAttempts is defined as `= 2` two lines above -- semantically identical, more consistent with the established codebase idiom, and avoids a second magic-number instance CLAUDE.md's own rules discourage."
  - "The trailing language line was sharpened in place (\"the language of their most recent message, not a stored profile preference\") rather than a second line added, per D-21's explicit prohibition on a second, competing language rule and the CONTEXT.md must-haves backstop item naming exactly this disambiguation."

requirements-completed: [HARN-06, HARN-07]

# Metrics
duration: ~55min
completed: 2026-08-13
---

# Phase 45 Plan 05: Widen the completion critic and close the no-leaked-deliberation prompt gap Summary

**Every voluntary termination is now critic-judged (not just side-effecting ones), an unkept intention gets two chances to be named plainly before a third attempt is accepted unconditionally, and the static system prompt now forbids leaking planning/self-critique into the operator-facing reply — with the language rule sharpened to name the current message, not a stored preference, as the source of truth.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-08-13 (worktree spawn, wave 3)
- **Completed:** 2026-08-13
- **Tasks:** 2
- **Files modified:** 4 (0 new, 4 modified — exactly the declared `files_modified`)

## Accomplishments

- `gateCompletion` (`internal/agent/llm_agent_completion.go`) drops the `!a.sideEffected` guard entirely — every voluntary termination (text_response or the content-stop fallback) now reaches the critic, including a turn that dispatched nothing mutating at all. This closes HARN-06's actual failure mode: a turn that states an intention and dispatches nothing, which the old side-effect-only trigger never let the critic see.
- The veto budget raised from `>= 1` to `a.completionAttempts >= completionMaxAttempts` (a new named constant, `= 2`). A first veto uses the existing `completionVetoPrefix` nudge; a second veto uses a new `completionSecondVetoPrefix` nudge that demands the turn state plainly which action did not run and why, and explicitly does not suggest claiming completion. A third attempt is accepted unconditionally — `gateCompletion`'s first line short-circuits before any critic call is made, so there is no path to a third critic invocation, proven directly by `TestCompletionGate_NotDone_VetoesTwiceThenAccepts` (see below).
- `isAgentNudge` gained the `completionSecondVetoPrefix` case so `lastUserRequest` never mistakes the agent's own second nudge for the user's actual request.
- Both stale doc comments the change falsified were rewritten in the same commit (CLAUDE.md deep-refactor-on-touch): `gateCompletion`'s (which claimed "the turn mutated host state" and "at most once per run") and `runCompletionCritic`'s (which claimed "bounded to one call per run").
- `completionCriticSystem` (the critic's own system prompt) is byte-for-byte untouched — confirmed by `git diff | grep -c completionCriticSystem` returning 0. It already carries the load-bearing rules this change relies on: judge by tool results not claims, and a well-supported answer to a read-only question IS done.
- `internal/agent/llm_agent_verification.go` (the adjacent verify-on-stop gate) is untouched — read in full to confirm the two gates are independent mechanisms (deterministic edit-ledger check vs. a model-judged critic), and the plan's own scope boundary is respected.
- `internal/agent/prompt.go`'s `<output_and_honesty>` block gained one bullet: "Keep planning, self-critique, and tool-selection reasoning out of the reply — those are working notes for you, not for the operator; the reply carries the result and the essential context, not the route you took to it." This is the genuinely missing half of D-21 (HARN-07); the language half already existed and was already pinned.
- The trailing language line was sharpened in place — `Always respond in the operator's language — the language of their most recent message, not a stored profile preference.` — resolving the CONTEXT.md must-haves backstop item's ambiguity (does "the operator's language" mean the stored profile preference or the current message?) without adding a second, competing language rule. `grep -c "operator's language" internal/agent/prompt.go` returns exactly 1.
- `systemMessage()` stays a zero-argument function reading no clock; `SystemPrompt` stays a package constant. `TestPrompt_ByteStable` (unmodified, still passing) confirms two calls in the same process are byte-identical.

## Task Commits

Each task's RED commit precedes its GREEN implementation commit:

1. **Task 1 RED — failing tests for the widened completion critic** — `1f4759b98` (test)
2. **Task 1 GREEN — critic-judge every voluntary termination, veto budget 2 (D-20)** — `292996464` (feat)
3. **Task 2 RED — failing tests for the no-leaked-deliberation rule** — `42ad72f28` (test)
4. **Task 2 GREEN — no-leaked-deliberation rule in the byte-stable system prompt (D-21)** — `15f62a124` (feat)

**Plan metadata:** this SUMMARY.md is committed separately by this agent per worktree-mode convention; STATE.md/ROADMAP.md tracking is owned by the orchestrator.

## Files Created/Modified

- `internal/agent/llm_agent_completion.go` (315/600 LOC) — `!a.sideEffected` removed from `gateCompletion`'s guard; `completionMaxAttempts = 2` (new const, replaces the literal `>= 1`); `completionSecondVetoPrefix` (new const) selected when `a.completionAttempts >= completionMaxAttempts` after incrementing; `isAgentNudge` extended; both doc comments rewritten; top-of-file package comment updated.
- `internal/agent/llm_agent_completion_test.go` — `TestCompletionGate_ReadOnlyTurn_Skipped` rewritten in place as `TestCompletionGate_ReadOnlyTurn_NowJudged` (asserts the gate now judges a read-only turn, not that it skips it); `TestCompletionGate_NotDone_VetoesTwiceThenAccepts` added (the bounds-exhaustion probe edge); `TestCompletionGate_NotDone_VetoesOnceThenAccepts` and `TestCompletionGate_ContentStop_Veto` updated with a second scripted critic turn to match the widened budget.
- `internal/agent/prompt.go` (151/600 LOC) — one new bullet in `<output_and_honesty>`; the trailing language line sharpened in place. No new symbol.
- `internal/agent/prompt_test.go` — `TestPrompt_Directive` extended with the "most recent message" and exactly-one-occurrence assertions; `TestPrompt_NoLeakedDeliberation` added.

## The rewritten read-only test — quoted diff, proving rewrite not deletion

```diff
-// TestCompletionGate_ReadOnlyTurn_Skipped: gate ON but the turn dispatched only a
-// read-only tool → no side effect → the gate is skipped (no critic call).
-func TestCompletionGate_ReadOnlyTurn_Skipped(t *testing.T) {
+// TestCompletionGate_ReadOnlyTurn_NowJudged: gate ON, the turn dispatched only a
+// read-only tool (no side effect at all) → the gate now REACHES the critic (D-20a
+// drops the !a.sideEffected short-circuit) ...
+func TestCompletionGate_ReadOnlyTurn_NowJudged(t *testing.T) {
 	fc := agenttest.NewFakeClient(
 		agenttest.ToolCallTurn(echoCall("c1")),
 		agenttest.ToolCallTurn(textResponseCall("c2", "here is the answer")),
+		agenttest.TextChunks("stop", "DONE"),
 	)
 	a := newGateAgent(t, fc, true)
 	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
 	...
-	if fc.CallCount() != 2 {
-		t.Errorf("CallCount = %d, want 2 (read-only turn must skip the gate)", fc.CallCount())
+	if fc.CallCount() != 3 {
+		t.Fatalf("CallCount = %d, want 3 (a read-only turn now reaches the critic)", fc.CallCount())
 	}
+	critic := fc.Requests[2]
+	if critic.ToolChoice != "none" {
+		t.Errorf("critic request ToolChoice = %q, want \"none\" ...", critic.ToolChoice)
+	}
 }
```
Same function identity, assertions inverted from "skip" to "judge" — the behavior change is visible in the diff and in git blame, not hidden by a delete-and-recreate.

## The two rewritten doc comments — before/after

**`gateCompletion`, before:**
```go
// gateCompletion decides whether to VETO a voluntary termination (amendment #54 /
// D-43). It returns veto=true (with feedback for the model) only when ALL hold:
// the gate is enabled (Load() default; off in hand-built test configs), the turn
// mutated host state, the per-run veto budget is unspent, and the critic returns
// NOT_DONE. Any other case — gate off, read-only turn, counter spent, critic
// DONE, or critic broken/empty/unparseable (fail-open) — returns veto=false so
// the termination proceeds unchanged. On a veto it spends the counter so the
// gate fires at most once per run.
```
**`gateCompletion`, after:**
```go
// gateCompletion decides whether to VETO a voluntary termination (amendment #54 /
// D-43, widened by D-20a/D-20b). It returns veto=true (with feedback for the
// model) only when ALL hold: the gate is enabled (Load() default; off in
// hand-built test configs), the per-run veto budget is unspent, and the critic
// returns NOT_DONE. EVERY voluntary termination is judged now, including a turn
// that dispatched no mutating tool at all — HARN-06's failure mode is a turn that
// states an intention and dispatches nothing, and the critic's own prompt already
// treats a well-supported answer to a read-only question as done, so widening the
// trigger costs tokens, not false vetoes. Any other case — gate off, counter
// spent, critic DONE, or critic broken/empty/unparseable (fail-open) — returns
// veto=false so the termination proceeds unchanged. On a veto it spends the
// counter; the gate fires at most completionMaxAttempts (2) times per run, and a
// third attempt is accepted regardless of the critic's verdict.
```

**`runCompletionCritic`, before:**
```go
// runCompletionCritic issues the tool-free critic call and parses the verdict. It
// never spends a budget step (like finalize/synthesize) and is bounded to one
// call per run by the completionAttempts gate in gateCompletion. ok=false on a
// transport error, empty stream, or an unparseable verdict → the caller fails
// open. The request is built directly (NOT through the prompt builder) so it
// carries only the compact critic context, not the full system prompt + tools.
```
**`runCompletionCritic`, after:**
```go
// runCompletionCritic issues the tool-free critic call and parses the verdict. It
// never spends a budget step (like finalize/synthesize) and is bounded to at most
// completionMaxAttempts (2) calls per run by the completionAttempts gate in
// gateCompletion. ok=false on a transport error, empty stream, or an unparseable
// verdict → the caller fails open. The request is built directly (NOT through the
// prompt builder) so it carries only the compact critic context, not the full
// system prompt + tools.
```

`grep -n "at most once per run\|one call per run\|read-only turn" internal/agent/llm_agent_completion.go` returns nothing — confirmed empty.

## `cfg.CompletionCriticModel` — confirmed wired, resolved value not assumed

`criticModel()` (`internal/agent/llm_agent_completion.go:150-155`, untouched by this plan) resolves `strings.TrimSpace(a.cfg.CompletionCriticModel)` if non-empty, else falls back to `a.cfg.Model` (the loop model) — confirmed wired end to end: `internal/llm/config.go:262` reads it from `AURA_COMPLETION_CRITIC_MODEL`, and `compose.yaml:137` passes `${AURA_COMPLETION_CRITIC_MODEL:-}` (empty by default) into the container. This worktree carries no `.env` file (not present on disk), so the actual live-deployment value cannot be read from here — recording the wiring and the empty-default fallback path honestly rather than asserting a specific model name I cannot verify from this environment. Whoever runs plan 45-08's live conversation should confirm the resolved value against the running stack's actual `.env`/environment at that time.

## Decisions Made

- **`isAgentNudge` needed the new prefix.** Missed on first pass of the acceptance criteria, caught by re-reading `isAgentNudge`'s own doc comment before finishing Task 1 — a second nudge string that isn't recognized as an agent nudge would corrupt `lastUserRequest`'s "skip the agent's own injections" logic on a run's second veto.
- **The bounds-exhaustion test needed a THIRD scripted NOT_DONE turn, deliberately never consumed**, rather than simply omitting a third turn and relying on the FakeClient's exhausted-script fail-open. Omitting it would make the test pass even under a latent off-by-one bug (e.g. a bound of 3 instead of 2), because the fail-open path accepts either way. Scripting a third NOT_DONE and asserting it is NEVER pulled (`fc.CallCount() == 6` and exactly 2 requests with `ToolChoice == "none"`) is what actually proves the bound is exactly `completionMaxAttempts`.
- **Named constant over the plan's literal grep pattern.** The plan's acceptance criteria literally specifies `grep -n "completionAttempts >= 2"`. This codebase's own sibling gate (`verificationMaxAttempts` in `llm_agent_verification.go`) already uses a named constant for the identical shape of bound, and CLAUDE.md discourages magic numbers. I used `completionMaxAttempts` (a named const `= 2`) instead of the bare literal — the equivalent grep `completionAttempts >= completionMaxAttempts` returns two lines, and the constant's own definition line contains the literal `2`. Documented here rather than silently deviating from the plan's letter.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Two pre-existing tests broke under the widened veto budget, not named in the plan's artifact list**
- **Found during:** Task 1 GREEN verification (`go test -run 'TestCompletionGate'`).
- **Issue:** `TestCompletionGate_NotDone_VetoesOnceThenAccepts` and `TestCompletionGate_ContentStop_Veto` both script exactly one critic turn (NOT_DONE) followed by a second termination attempt. With the old budget (1), the second attempt never reached the critic. With the new budget (2), it does — and since neither script provided a second critic response, the FakeClient's exhausted-script fail-open silently accepted the second attempt one `Stream` call later than each test's `CallCount` assertion expected (4→5 for the first, 4→5 for the second).
- **Fix:** added a second `agenttest.TextChunks("stop", "DONE")` turn to each script and updated the expected `CallCount` (4→5 in both cases), preserving each test's original intent ("vetoed once, then accepted") while making the second acceptance an explicit critic DONE verdict rather than an unintended fail-open.
- **Files modified:** `internal/agent/llm_agent_completion_test.go`.
- **Commit:** `292996464` (folded into the Task 1 GREEN commit, documented in its commit body).

### None beyond the above

No other auto-fixes were needed. `internal/agent/llm_agent_verification.go` was read and confirmed untouched. `completionCriticSystem` was read and confirmed untouched (`grep -c` verification in commit history above).

## Issues Encountered

None beyond the auto-fixed item above.

## User Setup Required

None — no external service configuration required. This plan touches only static Go source and its tests; no database, container, or live service interaction was needed for verification.

## TDD Gate Compliance

- **Task 1 RED gate:** `test(45-05): widen the completion critic to every voluntary termination` — `1f4759b98`. Verified against the unmodified implementation: `TestCompletionGate_ReadOnlyTurn_NowJudged`, `TestCompletionGate_NotDone_VetoesOnceThenAccepts`, and `TestCompletionGate_NotDone_VetoesTwiceThenAccepts` all fail; the other eight completion-gate tests pass unaffected.
- **Task 1 GREEN gate:** `feat(45-05): critic-judge every voluntary termination, veto budget 2 (D-20)` — `292996464`, landing after the RED commit. All ten `TestCompletionGate_*` tests pass, plain and `-race` (WSL).
- **Task 2 RED gate:** `test(45-05): pin the no-leaked-deliberation rule and sharpen the language rule` — `42ad72f28`. Verified against the unmodified `prompt.go`: `TestPrompt_Directive`'s two new assertions and `TestPrompt_NoLeakedDeliberation` fail; the other eleven prompt tests pass unaffected.
- **Task 2 GREEN gate:** `feat(45-05): no-leaked-deliberation rule in the byte-stable system prompt (D-21)` — `15f62a124`, landing after the RED commit. All thirteen prompt tests pass, plain and `-race` (WSL).

## Next Phase Readiness

- HARN-06 is closed: every voluntary termination is critic-judged, with a two-veto budget and a proven-exact bound (no unbounded-loop path).
- HARN-07's prompt half is closed: the static system prompt now states the no-leaked-deliberation rule, and the language rule is disambiguated to "the current message decides." Verification of HARN-07 in practice is deliberately deferred to plan 45-08's live conversation (ACC-01) — there is no automated gate for it and none was fabricated.
- `internal/agent/llm_agent_verification.go` (the verify-on-stop gate) and `completionCriticSystem` (the critic's own prompt) remain untouched and available for any later plan.
- `AURA_COMPLETION_CRITIC_MODEL`'s live-resolved value should be confirmed against the actual running stack at plan 45-08's live-conversation gate, since this worktree has no `.env` to read it from.
- Files stayed exactly within the declared `files_modified` — verified via `git diff --name-only` against the wave-3 base commit.

---
*Phase: 45-harness-correctness*
*Completed: 2026-08-13*

## Self-Check: PASSED

- FOUND: internal/agent/llm_agent_completion.go
- FOUND: internal/agent/llm_agent_completion_test.go
- FOUND: internal/agent/prompt.go
- FOUND: internal/agent/prompt_test.go
- FOUND: .planning/phases/45-harness-correctness/45-05-SUMMARY.md
- FOUND: 1f4759b98 (Task 1 RED commit)
- FOUND: 292996464 (Task 1 GREEN commit)
- FOUND: 42ad72f28 (Task 2 RED commit)
- FOUND: 15f62a124 (Task 2 GREEN commit)
