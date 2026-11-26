# Mutation on five files the gates left out (2026-11-26)

Five files had a 70% mutation requirement written down and never met or never measured:

- `internal/examplebank/bank.go`, `internal/pimwatch/centroid.go`, `internal/pimwatch/shadow_report.go`:
  "Adding them is open" (`2026-11-03-urgency-centroid-shadow-live.md`); the critical gate scoped none of them.
- `internal/channels/telegram/store.go`: "mutation on `store.go` again" after Task 7
  (`plans/2026-10-27-telegram-account-links.md`).
- the branch snapshot projection, `internal/agui/server_display_tree.go`: "not measured, because the run was
  stopped" (`plans/2026-10-27-conversation-branches.md`, Task 7).

## Method

`scripts/go_mutation_cache.py --advisory <file>`, the tool CI's critical gate runs, with a throwaway cache, one
file at a time, on branch `ccr-d742c0b3-jvadt4-mutation-scopes` (master `40a41e6b9` plus the new tests). The
Telegram store's tests are `db_integration` only, so it ran with `GOFLAGS=-tags=db_integration` against a
disposable Postgres; its 287 tests passed and none skipped beforehand. `internal/agui` gets the tool's 90 s exec
timeout. Times are cold, in a cloud container, not on a runner.

## Results

| File | Before | After | Mutants | Cold time | Where it is gated now |
|---|---|---|---|---|---|
| `internal/examplebank/bank.go` | 82.1% | 82.1% | 56 | 82 s | CI scope `example_bank` |
| `internal/pimwatch/centroid.go` | 62.5% | 87.5% | 40 | 90 s | CI scope `urgency_centroid` |
| `internal/pimwatch/shadow_report.go` | 67.6% | 95.5% | 111 | 286 s | CI scope `shadow_report` |
| `internal/channels/telegram/store.go` | not measured since Task 7 | 89.2% | 37 | 171 s | measured once, as its plan asks |
| `internal/agui/server_display_tree.go` | not measured | 100% | 31 | 923 s | measured once, as its plan asks |

The two files under 70% got tests, not exemptions: `centroid_errors_test.go` (the error paths of `Judge` and
`Learn`, the corrected-account set, the shipped seeds' checks) and `shadow_report_edges_test.go` (the 48-hour
cutoff, the digest's `now` mail, the macro F1 with absent tiers, the percentile ranks, the exact report text).

Survivors left on purpose:
- `centroid.go`: `correctionWeight` ±1 and `shadowTimeout` scaled. Both are measured parameters; a test
  that pins them tests a constant.
- `shadow_report.go`: `row.By == ByPerson` replaced by `true` in the strong-truth case, and
  `!lastDigest.IsZero()` replaced by `true`. No row reaches the first (a row not decided by the person
  carries the model's or the ladder's tier), and with a zero digest the date test that follows is already
  false.

## The CI groups

The three CI scopes ride with `elicitation-boundaries`. Read from run 45880718801's artifacts, each group's
scopes total, as last measured on a runner: turn-reading 2016 s, elicitation-boundaries 2935 s, media 4827 s.
Each timeout must be at least 2.7 times its group's cold total:

| Group | Cold total | Timeout before | 2.7× | Timeout now |
|---|---|---|---|---|
| turn-reading | 2016 s | 90 min | 91 min | 95 min |
| elicitation-boundaries | 2935 s + about 458 s | 290 min | 153 min | 290 min |
| media | 4827 s | 150 min | 218 min | 220 min |

## Gates of the Telegram plan's Task 7, on today's code

`go vet ./...`; golangci-lint 2.14.0 on `internal/channels/telegram`, untagged and with `db_integration`
(0 issues); `-race` on `internal/channels/telegram`, unit and `db_integration`; `internal/breakglass`
`db_integration`. All green.

## What this does not show

- **Runner times for the three new scopes.** The 82, 90 and 286 s are a cloud container's; the first CI run
  measures them on a runner.
- **`runner_context.go`**, the third file of the branches plan's mutation item, still at 58.3% with survivors
  in code that plan did not touch. That item stays open.
- **The Telegram store and the projection in CI.** They are measured once, as their plans ask, and not
  gated: the mutation job has no Postgres, and `internal/agui` already costs the most per mutant.
