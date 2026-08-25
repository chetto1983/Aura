---
phase: 52-mid-turn-steering
plan: 02
subsystem: agent
tags: [steering, tdd, agent-loop, prompt-injection-defense, kv-cache, internal-steer]

requires:
  - phase: 52-mid-turn-steering (plan 01)
    provides: "Config.AGUISteer{Enabled,Max,MaxBytes} — the caps this plan reads, never hardcodes"
provides:
  - "internal/steer: a conversation-id-keyed bounded FIFO steer inbox (Push/Drain/Close), single-replica by construction"
  - "internal/agent: both drain points wired into the round loop, the nonce-marked <user_steer> attribution envelope, the lookalike scrub ahead of renderToolResultForPrompt's trust branch, and SteerChannelNote concatenated into SystemPrompt"
  - "The narrow SteerInbox consumer interface, so 52-04/52-06's composition root can wire *steer.Inbox in without internal/agent taking a hard dependency on the concrete type"
affects: [52-04, 52-05, 52-06, 52-07, 52-08]

actuals:
  tokens: 9800
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Concern-split sibling file for a loop concern too big for llm_agent.go's LOC ceiling (llm_agent_finalize.go's own precedent, now shared by llm_agent_steer.go)"
    - "Scrub-ahead-of-branch: a defense that must see literal (unescaped) bytes runs BEFORE the escaping branch, never after"
    - "RED commits ship a compiling stub (not an undefined symbol) when the pre-commit hook vets the whole tree — phase 45-04's established convention, reused twice this plan"

key-files:
  created:
    - internal/steer/inbox.go
    - internal/steer/inbox_test.go
    - internal/agent/llm_agent_steer.go
    - internal/agent/llm_agent_steer_test.go
  modified:
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_construct.go
    - internal/agent/event.go
    - internal/agent/prompt.go
    - internal/agent/trust.go
    - internal/agent/agent_fuzz_test.go
    - internal/config/config_document_retrieval.go

key-decisions:
  - "Marker shape (Claude's Discretion, D-07): <user_steer nonce=\"HEXHEX...\">TEXT</user_steer>, minted by the SAME toolOutputNonce() trust.go already owns — reused, not re-implemented. steerMarkerOpen = `<user_steer nonce=\"` and steerMarkerClose = `</user_steer>` are package-visible so 52-04/52-06/52-07/52-08 and tests can locate the marker without re-deriving the tag shape."
  - "wrapUserSteer does NOT HTML-escape the operator's text (unlike wrapUntrustedToolOutput) — the envelope wraps trusted words appended OUTSIDE the already-escaped tool envelope, so double-escaping would corrupt what the operator actually said."
  - "SteerDelta wire shape: {conversation_id, round, steers: [{id, source, text, delivery}]} where delivery is \"tool_result_append\" or \"user_message_fallback\". One Event per drain call, aggregating every message delivered in that drain (not one Event per message)."
  - "scrubSteerLookalikes runs on the RAW preview inside renderToolResultForPrompt, ahead of the trusted/untrusted branch, and is a REGEX-based structural match on the full open+content+close tag shape (any/absent nonce) — HTML-escaping only the matched span (inert, not deleted). A bare mention of the tag name, or a truncated tag missing its close, does not match and passes through untouched."
  - "Drain point A sits AFTER modelRound is assigned (not at the budget-assembly line the pattern map cited, which drifted — <no_stale_inputs> warned exactly this) so the SteerDelta payload can carry a real round ordinal; still strictly before the request is built, and still guarded on !transportRetry."
  - "llm_agent.go's cumulative touch for this whole plan (RED + GREEN) is 12 added / 0 deleted lines against the pre-plan baseline — at the acceptance ceiling, not under it. All real logic lives in the sibling file; llm_agent.go gained only the two struct fields (single-line trailing comments) and the two drain call sites."

patterns-established:
  - "A scrub or trust decision that depends on seeing literal (pre-escaping) bytes must run strictly before the branch that would escape them — verified here by a fuzz-test restatement (FuzzRenderToolResultForPrompt) rather than merely a unit test, so the invariant survives arbitrary future input."

requirements-completed: [STEER-01, STEER-02]

coverage:
  - id: D1
    description: "internal/steer ships a conversation-id-keyed bounded FIFO inbox (Push validates empty/whitespace -> oversize -> closed -> queue-full in order, each with a distinct sentinel; Drain pops-and-clears atomically; Close keeps queued messages but refuses new pushes), configured (never hardcoding the ratified caps), race-clean under -race including concurrent push/drain with no lost or duplicated message"
    requirement: "STEER-01"
    verification:
      - kind: unit
        ref: "internal/steer/inbox_test.go#TestInboxPushDrainFIFO"
        status: pass
      - kind: unit
        ref: "internal/steer/inbox_test.go#TestInboxCapsAtMax"
        status: pass
      - kind: unit
        ref: "internal/steer/inbox_test.go#TestInboxRejectsEmptyAndOversize"
        status: pass
      - kind: integration
        ref: "internal/steer/inbox_test.go#TestInboxConcurrentPushDrain (WSL -race)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Both round-boundary drain points are wired into the LlmAgent loop; a drained steer lands as a suffix on the last tool-result message (outside its trust envelope) or as a new user message when the tail is not a tool result; the model is taught (SteerChannelNote, SystemPrompt) to trust the marked text and ignore lookalikes; the round loop never spends extra budget on a drain"
    requirement: "STEER-01"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_steer_test.go#TestDrainSteerAppendsToLastToolResult"
        status: pass
      - kind: unit
        ref: "internal/agent/llm_agent_steer_test.go#TestDrainSteerFallsBackToUserMessage"
        status: pass
      - kind: unit
        ref: "internal/agent/llm_agent_steer_test.go#TestSteerMarkerSitsOutsideToolOutputEnvelope"
        status: pass
      - kind: integration
        ref: "internal/agent/ (WSL -race, whole package)"
        status: pass
    human_judgment: false
  - id: D3
    description: "A steer costs no extra step or wallclock budget (STEER-02): drainSteer calls neither ConsumeStep nor any deadline mutator, proven by a Budget snapshot taken before/after a drain of 0, 1 and several steers"
    requirement: "STEER-02"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_steer_test.go#TestDrainSteerDoesNotConsumeBudget"
        status: pass
    human_judgment: true
    rationale: "The code invariant is proven here; STEER-02's OTHER half — a live A/B on aura.conversation_turns comparing a steered and an unsteered run (D-13) — is explicitly deferred to 52-08 per the plan's own FA-1 flagged assumption. A green invariant test alone does not close STEER-02 (ACC-01)."
  - id: D4
    description: "The steer marker never enters the cacheable prefix (messages[0..2] byte-identical with/without a steer); a forged marker is neutralised on the branch that does not escape it (TRUSTED), while legitimate lookalike prose survives, proven against a fixture the real render path produced; the scrub never disturbs the dedup input (run.Result.Preview)"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_steer_test.go#TestSteerNeverEntersCacheablePrefix"
        status: pass
      - kind: unit
        ref: "internal/agent/llm_agent_steer_test.go#TestScrubMatchesLiteralNotEscapedForm"
        status: pass
      - kind: unit
        ref: "internal/agent/llm_agent_steer_test.go#TestScrubKeepsLegitimateLookalikeProse"
        status: pass
      - kind: unit
        ref: "internal/agent/llm_agent_steer_test.go#TestScrubLeavesDedupInputUntouched"
        status: pass
      - kind: e2e
        ref: "internal/agent/agent_fuzz_test.go#FuzzRenderToolResultForPrompt (WSL, 30s, 358999 execs, 0 new corpus failures)"
        status: pass
    human_judgment: false

duration: 44min
completed: 2026-08-25
status: complete
---

# Phase 52 Plan 02: Mid-turn steering — agent-side drain points Summary

**A conversation-keyed bounded steer inbox plus two round-boundary drain points that deliver an operator redirect behind a crypto/rand-nonce `<user_steer>` envelope, appended outside the untrusted tool-output wrapper and scrubbed of forgeries on the branch that doesn't escape them — at zero extra budget cost and with the cacheable `messages[0..2]` prefix untouched.**

## Performance

- **Duration:** 44 min
- **Started:** 2026-08-25T16:21:35+02:00
- **Completed:** 2026-08-25T17:05:02+02:00
- **Tasks:** 2 (each RED→GREEN)
- **Files modified:** 11 (4 created, 7 modified)

## Accomplishments

- `internal/steer.Inbox`: a mutex-guarded `map[string][]Message` keyed on conversation id (never run id, D-01 — the Telegram dispatch path has no run identity in scope at all), with `Push`/`Drain`/`Close`, four distinct sentinel errors, and byte-counted (not rune-counted) size caps read from `Config`, never hardcoded.
- `internal/agent/llm_agent_steer.go` (new sibling of `llm_agent_finalize.go`): `SteerInbox` interface, `wrapUserSteer` (reuses `trust.go`'s sole `toolOutputNonce()` minter), `scrubSteerLookalikes` + its structural regex, and `drainSteer` — the single function implementing both the tool-result-suffix delivery and the user-message fallback as one branch.
- Both round-boundary drain points wired into `LlmAgent.Run`: before the API call (skipped on an exact transport retry) and after a non-terminal dispatch, each a two-line `if ev := a.drainSteer(...); ev != nil && !yield(ev, nil) { return }` block.
- `Actions.SteerDelta` (additive `omitempty` map, matching `ArtifactDelta`/`ViewDelta`'s untyped-wire-payload convention) and the `SteerChannelNote` static teaching block concatenated into `SystemPrompt` — a one-time cacheable-prefix change, never per-turn.
- `trust.go`'s `renderToolResultForPrompt` calls the scrub ahead of the trusted/untrusted branch on the raw preview, never writing back to `res.Preview`; `agent_fuzz_test.go`'s `FuzzRenderToolResultForPrompt` restates its trusted-passthrough invariant against `scrubSteerLookalikes(preview)` (with a new live-marker seed) instead of the now-false `out == preview`.

## Task Commits

Each TDD task landed as RED then GREEN (Task 2's GREEN commit unexpectedly also carries an unrelated Rule-3 unblock fix — see Deviations):

1. **Task 1 RED: failing test for the steer inbox** — `5250c619b` (test)
2. **Task 1 GREEN: implement the steer inbox** — `b53bf2320` (feat)
3. **Task 2 RED: failing test for both drain points + scrub** — `ec181d05d` (test)
4. **Task 2 GREEN: implement both drain points + scrub** (bundled with the Rule-3 config-lint fix) — `60836960e` (fix+feat)

_Note: a `git pull` merge (`18231becc`) landed an unrelated commit (`ad0bee571`) between Task 1's GREEN and Task 2's RED — see Deviations for its effect._

## Files Created/Modified

- `internal/steer/inbox.go` — the conversation-id-keyed bounded FIFO steer inbox
- `internal/steer/inbox_test.go` — FIFO/cap/boundary/concurrency tests
- `internal/agent/llm_agent_steer.go` — both drain points' shared logic, the marker minter, the lookalike scrub
- `internal/agent/llm_agent_steer_test.go` — drain, marker-position, budget-invariant, and scrub tests
- `internal/agent/llm_agent.go` — `steer SteerInbox` / `Steer SteerInbox` fields; two `drainSteer` call sites in the round loop
- `internal/agent/llm_agent_construct.go` — threads `cfg.Steer` into `LlmAgent.steer`
- `internal/agent/event.go` — additive `Actions.SteerDelta` field
- `internal/agent/prompt.go` — `SteerChannelNote` constant, concatenated into `SystemPrompt`
- `internal/agent/trust.go` — one `scrubSteerLookalikes` call site ahead of the trust branch
- `internal/agent/agent_fuzz_test.go` — restated `FuzzRenderToolResultForPrompt` invariant + live-marker seed
- `internal/config/config_document_retrieval.go` — two missing doc comments (Rule 3, unrelated unblock — see Deviations)

## Decisions Made

See `key-decisions` in the frontmatter for the marker shape, the SteerDelta wire shape, the scrub's structural-regex design, drain point A's exact placement, and the llm_agent.go line-budget accounting.

## Deviations from Plan

### Informational (environment/process, not Rule 1-4 code fixes)

**1. A concurrent `git pull` merged an unrelated commit mid-plan.** Between Task 1's GREEN commit and Task 2's RED commit, a scheduled `git pull --tags origin master` (visible in `git reflog`, a recurring pattern in this checkout predating this session) fast-forwarded/merged `ad0bee571` ("fix(documents): converge on cocoindex production path", authored by the operator, pushed from elsewhere) into `master`, producing merge commit `18231becc`. That commit introduced two undocumented-exported-symbol `revive` lint findings in `internal/config/config_document_retrieval.go` — a file this plan never otherwise touches — which then blocked every subsequent commit because the pre-commit hook lints the whole tree, not just staged files.
- **Found during:** Task 2's GREEN commit attempt (first `git commit` invocation failed with the lint error)
- **Impact:** None on Task 2's own code; see the Rule 3 fix below for the resolution and its accidental commit-bundling side effect.

**2. Task 2's GREEN commit accidentally bundles the Rule-3 unblock fix.** After fixing the two doc-comment gaps in `config_document_retrieval.go`, `git add internal/config/config_document_retrieval.go && git commit ...` picked up the three agent-package GREEN files that were still staged from the prior failed commit attempt, landing all four files in one commit (`60836960e`) whose message only describes the config fix. Per this execution's sequential-mode git-safety instructions, no destructive rewrite (`reset`, `commit --amend`) was used to split it after the fact. The commit's actual diff is fully correct and traceable (verified via `git show --stat` and `git show --name-only`); only the commit *message* undersells its contents. Recorded here rather than silently left implicit.
- **Found during:** Post-commit verification (`git show --stat HEAD`)
- **Files affected:** none beyond what was already correctly implemented
- **Verification:** `git show --stat 60836960e` confirms exactly the four intended files, no unintended change

### Auto-fixed Issues

**3. [Rule 3 - Blocking] Added two missing doc comments in an unrelated file to unblock the whole-tree pre-commit lint gate.**
- **Found during:** Task 2's first GREEN commit attempt
- **Issue:** `golangci-lint`'s `revive` linter flagged `DefaultAssetProcessingLeaseSec` (const) and `DocumentRetrievalConfig.Validate` (method) as undocumented exported symbols, introduced by an unrelated merged commit (`ad0bee571`), blocking every commit via the whole-tree pre-commit hook
- **Fix:** Added a one-line doc comment on the const block and on `Validate`; no behavior change
- **Files modified:** `internal/config/config_document_retrieval.go`
- **Verification:** `golangci-lint` reports 0 issues on the next commit attempt; `go build ./internal/config/...` unchanged
- **Committed in:** `60836960e` (bundled with Task 2's GREEN commit, see Deviation 2)

---

**Total deviations:** 1 auto-fixed (Rule 3, out-of-scope unblock), 2 informational (a concurrent merge and its accidental commit-bundling side effect).
**Impact on plan:** None on scope, correctness, or the plan's own acceptance criteria — all structural greps and `-race` runs pass against the final committed state, independently re-verified after the bundling was discovered.

## Issues Encountered

- The plan's own pattern-map citation for drain point A's line position had drifted by the time of implementation (exactly the class of drift `<no_stale_inputs>` warned about): the cited `budget := a.roundBudget(ic)` line is before `modelRound` is assigned, but `drainSteer`'s signature needs a real `modelRound` for the `SteerDelta` payload. Resolved by placing the call immediately after `modelRound` is assigned instead — still strictly before the request is built, and still guarded by `!transportRetry`, satisfying the plan's stated intent rather than its literal (stale) line citation.
- `llm_agent.go`'s file-size headroom was tighter than expected once `span.End(); cancel()` had to be added to drain point A's early-return branch (a real `go vet` context-leak finding, not anticipated in the plan's line-budget estimate) — the two struct-field comments were trimmed to single-line trailing comments to stay at exactly the 12-added-line ceiling.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- `internal/steer.Inbox` and `internal/agent`'s `SteerInbox`/`drainSteer`/`Actions.SteerDelta`/`aura.steer`-payload-shape are ready for 52-04 (the AG-UI route + composition-root wiring) and 52-06 (the Telegram dispatch branch) to consume. Nothing in this plan is reachable from outside the process yet — by design, per the plan's own objective — so there is no intermediate state in which a route accepts a steer nothing drains.
- The marker tag (`<user_steer nonce="...">...</user_steer>`), the `SteerDelta` payload keys (`conversation_id`, `round`, `steers[].{id,source,text,delivery}`), and the scrub's structural-regex matching rule are now load-bearing for 52-04/52-06/52-07/52-08 — assert against them literally rather than re-deriving the shape.
- `internal/agent/llm_agent.go` is now at **573/600 LOC** (was 561 at plan-authoring time), 27 lines of headroom remaining for the next plan that must touch it.
- STEER-01 and STEER-02 are NOT marked Complete in `.planning/REQUIREMENTS.md` by this plan alone: STEER-01 is also declared by 52-01 (shipped), 52-04, 52-07 and 52-08 (not yet shipped), and STEER-02 is also declared by 52-08 — the #2388 shared-ID gate withholds `Complete` until every declaring plan finishes, exactly as it did for MCP-01/MCP-03 and RESUME-01 earlier in this milestone.

## Self-Check: PASSED

- `internal/steer/inbox.go` — FOUND
- `internal/steer/inbox_test.go` — FOUND
- `internal/agent/llm_agent_steer.go` — FOUND
- `internal/agent/llm_agent_steer_test.go` — FOUND
- Commit `5250c619b` (Task 1 RED) — FOUND in `git log --oneline --all`
- Commit `b53bf2320` (Task 1 GREEN) — FOUND in `git log --oneline --all`
- Commit `ec181d05d` (Task 2 RED) — FOUND in `git log --oneline --all`
- Commit `60836960e` (Task 2 GREEN + Rule-3 fix) — FOUND in `git log --oneline --all`
- `go build ./...`, `go vet ./...` — PASS (Windows + WSL)
- `go test -race ./internal/steer/ ./internal/agent/ ./internal/agent/prompt/ ./internal/config/...` (WSL) — PASS
- `go test -run '^$' -fuzz=FuzzRenderToolResultForPrompt -fuzztime=30s ./internal/agent/` (WSL) — PASS, 0 new corpus failures
- All Task 1 and Task 2 `<verify><automated>` bash blocks — PASS (re-run verbatim; see Coverage/Decisions above)
- `git show --name-only` on both Task 2 commits — `internal/agent/llm_agent_dispatch.go` absent from both, confirmed
- `internal/agent/llm_agent.go` cumulative diff vs. pre-plan baseline — 12 insertions / 0 deletions (at the ≤12 ceiling)
