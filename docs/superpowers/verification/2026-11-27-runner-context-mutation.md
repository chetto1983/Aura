# `runner_context.go` mutation score (2026-11-27)

The conversation-branches plan (`docs/superpowers/plans/2026-10-27-conversation-branches.md`, Task 7) left its
mutation item open on `internal/runner/runner_context.go` at 58.3%. The backlog run of 2026-11-26
(`2026-11-26-mutation-scopes-backlog.md`) closed the other files and kept this one open.

## Method

`scripts/go_mutation_cache.py --advisory internal/runner/runner_context.go`, the tool CI's critical gate runs,
with a throwaway cache, in a worktree of master `03e78459c`, before and after the new tests. Times are cold, in
a cloud container.

## Results

| | Killed | Score | Time |
|---|---|---|---|
| Before | 21/36 | 58.33% | 207 s |
| After | 35/36 | 97.22% | 238 s |

The 14 mutants killed, by what now holds them:

| Code | Mutants | Test |
|---|---|---|
| `compactionSummarizer`: off with compaction, the route's model unless it has one, the route's total timeout | 3 | `TestCompactionSummarizerUsesTheRouteUnlessItHasAModel`, `TestCompactionSummarizerIsOffWithCompaction` |
| `memoryDigest` and `memoryRecall` drop what comes with an error; a blank message is not searched | 3 | the error cases now carry stale content; `TestLoadMemoryContext_BlankUserMessageSkipsPreload` |
| the block: one newline between digest and recall, none before a recall alone | 3 | `TestLoadMemoryContext_PreloadInjectsRecall`, `TestLoadMemoryContext_PreloadOnlyWhenDigestEmpty` |
| `renderContextBlock` joins the profile | 1 | `TestRenderContextBlockJoinsTheAlwaysBlockAndTheProfile` |
| the four log lines: no identity, digest and preload unavailable, and the line that says what was assembled | 4 | `TestLoadMemoryContextLogsWhatItAssembledAndWhatFailed` |

The log lines are asserted because both memory legs are fail-soft: the log is the only way to tell an outage
from a memory that had nothing to say, which is what the "assembled" line was added for on 2026-09-03.

## The survivor

`if b.Len() > 0` → `if b.Len() > 1`, before the recall block. It is equivalent: that branch runs after the
digest block was written, header included, so the builder never holds exactly one byte.

## What this does not show

- A runner's time: these are a cloud container's.
- CI: the file is not a scope of `scripts/critical_mutation_gate.py`, so nothing gates the score.
