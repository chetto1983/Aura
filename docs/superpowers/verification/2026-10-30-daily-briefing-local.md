# Daily briefing — local verification, 2026-10-30

Spec: `docs/superpowers/specs/2026-10-09-daily-briefing-design.md`.
Plan: `docs/superpowers/plans/2026-10-30-daily-briefing.md`.
Where: a cloud container with Go 1.27.3 and Docker 29.9.2. The stack is Postgres, ArcadeDB and
EmbeddingGemma, brought up as CI's coverage job brings it up. **Not the lab VM. No model ran.**

## What ran

| Check | Result |
|---|---|
| `go vet ./...`, `go build ./...` | clean |
| `go test -race` on `internal/agent/tools`, `internal/cron/handlers`, `internal/skills`, `internal/agent/...`, `cmd/aura` | ok |
| `golangci-lint` v2.14.0 on the touched packages | 0 issues |
| `ruff check` on `scripts/briefing_baseline*.py` | passed |
| `scripts/briefing_baseline_test.py` (8 cases) | ok; joined CI's production-evidence contract list |
| `TestACronWithoutTZRunsInTheIdentitysZone` (unit, 6 cases) | profile zone, trimmed; deployment zone; past a broken profile zone; Rome last; explicit tz wins |
| `TestACronWithoutTZFiresOnTheIdentitysWallClock` (db_integration, disposable `aura_it`) | red without the fallback: `Europe/Rome`, 02:30 in New York. Green with it: `America/New_York`, 07:30 |
| `TestAScheduledAgentReadsTheClockInTheIdentitysZone` | red without `Location` for New York (−04:00) and Tokyo (+09:00); green with it |
| `TestBoardListShowsWhatIsDueFirst` and `board_render_test.go` | an overdue card at position 40 is listed first; the window has two edges; nothing due means no section; the cap holds |
| `TestEveryBuiltinLoads`, `TestBuiltinNamesAreTheEmbeddedSet` | `briefing-aura` loads |
| Coverage, `bash scripts/coverage_docker.sh` at `bbe5848` | **passed**: 50082/57151 = 87.6% ≥ 85%, package policy passed. `internal/agent/tools` 90.01%, `internal/cron/handlers` 93.02%, `internal/board` 90.76% |
| Mutation, `go-mutesting internal/agent/tools/task_timezone.go` | 66.7%, then **100%** (5/5) after `2e619a3` |
| Mutation, `go-mutesting internal/agent/tools/board_actions.go` | 63.9% (78/122), then **98.4%** (120/122) after `b237892`, then one more case for a single tag |

**Survivors.**
- **`board_actions.go`, the column loop's `break` at the cap turned into `continue`.** Equivalent:
  every later iteration checks the cap again and skips.
- **The single-tag mutant.** Killed afterwards by the "one tag" case, checked by hand with the
  mutant applied.

**What the first mutation run found.** In `TestBoardListShowsWhatIsDueFirst`, every due card sat
beyond the columns' cap, so the "listed once" check could never see a duplicate. Removing the map
that prevents duplicates survived. The new tests put due cards early in their column.

## Added 2026-10-31: keeping a run's reads

- **The finding.** The ledger cannot carry the baseline's inputs. `result_preview` is cut at
  2 KiB and redacted, and a sidecar exists only above 30,000 bytes (spec, "Open question 1 does
  not hold as answered").
- **The decision.** The operator chose to keep the results during the acceptance only (spec,
  "Keeping a run's reads").
- **The checks.**

| Check | Result |
|---|---|
| `agentjob_results_test.go` | a job with `keep_results` writes one line per finished call, mode 0600, with arguments and the whole result. Without the flag, nothing. With no run dir, the run fails before the model is called. A log that cannot write says so |
| Mutation, `agentjob_results.go` | 69.2%, then **92.3%** after the error-path tests; the survivor, MkdirAll's return, is equivalent |
| `briefing_baseline_test.py` (14 cases) | `--results` builds the bundle: last successful read wins, a missing or failed source is null, a sidecar is read, `now` compares instants |
| End to end, scratch Go test (not committed) | a job with canned `board` and `calendar__calendar` tools kept three lines; `briefing_baseline.py --results ... --zone Europe/Rome` rendered them |

## What this does not show

- **What the briefing says.** No model ran here. The spec's acceptance is the lab VM over five
  working days:
  - a real calendar, mailbox and board;
  - the briefing on Telegram in the operator's zone;
  - grounding checked per run;
  - the blind choice against the baseline, four wins in five;
  - cost per run.

  All of it is owed, and it needs the operator.
- **A real run's capture.** The capture was exercised with canned tools only. On the lab VM, the
  briefing task is scheduled with `"keep_results": true`. Each morning, `results.jsonl` is
  copied out of the daemon's run dir, together with any sidecar it names, before the 24-hour
  retention takes it.
- **The PIM's real payloads.** The calendar arguments come from the deployed manifest corpus of
  2026-10-25. Whether `from` is a string or an object in `get_emails` is not read. The baseline
  accepts both.
- **`dev` and `local_trusted`.** There the gateway is a no-op, so the skill's read-only rule is an
  instruction only. The acceptance counts any write.
