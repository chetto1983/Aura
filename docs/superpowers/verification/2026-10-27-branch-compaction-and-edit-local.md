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
  - `conversations_compaction_api.go`: 32/35, **91.4%**.
  - `store_compaction.go`: 29/49, **59.2%**. 17 of the 20 survivors are equivalent:
    - the save guard repeats migration 0096's CHECKs (`covers_through_seq > 0`,
      `btrim(summary) <> ''`), and the database refuses the same rows;
    - the `seq <= 0` guards return what the query's "no row" returns;
    - `""` and the canonical uuid string are one key in `compactionKey`;
    - two survivors change only the warn line.

    The other three need an injected database error. Without the equivalent mutants the
    score is 29/32, 90.6%.
  - `server_run_branch.go`: 32/58, **55.2%**, on the unit tier.
    - The six survivors in `requireTurnSeq` are values returned beside a refusal, which
      callers ignore.
    - The other twenty are in `reportTurnSeqs`, `startTurn` and `threadHasPendingPause`,
      which come from #193. The db_integration HTTP E2E (`branch_http_e2e_integration_test.go`)
      asserts `head_seq` there, and this run did not include that tier.

## What this does not show

- **A live `/compact` with a real summarizer.** The store test uses a scripted summarizer.
  Nothing here calls a model.
- **The lab VM, WebKit or iOS.**
- **A raw mutation score of 70% on two files.** `store_compaction.go` and `server_run_branch.go`
  are below it. The previous section explains each survivor. Two measurements were not made:
  a database fault injection for the three store error paths, and a db_integration mutation
  run of `server_run_branch.go`, at about 90 s per mutant.
- **Pre-0095 forks.** PRD §7 keeps them open.
- **The interaction of the two refreshes.** The marker read after a run and the history read
  after a run are separate requests. Their ordering on a slow link was not measured.

## Notes for the next run

`aura serve` on the disposable database re-provisions identities: the local identity `…0001`
that the conversations tests use is gone afterwards. Running `cmd/aura` db_integration in
parallel with `internal/agui` on one database also failed `TestAgentRunCapability`. Both passed
on a fresh database. Recreate it with the same script before a db_integration run that follows
either.
