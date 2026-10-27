# `/compact` on the branch on screen, and no refused edit — local verification

Date: 2026-10-27. Branch `ccr-648b75d9-4hbqnw`, from `2e41ead`. Spec:
`docs/superpowers/specs/2026-10-27-conversation-branches-design.md` §7 (D13–D15). Plan:
`docs/superpowers/plans/2026-10-27-branch-compaction-and-edit.md`.

## What was measured

**Store, on a disposable Postgres 18 (`aura_it`, 118 migrations), db_integration:**
`TestCompact_CondensesTheBranchItsLeafNames`. The thread has two branches: 1–6, and 1, 2, 7, 8,
which forks at seq 3.
- `Compact` with leaf 6 covers through 4. Its summarizer received `domanda-3` and not
  `domanda-7`.
- After that compaction, the rows of leaf 8 and of leaf 0 are still empty.
- `Compact` with leaf 0 covers through 2. It files under the fork's branch, and leaf 6's row is
  unchanged.
- `LoadPathCompaction(99)` is `ErrTurnNotFound`.
- The whole package passes under `-race`: 25.9 s.

**HTTP, agui unit:**
- `?leaf_seq` = 6, 8 or absent reaches the compactor and the read as 6, 8 or 0.
- The values 0, -3, `six` and empty are 400, and 9 (above the turn written last) is 404, on
  both routes. Neither the compactor nor the read is reached.
- `internal/agui` and `internal/runner` pass under `-race`, both unit and db_integration.

**Cockpit, Vitest, under Node 24.22.1 (CI's `AURA_CI_NODE_VERSION`):**
- After Previous, the reads are `compaction?leaf_seq=4`, then `compaction?leaf_seq=2`, then
  `compact?leaf_seq=2`, and nothing else.
- The marker stays while a run grows the branch, with the reads for the new leaf held open.
- A switch drops the marker of the branch left, with the read for the new branch held open.
- A user message with no seq offers no Edit, and an assistant message with no seq offers no
  Regenerate.
- A 409 on `/edit` leaves the edited text on screen, with the error. This case passed before
  the change: it pins the spec's claim.
- `src/chat`: 1436 of 1436 pass.
- Hand mutations of the placeholder rule each fail their own case: "never keep" fails "stays
  while a run grows", and "always keep" fails "drops on a switch".

**Playwright, against `aura serve` on the rebuilt binary and dist** (chrome project, 4 workers):
`voice.spec.ts`, `composer-skills.spec.ts` and `chat.spec.ts`.
- `/compact` posts `leaf_seq=2` on the two-turn transcript.
- Two fixes to the specs:
  - **The compaction stubs used globs.** A Playwright glob is anchored on the whole URL,
    query included (`globToRegexPattern`, playwright-core 1.64.1), so the reads with
    `?leaf_seq` no longer matched. Both stubs are now regexes that allow a query.
  - **The voice mock had no seqs.** Its run lacked the `head_seq`/`leaf_seq` every real run
    reports, and the no-voice case reads Regenerate. The frames now carry them.
- **A race that predates the change.** "voice mode … speaks the reply" read the audio play
  count once, right after "Answering…", but the overlay builds its `Audio` only after
  `/api/tts` answers.
  - Measured before the fix: master failed 1 run in 6, this branch 2 in 10, always on that
    assertion.
  - The count is now polled. After that, 8 runs in 8 passed with all 11 tests.

**A branch that cannot be read now fails the load** (CodeRabbit on #195). `loadManagedPath`
used to fall back to the canonical row when `GetTurnPointers` failed with anything but "no
row". A fork could then reuse the canonical summary, or file its own summary there. It now
returns the error. `TestCompact_AnUnreadableBranchFailsBeforeAnySummary` drives a failing
read through the fake DBTX: the error comes back, and neither the summarizer nor a write is
reached. Before the fix the test panicked inside the path walk the fallback let it reach.

**Mutation (`go-mutesting`)** ran in a separate worktree. The exec timeout was 180 s for
`internal/agui` and 300 s for `internal/conversations` with db_integration.
- **A first run was discarded.** go-mutesting's default `--exec-timeout` is 10 s, and the agui
  unit suite takes about 30 s. Every mutant timed out and counted as killed.
- **Round 1, on `eb24cde`:**
  - `conversations_compaction_api.go`: 29/38 (76.3%);
  - `server_run_branch.go`: 28/58 (48.3%);
  - `store_compaction.go`: 18/48 (37.5%).

  The survivors in code this PR adds or moves got tests in `1350da1` and `bdadd05`.
- **Round 2, on `bdadd05`:**
  - `conversations_compaction_api.go`: 32/35, **91.4%**;
  - `store_compaction.go`: 29/49, 59.2%;
  - `server_run_branch.go`: 32/58, 55.2%.

  The operator asked to close the two gaps with tests. `271e27e` added them:
  - **`store_compaction.go`, through the fake DBTX.** Under db_integration the database
    answers for these guards: its CHECKs refuse an empty summary, and a seq that names no turn
    finds no row. The fake answers every statement, so each guard is observable on its own.
    The tests cover the seq bounds of `branchOfTurn`, a canonical turn and a forked one, a
    failing read and write, and the save guards.
  - **`server_run_branch.go`.**
    - `requireTurnSeq` now has named results, so a refusal returns zero values instead of
      six literals nobody reads.
    - A failing pause check is a 500 that starts no run.
    - `leaf_seq` 1 is reported.
    - A run with no recorded head reports no `head_seq`.
- **Round 3, on `271e27e`:**
  - `store_compaction.go`: 44/46, **95.7%**. The two survivors are equivalent:
    - the `err != nil` arm of `LoadPathCompaction`'s empty-thread check returns that error
      either way;
    - a NULL branch id has the canonical id's zero bytes.
  - `server_run_branch.go`: 39/52, **75.0%**, on the unit tier. The 13 survivors are in
    #193's code:
    - nine need a head the real runner records (`runner.WithTurnHead`). The db_integration
      HTTP E2E asserts `head_seq` there;
    - two are the returns after a consumer stops reading;
    - two survive in `startTurn`: they would dereference a nil message, but no caller passes
      one.
  - `conversations_compaction_api.go` is unchanged since round 2: 91.4%.

## What this does not show

- **A live `/compact` with a real summarizer.** The store test uses a scripted summarizer.
  Nothing here calls a model.
- **The lab VM, WebKit or iOS.**
- **`server_run_branch.go` under db_integration.** Its head paths are measured on the unit tier
  only, where no head is recorded. The db_integration mutation run would take about 90 s per
  mutant.
- **Pre-0095 forks.** PRD §7 keeps them open.
- **The interaction of the two refreshes.** The marker read after a run and the history read
  after a run are separate requests. Their ordering on a slow link was not measured.

## Notes for the next run

`aura serve` on the disposable database re-provisions identities: the local identity `…0001`
that the conversations tests use is gone afterwards. Running `cmd/aura` db_integration in
parallel with `internal/agui` on one database also failed `TestAgentRunCapability`. Both passed
on a fresh database. Recreate it with the same script before a db_integration run that follows
either.
